package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/miolamio/agent-runtime/internal/config"
	"github.com/miolamio/agent-runtime/internal/envfile"
	"github.com/miolamio/agent-runtime/internal/history"
	"github.com/miolamio/agent-runtime/internal/profile"
)

type RunOpts struct {
	Prompt      string
	Provider    string // z/zai | m/mm/minimax | k/kimi | r/remote
	Profile     string // profile name (loads skills, settings, provider)
	Model       string // model override (e.g. kimi-k2.5, glm-5.1)
	Loop        bool
	MaxLoops    int
	Name        string
	Interactive bool   // -it mode, no prompt
	Mount       string // explicit mount path (overrides config workspace)
	Output      string // export workspace to this directory after run
}

func Run(cfg *config.Config, opts RunOpts) error {
	// Load profile if specified
	var extraVolumes []string
	var settingsTmp string
	if opts.Profile != "" {
		prof, err := profile.Load(opts.Profile)
		if err != nil {
			return fmt.Errorf("profile: %w", err)
		}
		fmt.Fprintf(os.Stderr, "[airun] profile=%s (%s)\n", prof.Name, prof.Description)

		extraVolumes, settingsTmp, err = profileMounts(prof)
		if err != nil {
			return fmt.Errorf("profile mounts: %w", err)
		}
		if settingsTmp != "" {
			defer os.Remove(settingsTmp)
		}

		if opts.Provider == "" && prof.Provider != "" {
			opts.Provider = prof.Provider
		}
	}

	provider := config.NormalizeProvider(opts.Provider)
	if opts.Provider == "" {
		provider = config.NormalizeProvider(cfg.Provider)
	}

	// Resolve model: CLI flag > config default for provider
	model := opts.Model
	if model == "" {
		switch provider {
		case "minimax":
			model = cfg.MinimaxModel
		case "kimi":
			model = cfg.KimiModel
		case "remote":
			model = cfg.RemoteDefaultModel
		default:
			model = cfg.ZaiModel
		}
	}

	// Fix 2: mount = pwd (not config.Workspace)
	mount := opts.Mount
	if mount == "" {
		mount, _ = os.Getwd()
	}
	if mount == "" {
		mount = cfg.Workspace
	}

	if opts.Interactive {
		fmt.Fprintf(os.Stderr, "[airun] interactive: provider=%s model=%s mount=%s\n", provider, model, mount)
		return runDocker(cfg, RunOpts{Interactive: true, Mount: mount, Profile: opts.Profile}, provider, model, extraVolumes)
	}

	// Fix 2: log actual mount, not config.Workspace
	fmt.Fprintf(os.Stderr, "[airun] provider=%s model=%s workspace=%s\n", provider, model, mount)

	if opts.Output != "" {
		return runDockerWithExport(cfg, RunOpts{Prompt: opts.Prompt, Mount: mount, Output: opts.Output, Profile: opts.Profile}, provider, model, extraVolumes)
	}

	return runDocker(cfg, RunOpts{Prompt: opts.Prompt, Mount: mount, Profile: opts.Profile}, provider, model, extraVolumes)
}

func runDocker(cfg *config.Config, opts RunOpts, provider, model string, extraVolumes []string) error {
	imageName := "agent-runtime:latest"

	envPath, err := envfile.Write(cfg.ContainerEnvWithModel(provider, model))
	if err != nil {
		return err
	}
	defer envfile.Cleanup(envPath)

	var args []string
	if opts.Interactive {
		args = []string{"run", "-it", "--rm"}
	} else {
		args = []string{"run", "--rm"}
	}
	args = append(args, "--env-file", envPath)

	if opts.Mount != "" {
		args = append(args, "-v", opts.Mount+":/workspace")
	}

	for _, v := range extraVolumes {
		args = append(args, "-v", v)
	}

	args = append(args, imageName)

	// Claude Code command (non-interactive only)
	if !opts.Interactive {
		args = append(args, "claude", "-p", opts.Prompt, "--dangerously-skip-permissions")
	}

	fmt.Fprintf(os.Stderr, "[airun] docker %s --env-file %s %s\n",
		args[0]+" "+args[1], envfile.MaskLog(envPath), imageName)

	if opts.Interactive {
		cmd := exec.Command("docker", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		return cmd.Run()
	}

	// Non-interactive: capture output for history
	var outputBuf bytes.Buffer
	start := time.Now()

	cmd := exec.Command("docker", args...)
	cmd.Stdout = io.MultiWriter(os.Stdout, &outputBuf)
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	err = cmd.Run()
	exitCode := 0
	if err != nil {
		exitCode = 1
	}

	rec := history.RunRecord{
		Timestamp:  time.Now().Format("2006-01-02_15-04-05"),
		Profile:    opts.Profile,
		Provider:   provider,
		Model:      model,
		Prompt:     opts.Prompt,
		DurationMs: time.Since(start).Milliseconds(),
		ExitCode:   exitCode,
		RunDir:     history.NewRunDir(opts.Profile, provider),
	}
	history.Save(rec, outputBuf.String())

	fmt.Fprintf(os.Stderr, "[airun] done in %.1fs | profile=%s provider=%s | exit=%d\n",
		float64(rec.DurationMs)/1000, rec.Profile, rec.Provider, rec.ExitCode)
	fmt.Fprintf(os.Stderr, "[airun] log: %s\n", rec.RunDir)

	return err
}

func runDockerWithExport(cfg *config.Config, opts RunOpts, provider, model string, extraVolumes []string) error {
	imageName := "agent-runtime:latest"
	containerName := fmt.Sprintf("airun-export-%d", time.Now().Unix())

	envPath, err := envfile.Write(cfg.ContainerEnvWithModel(provider, model))
	if err != nil {
		return err
	}
	defer envfile.Cleanup(envPath)

	createArgs := []string{"create", "--name", containerName, "--env-file", envPath}
	if opts.Mount != "" {
		createArgs = append(createArgs, "-v", opts.Mount+":/workspace")
	}
	for _, v := range extraVolumes {
		createArgs = append(createArgs, "-v", v)
	}
	createArgs = append(createArgs, imageName, "claude", "-p", opts.Prompt, "--dangerously-skip-permissions")

	fmt.Fprintf(os.Stderr, "[airun] docker create --name %s --env-file %s\n", containerName, envfile.MaskLog(envPath))
	if out, err := exec.Command("docker", createArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("docker create failed: %s: %w", string(out), err)
	}

	var outputBuf bytes.Buffer
	start := time.Now()

	startCmd := exec.Command("docker", "start", "-a", containerName)
	startCmd.Stdout = io.MultiWriter(os.Stdout, &outputBuf)
	startCmd.Stderr = os.Stderr
	runErr := startCmd.Run()

	exitCode := 0
	if runErr != nil {
		exitCode = 1
	}

	os.MkdirAll(opts.Output, 0755)
	cpCmd := exec.Command("docker", "cp", containerName+":/workspace/.", opts.Output)
	if cpOut, err := cpCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "[airun] warning: docker cp failed: %s\n", string(cpOut))
	} else {
		fmt.Fprintf(os.Stderr, "[airun] exported workspace to %s\n", opts.Output)
	}

	exec.Command("docker", "rm", containerName).Run()

	rec := history.RunRecord{
		Timestamp:  time.Now().Format("2006-01-02_15-04-05"),
		Profile:    opts.Profile,
		Provider:   provider,
		Model:      model,
		Prompt:     opts.Prompt,
		DurationMs: time.Since(start).Milliseconds(),
		ExitCode:   exitCode,
		RunDir:     history.NewRunDir(opts.Profile, provider),
	}
	history.Save(rec, outputBuf.String())

	fmt.Fprintf(os.Stderr, "[airun] done in %.1fs | profile=%s provider=%s | exit=%d\n",
		float64(rec.DurationMs)/1000, rec.Profile, rec.Provider, rec.ExitCode)
	fmt.Fprintf(os.Stderr, "[airun] log: %s\n", rec.RunDir)

	return runErr
}

// profileMounts generates Docker volume mount args from a profile's skills and settings.
func profileMounts(p *profile.Profile) (volumes []string, settingsPath string, err error) {
	for _, skillPath := range p.SkillPaths() {
		skillName := filepath.Base(skillPath)
		volumes = append(volumes, fmt.Sprintf("%s:/home/claude/.claude/skills/%s:ro", skillPath, skillName))
	}

	if len(p.Settings) > 0 {
		settingsJSON, err := json.Marshal(p.Settings)
		if err != nil {
			return nil, "", fmt.Errorf("marshal settings: %w", err)
		}
		f, err := os.CreateTemp(os.TempDir(), ".airun-settings-*.json")
		if err != nil {
			return nil, "", fmt.Errorf("create settings temp file: %w", err)
		}
		if _, err := f.Write(settingsJSON); err != nil {
			f.Close()
			os.Remove(f.Name())
			return nil, "", fmt.Errorf("write settings: %w", err)
		}
		f.Close()
		settingsPath = f.Name()
		volumes = append(volumes, fmt.Sprintf("%s:/home/claude/.claude/settings.json:ro", settingsPath))
	}

	return volumes, settingsPath, nil
}

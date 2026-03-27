package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/codegeek/automatica-agent-runtime/internal/config"
	"github.com/codegeek/automatica-agent-runtime/internal/envfile"
	"github.com/codegeek/automatica-agent-runtime/internal/profile"
)

type RunOpts struct {
	Prompt      string
	Provider    string // zai | minimax (overrides config)
	Profile     string // profile name (loads skills, settings, provider)
	Loop        bool
	MaxLoops    int
	Name        string
	Interactive bool   // -it mode, no prompt
	Mount       string // explicit mount path (overrides config workspace)
}

func Run(cfg *config.Config, opts RunOpts) error {
	// Load profile if specified
	var prof *profile.Profile
	var extraVolumes []string
	var settingsTmp string
	if opts.Profile != "" {
		var err error
		prof, err = profile.Load(opts.Profile)
		if err != nil {
			return fmt.Errorf("profile: %w", err)
		}
		fmt.Fprintf(os.Stderr, "[arun] profile=%s (%s)\n", prof.Name, prof.Description)

		extraVolumes, settingsTmp, err = profileMounts(prof)
		if err != nil {
			return fmt.Errorf("profile mounts: %w", err)
		}
		if settingsTmp != "" {
			defer os.Remove(settingsTmp)
		}

		// Profile provider is used if --provider not explicitly set
		if opts.Provider == "" && prof.Provider != "" {
			opts.Provider = prof.Provider
		}
	}

	provider := config.NormalizeProvider(opts.Provider)
	if opts.Provider == "" {
		provider = config.NormalizeProvider(cfg.Provider)
	}

	model := cfg.ZaiModel
	if provider == "minimax" {
		model = cfg.MinimaxModel
	}

	mount := opts.Mount
	if mount == "" {
		mount, _ = os.Getwd()
	}
	if mount == "" {
		mount = cfg.Workspace
	}

	if opts.Interactive {
		fmt.Fprintf(os.Stderr, "[arun] interactive session: provider=%s model=%s\n", provider, model)
		if mount != "" {
			fmt.Fprintf(os.Stderr, "[arun] mount: %s → /workspace\n", mount)
		}
		return runDockerInteractive(cfg, provider, mount, extraVolumes)
	}

	fmt.Fprintf(os.Stderr, "[arun] provider=%s model=%s workspace=%s mode=%s\n",
		provider, model, cfg.Workspace, cfg.Mode)

	// Try clawker first, fall back to docker run if socket bridge fails
	args, envPath, err := buildClawkerArgs(cfg, opts, provider)
	if err != nil {
		return err
	}
	defer envfile.Cleanup(envPath)

	fmt.Fprintf(os.Stderr, "[arun] clawker run --mode %s --env-file %s @\n",
		cfg.Mode, envfile.MaskLog(envPath))

	cmd := exec.Command("clawker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	err = cmd.Run()
	if err != nil && strings.Contains(err.Error(), "exit status 1") {
		fmt.Fprintf(os.Stderr, "[arun] clawker failed, trying docker run fallback...\n")
		envfile.Cleanup(envPath) // clean up before creating a new one in runDocker
		return runDocker(cfg, opts, provider, extraVolumes)
	}
	return err
}

func runDockerInteractive(cfg *config.Config, provider, mount string, extraVolumes []string) error {
	imageName := "clawker-agent-runtime:latest"

	envPath, err := envfile.Write(cfg.ContainerEnv(provider))
	if err != nil {
		return err
	}
	defer envfile.Cleanup(envPath)

	args := []string{"run", "-it", "--rm"}
	args = append(args, "--env-file", envPath)

	if mount != "" {
		args = append(args, "-v", mount+":/workspace")
	}

	for _, v := range extraVolumes {
		args = append(args, "-v", v)
	}

	args = append(args, imageName)

	fmt.Fprintf(os.Stderr, "[arun] docker run -it --rm --env-file %s %s\n",
		envfile.MaskLog(envPath), imageName)

	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func runDocker(cfg *config.Config, opts RunOpts, provider string, extraVolumes []string) error {
	// Find project image name
	imageName := "clawker-agent-runtime:latest"

	envPath, err := envfile.Write(cfg.ContainerEnv(provider))
	if err != nil {
		return err
	}
	defer envfile.Cleanup(envPath)

	args := []string{"run", "--rm"}
	args = append(args, "--env-file", envPath)

	// Always mount workspace (pwd or explicit mount)
	mount := opts.Mount
	if mount == "" {
		mount, _ = os.Getwd()
	}
	if mount != "" {
		args = append(args, "-v", mount+":/workspace")
	}

	for _, v := range extraVolumes {
		args = append(args, "-v", v)
	}

	args = append(args, imageName)

	// Claude Code command
	args = append(args, "claude", "-p", opts.Prompt, "--dangerously-skip-permissions")

	fmt.Fprintf(os.Stderr, "[arun] docker run --rm --env-file %s %s\n",
		envfile.MaskLog(envPath), imageName)

	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func buildClawkerArgs(cfg *config.Config, opts RunOpts, provider string) ([]string, string, error) {
	envPath, err := envfile.Write(cfg.ContainerEnv(provider))
	if err != nil {
		return nil, "", err
	}

	var args []string

	if opts.Loop {
		args = append(args, "loop", "iterate")
		args = append(args, "--prompt", opts.Prompt)
		if opts.MaxLoops > 0 {
			args = append(args, "--max-loops", fmt.Sprintf("%d", opts.MaxLoops))
		}
	} else {
		args = append(args, "run")
	}

	args = append(args, "--mode", cfg.Mode)

	if opts.Name != "" {
		args = append(args, "--agent", opts.Name)
	}

	args = append(args, "--rm")
	args = append(args, "--env-file", envPath)

	args = append(args, "@")

	if !opts.Loop {
		args = append(args, "-p", opts.Prompt, "--dangerously-skip-permissions")
	}

	return args, envPath, nil
}

// profileMounts generates Docker volume mount args from a profile's skills and settings.
func profileMounts(p *profile.Profile) (volumes []string, settingsPath string, err error) {
	// Mount each skill directory
	for _, skillPath := range p.SkillPaths() {
		skillName := filepath.Base(skillPath)
		volumes = append(volumes, fmt.Sprintf("%s:/home/claude/.claude/skills/%s:ro", skillPath, skillName))
	}

	// Generate temp settings.json from profile settings
	if len(p.Settings) > 0 {
		settingsJSON, err := json.Marshal(p.Settings)
		if err != nil {
			return nil, "", fmt.Errorf("marshal settings: %w", err)
		}
		f, err := os.CreateTemp(os.TempDir(), ".arun-settings-*.json")
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

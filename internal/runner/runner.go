package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/miolamio/agent-runtime/internal/config"
	"github.com/miolamio/agent-runtime/internal/envfile"
	"github.com/miolamio/agent-runtime/internal/history"
	"github.com/miolamio/agent-runtime/internal/profile"
)

const (
	stateVolumeName = "airun-claude-state"
	stateMountPath  = "/home/claude/.claude"
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
	NoState     bool   // disable persistent state volume (ephemeral)
	Browser     string // vnc | cdp | both — enable browser display
}

// stateVolumeForProfile returns the Docker volume name for the given profile.
// Without a profile, returns the default volume name.
func stateVolumeForProfile(profile string) string {
	if profile == "" {
		return stateVolumeName
	}
	return "airun-state-" + profile
}

// cleanupContainer removes the named container; a failure is logged to stderr
// but not propagated — the caller's primary result should remain authoritative.
func cleanupContainer(name string) {
	if err := exec.Command("docker", "rm", name).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "[airun] warning: docker rm %s: %v\n", name, err)
	}
}

// appendClaudeCmd appends the `claude -p <prompt> --dangerously-skip-permissions`
// invocation (with an optional --max-turns for loop mode) used in every
// non-interactive run.
func appendClaudeCmd(args []string, opts RunOpts) []string {
	args = append(args, "claude", "-p", opts.Prompt, "--dangerously-skip-permissions")
	if opts.Loop && opts.MaxLoops > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", opts.MaxLoops))
	}
	return args
}

func Run(cfg *config.Config, opts RunOpts) error {
	// Load profile if specified
	var extraVolumes []string
	var settingsTmp string
	var pluginScriptTmp string
	if opts.Profile != "" {
		prof, err := profile.Load(opts.Profile)
		if err != nil {
			return fmt.Errorf("profile: %w", err)
		}
		fmt.Fprintf(os.Stderr, "[airun] profile=%s (%s)\n", prof.Name, prof.Description)

		extraVolumes, settingsTmp, pluginScriptTmp, err = profileMounts(prof)
		if err != nil {
			return fmt.Errorf("profile mounts: %w", err)
		}
		if settingsTmp != "" {
			defer os.Remove(settingsTmp)
		}
		if pluginScriptTmp != "" {
			defer os.Remove(pluginScriptTmp)
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
		case "anthropic":
			model = cfg.AnthropicModel
		case "remote":
			model = cfg.RemoteDefaultModel
		default:
			model = cfg.ZaiModel
		}
	}

	// Validate model against available list for remote provider
	if provider == "remote" && cfg.RemoteModels != "" && opts.Model != "" {
		found := false
		for _, m := range strings.Split(cfg.RemoteModels, ",") {
			if strings.TrimSpace(m) == opts.Model {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("model %q not available on remote proxy (available: %s)", opts.Model, cfg.RemoteModels)
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
		return runDocker(cfg, RunOpts{Interactive: true, Mount: mount, Profile: opts.Profile, NoState: opts.NoState, Browser: opts.Browser}, provider, model, extraVolumes)
	}

	// Fix 2: log actual mount, not config.Workspace
	fmt.Fprintf(os.Stderr, "[airun] provider=%s model=%s workspace=%s\n", provider, model, mount)

	if opts.Output != "" {
		return runDockerWithExport(cfg, RunOpts{Prompt: opts.Prompt, Mount: mount, Output: opts.Output, Profile: opts.Profile, NoState: opts.NoState, Loop: opts.Loop, MaxLoops: opts.MaxLoops, Browser: opts.Browser}, provider, model, extraVolumes)
	}

	return runDocker(cfg, RunOpts{Prompt: opts.Prompt, Mount: mount, Profile: opts.Profile, NoState: opts.NoState, Loop: opts.Loop, MaxLoops: opts.MaxLoops, Browser: opts.Browser}, provider, model, extraVolumes)
}

func runDocker(cfg *config.Config, opts RunOpts, provider, model string, extraVolumes []string) error {
	imageName := "agent-runtime:latest"

	envPath, err := envfile.Write(cfg.ContainerEnvWithModel(provider, model))
	if err != nil {
		return err
	}
	defer envfile.Cleanup(envPath)

	mode := cfg.Mode
	if mode == "" {
		mode = "bind"
	}

	if mode == "snapshot" && !opts.Interactive {
		return runDockerSnapshot(cfg, opts, provider, model, envPath, extraVolumes, imageName)
	}

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
	if !opts.NoState {
		args = append(args, "-v", stateVolumeForProfile(opts.Profile)+":"+stateMountPath)
	}

	for _, v := range extraVolumes {
		args = append(args, "-v", v)
	}

	if info, err := os.Stat(cfg.AgentsDir); err == nil && info.IsDir() {
		args = append(args, "-v", cfg.AgentsDir+":/home/claude/.claude/agents:ro")
	}

	if opts.Browser != "" {
		args = append(args, "-e", "AIRUN_BROWSER="+opts.Browser)
		if opts.Browser == "vnc" || opts.Browser == "both" {
			args = append(args, "-p", "6080:6080")
		}
		if opts.Browser == "cdp" || opts.Browser == "both" {
			args = append(args, "-p", "9222:9222")
		}
	}

	args = append(args, imageName)

	// Claude Code command (non-interactive only)
	if !opts.Interactive {
		args = appendClaudeCmd(args, opts)
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

func runDockerSnapshot(cfg *config.Config, opts RunOpts, provider, model, envPath string, extraVolumes []string, imageName string) error {
	containerName := fmt.Sprintf("airun-snap-%d", time.Now().Unix())

	createArgs := []string{"create", "--name", containerName, "--env-file", envPath}

	// No -v for workspace — we'll docker cp instead
	if !opts.NoState {
		createArgs = append(createArgs, "-v", stateVolumeForProfile(opts.Profile)+":"+stateMountPath)
	}
	for _, v := range extraVolumes {
		createArgs = append(createArgs, "-v", v)
	}

	if info, err := os.Stat(cfg.AgentsDir); err == nil && info.IsDir() {
		createArgs = append(createArgs, "-v", cfg.AgentsDir+":/home/claude/.claude/agents:ro")
	}

	if opts.Browser != "" {
		createArgs = append(createArgs, "-e", "AIRUN_BROWSER="+opts.Browser)
		if opts.Browser == "vnc" || opts.Browser == "both" {
			createArgs = append(createArgs, "-p", "6080:6080")
		}
		if opts.Browser == "cdp" || opts.Browser == "both" {
			createArgs = append(createArgs, "-p", "9222:9222")
		}
	}

	createArgs = append(createArgs, imageName)
	createArgs = appendClaudeCmd(createArgs, opts)

	fmt.Fprintf(os.Stderr, "[airun] snapshot mode: creating container %s\n", containerName)
	if out, err := exec.Command("docker", createArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("docker create failed: %s: %w", string(out), err)
	}

	// Copy workspace into container
	if opts.Mount != "" {
		fmt.Fprintf(os.Stderr, "[airun] copying %s → container:/workspace\n", opts.Mount)
		cpCmd := exec.Command("docker", "cp", opts.Mount+"/.", containerName+":/workspace")
		if out, err := cpCmd.CombinedOutput(); err != nil {
			cleanupContainer(containerName)
			return fmt.Errorf("docker cp failed: %s: %w", string(out), err)
		}
	}

	// Start and capture output
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

	cleanupContainer(containerName)

	// Save history
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

func runDockerWithExport(cfg *config.Config, opts RunOpts, provider, model string, extraVolumes []string) error {
	imageName := "agent-runtime:latest"
	containerName := fmt.Sprintf("airun-export-%d", time.Now().Unix())

	envPath, err := envfile.Write(cfg.ContainerEnvWithModel(provider, model))
	if err != nil {
		return err
	}
	defer envfile.Cleanup(envPath)

	mode := cfg.Mode
	if mode == "" {
		mode = "bind"
	}

	createArgs := []string{"create", "--name", containerName, "--env-file", envPath}
	if opts.Mount != "" && mode != "snapshot" {
		createArgs = append(createArgs, "-v", opts.Mount+":/workspace")
	}
	if !opts.NoState {
		createArgs = append(createArgs, "-v", stateVolumeForProfile(opts.Profile)+":"+stateMountPath)
	}
	for _, v := range extraVolumes {
		createArgs = append(createArgs, "-v", v)
	}
	if info, err := os.Stat(cfg.AgentsDir); err == nil && info.IsDir() {
		createArgs = append(createArgs, "-v", cfg.AgentsDir+":/home/claude/.claude/agents:ro")
	}
	if opts.Browser != "" {
		createArgs = append(createArgs, "-e", "AIRUN_BROWSER="+opts.Browser)
		if opts.Browser == "vnc" || opts.Browser == "both" {
			createArgs = append(createArgs, "-p", "6080:6080")
		}
		if opts.Browser == "cdp" || opts.Browser == "both" {
			createArgs = append(createArgs, "-p", "9222:9222")
		}
	}
	createArgs = append(createArgs, imageName)
	createArgs = appendClaudeCmd(createArgs, opts)

	fmt.Fprintf(os.Stderr, "[airun] docker create --name %s --env-file %s\n", containerName, envfile.MaskLog(envPath))
	if out, err := exec.Command("docker", createArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("docker create failed: %s: %w", string(out), err)
	}

	// Snapshot mode: copy workspace into container instead of bind mount
	if opts.Mount != "" && mode == "snapshot" {
		fmt.Fprintf(os.Stderr, "[airun] snapshot mode: copying %s → container:/workspace\n", opts.Mount)
		cpCmd := exec.Command("docker", "cp", opts.Mount+"/.", containerName+":/workspace")
		if out, err := cpCmd.CombinedOutput(); err != nil {
			cleanupContainer(containerName)
			return fmt.Errorf("docker cp failed: %s: %w", string(out), err)
		}
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

	if err := os.MkdirAll(opts.Output, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "[airun] warning: cannot create output dir %s: %v\n", opts.Output, err)
	}
	cpCmd := exec.Command("docker", "cp", containerName+":/workspace/.", opts.Output)
	if cpOut, err := cpCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "[airun] warning: docker cp failed: %s\n", string(cpOut))
	} else {
		fmt.Fprintf(os.Stderr, "[airun] exported workspace to %s\n", opts.Output)
	}

	cleanupContainer(containerName)

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

// profileMounts generates Docker volume mount args from a profile's skills, plugins, and settings.
func profileMounts(p *profile.Profile) (volumes []string, settingsPath string, pluginScriptPath string, err error) {
	if len(p.Plugins) > 0 {
		scriptPath, scriptErr := generatePluginScript(p.Plugins)
		if scriptErr != nil {
			return nil, "", "", fmt.Errorf("generate plugin script: %w", scriptErr)
		}
		if scriptPath != "" {
			pluginScriptPath = scriptPath
			volumes = append(volumes, fmt.Sprintf("%s:/home/claude/.airun/post-init.sh:ro", scriptPath))
			fmt.Fprintf(os.Stderr, "[airun] plugins: %s (via post-init)\n", strings.Join(p.Plugins, ", "))
		}
	}

	for _, skillPath := range p.SkillPaths() {
		skillName := filepath.Base(skillPath)
		volumes = append(volumes, fmt.Sprintf("%s:/home/claude/.claude/skills/%s:ro", skillPath, skillName))
	}

	if len(p.Settings) > 0 {
		settingsJSON, err := json.Marshal(p.Settings)
		if err != nil {
			return nil, "", pluginScriptPath, fmt.Errorf("marshal settings: %w", err)
		}
		f, err := os.CreateTemp(os.TempDir(), ".airun-settings-*.json")
		if err != nil {
			return nil, "", pluginScriptPath, fmt.Errorf("create settings temp file: %w", err)
		}
		if _, err := f.Write(settingsJSON); err != nil {
			f.Close()
			os.Remove(f.Name())
			return nil, "", pluginScriptPath, fmt.Errorf("write settings: %w", err)
		}
		f.Close()
		settingsPath = f.Name()
		volumes = append(volumes, fmt.Sprintf("%s:/home/claude/.claude/settings.json:ro", settingsPath))
	}

	return volumes, settingsPath, pluginScriptPath, nil
}

// generatePluginScript creates a temporary shell script that installs Claude Code
// plugins listed in a profile. The script is mounted as post-init.sh and executed
// by the container entrypoint.
func generatePluginScript(plugins []string) (string, error) {
	var lines []string
	lines = append(lines, "#!/bin/bash")
	lines = append(lines, "# Auto-generated by airun profile — installs plugins via claude CLI")
	for _, plugin := range plugins {
		// Plugin format: "name@marketplace" or just "name"
		parts := strings.SplitN(plugin, "@", 2)
		name := parts[0]
		if len(parts) == 2 {
			marketplace := parts[1]
			lines = append(lines, fmt.Sprintf("claude plugin install %s --marketplace %s 2>/dev/null || true", name, marketplace))
		} else {
			lines = append(lines, fmt.Sprintf("claude plugin install %s 2>/dev/null || true", name))
		}
	}

	f, err := os.CreateTemp(os.TempDir(), ".airun-plugins-*.sh")
	if err != nil {
		return "", err
	}
	content := strings.Join(lines, "\n") + "\n"
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	f.Close()
	os.Chmod(f.Name(), 0755)
	return f.Name(), nil
}

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
	// ImageName is the tag airun expects for the agent runtime container
	// image. Exported because `airun rebuild` in cmd/airun references it.
	ImageName = "agent-runtime:latest"

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

// recordHistoryEntry saves a run record to ~/.airun/runs/ and prints the
// final `done in …` summary line. Shared across every non-interactive flow.
func recordHistoryEntry(opts RunOpts, provider, model string, start time.Time, exitCode int, output string) {
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
	if err := history.Save(rec, output); err != nil {
		fmt.Fprintf(os.Stderr, "[airun] warning: could not save run history: %v\n", err)
	}

	fmt.Fprintf(os.Stderr, "[airun] done in %.1fs | profile=%s provider=%s | exit=%d\n",
		float64(rec.DurationMs)/1000, rec.Profile, rec.Provider, rec.ExitCode)
	fmt.Fprintf(os.Stderr, "[airun] log: %s\n", rec.RunDir)
}

// appendStateAndExtras appends the per-profile state volume, profile-provided
// extra volumes, optional agents dir mount, and browser env/port args to a
// docker `run`/`create` argv. The workspace mount is intentionally left to
// callers because bind and snapshot flows differ on that point.
func appendStateAndExtras(args []string, cfg *config.Config, opts RunOpts, extraVolumes []string) []string {
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
	return args
}

func Run(cfg *config.Config, opts RunOpts) error {
	// Load profile if specified
	var extraVolumes []string
	var extraEnv []string
	var settingsTmp string
	if opts.Profile != "" {
		prof, err := profile.Load(opts.Profile)
		if err != nil {
			return fmt.Errorf("profile: %w", err)
		}
		fmt.Fprintf(os.Stderr, "[airun] profile=%s (%s)\n", prof.Name, prof.Description)

		extraVolumes, settingsTmp, extraEnv, err = profileMounts(prof)
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

	mount := opts.Mount
	if mount == "" {
		mount, _ = os.Getwd()
	}
	if mount == "" {
		mount = cfg.Workspace
	}

	if opts.Interactive {
		fmt.Fprintf(os.Stderr, "[airun] interactive: provider=%s model=%s mount=%s\n", provider, model, mount)
		return runDocker(cfg, RunOpts{Interactive: true, Mount: mount, Profile: opts.Profile, NoState: opts.NoState, Browser: opts.Browser}, provider, model, extraVolumes, extraEnv)
	}

	fmt.Fprintf(os.Stderr, "[airun] provider=%s model=%s workspace=%s\n", provider, model, mount)

	mode := cfg.Mode
	if mode == "" {
		mode = "bind"
	}
	snapshotIn := mode == "snapshot"

	subOpts := RunOpts{
		Prompt:   opts.Prompt,
		Mount:    mount,
		Output:   opts.Output,
		Profile:  opts.Profile,
		NoState:  opts.NoState,
		Loop:     opts.Loop,
		MaxLoops: opts.MaxLoops,
		Browser:  opts.Browser,
	}

	// Any flow that needs docker cp (snapshot workspace in, or export workspace out)
	// goes through the create/start/rm lifecycle; simple bind+no-export uses docker run --rm.
	if snapshotIn || opts.Output != "" {
		namePrefix := "airun-snap"
		if opts.Output != "" {
			namePrefix = "airun-export"
		}
		return runContainerCreate(cfg, subOpts, provider, model, extraVolumes, extraEnv, snapshotIn, opts.Output, namePrefix)
	}

	return runDocker(cfg, subOpts, provider, model, extraVolumes, extraEnv)
}

// runDocker handles the simple `docker run --rm` path: interactive shells and
// non-interactive bind-mode runs with no workspace export. Flows that require
// `docker cp` — snapshot input or workspace export — go through
// runContainerCreate instead.
func runDocker(cfg *config.Config, opts RunOpts, provider, model string, extraVolumes, extraEnv []string) error {
	envPath, err := envfile.Write(append(cfg.ContainerEnvWithModel(provider, model), extraEnv...))
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
	args = appendStateAndExtras(args, cfg, opts, extraVolumes)

	args = append(args, ImageName)

	// Claude Code command (non-interactive only)
	if !opts.Interactive {
		args = appendClaudeCmd(args, opts)
	}

	fmt.Fprintf(os.Stderr, "[airun] docker %s --env-file %s %s\n",
		args[0]+" "+args[1], envfile.MaskLog(envPath), ImageName)

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

	recordHistoryEntry(opts, provider, model, start, exitCode, outputBuf.String())

	return err
}

// runContainerCreate handles the `docker create → [cp in] → start → [cp out]
// → rm` lifecycle used whenever a run needs to stage workspace files via
// docker cp. copyIn=true stages opts.Mount into the container; copyOut (when
// non-empty) exports /workspace to that host path after the run finishes.
func runContainerCreate(
	cfg *config.Config,
	opts RunOpts,
	provider, model string,
	extraVolumes, extraEnv []string,
	copyIn bool,
	copyOut string,
	namePrefix string,
) error {
	containerName := fmt.Sprintf("%s-%d", namePrefix, time.Now().Unix())

	envPath, err := envfile.Write(append(cfg.ContainerEnvWithModel(provider, model), extraEnv...))
	if err != nil {
		return err
	}
	defer envfile.Cleanup(envPath)

	createArgs := []string{"create", "--name", containerName, "--env-file", envPath}
	if !copyIn && opts.Mount != "" {
		createArgs = append(createArgs, "-v", opts.Mount+":/workspace")
	}
	createArgs = appendStateAndExtras(createArgs, cfg, opts, extraVolumes)
	createArgs = append(createArgs, ImageName)
	createArgs = appendClaudeCmd(createArgs, opts)

	if copyIn {
		fmt.Fprintf(os.Stderr, "[airun] snapshot mode: creating container %s\n", containerName)
	} else {
		fmt.Fprintf(os.Stderr, "[airun] docker create --name %s --env-file %s\n", containerName, envfile.MaskLog(envPath))
	}
	if out, err := exec.Command("docker", createArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("docker create failed: %s: %w", string(out), err)
	}

	if copyIn && opts.Mount != "" {
		fmt.Fprintf(os.Stderr, "[airun] copying %s → container:/workspace\n", opts.Mount)
		if out, err := exec.Command("docker", "cp", opts.Mount+"/.", containerName+":/workspace").CombinedOutput(); err != nil {
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

	if copyOut != "" {
		if err := os.MkdirAll(copyOut, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "[airun] warning: cannot create output dir %s: %v\n", copyOut, err)
		}
		if cpOut, err := exec.Command("docker", "cp", containerName+":/workspace/.", copyOut).CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "[airun] warning: docker cp failed: %s\n", string(cpOut))
		} else {
			fmt.Fprintf(os.Stderr, "[airun] exported workspace to %s\n", copyOut)
		}
	}

	cleanupContainer(containerName)
	recordHistoryEntry(opts, provider, model, start, exitCode, outputBuf.String())

	return runErr
}

// basePlugins are installed at image build time by seed-plugins.sh and
// activated into installed_plugins.json by the container entrypoint. They
// don't need a runtime install, so the runner filters them out of any
// profile-declared plugin list before passing the remainder to the
// container as AIRUN_PLUGINS.
var basePlugins = map[string]bool{
	"superpowers":   true,
	"context7":      true,
	"skill-creator": true,
}

// filterBasePlugins returns a profile's plugin list with build-time base
// plugins removed. The base set is pre-installed in the image; listing them
// in a profile is harmless but passing them to `claude plugin install` would
// be a pointless no-op at container startup.
//
// A plugin entry is "name" or "name@marketplace"; the plugin's identity for
// filtering purposes is the "name" segment.
func filterBasePlugins(plugins []string) []string {
	out := make([]string, 0, len(plugins))
	for _, p := range plugins {
		name := p
		if at := strings.IndexByte(p, '@'); at >= 0 {
			name = p[:at]
		}
		if basePlugins[name] {
			continue
		}
		out = append(out, p)
	}
	return out
}

// profileMounts converts a profile into container mounts and extra env vars.
//
// Volumes cover skill directories and (when the profile declares a non-empty
// settings block) a one-shot settings.json mounted at /home/claude/.claude/.
// Extra env vars currently cover only AIRUN_PLUGINS — profile-declared plugins
// beyond the base set are joined into a single comma-separated env var that
// the entrypoint parses and feeds to `claude plugin install` at container
// startup.
//
// The caller is responsible for removing settingsPath after the container
// exits.
func profileMounts(p *profile.Profile) (volumes []string, settingsPath string, env []string, err error) {
	if extras := filterBasePlugins(p.Plugins); len(extras) > 0 {
		env = append(env, "AIRUN_PLUGINS="+strings.Join(extras, ","))
		fmt.Fprintf(os.Stderr, "[airun] extra plugins: %s\n", strings.Join(extras, ", "))
	}

	for _, skillPath := range p.SkillPaths() {
		skillName := filepath.Base(skillPath)
		volumes = append(volumes, fmt.Sprintf("%s:/home/claude/.claude/skills/%s:ro", skillPath, skillName))
	}

	if len(p.Settings) > 0 {
		settingsJSON, mErr := json.Marshal(p.Settings)
		if mErr != nil {
			return nil, "", env, fmt.Errorf("marshal settings: %w", mErr)
		}
		f, cErr := os.CreateTemp(os.TempDir(), ".airun-settings-*.json")
		if cErr != nil {
			return nil, "", env, fmt.Errorf("create settings temp file: %w", cErr)
		}
		if _, wErr := f.Write(settingsJSON); wErr != nil {
			f.Close()
			os.Remove(f.Name())
			return nil, "", env, fmt.Errorf("write settings: %w", wErr)
		}
		f.Close()
		settingsPath = f.Name()
		volumes = append(volumes, fmt.Sprintf("%s:/home/claude/.claude/settings.json:ro", settingsPath))
	}

	return volumes, settingsPath, env, nil
}

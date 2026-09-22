package runner

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
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

	stateVolumeName       = "airun-claude-state"
	stateMountPath        = "/home/claude/.claude"
	profileStateMountPath = "/var/lib/airun/state"
	componentVolumeName   = "airun-components-cache"
	componentMountPath    = "/var/lib/airun/components"
	hostAgentsMountPath   = "/run/airun/host-agents"
)

type RunOpts struct {
	runID       string
	Prompt      string
	Provider    string // z/zai | m/mm/minimax | k/kimi | r/remote
	Profile     string // YAML selector (components, settings, provider)
	Model       string // model override (e.g. kimi-k2.5, glm-5.3)
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
func recordHistoryEntry(opts RunOpts, provider, model string, start time.Time, runErr error, recoveryContainer, output string) {
	exitCode := 0
	message := ""
	if runErr != nil {
		exitCode = 1
		message = runErr.Error()
	}
	rec := history.RunRecord{
		RunID:             opts.runID,
		AgentName:         opts.Name,
		Error:             message,
		RecoveryContainer: recoveryContainer,
		Timestamp:         time.Now().Format("2006-01-02_15-04-05"),
		Profile:           opts.Profile,
		Provider:          provider,
		Model:             model,
		Prompt:            opts.Prompt,
		DurationMs:        time.Since(start).Milliseconds(),
		ExitCode:          exitCode,
		RunDir:            history.RunDir(opts.runID, opts.Profile, provider),
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
		target := stateMountPath
		if opts.Profile != "" {
			target = profileStateMountPath
			args = append(args, "-e", "AIRUN_PROFILE_STATE="+target)
		}
		args = append(args, "-v", stateVolumeForProfile(opts.Profile)+":"+target)
	}
	if opts.Profile != "" {
		args = append(args, "-v", componentVolumeName+":"+componentMountPath,
			"-e", "AIRUN_COMPONENT_CACHE="+componentMountPath)
	}
	for _, v := range extraVolumes {
		args = append(args, "-v", v)
	}
	if info, err := os.Stat(cfg.AgentsDir); err == nil && info.IsDir() {
		target := "/home/claude/.claude/agents"
		if opts.Profile != "" {
			target = hostAgentsMountPath
			args = append(args, "-e", "AIRUN_HOST_AGENTS="+target)
		}
		args = append(args, "-v", cfg.AgentsDir+":"+target+":ro")
	}
	if opts.Browser != "" {
		args = append(args, "-e", "AIRUN_BROWSER="+opts.Browser)
		if opts.Browser == "vnc" || opts.Browser == "both" {
			args = append(args, "-p", "127.0.0.1:6080:6080")
		}
		if opts.Browser == "cdp" || opts.Browser == "both" {
			args = append(args, "-p", "127.0.0.1:9222:9222")
		}
	}
	return args
}

func Run(cfg *config.Config, opts RunOpts) error {
	mode := cfg.Mode
	if mode == "" {
		mode = "snapshot"
	}
	if mode != "snapshot" && mode != "bind" {
		return fmt.Errorf("invalid ARUN_MODE %q: expected snapshot or bind", mode)
	}
	opts.runID = history.NewRunID()

	// Load profile if specified
	var extraVolumes []string
	var extraEnv []string
	var manifestTmp string
	if opts.Profile != "" {
		prof, err := profile.Load(opts.Profile)
		if err != nil {
			return fmt.Errorf("profile: %w", err)
		}
		fmt.Fprintf(os.Stderr, "[airun] profile=%s (%s)\n", prof.Name, prof.Description)

		extraVolumes, manifestTmp, extraEnv, err = profileMounts(prof)
		if err != nil {
			return fmt.Errorf("profile mounts: %w", err)
		}
		if manifestTmp != "" {
			defer os.Remove(manifestTmp)
		}
		if err := requireProfileImage(); err != nil {
			return err
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

	fmt.Fprintf(os.Stderr, "[airun] provider=%s model=%s workspace=%s mode=%s\n", provider, model, mount, mode)
	snapshotIn := mode == "snapshot"
	subOpts := opts
	subOpts.Mount = mount

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
	args = append(args, "--name", "airun-"+opts.runID, "--env-file", envPath)

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
	recordHistoryEntry(opts, provider, model, start, err, "", outputBuf.String())

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
	containerName := namePrefix + "-" + opts.runID

	envPath, err := envfile.Write(append(cfg.ContainerEnvWithModel(provider, model), extraEnv...))
	if err != nil {
		return err
	}
	defer envfile.Cleanup(envPath)

	createArgs := []string{"create", "--name", containerName, "--env-file", envPath}
	if opts.Interactive {
		createArgs = append(createArgs, "-it")
	}
	if copyIn {
		createArgs = append(createArgs, "-e", "AIRUN_WORKSPACE_MODE=snapshot")
	}
	if !copyIn && opts.Mount != "" {
		createArgs = append(createArgs, "-v", opts.Mount+":/workspace")
	}
	createArgs = appendStateAndExtras(createArgs, cfg, opts, extraVolumes)
	createArgs = append(createArgs, ImageName)
	if !opts.Interactive {
		createArgs = appendClaudeCmd(createArgs, opts)
	}

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

	startArgs := []string{"start", "-a"}
	if opts.Interactive {
		startArgs = append(startArgs, "-i")
	}
	startCmd := exec.Command("docker", append(startArgs, containerName)...)
	startCmd.Stdin = os.Stdin
	startCmd.Stdout = io.MultiWriter(os.Stdout, &outputBuf)
	startCmd.Stderr = os.Stderr
	runErr := startCmd.Run()

	// docker start's exit status can describe attachment rather than the process.
	if runErr == nil {
		status, err := exec.Command("docker", "inspect", "--format", "{{.State.ExitCode}}", containerName).Output()
		if err != nil {
			runErr = fmt.Errorf("inspect container exit status: %w", err)
		} else if code, err := strconv.Atoi(strings.TrimSpace(string(status))); err != nil {
			runErr = fmt.Errorf("invalid container exit status %q", strings.TrimSpace(string(status)))
		} else if code != 0 {
			runErr = fmt.Errorf("container exited with code %d", code)
		}
	}

	var exportErr error
	if copyOut != "" {
		if err := os.MkdirAll(copyOut, 0755); err != nil {
			exportErr = fmt.Errorf("create output directory %s: %w", copyOut, err)
		} else if out, err := exec.Command("docker", "cp", containerName+":/workspace/.", copyOut).CombinedOutput(); err != nil {
			exportErr = fmt.Errorf("export workspace to %s: %s: %w", copyOut, out, err)
		} else {
			fmt.Fprintf(os.Stderr, "[airun] exported workspace to %s\n", copyOut)
		}
	}
	recoveryContainer := ""
	if exportErr != nil {
		recoveryContainer = containerName
		fmt.Fprintf(os.Stderr, "[airun] export failed; result preserved in container %s (destination may be partial).\n", containerName)
		fmt.Fprintf(os.Stderr, "[airun] recover: docker cp %s:/workspace/. <recovery-directory>\n", containerName)
	} else {
		cleanupContainer(containerName)
	}
	runErr = errors.Join(runErr, exportErr)
	if !opts.Interactive {
		recordHistoryEntry(opts, provider, model, start, runErr, recoveryContainer, outputBuf.String())
	}

	return runErr
}

package runner

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/codegeek/automatica-agent-runtime/internal/config"
)

type RunOpts struct {
	Prompt      string
	Provider    string // zai | minimax (overrides config)
	Loop        bool
	MaxLoops    int
	Name        string
	Interactive bool   // -it mode, no prompt
	Mount       string // explicit mount path (overrides config workspace)
}

func Run(cfg *config.Config, opts RunOpts) error {
	provider := opts.Provider
	if provider == "" {
		provider = cfg.Provider
	}

	model := cfg.ZaiModel
	if provider == "minimax" {
		model = cfg.MinimaxModel
	}

	mount := opts.Mount
	if mount == "" {
		mount = cfg.Workspace
	}

	if opts.Interactive {
		fmt.Fprintf(os.Stderr, "[arun] interactive session: provider=%s model=%s\n", provider, model)
		if mount != "" {
			fmt.Fprintf(os.Stderr, "[arun] mount: %s → /workspace\n", mount)
		}
		return runDockerInteractive(cfg, provider, mount)
	}

	fmt.Fprintf(os.Stderr, "[arun] provider=%s model=%s workspace=%s mode=%s\n",
		provider, model, cfg.Workspace, cfg.Mode)

	// Try clawker first, fall back to docker run if socket bridge fails
	args := buildClawkerArgs(cfg, opts, provider)
	fmt.Fprintf(os.Stderr, "[arun] clawker %s\n", strings.Join(args, " "))

	cmd := exec.Command("clawker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	err := cmd.Run()
	if err != nil && strings.Contains(err.Error(), "exit status 1") {
		fmt.Fprintf(os.Stderr, "[arun] clawker failed, trying docker run fallback...\n")
		return runDocker(cfg, opts, provider)
	}
	return err
}

func runDockerInteractive(cfg *config.Config, provider, mount string) error {
	imageName := "clawker-agent-runtime:latest"

	args := []string{"run", "-it", "--rm"}

	for _, env := range cfg.ContainerEnv(provider) {
		args = append(args, "-e", env)
	}

	if mount != "" {
		args = append(args, "-v", mount+":/workspace")
	}

	args = append(args, imageName)

	fmt.Fprintf(os.Stderr, "[arun] docker %s\n", strings.Join(args, " "))

	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func runDocker(cfg *config.Config, opts RunOpts, provider string) error {
	// Find project image name
	imageName := "clawker-agent-runtime:latest"

	args := []string{"run", "--rm"}

	// Pass environment variables
	for _, env := range cfg.ContainerEnv(provider) {
		args = append(args, "-e", env)
	}

	// Mount workspace if in bind mode
	if cfg.Mode == "bind" {
		args = append(args, "-v", cfg.Workspace+":/workspace")
	}

	args = append(args, imageName)

	// Claude Code command
	args = append(args, "claude", "-p", opts.Prompt, "--dangerously-skip-permissions")

	fmt.Fprintf(os.Stderr, "[arun] docker %s\n", strings.Join(args, " "))

	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func buildClawkerArgs(cfg *config.Config, opts RunOpts, provider string) []string {
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

	for _, env := range cfg.ContainerEnv(provider) {
		args = append(args, "-e", env)
	}

	args = append(args, "@")

	if !opts.Loop {
		args = append(args, "-p", opts.Prompt, "--dangerously-skip-permissions")
	}

	return args
}

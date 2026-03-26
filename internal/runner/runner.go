package runner

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/codegeek/automatica-agent-runtime/internal/config"
)

type RunOpts struct {
	Prompt   string
	Model    string
	Provider string // zai | minimax (overrides config)
	Loop     bool
	MaxLoops int
	Name     string
}

func Run(cfg *config.Config, opts RunOpts) error {
	provider := opts.Provider
	if provider == "" {
		provider = cfg.Provider
	}

	args := buildClawkerArgs(cfg, opts, provider)
	cmd := exec.Command("clawker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	fmt.Fprintf(os.Stderr, "[arun] provider=%s model=%s workspace=%s mode=%s\n",
		provider, cfg.ActiveModel(), cfg.Workspace, cfg.Mode)
	fmt.Fprintf(os.Stderr, "[arun] clawker %s\n", strings.Join(args, " "))
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

	// Workspace mode
	args = append(args, "--mode", cfg.Mode)

	if opts.Name != "" {
		args = append(args, "--agent", opts.Name)
	}

	args = append(args, "--rm")

	// Pass environment variables from config
	for _, env := range cfg.ContainerEnv(provider) {
		args = append(args, "-e", env)
	}

	// Image reference
	args = append(args, "@")

	// Claude Code flags (after @)
	if !opts.Loop {
		args = append(args, "-p", opts.Prompt, "--dangerously-skip-permissions")
	}

	return args
}

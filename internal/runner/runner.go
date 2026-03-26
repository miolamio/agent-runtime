package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/codegeek/automatica-agent-runtime/internal/config"
	"github.com/codegeek/automatica-agent-runtime/internal/prereq"
)

type RunOpts struct {
	Prompt   string
	Model    string
	Profile  string
	Loop     bool
	MaxLoops int
	Skills   []string
	Name     string
}

func Run(cfg *config.Config, opts RunOpts) error {
	status, err := prereq.Check()
	if err != nil {
		return fmt.Errorf("prerequisite check failed: %w", err)
	}
	if !status.Ready() {
		return fmt.Errorf("prerequisites not met:\n%s", status)
	}

	if opts.Profile == "" || opts.Profile == "router" {
		if err := prereq.EnsureRouterRunning(); err != nil {
			fmt.Fprintf(os.Stderr, "[arun] warning: could not start Router: %v\n", err)
		}
	}

	args := buildClawkerArgs(cfg, opts)
	cmd := exec.Command("clawker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	fmt.Fprintf(os.Stderr, "[arun] Running: clawker %s\n", strings.Join(args, " "))
	return cmd.Run()
}

func buildClawkerArgs(cfg *config.Config, opts RunOpts) []string {
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

	if opts.Name != "" {
		args = append(args, "--name", opts.Name)
	}

	args = append(args, "--skip-permissions")

	if opts.Profile != "" {
		envFile := filepath.Join(cfg.ProfilesDir, opts.Profile+".env")
		if _, err := os.Stat(envFile); err == nil {
			args = append(args, "--env-file", envFile)
		}
	}

	if !opts.Loop {
		args = append(args, "--", "claude", "-p", opts.Prompt)
	}

	return args
}

package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/miolamio/agent-runtime/internal/config"
	"github.com/miolamio/agent-runtime/internal/history"
	"github.com/miolamio/agent-runtime/internal/keys"
	"github.com/miolamio/agent-runtime/internal/monitor"
	"github.com/miolamio/agent-runtime/internal/prereq"
	"github.com/miolamio/agent-runtime/internal/runner"
	"github.com/miolamio/agent-runtime/internal/setup"
)

const version = "0.3.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "--version", "-v":
		fmt.Println("airun", version)
		return
	case "--help", "-h":
		printUsage()
		return
	case "--status":
		if err := monitor.ShowStatus(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	case "--check":
		runCheck()
		return
	case "shell":
		runShell(os.Args[2:])
		return
	case "init":
		if err := setup.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	case "rebuild":
		fmt.Println("[airun] Rebuilding agent-runtime image...")
		args := []string{"build", "-t", "agent-runtime:latest"}
		if len(os.Args) > 2 && os.Args[2] == "--no-cache" {
			args = append(args, "--no-cache")
		}
		// Find docker/ directory with our Dockerfile
		dockerDir := "docker"
		if _, err := os.Stat(filepath.Join(dockerDir, "Dockerfile")); err != nil {
			usr, _ := user.Current()
			for _, dir := range []string{
				filepath.Join(usr.HomeDir, "src", "agent-runtime", "docker"),
				filepath.Join(usr.HomeDir, "agent-runtime", "docker"),
			} {
				if _, err := os.Stat(filepath.Join(dir, "Dockerfile")); err == nil {
					dockerDir = dir
					break
				}
			}
		}
		args = append(args, dockerDir)
		cmd := exec.Command("docker", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "[airun] rebuild failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("[airun] Rebuild complete: agent-runtime:latest")
		return
	case "history":
		records, err := history.List(20)
		if err != nil {
			fmt.Fprintf(os.Stderr, "No run history yet.\n")
			return
		}
		if len(records) == 0 {
			fmt.Println("No runs yet.")
			return
		}
		fmt.Print(history.FormatTable(records))
		return
	case "keys":
		if len(os.Args) < 3 {
			fmt.Println("Usage: airun keys <list|add|remove|test|default> [provider]")
			os.Exit(1)
		}
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "config error: %v\n", err)
			os.Exit(1)
		}
		subcmd := os.Args[2]
		arg := ""
		if len(os.Args) > 3 {
			arg = os.Args[3]
		}
		var kerr error
		switch subcmd {
		case "list", "ls":
			kerr = keys.List(cfg.EnvFile)
		case "add":
			if arg == "" {
				fmt.Fprintln(os.Stderr, "Usage: airun keys add <provider>")
				os.Exit(1)
			}
			kerr = keys.Add(cfg.EnvFile, arg)
		case "remove", "rm":
			if arg == "" {
				fmt.Fprintln(os.Stderr, "Usage: airun keys remove <provider>")
				os.Exit(1)
			}
			kerr = keys.Remove(cfg.EnvFile, arg)
		case "test":
			kerr = keys.Test(cfg.EnvFile, arg)
		case "default":
			if arg == "" {
				fmt.Fprintln(os.Stderr, "Usage: airun keys default <provider>")
				os.Exit(1)
			}
			kerr = keys.SetDefault(cfg.EnvFile, arg)
		default:
			fmt.Fprintf(os.Stderr, "Unknown keys subcommand: %s\n", subcmd)
			fmt.Println("Usage: airun keys <list|add|remove|test|default> [provider]")
			os.Exit(1)
		}
		if kerr != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", kerr)
			os.Exit(1)
		}
		return
	}

	// Parse run flags
	fs := flag.NewFlagSet("airun", flag.ExitOnError)
	provider := fs.String("provider", "", "Provider: zai (default) | minimax")
	profileName := fs.String("p", "", "Profile name (dev, text, default)")
	fs.StringVar(profileName, "profile", "", "Profile name (dev, text, default)")
	loop := fs.Bool("loop", false, "Enable autonomous loop mode")
	maxLoops := fs.Int("max-loops", 5, "Maximum loops in loop mode")
	name := fs.String("name", "", "Agent name")
	output := fs.String("output", "", "Export workspace to this directory after run")
	parallel := fs.Bool("parallel", false, "Run agents in parallel")
	var agents []string
	fs.Func("agent", "Agent spec 'name:prompt' (repeatable, use with --parallel)", func(s string) error {
		agents = append(agents, s)
		return nil
	})
	fs.Parse(os.Args[1:])

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	if *parallel && len(agents) > 0 {
		var specs []runner.AgentSpec
		for _, a := range agents {
			spec, err := runner.ParseAgentSpec(a)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			specs = append(specs, spec)
		}
		if err := runner.RunParallel(cfg, specs, *provider); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	prompt := strings.Join(fs.Args(), " ")
	if prompt == "" {
		fmt.Fprintln(os.Stderr, "error: no prompt provided")
		printUsage()
		os.Exit(1)
	}

	opts := runner.RunOpts{
		Prompt:   prompt,
		Provider: *provider,
		Profile:  *profileName,
		Loop:     *loop,
		MaxLoops: *maxLoops,
		Name:     *name,
		Output:   *output,
	}
	if err := runner.Run(cfg, opts); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runShell(args []string) {
	fs := flag.NewFlagSet("shell", flag.ExitOnError)
	provider := fs.String("provider", "", "Provider: zai (default) | minimax")
	profileName := fs.String("p", "", "Profile name (dev, text, default)")
	fs.StringVar(profileName, "profile", "", "Profile name (dev, text, default)")
	mount := fs.String("mount", "", "Directory to mount into /workspace")
	fs.Parse(args)

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	opts := runner.RunOpts{
		Interactive: true,
		Provider:    *provider,
		Profile:     *profileName,
		Mount:       *mount,
	}
	if err := runner.Run(cfg, opts); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runCheck() {
	fmt.Println("Agent Runtime — Check")
	fmt.Println()

	// Prerequisites
	status, _ := prereq.Check()
	fmt.Println("Prerequisites:")
	fmt.Println(status)
	fmt.Println()

	// Config
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Config error: %v\n", err)
		return
	}
	fmt.Println("Config (~/.airun.env):")
	fmt.Println(cfg.Show())
	fmt.Println()

	fmt.Printf("Env file: %s\n", cfg.EnvFile)
}

func printUsage() {
	fmt.Print(`airun — Agent Runtime CLI v` + version + `

Usage:
  airun "prompt"                              Run agent task
  airun -p dev "prompt"                       Run with profile (skills, settings)
  airun --provider mm "prompt"                Run with specific provider
  airun shell                                 Interactive Claude Code session
  airun shell -p dev                          Interactive with profile
  airun shell --mount /path/to/project        Interactive with project mounted
  airun shell --provider mm                   Interactive with MiniMax
  airun --loop --max-loops N "prompt"         Autonomous loop mode
  airun --output /tmp/out "prompt"            Export workspace after run
  airun --parallel --agent "n:prompt" [...]   Parallel agents
  airun history                               Show recent run history
  airun keys list                              Show configured API keys
  airun keys add <provider>                    Add/replace key with guide
  airun keys remove <provider>                 Remove provider key
  airun keys test [provider]                   Validate keys via API call
  airun keys default <provider>                Change default provider
  airun init                                  Interactive global setup
  airun rebuild                               Rebuild docker image
  airun rebuild --no-cache                    Rebuild without cache
  airun --status                              Show running agents
  airun --check                               Show config and prerequisites
  airun --version                             Show version

Flags:
  -p, --profile    Profile name (loads skills, settings, provider)
  --provider       Provider override: z/zai | m/mm/minimax | k/kimi
  --output         Export workspace to this directory after run

Config: ~/.airun.env (workspace, API keys, provider, mode)
Profiles: ~/airun-profiles/*.yaml
Providers: z/zai (Z.AI GLM-4.7) | m/mm/minimax (MiniMax M2.7) | k/kimi (Kimi K2.5)
`)
}

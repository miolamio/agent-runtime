package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/codegeek/automatica-agent-runtime/internal/config"
	"github.com/codegeek/automatica-agent-runtime/internal/history"
	"github.com/codegeek/automatica-agent-runtime/internal/monitor"
	"github.com/codegeek/automatica-agent-runtime/internal/prereq"
	"github.com/codegeek/automatica-agent-runtime/internal/runner"
)

const version = "0.2.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "--version", "-v":
		fmt.Println("arun", version)
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
	case "--monitor":
		if err := monitor.StartMonitoring(); err != nil {
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
	}

	// Parse run flags
	fs := flag.NewFlagSet("arun", flag.ExitOnError)
	provider := fs.String("provider", "", "Provider: zai (default) | minimax")
	profileName := fs.String("p", "", "Profile name (dev, text, default)")
	fs.StringVar(profileName, "profile", "", "Profile name (dev, text, default)")
	loop := fs.Bool("loop", false, "Enable autonomous loop mode")
	maxLoops := fs.Int("max-loops", 5, "Maximum loops in loop mode")
	name := fs.String("name", "", "Agent name")
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
	fmt.Println("AUTOMATICA Agent Runtime — Check")
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
	fmt.Println("Config (~/.automatica.env):")
	fmt.Println(cfg.Show())
	fmt.Println()

	fmt.Printf("Env file: %s\n", cfg.EnvFile)
}

func printUsage() {
	fmt.Print(`arun — AUTOMATICA Agent Runtime CLI v` + version + `

Usage:
  arun "prompt"                              Run agent task
  arun -p dev "prompt"                       Run with profile (skills, settings)
  arun --provider mm "prompt"                Run with specific provider
  arun shell                                 Interactive Claude Code session
  arun shell -p dev                          Interactive with profile
  arun shell --mount /path/to/project        Interactive with project mounted
  arun shell --provider mm                   Interactive with MiniMax
  arun --loop --max-loops N "prompt"         Autonomous loop mode
  arun --parallel --agent "n:prompt" [...]   Parallel agents
  arun history                               Show recent run history
  arun --status                              Show running agents
  arun --check                               Show config and prerequisites
  arun --version                             Show version

Flags:
  -p, --profile    Profile name (loads skills, settings, provider)
  --provider       Provider override: z/zai | m/mm/minimax

Config: ~/.automatica.env (workspace, API keys, provider, mode)
Profiles: ~/automatica-profiles/*.yaml
Providers: z/zai (Z.AI GLM-4.7) | m/mm/minimax (MiniMax M2.7)
`)
}

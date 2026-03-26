package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/codegeek/automatica-agent-runtime/internal/config"
	"github.com/codegeek/automatica-agent-runtime/internal/monitor"
	"github.com/codegeek/automatica-agent-runtime/internal/prereq"
	"github.com/codegeek/automatica-agent-runtime/internal/runner"
)

const version = "0.1.0"

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
	}

	// Parse run flags
	fs := flag.NewFlagSet("arun", flag.ExitOnError)
	model := fs.String("model", "", "Model to use (provider,model-name)")
	profile := fs.String("profile", "", "Environment profile (anthropic, zai, minimax, router)")
	loop := fs.Bool("loop", false, "Enable autonomous loop mode")
	maxLoops := fs.Int("max-loops", 5, "Maximum loops in loop mode")
	name := fs.String("name", "", "Agent name (for parallel runs)")
	parallel := fs.Bool("parallel", false, "Run agents in parallel")
	skillsFlag := fs.String("skills", "", "Comma-separated list of skills to load")
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
		if err := runner.RunParallel(cfg, specs, *profile); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Collect remaining args as prompt
	prompt := strings.Join(fs.Args(), " ")
	if prompt == "" {
		fmt.Fprintln(os.Stderr, "error: no prompt provided")
		printUsage()
		os.Exit(1)
	}

	var skills []string
	if *skillsFlag != "" {
		skills = strings.Split(*skillsFlag, ",")
	}

	opts := runner.RunOpts{
		Prompt:   prompt,
		Model:    *model,
		Profile:  *profile,
		Loop:     *loop,
		MaxLoops: *maxLoops,
		Name:     *name,
		Skills:   skills,
	}
	if err := runner.Run(cfg, opts); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runCheck() {
	fmt.Println("AUTOMATICA Agent Runtime — Prerequisites Check")
	fmt.Println()
	status, _ := prereq.Check()
	fmt.Println(status)
	fmt.Println()
	if status.Ready() {
		fmt.Println("All prerequisites met.")
	} else {
		fmt.Println("Some prerequisites are missing. Install them before running agents.")
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("\nConfig error: %v\n", err)
		return
	}
	if err := cfg.Validate(); err != nil {
		fmt.Printf("\nConfig validation: %v\n", err)
	} else {
		fmt.Println("\nDirectories: OK")
	}
}

func printUsage() {
	fmt.Print(`arun — AUTOMATICA Agent Runtime CLI v` + version + `

Usage:
  arun "prompt"                              Run a single agent task
  arun --model provider,model "prompt"       Run with specific model
  arun --profile name "prompt"               Run with specific profile
  arun --loop --max-loops N "prompt"         Run in autonomous loop mode
  arun --parallel --agent "n:prompt" [...]   Run parallel agents
  arun --status                              Show running agents
  arun --monitor                             Start monitoring dashboard
  arun --check                               Check prerequisites
  arun --version                             Show version

Profiles: zai (default), minimax
Models:   glm-4.7 (Z.AI) | MiniMax-M2.7 (MiniMax)
`)
}

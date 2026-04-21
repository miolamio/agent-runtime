package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/miolamio/agent-runtime/internal/config"
	"github.com/miolamio/agent-runtime/internal/history"
	"github.com/miolamio/agent-runtime/internal/keys"
	"github.com/miolamio/agent-runtime/internal/monitor"
	"github.com/miolamio/agent-runtime/internal/prereq"
	"github.com/miolamio/agent-runtime/internal/proxy"
	"github.com/miolamio/agent-runtime/internal/runner"
	"github.com/miolamio/agent-runtime/internal/setup"
)

const version = "0.6.0"

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
	case "state":
		if len(os.Args) < 3 {
			fmt.Println("Usage: airun state <info|reset>")
			os.Exit(1)
		}
		switch os.Args[2] {
		case "info":
			cmd := exec.Command("docker", "volume", "inspect", "airun-claude-state")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Println("No state volume found. Will be created on first run.")
			}
		case "reset":
			out, err := exec.Command("docker", "volume", "rm", "airun-claude-state").CombinedOutput()
			if err != nil {
				if strings.Contains(string(out), "no such volume") {
					fmt.Println("[airun] no state volume to remove.")
				} else {
					fmt.Fprintf(os.Stderr, "[airun] docker volume rm failed: %s\n", strings.TrimSpace(string(out)))
					os.Exit(1)
				}
			} else {
				fmt.Println("[airun] state volume removed. Will be re-seeded on next run.")
			}
		default:
			fmt.Fprintf(os.Stderr, "Unknown state subcommand: %s\n", os.Args[2])
			os.Exit(1)
		}
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
		rebuildFlags := map[string]bool{}
		for _, a := range os.Args[2:] {
			rebuildFlags[a] = true
		}
		if rebuildFlags["--no-cache"] {
			args = append(args, "--no-cache")
		}
		if rebuildFlags["--fresh"] {
			// Bust the Claude Code install cache to get the latest version
			args = append(args, "--build-arg", fmt.Sprintf("CLAUDE_BUST_CACHE=%d", time.Now().Unix()))
			fmt.Println("[airun] --fresh: will reinstall Claude Code CLI")
		}
		// Find docker/ directory with our Dockerfile
		dockerDir := "docker"
		if _, err := os.Stat(filepath.Join(dockerDir, "Dockerfile")); err != nil {
			if home, herr := os.UserHomeDir(); herr == nil {
				for _, dir := range []string{
					filepath.Join(home, "src", "agent-runtime", "docker"),
					filepath.Join(home, "agent-runtime", "docker"),
				} {
					if _, err := os.Stat(filepath.Join(dir, "Dockerfile")); err == nil {
						dockerDir = dir
						break
					}
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
		case "model":
			if arg == "" {
				fmt.Fprintln(os.Stderr, "Usage: airun keys model <model-name>")
				os.Exit(1)
			}
			kerr = keys.SetModel(cfg.EnvFile, arg)
		default:
			fmt.Fprintf(os.Stderr, "Unknown keys subcommand: %s\n", subcmd)
			fmt.Println("Usage: airun keys <list|add|remove|test|default|model> [arg]")
			os.Exit(1)
		}
		if kerr != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", kerr)
			os.Exit(1)
		}
		return
	case "proxy":
		if len(os.Args) < 3 {
			fmt.Println("Usage: airun proxy <init|serve|user|connect|disconnect> [args]")
			os.Exit(1)
		}
		configPath, studentsPath := proxy.DefaultPaths()
		subcmd := os.Args[2]
		switch subcmd {
		case "connect":
			// airun proxy connect [url] [token]
			var url, token string
			if len(os.Args) > 3 {
				url = os.Args[3]
			}
			if len(os.Args) > 4 {
				token = os.Args[4]
			}
			fmt.Println("\n  --- Connect Claude Code to Proxy ---")
			if err := proxy.Connect(url, token); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			return
		case "disconnect":
			if err := proxy.Disconnect(); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			return
		case "init":
			if err := proxy.Init(configPath, studentsPath); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
		case "serve":
			listen := ""
			if len(os.Args) > 3 && os.Args[3] == "--port" && len(os.Args) > 4 {
				listen = ":" + os.Args[4]
			}
			if err := proxy.Serve(configPath, studentsPath, listen); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
		case "user":
			if len(os.Args) < 4 {
				fmt.Println("Usage: airun proxy user <add|list|revoke|restore|import|export> [name]")
				os.Exit(1)
			}
			action := os.Args[3]
			arg := ""
			if len(os.Args) > 4 {
				arg = os.Args[4]
			}
			var perr error
			switch action {
			case "add":
				if arg == "" {
					fmt.Fprintln(os.Stderr, "Usage: airun proxy user add <name>")
					os.Exit(1)
				}
				perr = proxy.StudentAdd(studentsPath, arg)
			case "list", "ls":
				perr = proxy.StudentList(studentsPath)
			case "revoke":
				if arg == "" {
					fmt.Fprintln(os.Stderr, "Usage: airun proxy user revoke <name>")
					os.Exit(1)
				}
				perr = proxy.StudentRevoke(studentsPath, arg)
			case "restore":
				if arg == "" {
					fmt.Fprintln(os.Stderr, "Usage: airun proxy user restore <name>")
					os.Exit(1)
				}
				perr = proxy.StudentRestore(studentsPath, arg)
			case "import":
				if arg == "" {
					fmt.Fprintln(os.Stderr, "Usage: airun proxy user import <file>")
					os.Exit(1)
				}
				perr = proxy.StudentImport(studentsPath, arg)
			case "export":
				perr = proxy.StudentExport(studentsPath)
			default:
				fmt.Fprintf(os.Stderr, "Unknown user action: %s\n", action)
				os.Exit(1)
			}
			if perr != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", perr)
				os.Exit(1)
			}
		default:
			fmt.Fprintf(os.Stderr, "Unknown proxy subcommand: %s\n", subcmd)
			os.Exit(1)
		}
		return
	}

	// Parse run flags
	fs := flag.NewFlagSet("airun", flag.ExitOnError)
	provider := fs.String("provider", "", "Provider: zai | minimax | kimi | remote")
	modelFlag := fs.String("model", "", "Model override (e.g. kimi-k2.5, glm-5.1)")
	fs.StringVar(modelFlag, "m", "", "Model override (short)")
	profileName := fs.String("p", "", "Profile name (dev, text, default)")
	fs.StringVar(profileName, "profile", "", "Profile name (dev, text, default)")
	loop := fs.Bool("loop", false, "Enable autonomous loop mode")
	maxLoops := fs.Int("max-loops", 5, "Maximum loops in loop mode")
	name := fs.String("name", "", "Agent name")
	output := fs.String("output", "", "Export workspace to this directory after run")
	noState := fs.Bool("no-state", false, "Disable persistent state (ephemeral container)")
	browser := fs.String("browser", "", "Browser display: vnc | cdp | both")
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
		Model:    *modelFlag,
		Profile:  *profileName,
		Loop:     *loop,
		MaxLoops: *maxLoops,
		Name:     *name,
		Output:   *output,
		NoState:  *noState,
		Browser:  *browser,
	}
	if err := runner.Run(cfg, opts); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runShell(args []string) {
	fs := flag.NewFlagSet("shell", flag.ExitOnError)
	provider := fs.String("provider", "", "Provider: zai | minimax | kimi | remote")
	modelFlag := fs.String("model", "", "Model override (e.g. kimi-k2.5, glm-5.1)")
	fs.StringVar(modelFlag, "m", "", "Model override (short)")
	profileName := fs.String("p", "", "Profile name (dev, text, default)")
	fs.StringVar(profileName, "profile", "", "Profile name (dev, text, default)")
	mount := fs.String("mount", "", "Directory to mount into /workspace")
	noState := fs.Bool("no-state", false, "Disable persistent state (ephemeral container)")
	browser := fs.String("browser", "", "Browser display: vnc | cdp | both")
	fs.Parse(args)

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	opts := runner.RunOpts{
		Interactive: true,
		Provider:    *provider,
		Model:       *modelFlag,
		Profile:     *profileName,
		Mount:       *mount,
		NoState:     *noState,
		Browser:     *browser,
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
	fmt.Println("Config (~/.airun/config.env):")
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
  airun --model kimi-k2.5 "prompt"            Run with specific model
  airun shell                                 Interactive Claude Code session
  airun shell -p dev                          Interactive with profile
  airun shell --model kimi-k2.5               Interactive with specific model
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
  airun keys model <model>                    Change default model
  airun proxy init                             Create proxy config
  airun proxy serve                            Start proxy server
  airun proxy serve --port 9090                Start on custom port
  airun proxy user add <name>                   Add user
  airun proxy user list                        List users
  airun proxy user revoke <name>               Revoke user access
  airun proxy user restore <name>              Restore user access
  airun proxy user import <file>               Bulk import users
  airun proxy user export                      Export user tokens
  airun state info                              Show state volume details
  airun state reset                             Reset state (re-seed from image)
  airun init                                  Interactive global setup
  airun rebuild                               Rebuild docker image
  airun rebuild --no-cache                    Rebuild without cache
  airun rebuild --fresh                       Reinstall Claude Code CLI (latest version)
  airun --status                              Show running agents
  airun --check                               Show config and prerequisites
  airun --version                             Show version

Flags:
  -p, --profile    Profile name (loads skills, settings, provider)
  --provider       Provider override: z/zai | m/mm/minimax | k/kimi | r/remote
  -m, --model      Model override (e.g. kimi-k2.5, glm-5.1, MiniMax-M2.7)
  --output         Export workspace to this directory after run
  --no-state       Disable persistent state (ephemeral container)
  --browser        Browser display in container: vnc | cdp | both (maps ports 6080/9222)

Config: ~/.airun/config.env (workspace, API keys, provider, mode)
Profiles: ~/.airun/profiles/*.yaml
Providers: z/zai (Z.AI GLM-5.1) | m/mm/minimax (MiniMax M2.7) | k/kimi (Kimi K2.5) | r/remote (Proxy)
`)
}

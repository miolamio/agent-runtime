package setup

import (
	"bufio"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/miolamio/agent-runtime/internal/keys"
)

func Run() error {
	usr, _ := user.Current()
	home := usr.HomeDir
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Agent Runtime — Setup")
	fmt.Println()

	envFile := filepath.Join(home, ".airun.env")
	isNew := true
	if _, err := os.Stat(envFile); err == nil {
		isNew = false
		fmt.Printf("  Config exists: %s\n", envFile)
		fmt.Print("  Reconfigure keys? [y/N] ")
		answer, _ := reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(answer)) != "y" {
			fmt.Println("  Keeping existing config.")
			goto dirs
		}
	}

	{
		// Write base config if new
		if isNew {
			content := fmt.Sprintf(`# Agent Runtime — Central Configuration
ARUN_WORKSPACE=%s
ARUN_MODE=snapshot
ARUN_PROVIDER=zai
ZAI_API_KEY=
ZAI_BASE_URL=https://api.z.ai/api/anthropic
ZAI_MODEL=glm-4.7
ZAI_HAIKU_MODEL=GLM-4.5-Air
MINIMAX_API_KEY=
MINIMAX_BASE_URL=https://api.minimax.io/anthropic
MINIMAX_MODEL=MiniMax-M2.7
KIMI_API_KEY=
KIMI_BASE_URL=https://api.kimi.com/coding/
KIMI_MODEL=kimi-k2.5
API_TIMEOUT_MS=3000000
CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1
`, filepath.Join(home, "src"))
			os.WriteFile(envFile, []byte(content), 0600)
		}

		// Provider selection
		fmt.Println()
		fmt.Println("  Select providers to configure:")
		fmt.Println()
		allProviders := keys.AllProviders()
		for _, p := range allProviders {
			fmt.Printf("    Configure %s (%s)? [y/N] ", p.Name, p.RegisterURL)
			answer, _ := reader.ReadString('\n')
			if strings.TrimSpace(strings.ToLower(answer)) == "y" {
				keys.Add(envFile, p.ID)
			}
		}

		// Set default provider
		fmt.Println()
		kv, _ := keys.ReadAllEnvKeys(envFile)
		var configured []string
		for _, p := range allProviders {
			if kv[p.EnvKey] != "" {
				configured = append(configured, p.ID)
			}
		}
		if len(configured) > 0 {
			current := kv["ARUN_PROVIDER"]
			if current == "" {
				current = configured[0]
				keys.UpdateEnvKey(envFile, "ARUN_PROVIDER", current)
			}
			if len(configured) > 1 {
				p := keys.ProviderByAlias(current)
				name := current
				if p != nil {
					name = p.Name
				}
				fmt.Printf("  Default provider: %s\n", name)
				fmt.Printf("  Change? [%s/N] ", strings.Join(configured, "/"))
				answer, _ := reader.ReadString('\n')
				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer != "" && answer != "n" {
					keys.SetDefault(envFile, answer)
				}
			}
		}

		fmt.Printf("\n  Config written: %s\n", envFile)
	}

dirs:
	// 2. Directories
	fmt.Println()
	fmt.Println("  Creating directories...")
	dirs := []string{
		filepath.Join(home, "airun-profiles"),
		filepath.Join(home, "airun-skills"),
		filepath.Join(home, "airun-agents"),
		filepath.Join(home, "airun-commands"),
		filepath.Join(home, ".airun", "runs"),
	}
	for _, d := range dirs {
		os.MkdirAll(d, 0755)
		fmt.Printf("    %s\n", d)
	}

	// 3. Copy profile templates
	fmt.Println()
	fmt.Println("  Copying profile templates...")
	srcProfiles := "configs/profiles"
	if entries, err := os.ReadDir(srcProfiles); err == nil {
		dstDir := filepath.Join(home, "airun-profiles")
		for _, e := range entries {
			if filepath.Ext(e.Name()) == ".yaml" {
				src := filepath.Join(srcProfiles, e.Name())
				dst := filepath.Join(dstDir, e.Name())
				if _, err := os.Stat(dst); err != nil {
					data, _ := os.ReadFile(src)
					os.WriteFile(dst, data, 0644)
					fmt.Printf("    %s\n", e.Name())
				}
			}
		}
	} else {
		fmt.Println("    (no profile templates found in configs/profiles/)")
	}

	// 4. Install binary
	fmt.Println()
	binSrc, _ := os.Executable()
	binDst := filepath.Join(home, ".local", "bin", "airun")
	os.MkdirAll(filepath.Dir(binDst), 0755)
	if binSrc != "" && binSrc != binDst {
		data, err := os.ReadFile(binSrc)
		if err == nil {
			os.WriteFile(binDst, data, 0755)
			fmt.Printf("  Installed: %s\n", binDst)
		}
	}

	// 5. Add to PATH
	zshrc := filepath.Join(home, ".zshrc")
	pathLine := `export PATH="$HOME/.local/bin:$PATH"`
	if data, err := os.ReadFile(zshrc); err == nil {
		if !strings.Contains(string(data), ".local/bin") {
			f, _ := os.OpenFile(zshrc, os.O_APPEND|os.O_WRONLY, 0644)
			fmt.Fprintf(f, "\n# Agent Runtime\n%s\n", pathLine)
			f.Close()
			fmt.Printf("  Added to ~/.zshrc: %s\n", pathLine)
		}
	}

	// 6. Test connectivity
	fmt.Println()
	fmt.Println("  Testing provider connectivity...")
	keys.Test(envFile, "")

	// 7. Summary
	fmt.Println()
	fmt.Println("Setup complete!")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  airun keys list               # see configured keys")
	fmt.Println("  airun keys add <provider>      # add more providers")
	fmt.Println("  airun --check                  # verify config")
	fmt.Println("  airun rebuild                  # build docker image")
	fmt.Println("  airun shell -p dev             # start interactive session")

	return nil
}

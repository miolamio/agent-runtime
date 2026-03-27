package setup

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

func Run() error {
	usr, _ := user.Current()
	home := usr.HomeDir
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("AUTOMATICA Agent Runtime — Setup")
	fmt.Println()

	// 1. API Keys
	envFile := filepath.Join(home, ".automatica.env")
	if _, err := os.Stat(envFile); err == nil {
		fmt.Printf("  Config exists: %s\n", envFile)
		fmt.Print("  Overwrite? [y/N] ")
		answer, _ := reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(answer)) != "y" {
			fmt.Println("  Keeping existing config.")
			goto dirs
		}
	}

	{
		fmt.Println()
		fmt.Print("  Z.AI API key (enter to skip): ")
		zaiKey, _ := reader.ReadString('\n')
		zaiKey = strings.TrimSpace(zaiKey)

		fmt.Print("  MiniMax API key (enter to skip): ")
		mmKey, _ := reader.ReadString('\n')
		mmKey = strings.TrimSpace(mmKey)

		provider := "zai"
		if zaiKey == "" && mmKey != "" {
			provider = "minimax"
		}

		content := fmt.Sprintf(`# AUTOMATICA Agent Runtime — Central Configuration
AUTOMATICA_WORKSPACE=%s
AUTOMATICA_MODE=snapshot
AUTOMATICA_PROVIDER=%s
ZAI_API_KEY=%s
ZAI_BASE_URL=https://api.z.ai/api/anthropic
ZAI_MODEL=glm-4.7
ZAI_HAIKU_MODEL=GLM-4.5-Air
MINIMAX_API_KEY=%s
MINIMAX_BASE_URL=https://api.minimax.io/anthropic
MINIMAX_MODEL=MiniMax-M2.7
API_TIMEOUT_MS=3000000
CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1
`, filepath.Join(home, "src"), provider, zaiKey, mmKey)

		os.WriteFile(envFile, []byte(content), 0600)
		fmt.Printf("  Written: %s\n", envFile)
	}

dirs:
	// 2. Directories
	fmt.Println()
	fmt.Println("  Creating directories...")
	dirs := []string{
		filepath.Join(home, "automatica-profiles"),
		filepath.Join(home, "automatica-skills"),
		filepath.Join(home, "automatica-agents"),
		filepath.Join(home, "automatica-commands"),
		filepath.Join(home, ".automatica", "runs"),
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
		dstDir := filepath.Join(home, "automatica-profiles")
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
	binDst := filepath.Join(home, ".local", "bin", "arun")
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
			fmt.Fprintf(f, "\n# AUTOMATICA Agent Runtime\n%s\n", pathLine)
			f.Close()
			fmt.Printf("  Added to ~/.zshrc: %s\n", pathLine)
		}
	}

	// 6. Test connectivity
	fmt.Println()
	fmt.Println("  Testing provider connectivity...")
	testProviders()

	// 7. Summary
	fmt.Println()
	fmt.Println("Setup complete!")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  arun --check              # verify config")
	fmt.Println("  arun rebuild              # build docker image")
	fmt.Println("  arun shell -p dev         # start interactive session")
	fmt.Println("  arun -p dev \"prompt\"      # run agent task")

	return nil
}

func testProviders() {
	// Simple connectivity test via curl
	providers := []struct {
		name string
		url  string
	}{
		{"Z.AI", "https://api.z.ai/api/anthropic"},
		{"MiniMax", "https://api.minimax.io/anthropic"},
	}
	for _, p := range providers {
		cmd := exec.Command("curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "5", p.url)
		out, err := cmd.Output()
		if err == nil && len(out) > 0 {
			fmt.Printf("    %s (%s): %s\n", p.name, p.url, string(out))
		} else {
			fmt.Printf("    %s (%s): unreachable\n", p.name, p.url)
		}
	}
}

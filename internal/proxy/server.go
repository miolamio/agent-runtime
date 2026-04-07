package proxy

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/miolamio/agent-runtime/internal/proxy/students"
)

// Serve starts the proxy HTTP server.
func Serve(configPath, studentsPath, listenOverride string) error {
	cfg, err := LoadProxyConfig(configPath)
	if err != nil {
		return err
	}
	if listenOverride != "" {
		cfg.Listen = listenOverride
	}

	mgr := students.New(studentsPath)
	handler := NewHandler(cfg, mgr)

	// SIGHUP reloads users
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGHUP)
	go func() {
		for range sigs {
			if err := mgr.Load(); err != nil {
				log.Printf("[proxy] reload users error: %v", err)
			} else {
				log.Printf("[proxy] users reloaded (%d total)", len(mgr.List()))
			}
		}
	}()

	models := cfg.AllModels()
	providerCount := len(cfg.Providers)
	userCount := len(mgr.List())
	rpmStr := "unlimited"
	if cfg.RPM > 0 {
		rpmStr = fmt.Sprintf("%d/min per user", cfg.RPM)
	}

	log.Printf("[proxy] Listening on %s", cfg.Listen)
	log.Printf("[proxy] Providers: %d (%d models: %s)", providerCount, len(models), strings.Join(models, ", "))
	log.Printf("[proxy] Users: %d active", userCount)
	log.Printf("[proxy] Rate limit: %s", rpmStr)

	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		log.Printf("[proxy] TLS enabled (cert: %s)", cfg.TLSCert)
		return http.ListenAndServeTLS(cfg.Listen, cfg.TLSCert, cfg.TLSKey, handler)
	}
	return http.ListenAndServe(cfg.Listen, handler)
}

// Init creates proxy.yaml and students.json with defaults.
func Init(configPath, studentsPath string) error {
	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf("%s already exists", configPath)
	}

	template := `# Proxy Configuration
listen: "127.0.0.1:8080"
rpm: 0
user_agent: "claude-cli/2.1.80 (external, cli)"

providers:
  # Uncomment and configure:
  # zai:
  #   base_url: "https://api.z.ai/api/anthropic"
  #   api_key: "YOUR_KEY"
  #   models:
  #     - glm-5.1
  #     - glm-4.7
  #     - GLM-4.5-Air
  # minimax:
  #   base_url: "https://api.minimax.io/anthropic"
  #   api_key: "YOUR_KEY"
  #   models:
  #     - MiniMax-M2.7
  # kimi:
  #   base_url: "https://api.kimi.com/coding/"
  #   api_key: "YOUR_KEY"
  #   models:
  #     - kimi-k2.5

# TLS (optional — uncomment for direct HTTPS without reverse proxy)
# tls_cert: "/path/to/cert.pem"
# tls_key: "/path/to/key.pem"
`

	if err := os.WriteFile(configPath, []byte(template), 0600); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}
	fmt.Printf("  Created: %s\n", configPath)

	if err := os.WriteFile(studentsPath, []byte("[]\n"), 0600); err != nil {
		return fmt.Errorf("write %s: %w", studentsPath, err)
	}
	fmt.Printf("  Created: %s\n", studentsPath)

	fmt.Println()
	fmt.Printf("  Edit %s to add provider API keys.\n", configPath)
	return nil
}

// StudentAdd adds a user and prints the token.
func StudentAdd(studentsPath, name string) error {
	mgr := students.New(studentsPath)
	tok, err := mgr.Add(name)
	if err != nil {
		return err
	}
	fmt.Printf("  %s: %s\n", name, tok)
	return nil
}

// StudentList prints all users.
func StudentList(studentsPath string) error {
	mgr := students.New(studentsPath)
	all := mgr.List()
	if len(all) == 0 {
		fmt.Println("  No users.")
		return nil
	}
	fmt.Printf("\n  %-20s %-10s %s\n", "Name", "Status", "Token")
	fmt.Printf("  %-20s %-10s %s\n", "----", "------", "-----")
	for _, s := range all {
		status := "active"
		if !s.Active {
			status = "revoked"
		}
		masked := s.Token
		if len(masked) >= 14 {
			masked = s.Token[:10] + "..." + s.Token[len(s.Token)-4:]
		}
		fmt.Printf("  %-20s %-10s %s\n", s.Name, status, masked)
	}
	fmt.Println()
	return nil
}

// StudentRevoke deactivates a user.
func StudentRevoke(studentsPath, name string) error {
	mgr := students.New(studentsPath)
	return mgr.Revoke(name)
}

// StudentRestore reactivates a user.
func StudentRestore(studentsPath, name string) error {
	mgr := students.New(studentsPath)
	return mgr.Restore(name)
}

// StudentImport reads names from a file (one per line) and adds them as users.
func StudentImport(studentsPath, listPath string) error {
	data, err := os.ReadFile(listPath)
	if err != nil {
		return err
	}
	mgr := students.New(studentsPath)
	lines := strings.Split(string(data), "\n")
	count := 0
	for _, line := range lines {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		tok, err := mgr.Add(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  warning: %s: %v\n", name, err)
			continue
		}
		fmt.Printf("  %s: %s\n", name, tok)
		count++
	}
	fmt.Printf("\n  Added %d users.\n", count)
	return nil
}

// StudentExport prints all active users with full tokens.
func StudentExport(studentsPath string) error {
	mgr := students.New(studentsPath)
	all := mgr.List()
	for _, s := range all {
		if s.Active {
			fmt.Printf("%s\t%s\n", s.Name, s.Token)
		}
	}
	return nil
}

// DefaultPaths returns default proxy config and students file paths.
func DefaultPaths() (configPath, studentsPath string) {
	home, _ := os.UserHomeDir()
	baseDir := filepath.Join(home, ".airun")

	configPath = filepath.Join(baseDir, "proxy.yaml")
	studentsPath = filepath.Join(baseDir, "students.json")

	// Migration from old paths
	oldConfig := filepath.Join(home, "proxy.yaml")
	oldStudents := filepath.Join(home, "students.json")

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if _, err := os.Stat(oldConfig); err == nil {
			os.MkdirAll(baseDir, 0700)
			if os.Rename(oldConfig, configPath) == nil {
				fmt.Fprintf(os.Stderr, "[airun] migrated %s → %s\n", oldConfig, configPath)
			}
		}
	}
	if _, err := os.Stat(studentsPath); os.IsNotExist(err) {
		if _, err := os.Stat(oldStudents); err == nil {
			os.MkdirAll(baseDir, 0700)
			if os.Rename(oldStudents, studentsPath) == nil {
				fmt.Fprintf(os.Stderr, "[airun] migrated %s → %s\n", oldStudents, studentsPath)
			}
		}
	}

	return configPath, studentsPath
}

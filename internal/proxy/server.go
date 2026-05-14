package proxy

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/miolamio/agent-runtime/internal/proxy/users"
)

// Serve starts the proxy HTTP server.
func Serve(configPath, usersPath, listenOverride string) error {
	cfg, err := LoadProxyConfig(configPath)
	if err != nil {
		return err
	}
	if listenOverride != "" {
		cfg.Listen = listenOverride
	}

	mgr := users.New(usersPath)
	handler := NewHandler(cfg, mgr)

	// SIGHUP reloads users + proxy.yaml (providers, rpm, user_agent).
	// Listen/TLS fields are not hot-swappable — changes are logged and ignored.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGHUP)
	go func() {
		for range sigs {
			if err := mgr.Load(); err != nil {
				log.Printf("[proxy] reload users error: %v", err)
			} else {
				log.Printf("[proxy] users reloaded (%d total)", len(mgr.List()))
			}
			newCfg, err := LoadProxyConfig(configPath)
			if err != nil {
				log.Printf("[proxy] reload config error: %v", err)
				continue
			}
			current := handler.config.Load()
			if newCfg.Listen != current.Listen {
				if listenOverride == "" {
					log.Printf("[proxy] warning: listen address change (%s → %s) requires restart; ignoring",
						current.Listen, newCfg.Listen)
				}
				newCfg.Listen = current.Listen
			}
			if newCfg.TLSCert != current.TLSCert || newCfg.TLSKey != current.TLSKey {
				log.Printf("[proxy] warning: TLS cert/key change requires restart; ignoring")
				newCfg.TLSCert = current.TLSCert
				newCfg.TLSKey = current.TLSKey
			}
			handler.ReloadConfig(newCfg)
			log.Printf("[proxy] config reloaded (%d providers, %d models, rpm=%d)",
				len(newCfg.Providers), len(newCfg.AllModels()), newCfg.RPM)
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

// Init creates proxy.yaml and users.json with defaults.
func Init(configPath, usersPath string) error {
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

	if err := os.WriteFile(usersPath, []byte("[]\n"), 0600); err != nil {
		return fmt.Errorf("write %s: %w", usersPath, err)
	}
	fmt.Printf("  Created: %s\n", usersPath)

	fmt.Println()
	fmt.Printf("  Edit %s to add provider API keys.\n", configPath)
	return nil
}

// UserAdd adds a user and prints the token.
func UserAdd(usersPath, name string) error {
	mgr := users.New(usersPath)
	tok, err := mgr.Add(name)
	if err != nil {
		return err
	}
	fmt.Printf("  %s: %s\n", name, tok)
	return nil
}

// UserList prints all users.
func UserList(usersPath string) error {
	mgr := users.New(usersPath)
	all := mgr.List()
	if len(all) == 0 {
		fmt.Println("  No users.")
		return nil
	}
	fmt.Printf("\n  %-20s %-10s %s\n", "Name", "Status", "Token")
	fmt.Printf("  %-20s %-10s %s\n", "----", "------", "-----")
	for _, u := range all {
		status := "active"
		if !u.Active {
			status = "revoked"
		}
		masked := u.Token
		if len(masked) >= 14 {
			masked = u.Token[:10] + "..." + u.Token[len(u.Token)-4:]
		}
		fmt.Printf("  %-20s %-10s %s\n", u.Name, status, masked)
	}
	fmt.Println()
	return nil
}

// UserRevoke deactivates a user.
func UserRevoke(usersPath, name string) error {
	mgr := users.New(usersPath)
	return mgr.Revoke(name)
}

// UserRestore reactivates a user.
func UserRestore(usersPath, name string) error {
	mgr := users.New(usersPath)
	return mgr.Restore(name)
}

// UserImport reads names from a file (one per line) and adds them as users.
func UserImport(usersPath, listPath string) error {
	data, err := os.ReadFile(listPath)
	if err != nil {
		return err
	}
	mgr := users.New(usersPath)
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

// UserExport prints all active users with full tokens.
func UserExport(usersPath string) error {
	mgr := users.New(usersPath)
	all := mgr.List()
	for _, u := range all {
		if u.Active {
			fmt.Printf("%s\t%s\n", u.Name, u.Token)
		}
	}
	return nil
}

// DefaultPaths returns default proxy config and users file paths, performing
// a best-effort one-shot migration of legacy locations:
//   - ~/proxy.yaml     → ~/.airun/proxy.yaml
//   - ~/students.json  → ~/.airun/users.json (pre-v0.7.0 layout)
//   - ~/.airun/students.json → ~/.airun/users.json (v0.6.x → v0.8.0 rename)
func DefaultPaths() (configPath, usersPath string) {
	home, _ := os.UserHomeDir()
	baseDir := filepath.Join(home, ".airun")

	configPath = filepath.Join(baseDir, "proxy.yaml")
	usersPath = filepath.Join(baseDir, "users.json")

	// Migration from old paths
	oldConfig := filepath.Join(home, "proxy.yaml")
	oldUsersPreAirun := filepath.Join(home, "students.json")
	oldUsersInAirun := filepath.Join(baseDir, "students.json")

	if _, err := os.Stat(configPath); errors.Is(err, fs.ErrNotExist) {
		if _, err := os.Stat(oldConfig); err == nil {
			_ = os.MkdirAll(baseDir, 0700) // best-effort; Rename surfaces the real failure
			if os.Rename(oldConfig, configPath) == nil {
				fmt.Fprintf(os.Stderr, "[airun] migrated %s → %s\n", oldConfig, configPath)
			}
		}
	}
	if _, err := os.Stat(usersPath); errors.Is(err, fs.ErrNotExist) {
		// Prefer the in-airun rename (v0.6.x → v0.8.0) over the home-dir one.
		if _, err := os.Stat(oldUsersInAirun); err == nil {
			_ = os.MkdirAll(baseDir, 0700)
			if os.Rename(oldUsersInAirun, usersPath) == nil {
				fmt.Fprintf(os.Stderr, "[airun] migrated %s → %s\n", oldUsersInAirun, usersPath)
			}
		} else if _, err := os.Stat(oldUsersPreAirun); err == nil {
			_ = os.MkdirAll(baseDir, 0700)
			if os.Rename(oldUsersPreAirun, usersPath) == nil {
				fmt.Fprintf(os.Stderr, "[airun] migrated %s → %s\n", oldUsersPreAirun, usersPath)
			}
		}
	}

	return configPath, usersPath
}

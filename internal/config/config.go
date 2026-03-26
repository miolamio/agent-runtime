package config

import (
	"bufio"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

type Config struct {
	// Paths
	Home      string
	EnvFile   string
	SkillsDir string
	AgentsDir string

	// From .automatica.env
	Workspace string // AUTOMATICA_WORKSPACE
	Mode      string // AUTOMATICA_MODE (snapshot|bind)
	Provider  string // AUTOMATICA_PROVIDER (zai|minimax)

	// Z.AI
	ZaiAPIKey    string
	ZaiBaseURL   string
	ZaiModel     string
	ZaiHaikuModel string

	// MiniMax
	MinimaxAPIKey  string
	MinimaxBaseURL string
	MinimaxModel   string

	// Container
	APITimeout    string
	DisableTraffic string
}

func Load() (*Config, error) {
	usr, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}
	home := usr.HomeDir
	envFile := filepath.Join(home, ".automatica.env")

	cfg := &Config{
		Home:      home,
		EnvFile:   envFile,
		SkillsDir: filepath.Join(home, "automatica-skills"),
		AgentsDir: filepath.Join(home, "automatica-agents"),
		// Defaults
		Workspace:      filepath.Join(home, "src"),
		Mode:           "snapshot",
		Provider:       "zai",
		ZaiBaseURL:     "https://api.z.ai/api/anthropic",
		ZaiModel:       "glm-4.7",
		ZaiHaikuModel:  "GLM-4.5-Air",
		MinimaxBaseURL: "https://api.minimax.io/anthropic",
		MinimaxModel:   "MiniMax-M2.7",
		APITimeout:     "3000000",
		DisableTraffic: "1",
	}

	if err := cfg.loadEnvFile(envFile); err != nil {
		// Not fatal — use defaults
		fmt.Fprintf(os.Stderr, "[arun] warning: %v (using defaults)\n", err)
	}

	return cfg, nil
}

func (c *Config) loadEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "AUTOMATICA_WORKSPACE":
			c.Workspace = val
		case "AUTOMATICA_MODE":
			c.Mode = val
		case "AUTOMATICA_PROVIDER":
			c.Provider = val
		case "ZAI_API_KEY":
			c.ZaiAPIKey = val
		case "ZAI_BASE_URL":
			c.ZaiBaseURL = val
		case "ZAI_MODEL":
			c.ZaiModel = val
		case "ZAI_HAIKU_MODEL":
			c.ZaiHaikuModel = val
		case "MINIMAX_API_KEY":
			c.MinimaxAPIKey = val
		case "MINIMAX_BASE_URL":
			c.MinimaxBaseURL = val
		case "MINIMAX_MODEL":
			c.MinimaxModel = val
		case "API_TIMEOUT_MS":
			c.APITimeout = val
		case "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC":
			c.DisableTraffic = val
		}
	}
	return scanner.Err()
}

// ActiveBaseURL returns the API base URL for the active provider.
func (c *Config) ActiveBaseURL() string {
	if c.Provider == "minimax" {
		return c.MinimaxBaseURL
	}
	return c.ZaiBaseURL
}

// ActiveAPIKey returns the API key for the active provider.
func (c *Config) ActiveAPIKey() string {
	if c.Provider == "minimax" {
		return c.MinimaxAPIKey
	}
	return c.ZaiAPIKey
}

// ActiveModel returns the default model for the active provider.
func (c *Config) ActiveModel() string {
	if c.Provider == "minimax" {
		return c.MinimaxModel
	}
	return c.ZaiModel
}

// ContainerEnv returns env vars to pass into the container.
func (c *Config) ContainerEnv(provider string) []string {
	if provider == "" {
		provider = c.Provider
	}
	var baseURL, apiKey, model string
	switch provider {
	case "minimax":
		baseURL = c.MinimaxBaseURL
		apiKey = c.MinimaxAPIKey
		model = c.MinimaxModel
	default: // zai
		baseURL = c.ZaiBaseURL
		apiKey = c.ZaiAPIKey
		model = c.ZaiModel
	}
	return []string{
		"ANTHROPIC_BASE_URL=" + baseURL,
		"ANTHROPIC_AUTH_TOKEN=" + apiKey,
		"ANTHROPIC_DEFAULT_SONNET_MODEL=" + model,
		"API_TIMEOUT_MS=" + c.APITimeout,
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=" + c.DisableTraffic,
	}
}

func (c *Config) Show() string {
	masked := func(key string) string {
		if len(key) < 8 {
			return "***"
		}
		return key[:4] + "..." + key[len(key)-4:]
	}
	return fmt.Sprintf(`  Workspace:  %s
  Mode:       %s
  Provider:   %s
  Z.AI:       %s (key: %s)
  MiniMax:    %s (key: %s)
  Timeout:    %s ms`,
		c.Workspace, c.Mode, c.Provider,
		c.ZaiModel, masked(c.ZaiAPIKey),
		c.MinimaxModel, masked(c.MinimaxAPIKey),
		c.APITimeout)
}

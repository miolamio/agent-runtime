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

	// From config.env
	Workspace string // ARUN_WORKSPACE
	Mode      string // ARUN_MODE (snapshot|bind)
	Provider  string // ARUN_PROVIDER (zai|minimax)

	// Z.AI
	ZaiAPIKey    string
	ZaiBaseURL   string
	ZaiModel     string
	ZaiHaikuModel string

	// MiniMax
	MinimaxAPIKey  string
	MinimaxBaseURL string
	MinimaxModel   string

	// Kimi
	KimiAPIKey  string
	KimiBaseURL string
	KimiModel   string

	// Anthropic (direct)
	AnthropicAPIKey  string
	AnthropicBaseURL string
	AnthropicModel   string

	// Remote proxy
	RemoteBaseURL      string
	RemoteAPIKey       string
	RemoteModels       string // comma-separated
	RemoteDefaultModel string

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
	baseDir := filepath.Join(home, ".airun")
	envFile := filepath.Join(baseDir, "config.env")

	// Migration: move old ~/.airun.env to new location
	oldEnvFile := filepath.Join(home, ".airun.env")
	migrateFile(oldEnvFile, envFile)

	cfg := &Config{
		Home:      home,
		EnvFile:   envFile,
		SkillsDir: filepath.Join(baseDir, "skills"),
		AgentsDir: filepath.Join(baseDir, "agents"),
		// Defaults
		Workspace:      filepath.Join(home, "src"),
		Mode:           "snapshot",
		Provider:       "zai",
		ZaiBaseURL:     "https://api.z.ai/api/anthropic",
		ZaiModel:       "glm-5.1",
		ZaiHaikuModel:  "GLM-4.5-Air",
		MinimaxBaseURL: "https://api.minimax.io/anthropic",
		MinimaxModel:   "MiniMax-M2.7",
		KimiBaseURL:      "https://api.kimi.com/coding/",
		KimiModel:        "kimi-k2.5",
		AnthropicBaseURL: "https://api.anthropic.com",
		AnthropicModel:   "claude-sonnet-4-6-20250514",
		APITimeout:     "3000000",
		DisableTraffic: "1",
	}

	// Migration: move old dirs to new locations
	migrateDir(filepath.Join(home, "airun-skills"), cfg.SkillsDir)
	migrateDir(filepath.Join(home, "airun-agents"), cfg.AgentsDir)

	if err := cfg.loadEnvFile(envFile); err != nil {
		// Not fatal — use defaults
		fmt.Fprintf(os.Stderr, "[airun] warning: %v (using defaults)\n", err)
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
		case "ARUN_WORKSPACE":
			c.Workspace = val
		case "ARUN_MODE":
			c.Mode = val
		case "ARUN_PROVIDER":
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
		case "KIMI_API_KEY":
			c.KimiAPIKey = val
		case "KIMI_BASE_URL":
			c.KimiBaseURL = val
		case "KIMI_MODEL":
			c.KimiModel = val
		case "ANTHROPIC_API_KEY":
			c.AnthropicAPIKey = val
		case "ANTHROPIC_BASE_URL_DIRECT":
			c.AnthropicBaseURL = val
		case "ANTHROPIC_MODEL":
			c.AnthropicModel = val
		case "REMOTE_BASE_URL":
			c.RemoteBaseURL = val
		case "REMOTE_API_KEY":
			c.RemoteAPIKey = val
		case "REMOTE_MODELS":
			c.RemoteModels = val
		case "REMOTE_DEFAULT_MODEL":
			c.RemoteDefaultModel = val
		case "API_TIMEOUT_MS":
			c.APITimeout = val
		case "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC":
			c.DisableTraffic = val
		}
	}
	return scanner.Err()
}

// NormalizeProvider resolves aliases: z/zai → zai, m/mm/minimax → minimax, k/kimi → kimi.
func NormalizeProvider(p string) string {
	switch strings.ToLower(p) {
	case "m", "mm", "minimax":
		return "minimax"
	case "k", "kimi":
		return "kimi"
	case "a", "anthropic":
		return "anthropic"
	case "r", "remote":
		return "remote"
	case "z", "zai", "":
		return "zai"
	default:
		return p
	}
}

// ContainerEnvWithModel returns env vars with optional model override.
func (c *Config) ContainerEnvWithModel(provider, modelOverride string) []string {
	if provider == "" {
		provider = c.Provider
	}
	provider = NormalizeProvider(provider)
	var baseURL, apiKey, model string
	switch provider {
	case "minimax":
		baseURL = c.MinimaxBaseURL
		apiKey = c.MinimaxAPIKey
		model = c.MinimaxModel
	case "kimi":
		baseURL = c.KimiBaseURL
		apiKey = c.KimiAPIKey
		model = c.KimiModel
	case "anthropic":
		baseURL = c.AnthropicBaseURL
		apiKey = c.AnthropicAPIKey
		model = c.AnthropicModel
	case "remote":
		baseURL = c.RemoteBaseURL
		apiKey = c.RemoteAPIKey
		model = c.RemoteDefaultModel
	default:
		baseURL = c.ZaiBaseURL
		apiKey = c.ZaiAPIKey
		model = c.ZaiModel
	}
	if modelOverride != "" {
		model = modelOverride
	}
	env := []string{
		"ANTHROPIC_BASE_URL=" + baseURL,
		"ANTHROPIC_AUTH_TOKEN=" + apiKey,
		"ANTHROPIC_DEFAULT_SONNET_MODEL=" + model,
		"API_TIMEOUT_MS=" + c.APITimeout,
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=" + c.DisableTraffic,
	}
	if provider == "kimi" {
		env = append(env, "ENABLE_TOOL_SEARCH=false")
	}
	return env
}

// ContainerEnv returns env vars to pass into the container (no model override).
func (c *Config) ContainerEnv(provider string) []string {
	return c.ContainerEnvWithModel(provider, "")
}

func (c *Config) Show() string {
	masked := func(key string) string {
		if len(key) < 8 {
			return "***"
		}
		return key[:4] + "..." + key[len(key)-4:]
	}
	remoteDisplay := c.RemoteDefaultModel
	if remoteDisplay == "" {
		remoteDisplay = "(not configured)"
	}
	return fmt.Sprintf(`  Workspace:  %s
  Mode:       %s
  Provider:   %s
  Z.AI:       %s (key: %s)
  MiniMax:    %s (key: %s)
  Kimi:       %s (key: %s)
  Anthropic:  %s (key: %s)
  Remote:     %s (key: %s)
  Timeout:    %s ms`,
		c.Workspace, c.Mode, c.Provider,
		c.ZaiModel, masked(c.ZaiAPIKey),
		c.MinimaxModel, masked(c.MinimaxAPIKey),
		c.KimiModel, masked(c.KimiAPIKey),
		c.AnthropicModel, masked(c.AnthropicAPIKey),
		remoteDisplay, masked(c.RemoteAPIKey),
		c.APITimeout)
}

// migrateFile moves a file from old to new path if old exists and new doesn't.
func migrateFile(oldPath, newPath string) {
	if _, err := os.Stat(newPath); err == nil {
		return // new path already exists
	}
	if _, err := os.Stat(oldPath); err != nil {
		return // old path doesn't exist
	}
	os.MkdirAll(filepath.Dir(newPath), 0700)
	if err := os.Rename(oldPath, newPath); err == nil {
		fmt.Fprintf(os.Stderr, "[airun] migrated %s → %s\n", oldPath, newPath)
	}
}

// migrateDir moves a directory from old to new path if old exists and new doesn't.
func migrateDir(oldPath, newPath string) {
	if _, err := os.Stat(newPath); err == nil {
		return
	}
	info, err := os.Stat(oldPath)
	if err != nil || !info.IsDir() {
		return
	}
	os.MkdirAll(filepath.Dir(newPath), 0700)
	if err := os.Rename(oldPath, newPath); err == nil {
		fmt.Fprintf(os.Stderr, "[airun] migrated %s → %s\n", oldPath, newPath)
	}
}

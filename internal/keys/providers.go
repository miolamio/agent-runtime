package keys

import "strings"

// Provider holds metadata for a model provider: identity, registration
// instructions, API defaults, and the env-var names used in ~/.airun/config.env.
type Provider struct {
	ID          string
	Name        string
	RegisterURL string
	Steps       []string
	BaseURL     string
	Model       string
	EnvKey      string
	EnvBaseURL  string
	EnvModel    string
}

var providers = []Provider{
	{
		ID:          "zai",
		Name:        "Z.AI",
		RegisterURL: "https://z.ai",
		Steps: []string{
			"Go to https://z.ai",
			"Sign up / Sign in",
			"Navigate to API Keys → Create new key",
			"Copy the key",
		},
		BaseURL:    "https://api.z.ai/api/anthropic",
		Model:      "glm-5.1",
		EnvKey:     "ZAI_API_KEY",
		EnvBaseURL: "ZAI_BASE_URL",
		EnvModel:   "ZAI_MODEL",
	},
	{
		ID:          "minimax",
		Name:        "MiniMax",
		RegisterURL: "https://minimax.io",
		Steps: []string{
			"Go to https://minimax.io",
			"Sign up / Sign in",
			"Navigate to API Keys → Create new key",
			"Copy the key",
		},
		BaseURL:    "https://api.minimax.io/anthropic",
		Model:      "MiniMax-M2.7",
		EnvKey:     "MINIMAX_API_KEY",
		EnvBaseURL: "MINIMAX_BASE_URL",
		EnvModel:   "MINIMAX_MODEL",
	},
	{
		ID:          "kimi",
		Name:        "Kimi",
		RegisterURL: "https://platform.moonshot.ai",
		Steps: []string{
			"Go to https://platform.moonshot.ai",
			"Sign up / Sign in",
			"Navigate to API Keys → Create new key",
			"Copy the key (starts with sk-)",
		},
		BaseURL:    "https://api.kimi.com/coding/",
		Model:      "kimi-k2.5",
		EnvKey:     "KIMI_API_KEY",
		EnvBaseURL: "KIMI_BASE_URL",
		EnvModel:   "KIMI_MODEL",
	},
	{
		ID:          "anthropic",
		Name:        "Anthropic",
		RegisterURL: "https://console.anthropic.com",
		Steps: []string{
			"Go to https://console.anthropic.com",
			"Sign up / Sign in",
			"Navigate to API Keys → Create Key",
			"Copy the key (starts with sk-ant-)",
		},
		BaseURL:    "https://api.anthropic.com",
		Model:      "claude-sonnet-4-6-20250514",
		EnvKey:     "ANTHROPIC_API_KEY",
		EnvBaseURL: "ANTHROPIC_BASE_URL_DIRECT",
		EnvModel:   "ANTHROPIC_MODEL",
	},
	{
		ID:          "remote",
		Name:        "Remote",
		RegisterURL: "",
		Steps: []string{
			"Get proxy URL and API key from your admin",
		},
		BaseURL:    "",
		Model:      "",
		EnvKey:     "REMOTE_API_KEY",
		EnvBaseURL: "REMOTE_BASE_URL",
		EnvModel:   "REMOTE_DEFAULT_MODEL",
	},
}

// AllProviders returns every registered provider.
func AllProviders() []Provider {
	result := make([]Provider, len(providers))
	copy(result, providers)
	return result
}

// ProviderByAlias resolves a short alias (e.g. "z", "mm", "k") or full ID
// to the corresponding Provider. Returns nil if no match is found.
func ProviderByAlias(alias string) *Provider {
	var idx int
	switch strings.ToLower(alias) {
	case "z", "zai":
		idx = 0
	case "m", "mm", "minimax":
		idx = 1
	case "k", "kimi":
		idx = 2
	case "a", "anthropic":
		idx = 3
	case "r", "remote":
		idx = 4
	default:
		return nil
	}
	p := providers[idx]
	return &p
}

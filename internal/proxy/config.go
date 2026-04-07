// internal/proxy/config.go
package proxy

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const defaultUserAgent = "claude-cli/2.1.80 (external, cli)"

type ProviderEntry struct {
	BaseURL string   `yaml:"base_url"`
	APIKey  string   `yaml:"api_key"`
	Models  []string `yaml:"models"`
}

type ProxyConfig struct {
	Listen    string                   `yaml:"listen"`
	RPM       int                      `yaml:"rpm"`
	UserAgent string                   `yaml:"user_agent"`
	Providers map[string]ProviderEntry `yaml:"providers"`
}

func LoadProxyConfig(path string) (*ProxyConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read proxy config: %w", err)
	}
	cfg := &ProxyConfig{Listen: "127.0.0.1:8080", UserAgent: defaultUserAgent}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse proxy config: %w", err)
	}
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:8080"
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = defaultUserAgent
	}
	return cfg, nil
}

func (c *ProxyConfig) ResolveModel(model string) (*ProviderEntry, bool) {
	for _, p := range c.Providers {
		for _, m := range p.Models {
			if m == model {
				return &ProviderEntry{BaseURL: p.BaseURL, APIKey: p.APIKey, Models: p.Models}, true
			}
		}
	}
	return nil, false
}

func (c *ProxyConfig) AllModels() []string {
	var models []string
	for _, p := range c.Providers {
		models = append(models, p.Models...)
	}
	return models
}

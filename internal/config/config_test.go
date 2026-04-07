package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeProvider(t *testing.T) {
	tests := []struct{ input, want string }{
		{"z", "zai"}, {"zai", "zai"}, {"", "zai"},
		{"m", "minimax"}, {"mm", "minimax"}, {"minimax", "minimax"},
		{"k", "kimi"}, {"kimi", "kimi"},
		{"a", "anthropic"}, {"anthropic", "anthropic"},
		{"r", "remote"}, {"remote", "remote"},
		{"Z", "zai"}, {"ZAI", "zai"}, {"MINIMAX", "minimax"},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		if got := NormalizeProvider(tt.input); got != tt.want {
			t.Errorf("NormalizeProvider(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestContainerEnvWithModel_ZAI(t *testing.T) {
	cfg := &Config{
		ZaiBaseURL: "https://api.z.ai/api/anthropic", ZaiAPIKey: "sk-test",
		ZaiModel: "glm-5.1", APITimeout: "3000000", DisableTraffic: "1",
	}
	env := cfg.ContainerEnvWithModel("zai", "")
	assertEnv(t, env, "ANTHROPIC_BASE_URL=https://api.z.ai/api/anthropic")
	assertEnv(t, env, "ANTHROPIC_AUTH_TOKEN=sk-test")
	assertEnv(t, env, "ANTHROPIC_DEFAULT_SONNET_MODEL=glm-5.1")
}

func TestContainerEnvWithModel_Kimi(t *testing.T) {
	cfg := &Config{
		KimiBaseURL: "https://api.kimi.com/coding/", KimiAPIKey: "sk-kimi",
		KimiModel: "kimi-k2.5", APITimeout: "3000000", DisableTraffic: "1",
	}
	env := cfg.ContainerEnvWithModel("kimi", "")
	assertEnv(t, env, "ENABLE_TOOL_SEARCH=false")
}

func TestContainerEnvWithModel_Anthropic(t *testing.T) {
	cfg := &Config{
		AnthropicBaseURL: "https://api.anthropic.com", AnthropicAPIKey: "sk-ant-test",
		AnthropicModel: "claude-sonnet-4-6-20250514", APITimeout: "3000000", DisableTraffic: "1",
	}
	env := cfg.ContainerEnvWithModel("anthropic", "")
	assertEnv(t, env, "ANTHROPIC_BASE_URL=https://api.anthropic.com")
	assertEnv(t, env, "ANTHROPIC_AUTH_TOKEN=sk-ant-test")
}

func TestContainerEnvWithModel_Override(t *testing.T) {
	cfg := &Config{
		ZaiBaseURL: "https://api.z.ai/api/anthropic", ZaiAPIKey: "sk-test",
		ZaiModel: "glm-5.1", APITimeout: "3000000", DisableTraffic: "1",
	}
	env := cfg.ContainerEnvWithModel("zai", "glm-4.7")
	assertEnv(t, env, "ANTHROPIC_DEFAULT_SONNET_MODEL=glm-4.7")
}

func TestLoadEnvFile(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "config.env")
	os.WriteFile(envFile, []byte("ARUN_WORKSPACE=/tmp/ws\nARUN_MODE=bind\nARUN_PROVIDER=minimax\nZAI_API_KEY=sk-zai\n"), 0600)
	cfg := &Config{}
	if err := cfg.loadEnvFile(envFile); err != nil {
		t.Fatalf("loadEnvFile: %v", err)
	}
	if cfg.Workspace != "/tmp/ws" {
		t.Errorf("Workspace = %q", cfg.Workspace)
	}
	if cfg.Mode != "bind" {
		t.Errorf("Mode = %q", cfg.Mode)
	}
	if cfg.Provider != "minimax" {
		t.Errorf("Provider = %q", cfg.Provider)
	}
	if cfg.ZaiAPIKey != "sk-zai" {
		t.Errorf("ZaiAPIKey = %q", cfg.ZaiAPIKey)
	}
}

func TestLoadEnvFile_EqualsInValue(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "config.env")
	os.WriteFile(f, []byte("ZAI_BASE_URL=https://api.z.ai/api?foo=bar\n"), 0600)
	cfg := &Config{}
	cfg.loadEnvFile(f)
	if cfg.ZaiBaseURL != "https://api.z.ai/api?foo=bar" {
		t.Errorf("URL with = not parsed correctly: %q", cfg.ZaiBaseURL)
	}
}

func assertEnv(t *testing.T, env []string, want string) {
	t.Helper()
	for _, e := range env {
		if e == want {
			return
		}
	}
	t.Errorf("env missing %q\ngot: %s", want, strings.Join(env, "\n"))
}

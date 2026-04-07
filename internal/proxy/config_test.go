// internal/proxy/config_test.go
package proxy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProxyConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy.yaml")
	os.WriteFile(path, []byte(`
listen: ":9090"
rpm: 10
user_agent: "test-agent/1.0"
providers:
  zai:
    base_url: "https://api.z.ai/api/anthropic"
    api_key: "test-key-zai"
    models:
      - glm-4.7
      - GLM-4.5-Air
  minimax:
    base_url: "https://api.minimax.io/anthropic"
    api_key: "test-key-mm"
    models:
      - MiniMax-M2.7
`), 0600)

	cfg, err := LoadProxyConfig(path)
	if err != nil {
		t.Fatalf("LoadProxyConfig error: %v", err)
	}
	if cfg.Listen != ":9090" {
		t.Errorf("Listen = %q, want :9090", cfg.Listen)
	}
	if cfg.RPM != 10 {
		t.Errorf("RPM = %d, want 10", cfg.RPM)
	}
	if cfg.UserAgent != "test-agent/1.0" {
		t.Errorf("UserAgent = %q", cfg.UserAgent)
	}
	if len(cfg.Providers) != 2 {
		t.Fatalf("Providers count = %d, want 2", len(cfg.Providers))
	}
	zai := cfg.Providers["zai"]
	if zai.APIKey != "test-key-zai" || len(zai.Models) != 2 {
		t.Errorf("zai: key=%q models=%v", zai.APIKey, zai.Models)
	}
}

func TestLoadProxyConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy.yaml")
	os.WriteFile(path, []byte("providers: {}\n"), 0600)
	cfg, err := LoadProxyConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:8080" {
		t.Errorf("default Listen = %q, want 127.0.0.1:8080", cfg.Listen)
	}
	if cfg.RPM != 0 {
		t.Errorf("default RPM = %d, want 0", cfg.RPM)
	}
	if cfg.UserAgent == "" {
		t.Error("default UserAgent should not be empty")
	}
}

func TestResolveModel(t *testing.T) {
	cfg := &ProxyConfig{
		Providers: map[string]ProviderEntry{
			"zai":     {BaseURL: "https://z.ai", APIKey: "k1", Models: []string{"glm-4.7"}},
			"minimax": {BaseURL: "https://mm.io", APIKey: "k2", Models: []string{"MiniMax-M2.7"}},
		},
	}
	p, ok := cfg.ResolveModel("glm-4.7")
	if !ok {
		t.Fatal("ResolveModel should find glm-4.7")
	}
	if p.APIKey != "k1" {
		t.Errorf("wrong provider for glm-4.7: key=%q", p.APIKey)
	}
	_, ok = cfg.ResolveModel("unknown-model")
	if ok {
		t.Error("ResolveModel should not find unknown model")
	}
}

func TestLoadProxyConfig_TLS(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy.yaml")
	os.WriteFile(path, []byte(`listen: ":8443"
tls_cert: "/etc/ssl/cert.pem"
tls_key: "/etc/ssl/key.pem"
providers: {}
`), 0600)
	cfg, err := LoadProxyConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TLSCert != "/etc/ssl/cert.pem" {
		t.Errorf("TLSCert = %q", cfg.TLSCert)
	}
	if cfg.TLSKey != "/etc/ssl/key.pem" {
		t.Errorf("TLSKey = %q", cfg.TLSKey)
	}
}

func TestAllModels(t *testing.T) {
	cfg := &ProxyConfig{
		Providers: map[string]ProviderEntry{
			"zai":     {Models: []string{"glm-4.7", "GLM-4.5-Air"}},
			"minimax": {Models: []string{"MiniMax-M2.7"}},
		},
	}
	models := cfg.AllModels()
	if len(models) != 3 {
		t.Errorf("AllModels count = %d, want 3", len(models))
	}
}

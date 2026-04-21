package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/miolamio/agent-runtime/internal/proxy/students"
)

func modelIDs(t *testing.T, h *Handler, tok string) []string {
	t.Helper()
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("x-api-key", tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("models: status = %d", rec.Code)
	}
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	ids := make([]string, len(resp.Data))
	for i, d := range resp.Data {
		ids[i] = d.ID
	}
	sort.Strings(ids)
	return ids
}

func TestReloadConfig_SwapsProviders(t *testing.T) {
	dir := t.TempDir()
	mgr := students.New(filepath.Join(dir, "students.json"))
	tok, _ := mgr.Add("U")

	oldCfg := &ProxyConfig{
		Listen:    ":8080",
		UserAgent: "old/1.0",
		Providers: map[string]ProviderEntry{
			"zai": {BaseURL: "http://a", APIKey: "k1", Models: []string{"m-old"}},
		},
	}
	h := NewHandler(oldCfg, mgr)

	if got := modelIDs(t, h, tok); len(got) != 1 || got[0] != "m-old" {
		t.Fatalf("pre-reload models = %v, want [m-old]", got)
	}

	newCfg := &ProxyConfig{
		Listen:    ":8080",
		UserAgent: "new/2.0",
		Providers: map[string]ProviderEntry{
			"zai": {BaseURL: "http://b", APIKey: "k2", Models: []string{"m-new-1", "m-new-2"}},
		},
	}
	h.ReloadConfig(newCfg)

	got := modelIDs(t, h, tok)
	if strings.Join(got, ",") != "m-new-1,m-new-2" {
		t.Errorf("post-reload models = %v, want [m-new-1 m-new-2]", got)
	}
	if cfg := h.config.Load(); cfg.UserAgent != "new/2.0" {
		t.Errorf("UserAgent not swapped: %q", cfg.UserAgent)
	}
}

func TestReloadConfig_SwapsRPM(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg","type":"message","model":"m1","content":[{"type":"text","text":"ok"}]}`))
	}))
	defer upstream.Close()

	dir := t.TempDir()
	mgr := students.New(filepath.Join(dir, "students.json"))
	tok, _ := mgr.Add("U")

	cfg := &ProxyConfig{
		RPM:       0, // unlimited
		UserAgent: "t",
		Providers: map[string]ProviderEntry{
			"p": {BaseURL: upstream.URL, APIKey: "k", Models: []string{"m1"}},
		},
	}
	h := NewHandler(cfg, mgr)
	body := `{"model":"m1","messages":[{"role":"user","content":"hi"}]}`

	// Burst 5 under unlimited — all succeed (and do NOT leave marks in the
	// bucket: rpm=0 short-circuits before touching the map).
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
		req.Header.Set("x-api-key", tok)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("unlimited burst %d: %d", i, rec.Code)
		}
	}

	// Reload to rpm=1: first request fills the bucket, second must be denied.
	h.ReloadConfig(&ProxyConfig{
		RPM:       1,
		UserAgent: "t",
		Providers: cfg.Providers,
	})
	req1 := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req1.Header.Set("x-api-key", tok)
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req1)
	if rec1.Code != 200 {
		t.Fatalf("rpm=1 req1: status = %d, want 200", rec1.Code)
	}
	req2 := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req2.Header.Set("x-api-key", tok)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("rpm=1 req2: status = %d, want 429", rec2.Code)
	}

	// Loosen back to unlimited — further requests pass.
	h.ReloadConfig(&ProxyConfig{
		RPM:       0,
		UserAgent: "t",
		Providers: cfg.Providers,
	})
	req3 := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req3.Header.Set("x-api-key", tok)
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Errorf("loosened rpm=0: status = %d, want 200", rec3.Code)
	}
}

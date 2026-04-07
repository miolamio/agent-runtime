// internal/proxy/handler_test.go
package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miolamio/agent-runtime/internal/proxy/students"
)

func testSetup(t *testing.T) (*Handler, string) {
	t.Helper()
	dir := t.TempDir()
	studentsPath := filepath.Join(dir, "students.json")
	mgr := students.New(studentsPath)
	tok, _ := mgr.Add("TestUser")

	cfg := &ProxyConfig{
		Listen:    ":8080",
		RPM:       0,
		UserAgent: "test/1.0",
		Providers: map[string]ProviderEntry{
			"zai": {BaseURL: "http://fake", APIKey: "k", Models: []string{"glm-4.7"}},
		},
	}
	h := NewHandler(cfg, mgr)
	return h, tok
}

func TestModelsEndpoint(t *testing.T) {
	h, tok := testSetup(t)
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("x-api-key", tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK { t.Fatalf("status = %d", rec.Code) }
	var resp struct {
		Data []struct { ID string `json:"id"` } `json:"data"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if len(resp.Data) != 1 || resp.Data[0].ID != "glm-4.7" { t.Errorf("unexpected models: %+v", resp) }
}

func TestAuthRequired(t *testing.T) {
	h, _ := testSetup(t)
	req := httptest.NewRequest("GET", "/v1/models", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized { t.Errorf("no auth: status = %d, want 401", rec.Code) }
}

func TestAuthInvalidToken(t *testing.T) {
	h, _ := testSetup(t)
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("x-api-key", "sk-ai-invalid")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized { t.Errorf("invalid token: status = %d, want 401", rec.Code) }
}

func TestAuthBearerToken(t *testing.T) {
	h, tok := testSetup(t)
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK { t.Errorf("bearer auth: status = %d, want 200", rec.Code) }
}

func TestMessagesUnknownModel(t *testing.T) {
	h, tok := testSetup(t)
	body := `{"model":"unknown","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req.Header.Set("x-api-key", tok)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest { t.Errorf("unknown model: status = %d, want 400", rec.Code) }
}

func TestMessagesBodyTooLarge(t *testing.T) {
	cfg := &ProxyConfig{
		Listen:    ":0",
		UserAgent: "test",
		Providers: map[string]ProviderEntry{
			"test": {BaseURL: "http://localhost", APIKey: "key", Models: []string{"m1"}},
		},
	}
	dir := t.TempDir()
	sp := filepath.Join(dir, "students.json")
	os.WriteFile(sp, []byte("[]"), 0600)
	mgr := students.New(sp)
	tok, _ := mgr.Add("BigUser")

	h := NewHandler(cfg, mgr)

	body := make([]byte, 11<<20) // 11 MB
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("x-api-key", tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
}

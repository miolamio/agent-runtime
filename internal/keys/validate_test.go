package keys

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateKeySuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":    "msg_test",
			"type":  "message",
			"model": "test-model",
			"content": []map[string]string{
				{"type": "text", "text": "hi"},
			},
		})
	}))
	defer srv.Close()

	result, err := ValidateKey(srv.URL, "test-key", "test-model")
	if err != nil {
		t.Fatalf("ValidateKey failed: %v", err)
	}
	if !result.Valid {
		t.Error("expected Valid=true")
	}
	if result.Model != "test-model" {
		t.Errorf("Model = %q, want %q", result.Model, "test-model")
	}
}

func TestValidateKeyInvalid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"message": "invalid api key"},
		})
	}))
	defer srv.Close()

	result, err := ValidateKey(srv.URL, "bad-key", "model")
	if err != nil {
		t.Fatalf("ValidateKey returned error: %v", err)
	}
	if result.Valid {
		t.Error("expected Valid=false for 401")
	}
	if result.Error == "" {
		t.Error("expected non-empty Error for 401")
	}
}

func TestValidateKeyUnreachable(t *testing.T) {
	result, err := ValidateKey("http://127.0.0.1:1", "key", "model")
	if err == nil && result.Valid {
		t.Error("expected error or invalid result for unreachable server")
	}
}

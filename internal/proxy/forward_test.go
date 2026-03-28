// internal/proxy/forward_test.go
package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestForwardRequest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "real-provider-key" {
			t.Errorf("x-api-key = %q, want real-provider-key", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("User-Agent") != "test-agent/1.0" {
			t.Errorf("User-Agent = %q, want test-agent/1.0", r.Header.Get("User-Agent"))
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("anthropic-version not preserved")
		}
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer upstream.Close()

	body := strings.NewReader(`{"model":"glm-4.7","messages":[]}`)
	req := httptest.NewRequest("POST", "/v1/messages", body)
	req.Header.Set("x-api-key", "user-token")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	ForwardRequest(rec, req, upstream.URL, "real-provider-key", "test-agent/1.0")

	if rec.Code != http.StatusOK { t.Errorf("status = %d, want 200", rec.Code) }
	if !strings.Contains(rec.Body.String(), "glm-4.7") { t.Error("response body should contain echoed request") }
}

func TestForwardRequestStreaming(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok { t.Fatal("expected flusher") }
		w.Write([]byte("data: chunk1\n\n"))
		flusher.Flush()
		w.Write([]byte("data: chunk2\n\n"))
		flusher.Flush()
	}))
	defer upstream.Close()

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader("{}"))
	req.Header.Set("x-api-key", "tok")
	rec := httptest.NewRecorder()
	ForwardRequest(rec, req, upstream.URL, "key", "agent")

	if rec.Code != http.StatusOK { t.Errorf("status = %d", rec.Code) }
	if !strings.Contains(rec.Body.String(), "chunk1") || !strings.Contains(rec.Body.String(), "chunk2") {
		t.Errorf("missing chunks in response: %s", rec.Body.String())
	}
}

func TestForwardRequestUpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer upstream.Close()

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader("{}"))
	req.Header.Set("x-api-key", "tok")
	rec := httptest.NewRecorder()
	ForwardRequest(rec, req, upstream.URL, "key", "agent")

	if rec.Code != http.StatusTooManyRequests { t.Errorf("status = %d, want 429", rec.Code) }
}

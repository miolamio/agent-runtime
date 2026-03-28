// internal/proxy/handler.go
package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/miolamio/agent-runtime/internal/proxy/students"
)

// Handler is the main HTTP handler for the proxy.
type Handler struct {
	config   *ProxyConfig
	students *students.Manager
	limiter  *RateLimiter
	mux      *http.ServeMux
}

// NewHandler creates a proxy handler.
func NewHandler(cfg *ProxyConfig, mgr *students.Manager) *Handler {
	h := &Handler{
		config:   cfg,
		students: mgr,
		limiter:  NewRateLimiter(cfg.RPM),
		mux:      http.NewServeMux(),
	}
	h.mux.HandleFunc("GET /v1/models", h.handleModels)
	h.mux.HandleFunc("POST /v1/messages", h.handleMessages)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("x-api-key")
	if token == "" {
		jsonError(w, http.StatusUnauthorized, "missing x-api-key header")
		return
	}
	student := h.students.FindByToken(token)
	if student == nil || !student.Active {
		jsonError(w, http.StatusUnauthorized, "invalid or revoked token")
		return
	}
	if !h.limiter.Allow(token) {
		jsonError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) handleModels(w http.ResponseWriter, r *http.Request) {
	type modelEntry struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
	}
	models := h.config.AllModels()
	data := make([]modelEntry, len(models))
	for i, m := range models {
		data[i] = modelEntry{ID: m, Object: "model", Created: time.Now().Unix()}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data":   data,
	})
}

func (h *Handler) handleMessages(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "cannot read body")
		return
	}
	r.Body.Close()

	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Model == "" {
		jsonError(w, http.StatusBadRequest, "missing model field")
		return
	}

	provider, ok := h.config.ResolveModel(req.Model)
	if !ok {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("unknown model: %s", req.Model))
		return
	}

	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	token := r.Header.Get("x-api-key")
	student := h.students.FindByToken(token)
	name := "unknown"
	if student != nil {
		name = student.Name
	}

	start := time.Now()
	ForwardRequest(w, r, provider.BaseURL, provider.APIKey, h.config.UserAgent)
	latency := time.Since(start)

	masked := token
	if len(token) > 10 {
		masked = token[:10] + "..."
	}
	log.Printf("[proxy] student=%s token=%s model=%s latency=%dms",
		name, masked, req.Model, latency.Milliseconds())
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    strings.ToLower(http.StatusText(code)),
			"message": msg,
		},
	})
}

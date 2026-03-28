package keys

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type ValidationResult struct {
	Valid   bool
	Model   string
	Latency time.Duration
	Error   string
}

func ValidateKey(baseURL, apiKey, model string) (*ValidationResult, error) {
	body := map[string]any{
		"model":      model,
		"max_tokens": 1,
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
	}
	payload, _ := json.Marshal(body)

	url := strings.TrimRight(baseURL, "/") + "/v1/messages"
	req, err := http.NewRequest("POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 15 * time.Second}
	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)
	if err != nil {
		return &ValidationResult{
			Valid: false,
			Error: fmt.Sprintf("connection failed: %v", err),
		}, err
	}
	defer resp.Body.Close()

	result := &ValidationResult{
		Latency: latency,
		Model:   model,
	}

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		result.Valid = true
		var respBody map[string]any
		if json.NewDecoder(resp.Body).Decode(&respBody) == nil {
			if m, ok := respBody["model"].(string); ok {
				result.Model = m
			}
		}
	} else {
		result.Valid = false
		var respBody map[string]any
		if json.NewDecoder(resp.Body).Decode(&respBody) == nil {
			if e, ok := respBody["error"].(map[string]any); ok {
				if msg, ok := e["message"].(string); ok {
					result.Error = msg
				}
			}
		}
		if result.Error == "" {
			result.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
	}

	return result, nil
}

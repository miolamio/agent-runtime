package proxy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Connect configures the native Claude Code CLI to use an airun proxy.
// It writes ANTHROPIC_BASE_URL, ANTHROPIC_AUTH_TOKEN, and model settings
// into ~/.claude/settings.json so that `claude` routes through the proxy.
func Connect(proxyURL, token string) error {
	reader := bufio.NewReader(os.Stdin)

	// Interactive: ask for URL and token if not provided
	if proxyURL == "" {
		fmt.Print("  Proxy URL (e.g. http://server:8080): ")
		line, _ := reader.ReadString('\n')
		proxyURL = strings.TrimSpace(line)
		if proxyURL == "" {
			return fmt.Errorf("proxy URL is required")
		}
	}
	if token == "" {
		fmt.Print("  API key (sk-ai-...): ")
		line, _ := reader.ReadString('\n')
		token = strings.TrimSpace(line)
		if token == "" {
			return fmt.Errorf("API key is required")
		}
	}

	proxyURL = strings.TrimRight(proxyURL, "/")

	// Validate connection
	fmt.Print("\n  Connecting... ")
	models, err := fetchModels(proxyURL, token)
	if err != nil {
		return fmt.Errorf("cannot connect to proxy: %w", err)
	}
	fmt.Printf("OK (%d models)\n\n", len(models))

	for _, m := range models {
		fmt.Printf("  [x] %s\n", m)
	}

	// Pick default model
	defaultModel := models[0]
	if len(models) > 1 {
		fmt.Printf("\n  Default model [%s]: ", defaultModel)
		answer, _ := reader.ReadString('\n')
		if a := strings.TrimSpace(answer); a != "" {
			defaultModel = a
		}
	}

	// Write to ~/.claude/settings.json
	settingsPath := claudeSettingsPath()
	if err := mergeClaudeSettings(settingsPath, proxyURL, token, defaultModel); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}

	fmt.Printf("\n  Claude Code configured to use proxy:\n")
	fmt.Printf("    URL:   %s\n", proxyURL)
	fmt.Printf("    Model: %s\n", defaultModel)
	fmt.Printf("    File:  %s\n\n", settingsPath)
	fmt.Println("  Run `claude` to start using the proxy.")
	return nil
}

// Disconnect removes proxy settings from ~/.claude/settings.json.
func Disconnect() error {
	settingsPath := claudeSettingsPath()
	settings, err := readSettings(settingsPath)
	if err != nil {
		return fmt.Errorf("read settings: %w", err)
	}

	env, _ := settings["env"].(map[string]any)
	if env == nil {
		fmt.Println("  No proxy settings found.")
		return nil
	}

	keysToRemove := []string{
		"ANTHROPIC_AUTH_TOKEN",
		"ANTHROPIC_BASE_URL",
		"ANTHROPIC_DEFAULT_SONNET_MODEL",
		"ANTHROPIC_DEFAULT_OPUS_MODEL",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL",
		"API_TIMEOUT_MS",
	}
	removed := 0
	for _, k := range keysToRemove {
		if _, ok := env[k]; ok {
			delete(env, k)
			removed++
		}
	}

	if removed == 0 {
		fmt.Println("  No proxy settings found.")
		return nil
	}

	settings["env"] = env
	if err := writeSettings(settingsPath, settings); err != nil {
		return err
	}
	fmt.Printf("  Proxy settings removed from %s\n", settingsPath)
	fmt.Println("  Claude Code will use its default Anthropic API.")
	return nil
}

func fetchModels(baseURL, apiKey string) ([]string, error) {
	url := baseURL + "/v1/models"
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("x-api-key", apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("invalid API key (HTTP 401)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	var models []string
	for _, m := range result.Data {
		models = append(models, m.ID)
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("no models available")
	}
	return models, nil
}

func claudeSettingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "settings.json")
}

func readSettings(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return settings, nil
}

func writeSettings(path string, settings map[string]any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0600)
}

func mergeClaudeSettings(path, proxyURL, token, model string) error {
	settings, err := readSettings(path)
	if err != nil {
		return err
	}

	env, ok := settings["env"].(map[string]any)
	if !ok {
		env = map[string]any{}
	}

	env["ANTHROPIC_AUTH_TOKEN"] = token
	env["ANTHROPIC_BASE_URL"] = proxyURL
	env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = model
	env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = model
	env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = model
	env["API_TIMEOUT_MS"] = "3000000"

	settings["env"] = env
	return writeSettings(path, settings)
}

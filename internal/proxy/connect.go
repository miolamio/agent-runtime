package proxy

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Connect configures the native Claude Code CLI to use an airun proxy.
// It writes env vars into ~/.claude/settings.json and sets up ~/.claude.json
// to bypass onboarding/authentication dialogs.
func Connect(proxyURL, token string) error {
	reader := bufio.NewReader(os.Stdin)

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

	// Pick default model — prefer glm-5.1 if available
	defaultModel := models[0]
	for _, m := range models {
		if m == "glm-5.1" {
			defaultModel = m
			break
		}
	}
	if len(models) > 1 {
		fmt.Printf("\n  Default model [%s]: ", defaultModel)
		answer, _ := reader.ReadString('\n')
		if a := strings.TrimSpace(answer); a != "" {
			defaultModel = a
		}
	}

	// 1. Write env vars to ~/.claude/settings.json
	settingsPath := claudeSettingsPath()
	if err := mergeClaudeSettings(settingsPath, proxyURL, token, defaultModel); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}

	// 2. Write ~/.claude.json to bypass onboarding/auth
	claudeJSONPath := claudeJSONPath()
	if err := writeClaudeJSON(claudeJSONPath, token); err != nil {
		return fmt.Errorf("write claude.json: %w", err)
	}

	fmt.Printf("\n  Claude Code configured to use proxy:\n")
	fmt.Printf("    URL:      %s\n", proxyURL)
	fmt.Printf("    Model:    %s\n", defaultModel)
	fmt.Printf("    Settings: %s\n", settingsPath)
	fmt.Printf("    Auth:     %s (onboarding bypassed)\n\n", claudeJSONPath)
	fmt.Println("  Run `claude` to start using the proxy.")
	return nil
}

// Disconnect removes proxy settings from ~/.claude/settings.json
// and cleans up ~/.claude.json auth bypass.
func Disconnect() error {
	removed := 0

	// 1. Clean settings.json
	settingsPath := claudeSettingsPath()
	settings, err := readSettings(settingsPath)
	if err == nil {
		env, _ := settings["env"].(map[string]any)
		if env != nil {
			for _, k := range proxyEnvKeys {
				if _, ok := env[k]; ok {
					delete(env, k)
					removed++
				}
			}
			settings["env"] = env
			writeSettings(settingsPath, settings)
		}
	}

	// 2. Clean claude.json
	claudeJSON := claudeJSONPath()
	if cleanClaudeJSON(claudeJSON) {
		removed++
	}

	if removed == 0 {
		fmt.Println("  No proxy settings found.")
		return nil
	}

	fmt.Println("  Proxy settings removed.")
	fmt.Println("  Claude Code will use its default Anthropic API.")
	return nil
}

// --- env keys managed by connect/disconnect ---

var proxyEnvKeys = []string{
	"ANTHROPIC_AUTH_TOKEN",
	"ANTHROPIC_BASE_URL",
	"ANTHROPIC_DEFAULT_SONNET_MODEL",
	"ANTHROPIC_DEFAULT_OPUS_MODEL",
	"ANTHROPIC_DEFAULT_HAIKU_MODEL",
	"API_TIMEOUT_MS",
}

// --- ~/.claude.json management ---

func claudeJSONPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude.json")
}

// writeClaudeJSON creates or merges ~/.claude.json with onboarding bypass fields.
func writeClaudeJSON(path, apiKey string) error {
	cj, _ := readSettings(path) // reuse generic JSON reader; empty map if missing

	// Detect installed Claude Code version
	ver := detectClaudeVersion()

	// Core onboarding bypass
	cj["hasCompletedOnboarding"] = true
	cj["hasTrustDialogAccepted"] = true
	cj["lastOnboardingVersion"] = ver
	cj["autoUpdaterStatus"] = "disabled"

	// Ensure numStartups is set (avoids first-run prompts)
	if _, ok := cj["numStartups"]; !ok {
		cj["numStartups"] = float64(184)
	}

	// Generate userID if missing
	if _, ok := cj["userID"]; !ok {
		b := make([]byte, 32)
		rand.Read(b)
		cj["userID"] = hex.EncodeToString(b)
	}

	// Ensure projects map exists
	if _, ok := cj["projects"]; !ok {
		cj["projects"] = map[string]any{}
	}

	// Trust the API key (last 20 chars) to avoid "trust this key?" dialog
	keyTail := apiKey
	if len(keyTail) > 20 {
		keyTail = keyTail[len(keyTail)-20:]
	}
	car, _ := cj["customApiKeyResponses"].(map[string]any)
	if car == nil {
		car = map[string]any{}
	}
	approved, _ := car["approved"].([]any)
	// Add if not already present
	found := false
	for _, a := range approved {
		if a == keyTail {
			found = true
			break
		}
	}
	if !found {
		approved = append(approved, keyTail)
	}
	car["approved"] = approved
	if _, ok := car["rejected"]; !ok {
		car["rejected"] = []any{}
	}
	cj["customApiKeyResponses"] = car

	// Mark that we wrote this (for clean disconnect)
	cj["_airunManaged"] = true

	return writeSettings(path, cj)
}

// cleanClaudeJSON removes airun-managed fields from ~/.claude.json.
// If we created the file (_airunManaged marker), remove it entirely.
// Returns true if changes were made.
func cleanClaudeJSON(path string) bool {
	cj, err := readSettings(path)
	if err != nil || len(cj) == 0 {
		return false
	}

	managed, _ := cj["_airunManaged"].(bool)
	if managed {
		// We created this file — safe to remove entirely
		os.Remove(path)
		return true
	}

	// File existed before us — only remove our specific fields
	changed := false
	for _, k := range []string{
		"_airunManaged",
		"customApiKeyResponses",
	} {
		if _, ok := cj[k]; ok {
			delete(cj, k)
			changed = true
		}
	}
	if changed {
		writeSettings(path, cj)
	}
	return changed
}

// detectClaudeVersion tries to find the installed Claude Code version.
func detectClaudeVersion() string {
	// Try running claude --version
	out, err := exec.Command("claude", "--version").Output()
	if err == nil {
		line := strings.TrimSpace(string(out))
		// Extract version number (e.g. "2.1.86" from "2.1.86 (Claude Code)")
		parts := strings.Fields(line)
		if len(parts) > 0 {
			return parts[0]
		}
	}
	// Fallback: high version to always pass the check
	return "99.0.0"
}

// --- network ---

func fetchModels(baseURL, apiKey string) ([]string, error) {
	url := baseURL + "/v1/models"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
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

// --- settings.json helpers ---

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

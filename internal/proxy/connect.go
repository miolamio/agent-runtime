package proxy

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/miolamio/agent-runtime/internal/keys"
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
	models, err := keys.FetchRemoteModels(proxyURL, token)
	if err != nil {
		return fmt.Errorf("cannot connect to proxy: %w", err)
	}
	fmt.Printf("OK (%d models)\n\n", len(models))

	for _, m := range models {
		fmt.Printf("  [x] %s\n", m)
	}

	// Pick default model — prefer glm-5.3 if available
	defaultModel := models[0]
	for _, m := range models {
		if m == "glm-5.3" {
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
	// Reject unreadable/damaged input before changing either document.
	for _, path := range []string{settingsPath, claudeJSONPath()} {
		document, err := readSettings(path)
		if err != nil {
			return err
		}
		if _, err := readBackup(document); err != nil {
			return err
		}
	}
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
	removed := false
	for _, path := range []string{claudeSettingsPath(), claudeJSONPath()} {
		changed, err := cleanManagedSettings(path)
		if err != nil {
			return fmt.Errorf("restore %s: %w", path, err)
		}
		removed = removed || changed
	}
	if removed {
		fmt.Println("  Previous settings restored; subsequent user edits preserved.")
	} else {
		fmt.Println("  No reversible proxy settings found.")
	}
	return nil
}

// --- ~/.claude.json management ---

func claudeJSONPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude.json")
}

// writeClaudeJSON creates or merges ~/.claude.json with onboarding bypass fields.
func writeClaudeJSON(path, apiKey string) error {
	return updateManagedSettings(path, func(cj map[string]any) error {
		if value, exists := cj["customApiKeyResponses"]; exists {
			if _, ok := value.(map[string]any); !ok {
				return fmt.Errorf("customApiKeyResponses must be an object")
			}
		}

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
			if _, err := rand.Read(b); err != nil {
				return fmt.Errorf("generate userID: %w", err)
			}
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

		return nil
	})
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

// --- settings.json helpers ---

func claudeSettingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "settings.json")
}

func readSettings(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	var settings map[string]any
	// Windows PowerShell 5.1's Set-Content -Encoding UTF8 writes a BOM.
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if settings == nil {
		return nil, fmt.Errorf("%s must contain a JSON object", path)
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
	f, err := os.CreateTemp(dir, ".airun-settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func mergeClaudeSettings(path, proxyURL, token, model string) error {
	return updateManagedSettings(path, func(settings map[string]any) error {
		if value, exists := settings["env"]; exists {
			if _, ok := value.(map[string]any); !ok {
				return fmt.Errorf("env must be an object")
			}
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
		return nil
	})
}

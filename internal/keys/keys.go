// internal/keys/keys.go
package keys

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// List prints all configured keys with masking.
func List(envPath string) error {
	kv, err := ReadAllEnvKeys(envPath)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", envPath, err)
	}

	defaultProvider := kv["ARUN_PROVIDER"]
	if defaultProvider == "" {
		defaultProvider = "zai"
	}

	// Only show providers that are configured, plus Remote if it has models
	hasAny := false
	for _, p := range AllProviders() {
		if kv[p.EnvKey] != "" {
			hasAny = true
			break
		}
	}

	fmt.Printf("\n  %-10s %-18s %-30s %s\n", "Provider", "Key", "Models", "Default")
	fmt.Printf("  %-10s %-18s %-30s %s\n", "--------", "---", "------", "-------")

	for _, p := range AllProviders() {
		key := kv[p.EnvKey]
		if key == "" && hasAny {
			continue // skip unconfigured providers when at least one is configured
		}
		keyDisplay := "(not set)"
		if key != "" {
			keyDisplay = maskKey(key)
		}
		def := ""
		if p.ID == defaultProvider {
			def = "  *"
		}
		// Remote shows all available models from REMOTE_MODELS
		if p.ID == "remote" && kv["REMOTE_MODELS"] != "" {
			models := kv["REMOTE_MODELS"]
			defaultModel := kv["REMOTE_DEFAULT_MODEL"]
			fmt.Printf("  %-10s %-18s %-30s %s\n", p.Name, keyDisplay, defaultModel+" (default)", def)
			for _, m := range strings.Split(models, ",") {
				m = strings.TrimSpace(m)
				if m != "" && m != defaultModel {
					fmt.Printf("  %-10s %-18s %-30s\n", "", "", m)
				}
			}
		} else {
			model := kv[p.EnvModel]
			if model == "" {
				model = p.Model
			}
			fmt.Printf("  %-10s %-18s %-30s %s\n", p.Name, keyDisplay, model, def)
		}
	}
	fmt.Println()
	return nil
}

// Add guides the user through adding a key for a provider.
func Add(envPath, alias string) error {
	if strings.ToLower(alias) == "remote" || strings.ToLower(alias) == "r" {
		return AddRemote(envPath)
	}
	p := ProviderByAlias(alias)
	if p == nil {
		return fmt.Errorf("unknown provider: %q (use zai, minimax, or kimi)", alias)
	}

	reader := bufio.NewReader(os.Stdin)

	// Check existing
	existing, _ := ReadEnvKey(envPath, p.EnvKey)
	if existing != "" {
		fmt.Printf("\n  %s key already configured: %s\n", p.Name, maskKey(existing))
		fmt.Print("  Replace? [y/N] ")
		answer, _ := reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(answer)) != "y" {
			fmt.Println("  Keeping existing key.")
			return nil
		}
	}

	// Show guide
	fmt.Printf("\n  --- %s (%s) ---\n\n", p.Name, p.RegisterURL)
	fmt.Println("  To get your API key:")
	for i, step := range p.Steps {
		fmt.Printf("  %d. %s\n", i+1, step)
	}
	fmt.Println()

	// Read key
	fmt.Print("  Paste your API key: ")
	apiKey, _ := reader.ReadString('\n')
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		fmt.Println("  Skipped.")
		return nil
	}

	// Validate
	fmt.Print("\n  Validating... ")
	result, err := ValidateKey(p.BaseURL, apiKey, p.Model)
	if err != nil {
		fmt.Printf("! Connection failed: %v\n", err)
		fmt.Print("  Save key anyway? [y/N] ")
		answer, _ := reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(answer)) != "y" {
			return nil
		}
	} else if !result.Valid {
		fmt.Printf("! Invalid key: %s\n", result.Error)
		fmt.Print("  Save key anyway? [y/N] ")
		answer, _ := reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(answer)) != "y" {
			return nil
		}
	} else {
		fmt.Printf("OK (%s, %dms)\n", result.Model, result.Latency.Milliseconds())
	}

	// Save
	if err := UpdateEnvKey(envPath, p.EnvKey, apiKey); err != nil {
		return fmt.Errorf("save key: %w", err)
	}
	fmt.Printf("  Key saved to %s\n\n", envPath)
	return nil
}

// Remove deletes a provider's key from the env file.
func Remove(envPath, alias string) error {
	p := ProviderByAlias(alias)
	if p == nil {
		return fmt.Errorf("unknown provider: %q (use zai, minimax, or kimi)", alias)
	}

	existing, _ := ReadEnvKey(envPath, p.EnvKey)
	if existing == "" {
		fmt.Printf("  %s: no key configured\n", p.Name)
		return nil
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("\n  Remove %s key (%s)? [y/N] ", p.Name, maskKey(existing))
	answer, _ := reader.ReadString('\n')
	if strings.TrimSpace(strings.ToLower(answer)) != "y" {
		fmt.Println("  Keeping key.")
		return nil
	}

	if err := UpdateEnvKey(envPath, p.EnvKey, ""); err != nil {
		return err
	}

	// Check if removed provider was default
	defaultProvider, _ := ReadEnvKey(envPath, "ARUN_PROVIDER")
	if defaultProvider == "" || defaultProvider == p.ID {
		kv, _ := ReadAllEnvKeys(envPath)
		for _, other := range AllProviders() {
			if other.ID != p.ID && kv[other.EnvKey] != "" {
				fmt.Printf("  Default provider was %s. Changed to %s.\n", p.Name, other.Name)
				if err := UpdateEnvKey(envPath, "ARUN_PROVIDER", other.ID); err != nil {
					fmt.Fprintf(os.Stderr, "  warning: could not update default provider: %v\n", err)
				}
				break
			}
		}
	}

	fmt.Printf("  Key removed from %s\n\n", envPath)
	return nil
}

// Test validates configured keys.
func Test(envPath, alias string) error {
	kv, err := ReadAllEnvKeys(envPath)
	if err != nil {
		return err
	}

	var targets []Provider
	if alias == "" {
		targets = AllProviders()
	} else {
		p := ProviderByAlias(alias)
		if p == nil {
			return fmt.Errorf("unknown provider: %q", alias)
		}
		targets = []Provider{*p}
	}

	fmt.Println()
	for _, p := range targets {
		apiKey := kv[p.EnvKey]
		if apiKey == "" {
			fmt.Printf("  %-10s ! No key configured\n", p.Name+":")
			continue
		}
		baseURL := p.BaseURL
		model := p.Model
		// Remote provider reads URL and model from env, not from struct
		if p.ID == "remote" {
			baseURL = kv["REMOTE_BASE_URL"]
			model = kv["REMOTE_DEFAULT_MODEL"]
		}
		result, err := ValidateKey(baseURL, apiKey, model)
		if err != nil {
			fmt.Printf("  %-10s ! %v\n", p.Name+":", err)
		} else if !result.Valid {
			fmt.Printf("  %-10s ! %s\n", p.Name+":", result.Error)
		} else {
			fmt.Printf("  %-10s OK (%s, %dms)\n", p.Name+":", result.Model, result.Latency.Milliseconds())
		}
	}
	fmt.Println()
	return nil
}

// SetDefault changes the default provider.
func SetDefault(envPath, alias string) error {
	p := ProviderByAlias(alias)
	if p == nil {
		return fmt.Errorf("unknown provider: %q (use zai, minimax, or kimi)", alias)
	}

	apiKey, _ := ReadEnvKey(envPath, p.EnvKey)
	if apiKey == "" {
		return fmt.Errorf("%s has no key configured — run: airun keys add %s", p.Name, p.ID)
	}

	old, _ := ReadEnvKey(envPath, "ARUN_PROVIDER")
	if old == "" {
		old = "zai"
	}

	if err := UpdateEnvKey(envPath, "ARUN_PROVIDER", p.ID); err != nil {
		return err
	}
	fmt.Printf("  Default provider changed: %s -> %s\n", old, p.ID)
	return nil
}

// SetModel changes the default model for the current provider.
func SetModel(envPath, model string) error {
	kv, err := ReadAllEnvKeys(envPath)
	if err != nil {
		return err
	}
	provider := kv["ARUN_PROVIDER"]
	if provider == "" {
		provider = "zai"
	}

	// Determine which env key to update
	var envKey string
	switch provider {
	case "remote":
		// Validate model is in REMOTE_MODELS list
		remoteModels := kv["REMOTE_MODELS"]
		if remoteModels != "" {
			found := false
			for _, m := range strings.Split(remoteModels, ",") {
				if strings.TrimSpace(m) == model {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("model %q not available (available: %s)", model, remoteModels)
			}
		}
		envKey = "REMOTE_DEFAULT_MODEL"
	case "zai":
		envKey = "ZAI_MODEL"
	case "minimax":
		envKey = "MINIMAX_MODEL"
	case "kimi":
		envKey = "KIMI_MODEL"
	default:
		return fmt.Errorf("unknown provider: %s", provider)
	}

	old := kv[envKey]
	if err := UpdateEnvKey(envPath, envKey, model); err != nil {
		return err
	}
	fmt.Printf("  Default model changed: %s -> %s (provider: %s)\n", old, model, provider)
	return nil
}

func maskKey(key string) string {
	if len(key) < 8 {
		return "***"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

// AddRemote guides through connecting to a remote proxy.
func AddRemote(envPath string) error {
	reader := bufio.NewReader(os.Stdin)

	existing, _ := ReadEnvKey(envPath, "REMOTE_API_KEY")
	if existing != "" {
		fmt.Printf("\n  Remote proxy already configured: %s\n", maskKey(existing))
		fmt.Print("  Reconfigure? [y/N] ")
		answer, _ := reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(answer)) != "y" {
			return nil
		}
	}

	fmt.Println("\n  --- Remote Proxy ---")
	fmt.Println("  Get proxy URL and API key from your admin.")
	fmt.Println()

	fmt.Print("  Proxy URL (e.g. http://server:8080): ")
	proxyURL, _ := reader.ReadString('\n')
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		fmt.Println("  Skipped.")
		return nil
	}

	fmt.Print("  API key: ")
	apiKey, _ := reader.ReadString('\n')
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		fmt.Println("  Skipped.")
		return nil
	}

	// Fetch models
	fmt.Print("\n  Fetching models... ")
	models, err := FetchRemoteModels(proxyURL, apiKey)
	if err != nil {
		fmt.Printf("! %v\n", err)
		return err
	}
	fmt.Printf("OK (%d models)\n\n", len(models))

	for _, m := range models {
		fmt.Printf("  [x] %s\n", m)
	}

	defaultModel := models[0]
	if len(models) > 1 {
		fmt.Printf("\n  Default model [%s]: ", defaultModel)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(answer)
		if answer != "" {
			defaultModel = answer
		}
	}

	// Save
	for _, kv := range [][2]string{
		{"REMOTE_BASE_URL", proxyURL},
		{"REMOTE_API_KEY", apiKey},
		{"REMOTE_MODELS", strings.Join(models, ",")},
		{"REMOTE_DEFAULT_MODEL", defaultModel},
	} {
		if err := UpdateEnvKey(envPath, kv[0], kv[1]); err != nil {
			return fmt.Errorf("write %s: %w", kv[0], err)
		}
	}

	fmt.Printf("\n  Remote proxy configured: %s (%d models)\n", proxyURL, len(models))
	fmt.Printf("  Default model: %s\n\n", defaultModel)
	return nil
}

// FetchRemoteModels calls GET /v1/models on the proxy.
func FetchRemoteModels(baseURL, apiKey string) ([]string, error) {
	url := strings.TrimRight(baseURL, "/") + "/v1/models"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("x-api-key", apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect to proxy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxy returned HTTP %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parse models: %w", err)
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

// internal/keys/keys.go
package keys

import (
	"bufio"
	"fmt"
	"os"
	"strings"
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

	fmt.Printf("\n  %-10s %-18s %-16s %s\n", "Provider", "Key", "Model", "Default")
	fmt.Printf("  %-10s %-18s %-16s %s\n", "--------", "---", "-----", "-------")

	for _, p := range AllProviders() {
		key := kv[p.EnvKey]
		model := kv[p.EnvModel]
		if model == "" {
			model = p.Model
		}
		keyDisplay := "(not set)"
		if key != "" {
			keyDisplay = maskKey(key)
		}
		def := ""
		if p.ID == defaultProvider {
			def = "  *"
		}
		fmt.Printf("  %-10s %-18s %-16s %s\n", p.Name, keyDisplay, model, def)
	}
	fmt.Println()
	return nil
}

// Add guides the user through adding a key for a provider.
func Add(envPath, alias string) error {
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
				UpdateEnvKey(envPath, "ARUN_PROVIDER", other.ID)
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
		result, err := ValidateKey(p.BaseURL, apiKey, p.Model)
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

func maskKey(key string) string {
	if len(key) < 8 {
		return "***"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

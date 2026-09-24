package profile

import (
	"fmt"
	"os"
	"path/filepath"
)

type Profile struct {
	Key         string         `yaml:"-"`
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Provider    string         `yaml:"provider"`
	Plugins     []string       `yaml:"plugins"`
	Settings    map[string]any `yaml:"settings"`
	Components  Components     `yaml:"components"`

	deprecatedSkills bool
}

type ComponentRef struct {
	ID string `yaml:"id" json:"id"`
}

// MCPRef contains source variable names only. Normalize converts these into
// transport aliases in a separate type before any manifest is serialized.
type MCPRef struct {
	ID  string            `yaml:"id"`
	Env map[string]string `yaml:"env" json:"-"`
}

type Components struct {
	Agents   []ComponentRef `yaml:"agents"`
	Skills   []ComponentRef `yaml:"skills"`
	Commands []ComponentRef `yaml:"commands"`
	MCPs     []MCPRef       `yaml:"mcps"`
	Mods     []ComponentRef `yaml:"mods"`
	Plugins  []ComponentRef `yaml:"plugins"`
}

func Load(name string) (*Profile, error) {
	if err := ValidateKey(name); err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}
	path := filepath.Join(home, ".airun", "profiles", name+".yaml")

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("profile %q not found: %w", name, err)
	}

	p, err := Parse(name, data)
	if err != nil {
		return nil, fmt.Errorf("invalid profile %q: %w", name, err)
	}

	if p.deprecatedSkills {
		fmt.Fprintf(os.Stderr,
			"[airun] warning: profile %q uses deprecated 'skills' field (ignored since v0.7.0); "+
				"declare marketplace plugins under 'plugins' instead\n", name)
	}

	return p, nil
}

func List() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}
	dir := filepath.Join(home, ".airun", "profiles")

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".yaml" {
			name := e.Name()[:len(e.Name())-5]
			if ValidateKey(name) == nil {
				names = append(names, name)
			}
		}
	}
	return names, nil
}

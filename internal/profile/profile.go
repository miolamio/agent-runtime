package profile

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Profile struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Provider    string         `yaml:"provider"`
	Plugins     []string       `yaml:"plugins"`
	Settings    map[string]any `yaml:"settings"`
}

// legacyProfile captures deprecated fields so we can emit a one-shot warning
// without silently ignoring them.
type legacyProfile struct {
	Skills []string `yaml:"skills"`
}

func Load(name string) (*Profile, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}
	path := filepath.Join(home, ".airun", "profiles", name+".yaml")

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("profile %q not found: %w", name, err)
	}

	var p Profile
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("invalid profile %q: %w", name, err)
	}

	var legacy legacyProfile
	if err := yaml.Unmarshal(data, &legacy); err == nil && len(legacy.Skills) > 0 {
		fmt.Fprintf(os.Stderr,
			"[airun] warning: profile %q uses deprecated 'skills' field (ignored since v0.7.0); "+
				"declare marketplace plugins under 'plugins' instead\n", name)
	}

	if p.Name == "" {
		p.Name = name
	}

	return &p, nil
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
			names = append(names, e.Name()[:len(e.Name())-5])
		}
	}
	return names, nil
}


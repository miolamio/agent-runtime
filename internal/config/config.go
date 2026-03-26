package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
)

type Config struct {
	ProfilesDir   string
	SkillsDir     string
	AgentsDir     string
	CommandsDir   string
	RouterConfig  string
	ClawkerConfig string
}

func Load() (*Config, error) {
	usr, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}
	home := usr.HomeDir

	return &Config{
		ProfilesDir:   filepath.Join(home, "automatica-profiles"),
		SkillsDir:     filepath.Join(home, "automatica-skills"),
		AgentsDir:     filepath.Join(home, "automatica-agents"),
		CommandsDir:   filepath.Join(home, "automatica-commands"),
		RouterConfig:  filepath.Join(home, ".claude-code-router", "config.json"),
		ClawkerConfig: "clawker.yaml",
	}, nil
}

func (c *Config) Validate() error {
	dirs := map[string]string{
		"profiles": c.ProfilesDir,
		"skills":   c.SkillsDir,
		"agents":   c.AgentsDir,
		"commands": c.CommandsDir,
	}
	for name, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return fmt.Errorf("%s directory does not exist: %s (run: mkdir -p %s)", name, dir, dir)
		}
	}
	return nil
}

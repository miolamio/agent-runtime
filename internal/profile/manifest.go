package profile

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	ManifestVersion    = 1
	componentEnvPrefix = "AIRUN_COMPONENT_ENV_"
)

type ManifestMCPRef struct {
	ID  string            `json:"id"`
	Env map[string]string `json:"env,omitempty"`
}

type ManifestComponents struct {
	Agents   []ComponentRef   `json:"agents"`
	Skills   []ComponentRef   `json:"skills"`
	Commands []ComponentRef   `json:"commands"`
	MCPs     []ManifestMCPRef `json:"mcps"`
	Mods     []ComponentRef   `json:"mods"`
	Plugins  []ComponentRef   `json:"plugins"`
}

type Manifest struct {
	Version       int                `json:"version"`
	ProfileKey    string             `json:"profile_key"`
	Settings      map[string]any     `json:"settings"`
	NativePlugins []string           `json:"native_plugins"`
	Components    ManifestComponents `json:"components"`
}

// Normalize constructs the secret-free adapter contract and returns sensitive
// transport entries separately for the protected Docker environment file. It
// never changes the source profile and returns no partial result on failure.
func Normalize(p *Profile, lookup func(string) (string, bool)) (Manifest, []string, error) {
	if p == nil {
		return Manifest{}, nil, fmt.Errorf("profile: required")
	}
	if err := ValidateKey(p.Key); err != nil {
		return Manifest{}, nil, err
	}
	for index, plugin := range p.Plugins {
		if err := validateNativePlugin(plugin, fmt.Sprintf("plugins[%d]", index)); err != nil {
			return Manifest{}, nil, err
		}
	}
	manifest := Manifest{
		Version:       ManifestVersion,
		ProfileKey:    p.Key,
		Settings:      p.Settings,
		NativePlugins: append([]string{}, p.Plugins...),
		Components: ManifestComponents{
			Agents:   append([]ComponentRef{}, p.Components.Agents...),
			Skills:   append([]ComponentRef{}, p.Components.Skills...),
			Commands: append([]ComponentRef{}, p.Components.Commands...),
			MCPs:     []ManifestMCPRef{},
			Mods:     append([]ComponentRef{}, p.Components.Mods...),
			Plugins:  append([]ComponentRef{}, p.Components.Plugins...),
		},
	}
	if manifest.Settings == nil {
		manifest.Settings = map[string]any{}
	}
	if _, err := json.Marshal(manifest.Settings); err != nil {
		return Manifest{}, nil, fmt.Errorf("settings: value is not JSON-compatible")
	}
	groups := []struct {
		name string
		refs []ComponentRef
	}{
		{"agents", p.Components.Agents},
		{"skills", p.Components.Skills},
		{"commands", p.Components.Commands},
		{"mods", p.Components.Mods},
		{"plugins", p.Components.Plugins},
	}
	for _, group := range groups {
		for index, ref := range group.refs {
			if err := validateID(ref.ID, fmt.Sprintf("components.%s[%d].id", group.name, index)); err != nil {
				return Manifest{}, nil, err
			}
		}
	}
	var transport []string
	for index, source := range p.Components.MCPs {
		path := fmt.Sprintf("components.mcps[%d]", index)
		if err := validateID(source.ID, path+".id"); err != nil {
			return Manifest{}, nil, err
		}
		ref := ManifestMCPRef{ID: source.ID}
		if len(source.Env) > 0 {
			ref.Env = make(map[string]string, len(source.Env))
		}
		targets := make([]string, 0, len(source.Env))
		for target := range source.Env {
			targets = append(targets, target)
		}
		sort.Strings(targets)
		for _, target := range targets {
			if err := validateVariable(target, path+".env", "target"); err != nil {
				return Manifest{}, nil, err
			}
			bindingPath := path + ".env." + target
			host := source.Env[target]
			if err := validateVariable(host, bindingPath, "host"); err != nil {
				return Manifest{}, nil, err
			}
			if lookup == nil {
				return Manifest{}, nil, fmt.Errorf("%s: host environment lookup is required", bindingPath)
			}
			value, ok := lookup(host)
			if !ok || value == "" {
				return Manifest{}, nil, fmt.Errorf("%s: required host environment variable is unset or empty", bindingPath)
			}
			if strings.ContainsAny(value, "\r\n\x00") {
				return Manifest{}, nil, fmt.Errorf("%s: host value cannot contain CR, LF or NUL", bindingPath)
			}
			alias := fmt.Sprintf("%s%04d", componentEnvPrefix, len(transport)+1)
			ref.Env[target] = alias
			transport = append(transport, alias+"="+value)
		}
		manifest.Components.MCPs = append(manifest.Components.MCPs, ref)
	}
	return manifest, transport, nil
}

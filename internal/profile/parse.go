package profile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	keyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	envPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// ValidateKey accepts a portable file basename, never a path. The selector is
// also used in Docker volume names and remains independent of YAML name.
func ValidateKey(selector string) error {
	if !keyPattern.MatchString(selector) {
		return fmt.Errorf("profile selector: expected a basename beginning with a letter or digit and containing only letters, digits, '.', '_' or '-'")
	}
	return nil
}

// Parse validates the source schema without reading environment values or
// consulting the catalog. Load is responsible for the deprecated-skills warning.
func Parse(selector string, data []byte) (*Profile, error) {
	if err := ValidateKey(selector); err != nil {
		return nil, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		if err == io.EOF {
			// Empty legacy profiles are valid and inherit all defaults.
			return &Profile{Key: selector, Name: selector}, nil
		}
		// Decoder errors can contain user-supplied scalar or anchor text.
		return nil, fmt.Errorf("profile: invalid YAML document")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("profile: expected exactly one YAML document")
	}
	if len(document.Content) != 1 {
		return nil, fmt.Errorf("profile: expected a mapping")
	}
	root := document.Content[0]
	if root.Kind == yaml.ScalarNode && root.Tag == "!!null" {
		return &Profile{Key: selector, Name: selector}, nil
	}
	fields, err := mapping(root, "profile")
	if err != nil {
		return nil, err
	}
	p := &Profile{Key: selector}
	for i := 0; i < len(root.Content); i += 2 {
		key := root.Content[i].Value
		node := fields[key]
		switch key {
		case "name", "description", "provider":
			value, err := optionalString(node, key)
			if err != nil {
				return nil, err
			}
			switch key {
			case "name":
				p.Name = value
			case "description":
				p.Description = value
			case "provider":
				p.Provider = value
			}
		case "plugins":
			p.Plugins, err = nativePlugins(node)
		case "settings":
			p.Settings, err = settings(node)
		case "components":
			p.Components, err = components(node)
		case "skills":
			// This field remains ignored, including unsupported legacy shapes.
			// Match the old warning for a nonempty list of string names.
			var legacy []string
			p.deprecatedSkills = node.Decode(&legacy) == nil && len(legacy) > 0
		default:
			return nil, fmt.Errorf("%s: unsupported profile field", key)
		}
		if err != nil {
			return nil, err
		}
	}
	if p.Name == "" {
		p.Name = selector
	}
	return p, nil
}

func mapping(node *yaml.Node, path string) (map[string]*yaml.Node, error) {
	if node.Kind != yaml.MappingNode || node.Tag != "!!map" {
		return nil, fmt.Errorf("%s: expected a mapping", path)
	}
	fields := make(map[string]*yaml.Node, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
			return nil, fmt.Errorf("%s: expected string field names", path)
		}
		if _, exists := fields[key.Value]; exists {
			return nil, fmt.Errorf("%s.%s: duplicate field", path, key.Value)
		}
		fields[key.Value] = node.Content[i+1]
	}
	return fields, nil
}

func scalarString(node *yaml.Node, path string) (string, error) {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return "", fmt.Errorf("%s: expected a string", path)
	}
	return node.Value, nil
}

func optionalString(node *yaml.Node, path string) (string, error) {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!null" {
		return "", nil
	}
	return scalarString(node, path)
}

func nativePlugins(node *yaml.Node) ([]string, error) {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!null" {
		return nil, nil
	}
	if node.Kind != yaml.SequenceNode || node.Tag != "!!seq" {
		return nil, fmt.Errorf("plugins: expected a list of native plugin strings")
	}
	plugins := make([]string, 0, len(node.Content))
	for i, item := range node.Content {
		value, err := scalarString(item, fmt.Sprintf("plugins[%d]", i))
		if err != nil {
			return nil, err
		}
		plugins = append(plugins, value)
	}
	return plugins, nil
}

func settings(node *yaml.Node) (map[string]any, error) {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!null" {
		return nil, nil
	}
	if _, err := mapping(node, "settings"); err != nil {
		return nil, err
	}
	value, err := jsonValue(node, "settings")
	if err != nil {
		return nil, err
	}
	return value.(map[string]any), nil
}

// Decode JSON-compatible values explicitly so duplicate and non-string keys
// cannot disappear during YAML-to-JSON conversion. YAML aliases and custom tags
// are not part of the profile schema.
func jsonValue(node *yaml.Node, path string) (any, error) {
	switch node.Kind {
	case yaml.MappingNode:
		fields, err := mapping(node, path)
		if err != nil {
			return nil, err
		}
		out := make(map[string]any, len(fields))
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i].Value
			value, err := jsonValue(fields[key], path+"."+key)
			if err != nil {
				return nil, err
			}
			out[key] = value
		}
		return out, nil
	case yaml.SequenceNode:
		if node.Tag != "!!seq" {
			break
		}
		out := make([]any, 0, len(node.Content))
		for i, item := range node.Content {
			value, err := jsonValue(item, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return nil, err
			}
			out = append(out, value)
		}
		return out, nil
	case yaml.ScalarNode:
		switch node.Tag {
		case "!!str", "!!null", "!!bool", "!!int", "!!float", "!!timestamp":
			var value any
			if err := node.Decode(&value); err != nil {
				return nil, fmt.Errorf("%s: invalid scalar", path)
			}
			if _, err := json.Marshal(value); err != nil {
				return nil, fmt.Errorf("%s: value is not JSON-compatible", path)
			}
			return value, nil
		}
	}
	return nil, fmt.Errorf("%s: unsupported YAML value", path)
}

func components(node *yaml.Node) (Components, error) {
	var out Components
	fields, err := mapping(node, "components")
	if err != nil {
		return out, err
	}
	for i := 0; i < len(node.Content); i += 2 {
		kind := node.Content[i].Value
		list := fields[kind]
		path := "components." + kind
		switch kind {
		case "agents", "skills", "commands", "mcps", "mods", "plugins":
		default:
			return Components{}, fmt.Errorf("%s: unsupported component category", path)
		}
		if list.Kind != yaml.SequenceNode || list.Tag != "!!seq" {
			return Components{}, fmt.Errorf("%s: expected a list", path)
		}
		for index, item := range list.Content {
			itemPath := fmt.Sprintf("%s[%d]", path, index)
			id, env, err := reference(item, itemPath, kind == "mcps")
			if err != nil {
				return Components{}, err
			}
			ref := ComponentRef{ID: id}
			switch kind {
			case "agents":
				out.Agents = append(out.Agents, ref)
			case "skills":
				out.Skills = append(out.Skills, ref)
			case "commands":
				out.Commands = append(out.Commands, ref)
			case "mcps":
				out.MCPs = append(out.MCPs, MCPRef{ID: id, Env: env})
			case "mods":
				out.Mods = append(out.Mods, ref)
			case "plugins":
				out.Plugins = append(out.Plugins, ref)
			}
		}
	}
	return out, nil
}

func reference(node *yaml.Node, path string, mcp bool) (string, map[string]string, error) {
	var id string
	var env map[string]string
	if node.Kind == yaml.ScalarNode {
		value, err := scalarString(node, path)
		if err != nil {
			return "", nil, err
		}
		id = value
	} else {
		fields, err := mapping(node, path)
		if err != nil {
			return "", nil, err
		}
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i].Value
			if key != "id" && (!mcp || key != "env") {
				return "", nil, fmt.Errorf("%s.%s: unsupported reference field", path, key)
			}
		}
		idNode, ok := fields["id"]
		if !ok {
			return "", nil, fmt.Errorf("%s.id: required", path)
		}
		id, err = scalarString(idNode, path+".id")
		if err != nil {
			return "", nil, err
		}
		if envNode, ok := fields["env"]; ok {
			bindings, err := mapping(envNode, path+".env")
			if err != nil {
				return "", nil, err
			}
			env = make(map[string]string, len(bindings))
			for i := 0; i < len(envNode.Content); i += 2 {
				target := envNode.Content[i].Value
				if err := validateVariable(target, path+".env", "target"); err != nil {
					return "", nil, err
				}
				host, err := scalarString(bindings[target], path+".env."+target)
				if err != nil {
					return "", nil, err
				}
				if err := validateVariable(host, path+".env."+target, "host"); err != nil {
					return "", nil, err
				}
				env[target] = host
			}
		}
	}
	if err := validateID(id, path+".id"); err != nil {
		return "", nil, err
	}
	return id, env, nil
}

func validateID(id, path string) error {
	for _, segment := range strings.Split(id, "/") {
		if !keyPattern.MatchString(segment) {
			return fmt.Errorf("%s: expected a relative catalog ID with nonempty letter/digit-led path segments", path)
		}
	}
	return nil
}

func validateVariable(name, path, role string) error {
	if !envPattern.MatchString(name) {
		return fmt.Errorf("%s: invalid %s environment variable name", path, role)
	}
	if strings.HasPrefix(name, componentEnvPrefix) {
		return fmt.Errorf("%s: %s environment variable uses the reserved transport prefix", path, role)
	}
	return nil
}

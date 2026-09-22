package profile

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseAllComponentCategories(t *testing.T) {
	p, err := Parse("reviewer", []byte(`
name: Display name
description: Review code
provider: mm
plugins: [superpowers, example@marketplace, example@marketplace]
settings:
  agent: code-reviewer
  effortLevel: high
  enabled: true
  count: 3
  nested:
    list: [one, false, null]
components:
  agents: [development-tools/code-reviewer, {id: development-tools/other}]
  skills: [development-tools/skill, {id: development-tools/other-skill}]
  commands: [development-tools/command, {id: development-tools/other-command}]
  mcps:
    - integrations/first
    - id: integrations/second
      env:
        TOKEN: HOST_TOKEN
  mods: [tools/mod, {id: tools/other-mod}]
  plugins: [tools/plugin, {id: tools/other-plugin}]
`))
	if err != nil {
		t.Fatal(err)
	}
	if p.Key != "reviewer" || p.Name != "Display name" || p.Description != "Review code" || p.Provider != "mm" {
		t.Fatalf("legacy fields or canonical key were not preserved: %+v", p)
	}
	if !reflect.DeepEqual(p.Plugins, []string{"superpowers", "example@marketplace", "example@marketplace"}) {
		t.Fatalf("native plugins changed: %v", p.Plugins)
	}
	groups := [][]ComponentRef{p.Components.Agents, p.Components.Skills, p.Components.Commands, p.Components.Mods, p.Components.Plugins}
	for _, refs := range groups {
		if len(refs) != 2 || refs[0].ID == "" || refs[1].ID == "" {
			t.Fatalf("string/object references not parsed: %v", refs)
		}
	}
	if len(p.Components.MCPs) != 2 || p.Components.MCPs[1].Env["TOKEN"] != "HOST_TOKEN" {
		t.Fatalf("MCP reference not parsed: %+v", p.Components.MCPs)
	}
	wantSettings := `{"agent":"code-reviewer","count":3,"effortLevel":"high","enabled":true,"nested":{"list":["one",false,null]}}`
	settingsJSON, err := json.Marshal(p.Settings)
	if err != nil || string(settingsJSON) != wantSettings {
		t.Fatalf("settings = %s, err = %v", settingsJSON, err)
	}
}

func TestParseShortAndObjectReferencesAreEquivalent(t *testing.T) {
	for _, category := range []string{"agents", "skills", "commands", "mcps", "mods", "plugins"} {
		t.Run(category, func(t *testing.T) {
			short, err := Parse("test", []byte("components:\n  "+category+": [category/item]\n"))
			if err != nil {
				t.Fatal(err)
			}
			object, err := Parse("test", []byte("components:\n  "+category+": [{id: category/item}]\n"))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(short, object) {
				t.Fatalf("forms differ: %+v / %+v", short, object)
			}
		})
	}
}

func TestParseEmptyLegacyProfiles(t *testing.T) {
	for _, source := range []string{"", "# defaults only\n", "---\n", "null\n", "{}", "name: null\nplugins: null\nsettings: null\n"} {
		p, err := Parse("default", []byte(source))
		if err != nil {
			t.Fatalf("empty legacy profile rejected: %v", err)
		}
		if p.Key != "default" || p.Name != "default" {
			t.Fatalf("default identity lost: %+v", p)
		}
		if _, _, err := Normalize(p, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestValidateKey(t *testing.T) {
	for _, key := range []string{"reviewer", "dev-2", "team_review.v1", "8"} {
		if err := ValidateKey(key); err != nil {
			t.Errorf("valid key %q rejected: %v", key, err)
		}
	}
	for _, key := range []string{"", ".", "..", "../reviewer", "a/../b", "/reviewer", `a\b`, "a b", "-option", "a\nline", "a:mount", "$(id)"} {
		if err := ValidateKey(key); err == nil {
			t.Errorf("invalid key %q accepted", key)
		}
	}
}

func TestParseRejectsMalformedSchema(t *testing.T) {
	cases := []struct {
		name, source, field string
	}{
		{"invalid YAML", "components: [", "profile"},
		{"extra document", "name: test\n---\nname: other", "profile"},
		{"extra empty document", "name: test\n---\n", "profile"},
		{"root list", "[test]", "profile"},
		{"unknown root", "bootstrap: command", "bootstrap"},
		{"root duplicate", "name: first\nname: second", "profile.name"},
		{"numeric name", "name: 123", "name"},
		{"components string", "components: broken", "components"},
		{"unknown category", "components: {hooks: []}", "components.hooks"},
		{"duplicate category", "components: {agents: [], agents: []}", "components.agents"},
		{"wrong collection", "components: {agents: category/item}", "components.agents"},
		{"null collection", "components: {agents: null}", "components.agents"},
		{"null reference", "components: {agents: [null]}", "components.agents[0]"},
		{"numeric reference", "components: {agents: [123]}", "components.agents[0]"},
		{"list reference", "components: {agents: [[category/item]]}", "components.agents[0]"},
		{"missing id", "components: {agents: [{}]}", "components.agents[0].id"},
		{"numeric id", "components: {agents: [{id: 123}]}", "components.agents[0].id"},
		{"empty id", "components: {agents: [{id: ''}]}", "components.agents[0].id"},
		{"unknown ref field", "components: {agents: [{id: tools/item, source: URL}]}", "components.agents[0].source"},
		{"duplicate id", "components: {agents: [{id: a, id: b}]}", "components.agents[0].id"},
		{"non-MCP env", "components: {agents: [{id: tools/item, env: {TOKEN: HOST}}]}", "components.agents[0].env"},
		{"MCP env list", "components: {mcps: [{id: tools/item, env: [HOST]}]}", "components.mcps[0].env"},
		{"MCP env duplicate", "components: {mcps: [{id: tools/item, env: {TOKEN: FIRST, TOKEN: SECOND}}]}", "components.mcps[0].env.TOKEN"},
		{"invalid target", "components: {mcps: [{id: tools/item, env: {BAD-NAME: HOST}}]}", "components.mcps[0].env"},
		{"invalid host", "components: {mcps: [{id: tools/item, env: {TOKEN: '${HOST}'}}]}", "components.mcps[0].env.TOKEN"},
		{"numeric host", "components: {mcps: [{id: tools/item, env: {TOKEN: 123}}]}", "components.mcps[0].env.TOKEN"},
		{"reserved host", "components: {mcps: [{id: tools/item, env: {TOKEN: AIRUN_COMPONENT_ENV_0001}}]}", "components.mcps[0].env.TOKEN"},
		{"reserved target", "components: {mcps: [{id: tools/item, env: {AIRUN_COMPONENT_ENV_0001: HOST}}]}", "components.mcps[0].env"},
		{"native plugin object", "plugins: [{id: plugin}]", "plugins[0]"},
		{"native plugin number", "plugins: [123]", "plugins[0]"},
		{"settings list", "settings: []", "settings"},
		{"nested duplicate", "settings: {nested: {key: one, key: two}}", "settings.nested.key"},
		{"non-string setting key", "settings: {nested: {123: one}}", "settings.nested"},
		{"non-finite setting", "settings: {nested: .nan}", "settings.nested"},
		{"custom tag", "settings: {nested: !custom thing}", "settings.nested"},
		{"reference alias", "components: {agents: [&ref a, *ref]}", "components.agents[1]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse("test", []byte(tc.source))
			if err == nil || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("wanted field %q in error; got %v", tc.field, err)
			}
		})
	}
	for _, id := range []string{"/absolute", "../escape", "tools/../escape", "tools//item", "tools/", "https://example.test/item", "tools/item;cmd", "tools/$(cmd)", "tools/a b", `tools\item`} {
		t.Run("invalid ID "+id, func(t *testing.T) {
			data := "components:\n  skills:\n    - id: '" + id + "'\n"
			_, err := Parse("test", []byte(data))
			if err == nil || !strings.Contains(err.Error(), "components.skills[0].id") {
				t.Fatalf("invalid ID accepted or wrong error: %v", err)
			}
		})
	}
}

func TestLoadLegacySkillsWarnsAndIgnores(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".airun", "profiles")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "legacy.yaml"), []byte("skills: [old-skill]\nprovider: z\nsettings: {effortLevel: high}\nplugins: [native@market]"), 0600); err != nil {
		t.Fatal(err)
	}
	capture, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close()
	original := os.Stderr
	os.Stderr = capture
	t.Cleanup(func() { os.Stderr = original })
	p, loadErr := Load("legacy")
	os.Stderr = original
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if _, err := capture.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	warning, err := io.ReadAll(capture)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(warning), "deprecated 'skills' field") != 1 {
		t.Fatalf("missing or repeated deprecation warning: %s", warning)
	}
	if p.Key != "legacy" || p.Name != "legacy" || len(p.Components.Skills) != 0 || p.Provider != "z" {
		t.Fatalf("legacy profile changed: %+v", p)
	}
	manifest, _, err := Normalize(p, nil)
	if err != nil || len(manifest.Components.Skills) != 0 {
		t.Fatalf("deprecated skills contributed capabilities: %+v, %v", manifest, err)
	}
}

func TestLoadValidatesSelectorBeforeFileAccess(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := Load("../outside"); err == nil || !strings.Contains(err.Error(), "selector") {
		t.Fatalf("expected selector validation, got %v", err)
	}
	if _, err := Load("missing"); err == nil || !strings.Contains(err.Error(), `profile "missing" not found`) {
		t.Fatalf("expected missing-profile error, got %v", err)
	}
}

func TestShippedLegacyProfilesParse(t *testing.T) {
	paths, err := filepath.Glob("../../configs/profiles/*.yaml")
	if err != nil || len(paths) == 0 {
		t.Fatalf("cannot find shipped profiles: %v", err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			key := strings.TrimSuffix(filepath.Base(path), ".yaml")
			p, err := Parse(key, data)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := Normalize(p, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

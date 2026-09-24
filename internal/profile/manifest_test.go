package profile

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestManifestEmptyCollectionsAndStableIdentity(t *testing.T) {
	var previous []byte
	for _, display := range []string{"First name", "Changed display name"} {
		p, err := Parse("reviewer", []byte("name: "+display))
		if err != nil {
			t.Fatal(err)
		}
		manifest, transport, err := Normalize(p, nil)
		if err != nil || len(transport) != 0 {
			t.Fatalf("unexpected normalization: %v, %v", transport, err)
		}
		data, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		want := `{"version":1,"profile_key":"reviewer","settings":{},"native_plugins":[],"components":{"agents":[],"skills":[],"commands":[],"mcps":[],"mods":[],"plugins":[]}}`
		if string(data) != want {
			t.Fatalf("contract differs:\n%s\nwant:\n%s", data, want)
		}
		if previous != nil && string(previous) != string(data) {
			t.Fatal("display-name edit changed canonical manifest")
		}
		previous = data
	}
}

func TestNormalizeIndependentBindingsAndSecretFreeManifest(t *testing.T) {
	p, err := Parse("test", []byte(`
components:
  mcps:
    - id: integrations/first
      env:
        TOKEN: FIRST_HOST_NAME
        API_KEY: API_HOST_NAME
    - id: integrations/second
      env:
        TOKEN: SECOND_HOST_NAME
        ANTHROPIC_AUTH_TOKEN: CHILD_PROVIDER_NAME
`))
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{
		"FIRST_HOST_NAME":     "synthetic-first-secret",
		"API_HOST_NAME":       "synthetic-api=secret",
		"SECOND_HOST_NAME":    "synthetic-second-secret",
		"CHILD_PROVIDER_NAME": "synthetic-child-secret",
	}
	var lookedUp []string
	lookup := func(name string) (string, bool) {
		lookedUp = append(lookedUp, name)
		value, ok := values[name]
		return value, ok
	}
	manifest, transport, err := Normalize(p, lookup)
	if err != nil {
		t.Fatal(err)
	}
	wantNames := []string{"API_HOST_NAME", "FIRST_HOST_NAME", "CHILD_PROVIDER_NAME", "SECOND_HOST_NAME"}
	if !reflect.DeepEqual(lookedUp, wantNames) {
		t.Fatalf("lookup order = %v, want %v", lookedUp, wantNames)
	}
	wantTransport := []string{
		"AIRUN_COMPONENT_ENV_0001=synthetic-api=secret",
		"AIRUN_COMPONENT_ENV_0002=synthetic-first-secret",
		"AIRUN_COMPONENT_ENV_0003=synthetic-child-secret",
		"AIRUN_COMPONENT_ENV_0004=synthetic-second-secret",
	}
	if !reflect.DeepEqual(transport, wantTransport) {
		t.Fatal("transport entries do not match expected binding order and values")
	}
	if manifest.Components.MCPs[0].Env["TOKEN"] != "AIRUN_COMPONENT_ENV_0002" || manifest.Components.MCPs[1].Env["TOKEN"] != "AIRUN_COMPONENT_ENV_0004" {
		t.Fatalf("target bindings collided: %+v", manifest.Components.MCPs)
	}
	if p.Components.MCPs[0].Env["TOKEN"] != "FIRST_HOST_NAME" {
		t.Fatal("normalization mutated source mappings")
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range values {
		if strings.Contains(string(data), name) || strings.Contains(string(data), value) {
			t.Fatal("manifest contains a source variable name or credential value")
		}
	}
	for _, entry := range transport {
		if !strings.HasPrefix(entry, "AIRUN_COMPONENT_ENV_") {
			t.Fatal("component target escaped into global environment")
		}
	}
	second, again, err := Normalize(p, lookup)
	if err != nil || !reflect.DeepEqual(manifest, second) || !reflect.DeepEqual(transport, again) {
		t.Fatal("normalization is not deterministic")
	}
}

func TestNormalizeRejectsUnsafeValuesWithoutDisclosure(t *testing.T) {
	cases := []struct {
		name, value string
		present     bool
	}{
		{"missing", "", false},
		{"empty", "", true},
		{"line feed", "canary-secret\nANTHROPIC_AUTH_TOKEN=injected", true},
		{"carriage return", "canary-secret\rnext", true},
		{"NUL", "canary-secret\x00next", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &Profile{Key: "test", Components: Components{MCPs: []MCPRef{
				{ID: "tools/first", Env: map[string]string{"TOKEN": "GOOD_HOST"}},
				{ID: "tools/second", Env: map[string]string{"TOKEN": "SECRET_HOST"}},
			}}}
			manifest, transport, err := Normalize(p, func(name string) (string, bool) {
				if name == "GOOD_HOST" {
					return "first-canary-secret", true
				}
				return tc.value, tc.present
			})
			if err == nil || !strings.Contains(err.Error(), "components.mcps[1].env.TOKEN") {
				t.Fatalf("expected field-specific error, got %v", err)
			}
			if strings.Contains(err.Error(), "canary-secret") || strings.Contains(err.Error(), "SECRET_HOST") || strings.Contains(err.Error(), "injected") {
				t.Fatal("error disclosed source name or credential value")
			}
			if !reflect.DeepEqual(manifest, Manifest{}) || transport != nil {
				t.Fatal("failed normalization returned partial outputs")
			}
		})
	}
}

func TestNormalizeValidatesProgrammaticProfiles(t *testing.T) {
	cases := []struct {
		name    string
		profile *Profile
	}{
		{"nil", nil},
		{"missing key", &Profile{}},
		{"invalid native plugin", &Profile{Key: "test", Plugins: []string{"example@"}}},
		{"invalid component", &Profile{Key: "test", Components: Components{Agents: []ComponentRef{{ID: "../outside"}}}}},
		{"invalid MCP", &Profile{Key: "test", Components: Components{MCPs: []MCPRef{{ID: ""}}}}},
		{"invalid target", &Profile{Key: "test", Components: Components{MCPs: []MCPRef{{ID: "tools/item", Env: map[string]string{"BAD-NAME": "HOST"}}}}}},
		{"invalid source", &Profile{Key: "test", Components: Components{MCPs: []MCPRef{{ID: "tools/item", Env: map[string]string{"TOKEN": "${HOST}"}}}}}},
		{"reserved target", &Profile{Key: "test", Components: Components{MCPs: []MCPRef{{ID: "tools/item", Env: map[string]string{"AIRUN_COMPONENT_ENV_0001": "HOST"}}}}}},
		{"reserved source", &Profile{Key: "test", Components: Components{MCPs: []MCPRef{{ID: "tools/item", Env: map[string]string{"TOKEN": "AIRUN_COMPONENT_ENV_0001"}}}}}},
		{"non-JSON settings", &Profile{Key: "test", Settings: map[string]any{"function": func() {}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := Normalize(tc.profile, func(string) (string, bool) {
				return "synthetic-secret", true
			})
			if err == nil {
				t.Fatal("invalid profile accepted")
			}
		})
	}
	p := &Profile{Key: "test", Components: Components{MCPs: []MCPRef{{ID: "tools/item", Env: map[string]string{"TOKEN": "HOST"}}}}}
	if _, _, err := Normalize(p, nil); err == nil {
		t.Fatal("binding accepted without environment lookup")
	}
}

func TestParseDoesNotEchoMalformedCredentialValues(t *testing.T) {
	for _, source := range []string{
		"components: {mcps: [{id: tools/item, env: {TOKEN: 'secret-canary!'}}]}",
		"components: {mcps: [{id: tools/item, env: {TOKEN: [secret-canary]}}]}",
		"components: {mcps: [*secret-canary]}",
	} {
		_, err := Parse("test", []byte(source))
		if err == nil || strings.Contains(err.Error(), "secret-canary") {
			t.Fatalf("unsafe schema diagnostic: %v", err)
		}
	}
}

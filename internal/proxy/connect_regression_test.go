package proxy

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestConnectDisconnectClientsPreserveSettings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"test-model"}]}`))
	}))
	defer server.Close()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\necho '2.1.80 (Claude Code)'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	clients := []string{"go", "bash", "pwsh"}
	for _, connect := range clients {
		for _, disconnect := range clients {
			for _, scenario := range []string{"existing", "new", "user-edits", "new-user-edits"} {
				t.Run(connect+"-"+disconnect+"/"+scenario, func(t *testing.T) {
					for _, client := range []string{connect, disconnect} {
						if client != "go" {
							if _, err := exec.LookPath(client); err != nil {
								t.Skip(client + " unavailable")
							}
						}
					}
					home := t.TempDir()
					t.Setenv("HOME", home)
					t.Setenv("USERPROFILE", home)
					initialSettings := map[string]any{"env": map[string]any{"ANTHROPIC_AUTH_TOKEN": "original", "API_TIMEOUT_MS": "123", "UNRELATED": "keep"}, "permissions": map[string]any{"allow": []any{"Read"}}}
					initialClaude := map[string]any{"userID": "original-user", "projects": map[string]any{"/project": map[string]any{"trusted": true}}, "hasCompletedOnboarding": false, "customApiKeyResponses": map[string]any{"approved": []any{"original-approval"}, "rejected": []any{"rejected-key"}}}
					isNew := strings.HasPrefix(scenario, "new")
					if !isNew {
						if err := writeSettings(claudeSettingsPath(), initialSettings); err != nil {
							t.Fatal(err)
						}
						if err := writeSettings(claudeJSONPath(), initialClaude); err != nil {
							t.Fatal(err)
						}
					} else {
						initialSettings = map[string]any{}
						initialClaude = map[string]any{}
					}
					run := func(client, action string) {
						t.Helper()
						if client == "go" {
							var err error
							if action == "connect" {
								err = Connect(server.URL, "sk-ai-test-token")
							} else {
								err = Disconnect()
							}
							if err != nil {
								t.Fatal(err)
							}
							return
						}
						args := []string{"../../scripts/connect-proxy.sh"}
						if client == "pwsh" {
							args = []string{"-NoProfile", "-File", "../../scripts/connect-proxy.ps1"}
						}
						if action == "connect" {
							args = append(args, server.URL, "sk-ai-test-token")
						} else {
							args = append(args, "--disconnect")
						}
						if out, err := exec.Command(client, args...).CombinedOutput(); err != nil {
							t.Fatalf("%s %s: %v\n%s", client, action, err, out)
						}
					}
					// Disconnect without connect is harmless; repeated connect keeps the original journal.
					run(disconnect, "disconnect")
					run(connect, "connect")
					run(connect, "connect")
					if strings.Contains(scenario, "user-edits") {
						settings, _ := readSettings(claudeSettingsPath())
						settings["env"].(map[string]any)["ANTHROPIC_BASE_URL"] = "user-edited-url"
						settings["theme"] = "dark"
						if err := writeSettings(claudeSettingsPath(), settings); err != nil {
							t.Fatal(err)
						}
						if initialSettings["env"] == nil {
							initialSettings["env"] = map[string]any{}
						}
						initialSettings["env"].(map[string]any)["ANTHROPIC_BASE_URL"] = "user-edited-url"
						initialSettings["theme"] = "dark"
						cj, _ := readSettings(claudeJSONPath())
						cj["projects"].(map[string]any)["/new"] = map[string]any{"trusted": true}
						car := cj["customApiKeyResponses"].(map[string]any)
						car["approved"] = append(car["approved"].([]any), "later-approval")
						if err := writeSettings(claudeJSONPath(), cj); err != nil {
							t.Fatal(err)
						}
						if initialClaude["projects"] == nil {
							initialClaude["projects"] = map[string]any{}
						}
						initialClaude["projects"].(map[string]any)["/new"] = map[string]any{"trusted": true}
						if initialClaude["customApiKeyResponses"] == nil {
							initialClaude["customApiKeyResponses"] = map[string]any{"approved": []any{}}
						}
						original := initialClaude["customApiKeyResponses"].(map[string]any)
						original["approved"] = append(original["approved"].([]any), "later-approval")
					}
					run(disconnect, "disconnect")
					run(disconnect, "disconnect")
					for path, want := range map[string]map[string]any{claudeSettingsPath(): initialSettings, claudeJSONPath(): initialClaude} {
						got, err := readSettings(path)
						if err != nil {
							t.Fatal(err)
						}
						if !reflect.DeepEqual(got, want) {
							t.Errorf("%s\ngot: %#v\nwant: %#v", path, got, want)
						}
					}
				})
			}
		}
	}
}

func TestDisconnectLeavesLegacyMarkerAndUnmanagedFilesUntouched(t *testing.T) {
	for _, contents := range []string{`{"_airunManaged":true,"userID":"keep","projects":{"x":{}}}`, `{"customApiKeyResponses":{"approved":["keep"]}}`} {
		path := filepath.Join(t.TempDir(), ".claude.json")
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		changed, err := cleanManagedSettings(path)
		if err != nil || changed {
			t.Fatalf("changed=%v err=%v", changed, err)
		}
		got, _ := os.ReadFile(path)
		if string(got) != contents {
			t.Fatal("unmanaged file changed")
		}
	}
}

func TestConnectRejectsDamagedDocumentsWithoutOverwriting(t *testing.T) {
	for _, content := range []string{"null", "[]", "{broken"} {
		path := filepath.Join(t.TempDir(), "settings.json")
		os.WriteFile(path, []byte(content), 0600)
		if err := mergeClaudeSettings(path, "url", "token", "model"); err == nil {
			t.Fatal("expected parse error")
		}
		if err := writeClaudeJSON(path, "token"); err == nil {
			t.Fatal("expected parse error")
		}
		got, _ := os.ReadFile(path)
		if string(got) != content {
			t.Fatal("damaged document overwritten")
		}
	}
}

func TestConnectAcceptsWindowsPowerShellUTF8BOM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	content := append([]byte{0xef, 0xbb, 0xbf}, []byte(`{"env":{"ANTHROPIC_AUTH_TOKEN":"original"}}`)...)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	if err := mergeClaudeSettings(path, "url", "token", "model"); err != nil {
		t.Fatal(err)
	}
	if _, err := cleanManagedSettings(path); err != nil {
		t.Fatal(err)
	}
	settings, err := readSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if settings["env"].(map[string]any)["ANTHROPIC_AUTH_TOKEN"] != "original" {
		t.Fatal("BOM file lost prior env")
	}
}

func TestProxyInitPreservesExistingFilesAndRepairsPartialInit(t *testing.T) {
	for _, files := range []string{"none", "config", "users", "both"} {
		t.Run(files, func(t *testing.T) {
			base := filepath.Join(t.TempDir(), ".airun")
			config, users := filepath.Join(base, "proxy.yaml"), filepath.Join(base, "users.json")
			original := map[string][]byte{}
			if files != "none" {
				os.MkdirAll(base, 0700)
			}
			if files == "config" || files == "both" {
				original[config] = []byte("# custom YAML\nrpm: 17\n")
			}
			if files == "users" || files == "both" {
				original[users] = []byte("[ {\"name\":\"keep\",\"active\":false} ]\n")
			}
			for p, data := range original {
				os.WriteFile(p, data, 0600)
			}
			err := Init(config, users)
			if (err != nil) != (files == "both") {
				t.Fatalf("Init: %v", err)
			}
			for p, want := range original {
				got, _ := os.ReadFile(p)
				if string(got) != string(want) {
					t.Fatalf("%s overwritten", p)
				}
			}
			for _, p := range []string{config, users} {
				info, err := os.Stat(p)
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatalf("%s mode/error: %v %v", p, info, err)
				}
			}
		})
	}
	t.Run("failure publishes neither file", func(t *testing.T) {
		base := t.TempDir()
		blocker := filepath.Join(base, "not-a-directory")
		os.WriteFile(blocker, []byte("keep"), 0600)
		config := filepath.Join(base, "new", "proxy.yaml")
		if err := Init(config, filepath.Join(blocker, "users.json")); err == nil {
			t.Fatal("expected error")
		}
		if _, err := os.Stat(config); !os.IsNotExist(err) {
			t.Fatal("partial config published")
		}
	})
}

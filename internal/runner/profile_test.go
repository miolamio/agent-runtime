package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/miolamio/agent-runtime/internal/config"
	"github.com/miolamio/agent-runtime/internal/profile"
)

func TestProfileManifestHandoff(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TEST_PROFILE_TOKEN", "synthetic-private-value")
	p, err := profile.Parse("reviewer", []byte(`name: Display name
plugins: [superpowers@another-marketplace]
settings:
  effortLevel: high
components:
  mcps:
    - id: integration/example
      env:
        TOKEN: TEST_PROFILE_TOKEN
`))
	if err != nil {
		t.Fatal(err)
	}
	volumes, path, env, err := profileMounts(p, "prepare")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	if !reflect.DeepEqual(volumes, []string{path + ":" + profileManifestPath + ":ro"}) {
		t.Fatalf("unexpected volumes: %v", volumes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, excluded := range []string{"synthetic-private-value", "TEST_PROFILE_TOKEN"} {
		if strings.Contains(string(data), excluded) {
			t.Fatal("secret or host binding leaked into manifest")
		}
	}
	var manifest profile.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.ProfileKey != "reviewer" || !reflect.DeepEqual(manifest.NativePlugins, p.Plugins) {
		t.Fatal("identity or native plugin references changed during transport")
	}
	if !strings.Contains(strings.Join(env, "\n"), "AIRUN_COMPONENT_ENV_0001=synthetic-private-value") {
		t.Fatal("credential alias was not forwarded")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0644 {
		t.Fatalf("manifest must be readable by container UID: %v", err)
	}
	info, err = os.Stat(filepath.Dir(path))
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("manifest host parent must be private: %v", err)
	}
}

func TestUpdateProfileRunsPreparationOnlyAndCleansTemporaryFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	profiles := filepath.Join(home, ".airun", "profiles")
	if err := os.MkdirAll(profiles, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ART25_MISSING_MCP_TOKEN", "")
	if err := os.WriteFile(filepath.Join(profiles, "reviewer.yaml"), []byte("name: Reviewer\ncomponents:\n  agents: [development-tools/code-reviewer]\n  mcps: [{id: integration/example, env: {TOKEN: ART25_MISSING_MCP_TOKEN}}]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(home, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
if [ "$1" = 'image' ]; then
  printf '%s\n' "${TEST_MANIFEST_VERSION:-1}"
  exit 0
fi
printf '%s\n' "$@" > "$HOME/docker-args"
previous=''
for arg do
  if [ "$previous" = '--env-file' ]; then
    cp "$arg" "$HOME/docker-env"
  fi
  previous="$arg"
done
exit "${TEST_DOCKER_EXIT:-0}"
`
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg := &config.Config{ZaiAPIKey: "provider-secret-must-not-be-forwarded"}
	if err := UpdateProfile(cfg, "reviewer"); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(filepath.Join(home, "docker-args"))
	if err != nil {
		t.Fatal(err)
	}
	for _, excluded := range []string{":/workspace", "airun-state-reviewer", "\nclaude\n", "\n-p\n"} {
		if strings.Contains(string(args), excluded) {
			t.Fatalf("update attached session resources or agent command: %q", excluded)
		}
	}
	if !strings.Contains(string(args), componentVolumeName+":"+componentMountPath) {
		t.Fatal("update did not attach retained artifacts")
	}
	env, err := os.ReadFile(filepath.Join(home, "docker-env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env), "AIRUN_PROFILE_ACTION=update") || strings.Contains(string(env), "provider-secret") || strings.Contains(string(env), "AIRUN_COMPONENT_ENV_") {
		t.Fatal("invalid preparation-only environment")
	}
	assertNoProfileTemporaryFiles(t, home)
	t.Setenv("TEST_MANIFEST_VERSION", "0")
	if err := UpdateProfile(cfg, "reviewer"); err == nil || !strings.Contains(err.Error(), "rebuild") {
		t.Fatal("obsolete image did not produce a rebuild diagnostic")
	}
	assertNoProfileTemporaryFiles(t, home)
	t.Setenv("TEST_MANIFEST_VERSION", "1")
	t.Setenv("TEST_DOCKER_EXIT", "9")
	if err := UpdateProfile(cfg, "reviewer"); err == nil {
		t.Fatal("preparation failure was swallowed")
	}
	assertNoProfileTemporaryFiles(t, home)
}

func assertNoProfileTemporaryFiles(t *testing.T, home string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(home, ".airun", "tmp", ".airun-*"))
	if err != nil || len(files) != 0 {
		t.Fatalf("profile temporary files remain: %v (%v)", files, err)
	}
}

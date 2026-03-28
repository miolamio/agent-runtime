// internal/keys/envfile_test.go
package keys

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadEnvKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".airun.env")
	os.WriteFile(path, []byte("FOO=bar\nBAZ=qux\n"), 0600)

	val, err := ReadEnvKey(path, "FOO")
	if err != nil {
		t.Fatal(err)
	}
	if val != "bar" {
		t.Errorf("ReadEnvKey FOO = %q, want %q", val, "bar")
	}

	val, _ = ReadEnvKey(path, "MISSING")
	if val != "" {
		t.Errorf("ReadEnvKey MISSING = %q, want empty", val)
	}
}

func TestUpdateEnvKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".airun.env")
	os.WriteFile(path, []byte("# comment\nFOO=old\nBAR=keep\n"), 0600)

	err := UpdateEnvKey(path, "FOO", "new")
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "FOO=new") {
		t.Errorf("expected FOO=new, got:\n%s", content)
	}
	if !strings.Contains(content, "BAR=keep") {
		t.Errorf("expected BAR=keep preserved, got:\n%s", content)
	}
	if !strings.Contains(content, "# comment") {
		t.Errorf("expected comment preserved, got:\n%s", content)
	}
}

func TestUpdateEnvKeyAppends(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".airun.env")
	os.WriteFile(path, []byte("FOO=bar\n"), 0600)

	err := UpdateEnvKey(path, "NEW_KEY", "value")
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "NEW_KEY=value") {
		t.Errorf("expected NEW_KEY=value appended, got:\n%s", string(data))
	}
}

func TestReadEnvKeyWithEqualsInValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".airun.env")
	os.WriteFile(path, []byte("URL=https://example.com/path?key=value\n"), 0600)

	val, err := ReadEnvKey(path, "URL")
	if err != nil {
		t.Fatal(err)
	}
	if val != "https://example.com/path?key=value" {
		t.Errorf("ReadEnvKey URL = %q, want %q", val, "https://example.com/path?key=value")
	}
}

func TestReadAllEnvKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".airun.env")
	os.WriteFile(path, []byte("A=1\n# comment\nB=2\n"), 0600)

	kv, err := ReadAllEnvKeys(path)
	if err != nil {
		t.Fatal(err)
	}
	if kv["A"] != "1" || kv["B"] != "2" {
		t.Errorf("unexpected kv: %v", kv)
	}
	if _, ok := kv["# comment"]; ok {
		t.Error("comments should not be in result")
	}
}

package users

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestSaveAtomicPreservesOnFailure proves that a failed Save (here forced by a
// read-only parent directory) leaves the existing users.json byte-for-byte
// unchanged and leaves no stray temp files behind.
func TestSaveAtomicPreservesOnFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX dir perms not enforced on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses dir-mode permission checks")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	mgr := New(path)
	if _, err := mgr.Add("Ivanov"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile baseline: %v", err)
	}

	// Force CreateTemp inside Save to fail by stripping write on the parent dir.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("Chmod ro: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	// Mutate in-memory state and attempt to persist.
	mgr.mu.Lock()
	mgr.users = append(mgr.users, User{Name: "Petrov", Token: "garbage", Active: true})
	mgr.mu.Unlock()

	if err := mgr.Save(); err == nil {
		t.Fatal("Save unexpectedly succeeded with read-only parent dir")
	}

	// Restore so we can read back and inspect.
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("Chmod restore: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after failed Save: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("users.json mutated after failed Save\nwant:\n%s\ngot:\n%s", want, got)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() == "users.json" {
			continue
		}
		if strings.HasPrefix(e.Name(), ".users-") && strings.HasSuffix(e.Name(), ".json.tmp") {
			t.Errorf("stray temp file left behind: %s", e.Name())
		}
	}
}

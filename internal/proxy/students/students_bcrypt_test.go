package students

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Newly-added users must be stored with a bcrypt hash, never SHA-256.
func TestAdd_PersistsBcryptHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "students.json")
	mgr := New(path)

	tok, err := mgr.Add("alice")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var got []Student
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 user, got %d", len(got))
	}
	if !isBcryptHash(got[0].Token) {
		t.Errorf("stored token is not bcrypt-shaped: %q", got[0].Token)
	}
	if got[0].Token == tok {
		t.Error("stored token is plaintext — security invariant broken")
	}
}

// A legacy students.json containing SHA-256 hashes must continue to authenticate
// so existing v0.6.0/v0.6.1 installations work after the bcrypt upgrade.
func TestFindByToken_BackwardCompatSHA256(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "students.json")

	tok := "sk-ai-legacy-token-abc"
	legacy := []Student{
		{
			Name:      "bob",
			Token:     HashToken(tok),
			Active:    true,
			CreatedAt: time.Now().UTC(),
		},
	}
	raw, _ := json.MarshalIndent(legacy, "", "  ")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}

	mgr := New(path)
	s := mgr.FindByToken(tok)
	if s == nil {
		t.Fatal("legacy SHA-256 token did not authenticate")
	}
	if s.Name != "bob" {
		t.Errorf("got name %q, want bob", s.Name)
	}
}

// On a successful SHA-256 match, the stored hash must be transparently upgraded
// to bcrypt and persisted to disk. A second auth against the on-disk state
// confirms the upgrade took effect.
func TestFindByToken_TransparentBcryptUpgrade(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "students.json")

	tok := "sk-ai-to-upgrade-xyz"
	initial := []Student{{
		Name: "carol", Token: HashToken(tok), Active: true, CreatedAt: time.Now().UTC(),
	}}
	raw, _ := json.MarshalIndent(initial, "", "  ")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}

	mgr := New(path)
	if s := mgr.FindByToken(tok); s == nil {
		t.Fatal("initial auth failed")
	}

	// Upgrade runs in a goroutine — give it a moment.
	waitFor(t, time.Second, func() bool {
		data, err := os.ReadFile(path)
		if err != nil {
			return false
		}
		var got []Student
		if err := json.Unmarshal(data, &got); err != nil {
			return false
		}
		return len(got) == 1 && isBcryptHash(got[0].Token)
	}, "on-disk token never upgraded to bcrypt")

	// A fresh Manager reading the upgraded file still authenticates.
	mgr2 := New(path)
	if s := mgr2.FindByToken(tok); s == nil {
		t.Fatal("auth failed after bcrypt upgrade persisted")
	}
}

// Concurrent auth requests for the same legacy token must not corrupt
// students.json — exactly one upgrade wins, the rest no-op.
func TestFindByToken_UpgradeIsConcurrencySafe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "students.json")

	tok := "sk-ai-concurrent-upgrade"
	initial := []Student{{
		Name: "dave", Token: HashToken(tok), Active: true, CreatedAt: time.Now().UTC(),
	}}
	raw, _ := json.MarshalIndent(initial, "", "  ")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	mgr := New(path)

	const workers = 16
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s := mgr.FindByToken(tok); s == nil {
				t.Error("concurrent auth failed")
			}
		}()
	}
	wg.Wait()

	// Eventually the stored hash is bcrypt.
	waitFor(t, 2*time.Second, func() bool {
		data, err := os.ReadFile(path)
		if err != nil {
			return false
		}
		var got []Student
		if err := json.Unmarshal(data, &got); err != nil {
			return false
		}
		return len(got) == 1 && isBcryptHash(got[0].Token)
	}, "concurrent upgrade did not converge on bcrypt")

	// A fresh reader still authenticates.
	mgr2 := New(path)
	if s := mgr2.FindByToken(tok); s == nil {
		t.Fatal("auth broke after concurrent upgrade")
	}
}

// Load() migrates plaintext sk-ai- tokens to bcrypt (skipping SHA-256 so the
// plaintext never round-trips through a less-secure format).
func TestLoad_PlaintextMigratesToBcrypt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "students.json")

	plaintext := "sk-ai-needs-migration"
	legacy := []Student{{Name: "eve", Token: plaintext, Active: true, CreatedAt: time.Now().UTC()}}
	raw, _ := json.MarshalIndent(legacy, "", "  ")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}

	mgr := New(path)

	// In-memory state: not plaintext, is bcrypt.
	if s := mgr.FindByToken(plaintext); s == nil {
		t.Fatal("plaintext→bcrypt migration broke auth")
	}

	// On-disk state: not plaintext, is bcrypt.
	data, _ := os.ReadFile(path)
	var got []Student
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 user, got %d", len(got))
	}
	if got[0].Token == plaintext {
		t.Fatal("plaintext token is still on disk — security regression")
	}
	if !isBcryptHash(got[0].Token) {
		t.Errorf("migrated token is not bcrypt: %q", got[0].Token)
	}
}

// waitFor polls cond every 10ms up to timeout, t.Fatal'ing with msg if the
// deadline expires first. Used for goroutine-driven effects like the async
// upgrade path.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(msg)
}

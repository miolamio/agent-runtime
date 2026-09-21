package users

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func seedUsers(t testing.TB, path string, records []User) {
	t.Helper()
	data, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestStaleUpgradePreservesRevocationAndAddedUsers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	token := "sk-ai-legacy-to-revoke"
	seedUsers(t, path, []User{{Name: "alice", Token: HashToken(token), Active: true}})
	stale := New(path)
	admin := New(path)
	if err := admin.Revoke("alice"); err != nil {
		t.Fatal(err)
	}
	newToken, err := admin.Add("bob")
	if err != nil {
		t.Fatal(err)
	}
	stale.upgradeToBcrypt("alice", HashToken(token), token)
	fresh := New(path)
	if fresh.FindByToken(token) != nil {
		t.Fatal("migration resurrected revoked token")
	}
	if fresh.FindByToken(newToken) == nil {
		t.Fatal("migration lost newly added user")
	}
	if !isBcryptHash(fresh.List()[0].Token) {
		t.Fatal("legacy upgrade was not persisted")
	}
}

func TestDifferentManagersDoNotLoseConcurrentChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	seedUsers(t, path, []User{})
	const count = 24
	managers := make([]*Manager, count)
	for i := range managers {
		managers[i] = New(path)
	}
	var wg sync.WaitGroup
	for i, mgr := range managers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := mgr.Add(fmt.Sprintf("user-%d", i)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := len(New(path).List()); got != count {
		t.Fatalf("users=%d want=%d", got, count)
	}
	for i, mgr := range managers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := mgr.Revoke(fmt.Sprintf("user-%d", i)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for _, u := range New(path).List() {
		if u.Active {
			t.Fatalf("revocation lost: %s", u.Name)
		}
	}
	for i, mgr := range managers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := mgr.Restore(fmt.Sprintf("user-%d", i)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for _, u := range New(path).List() {
		if !u.Active {
			t.Fatalf("restore lost: %s", u.Name)
		}
	}
}

func TestUserMutationProcess(t *testing.T) {
	path := os.Getenv("ART_USER_TEST_PATH")
	if path == "" {
		return
	}
	mgr := New(path)
	// Parent starts all workers with the same snapshot before allowing writes.
	if err := os.WriteFile(os.Getenv("ART_USER_READY"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(path + ".go"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("start gate timeout")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := mgr.Add(os.Getenv("ART_USER_TEST_NAME")); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentProcessesDoNotOverwriteSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	seedUsers(t, path, []User{})
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var commands []*exec.Cmd
	for i := 0; i < 8; i++ {
		name := fmt.Sprintf("process-%d", i)
		cmd := exec.Command(exe, "-test.run=^TestUserMutationProcess$")
		cmd.Env = append(os.Environ(), "ART_USER_TEST_PATH="+path, "ART_USER_TEST_NAME="+name, "ART_USER_READY="+path+"."+name)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, cmd)
	}
	t.Cleanup(func() {
		for _, cmd := range commands {
			if cmd.ProcessState == nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		}
	})
	waitFor(t, 10*time.Second, func() bool {
		for i := range commands {
			if _, err := os.Stat(fmt.Sprintf("%s.process-%d", path, i)); err != nil {
				return false
			}
		}
		return true
	}, "workers not ready")
	os.WriteFile(path+".go", nil, 0600)
	for _, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(New(path).List()); got != len(commands) {
		t.Fatalf("users=%d want=%d", got, len(commands))
	}
}

func TestMutationsRejectCorruptStoreAndKeepMemoryOnFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	mgr := New(path)
	token, err := mgr.Add("alice")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte("broken JSON"), 0600)
	for _, m := range []*Manager{mgr, New(path)} {
		if _, err := m.Add("bob"); err == nil {
			t.Fatal("Add accepted corrupted store")
		}
		if err := m.Revoke("alice"); err == nil {
			t.Fatal("Revoke accepted corrupted store")
		}
		if err := m.Restore("alice"); err == nil {
			t.Fatal("Restore accepted corrupted store")
		}
		if err := m.Save(); err == nil {
			t.Fatal("Save accepted corrupted store")
		}
	}
	got, _ := os.ReadFile(path)
	if string(got) != "broken JSON" {
		t.Fatal("corruption overwritten")
	}
	if u := mgr.FindByToken(token); u != nil {
		t.Fatal("unreadable store must fail closed")
	}
	if !mgr.List()[0].Active {
		t.Fatal("failed revoke mutated memory")
	}
}

func TestLiveManagerObservesExternalRevokeAndRestore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	admin := New(path)
	token, err := admin.Add("alice")
	if err != nil {
		t.Fatal(err)
	}
	live := New(path)
	if live.FindByToken(token) == nil {
		t.Fatal("initial auth failed")
	}
	if err := admin.Revoke("alice"); err != nil {
		t.Fatal(err)
	}
	if live.FindByToken(token) != nil {
		t.Fatal("external revoke requires reload")
	}
	if err := admin.Restore("alice"); err != nil {
		t.Fatal(err)
	}
	if live.FindByToken(token) == nil {
		t.Fatal("external restore not observed")
	}
}

func TestLegacyBcryptAuthenticationUsesBoundedResumableScans(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	token := "sk-ai-last-legacy-user"
	wrong, _ := HashTokenBcrypt("another-token")
	right, _ := HashTokenBcrypt(token)
	var records []User
	for i := 0; i < maxLegacyChecks*2; i++ {
		records = append(records, User{Name: fmt.Sprint(i), Token: wrong, Active: true})
	}
	records = append(records, User{Name: "last", Token: right, Active: true})
	seedUsers(t, path, records)
	mgr := New(path)
	for i := 0; i < 2; i++ {
		u, err := mgr.Authenticate(token)
		if u != nil || !errors.Is(err, ErrAuthBusy) {
			t.Fatalf("slice %d: user=%v err=%v", i, u, err)
		}
	}
	u, err := mgr.Authenticate(token)
	if err != nil || u == nil || u.Name != "last" {
		t.Fatalf("resumed auth: %v %v", u, err)
	}
	if New(path).FindByToken(token) == nil {
		t.Fatal("legacy fingerprint not persisted")
	}
	if u, err := mgr.Authenticate("unknown"); u != nil || !errors.Is(err, ErrAuthBusy) {
		t.Fatalf("unknown token did not yield after bounded work: %v %v", u, err)
	}
	// Admission is independent of the expensive loop.
	mgr.legacySlot <- struct{}{}
	if _, err := mgr.Authenticate("unknown-two"); !errors.Is(err, ErrAuthBusy) {
		t.Fatal("unbounded legacy concurrency")
	}
	<-mgr.legacySlot
}

func BenchmarkUnknownTokenProductionCost(b *testing.B) {
	// TestMain lowers the global cost; generate fixtures explicitly at cost 10.
	hash, err := bcrypt.GenerateFromPassword([]byte("valid-random-credential"), bcrypt.DefaultCost)
	if err != nil {
		b.Fatal(err)
	}
	for _, count := range []int{1, 25, 250} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "users.json")
			records := make([]User, count)
			for i := range records {
				records[i] = User{Name: fmt.Sprint(i), Token: string(hash), TokenID: HashToken(fmt.Sprint(i)), Active: true}
			}
			seedUsers(b, path, records)
			mgr := New(path)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if u, err := mgr.Authenticate("unknown-random-credential"); u != nil || err != nil {
					b.Fatalf("%v %v", u, err)
				}
			}
		})
	}
}

func TestTokenFingerprintIsNotAcceptedAsCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	mgr := New(path)
	token, err := mgr.Add("alice")
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{HashToken(token), mgr.List()[0].Token, strings.Repeat("x", 73)} {
		if mgr.FindByToken(invalid) != nil {
			t.Fatal("accepted stored hash as credential")
		}
	}
}

func TestSaveRejectsStaleSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	admin := New(path)
	if _, err := admin.Add("alice"); err != nil {
		t.Fatal(err)
	}
	stale := New(path)
	if err := admin.Revoke("alice"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if err := stale.Save(); err == nil {
		t.Fatal("stale Save succeeded")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("stale Save overwrote revocation")
	}
}

func BenchmarkLegacyUnknownTokenProductionCost(b *testing.B) {
	hash, err := bcrypt.GenerateFromPassword([]byte("valid-legacy-credential"), bcrypt.DefaultCost)
	if err != nil {
		b.Fatal(err)
	}
	for _, count := range []int{1, 25, 250} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "users.json")
			records := make([]User, count)
			for i := range records {
				records[i] = User{Name: fmt.Sprint(i), Token: string(hash), Active: true}
			}
			seedUsers(b, path, records)
			mgr := New(path)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				u, err := mgr.Authenticate("unknown-legacy-credential")
				if u != nil || (err != nil && !errors.Is(err, ErrAuthBusy)) {
					b.Fatalf("%v %v", u, err)
				}
			}
		})
	}
}

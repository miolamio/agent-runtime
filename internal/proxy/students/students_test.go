package students

import (
	"path/filepath"
	"testing"
)

func TestAddAndFind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "students.json")
	mgr := New(path)
	tok, err := mgr.Add("Ivanov")
	if err != nil {
		t.Fatalf("Add error: %v", err)
	}
	if tok == "" {
		t.Fatal("empty token")
	}
	s := mgr.FindByToken(tok)
	if s == nil {
		t.Fatal("FindByToken returned nil")
	}
	if s.Name != "Ivanov" {
		t.Errorf("Name = %q, want Ivanov", s.Name)
	}
	if !s.Active {
		t.Error("new user should be active")
	}
}

func TestAddDuplicate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "students.json")
	mgr := New(path)
	mgr.Add("Ivanov")
	_, err := mgr.Add("Ivanov")
	if err == nil {
		t.Error("expected error on duplicate name")
	}
}

func TestRevoke(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "students.json")
	mgr := New(path)
	tok, _ := mgr.Add("Ivanov")
	if err := mgr.Revoke("Ivanov"); err != nil {
		t.Fatal(err)
	}
	s := mgr.FindByToken(tok)
	if s == nil {
		t.Fatal("FindByToken returned nil after revoke")
	}
	if s.Active {
		t.Error("revoked user should be inactive")
	}
}

func TestRestore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "students.json")
	mgr := New(path)
	mgr.Add("Ivanov")
	mgr.Revoke("Ivanov")
	mgr.Restore("Ivanov")
	all := mgr.List()
	if len(all) != 1 || !all[0].Active {
		t.Error("restored user should be active")
	}
}

func TestPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "students.json")
	mgr1 := New(path)
	tok, _ := mgr1.Add("Ivanov")
	mgr2 := New(path)
	if err := mgr2.Load(); err != nil {
		t.Fatal(err)
	}
	s := mgr2.FindByToken(tok)
	if s == nil || s.Name != "Ivanov" {
		t.Error("user not found after reload")
	}
}

func TestFindByTokenInactive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "students.json")
	mgr := New(path)
	tok, _ := mgr.Add("Ivanov")
	mgr.Revoke("Ivanov")
	s := mgr.FindByToken(tok)
	if s == nil {
		t.Fatal("FindByToken should return inactive user")
	}
	if s.Active {
		t.Error("should be inactive")
	}
}

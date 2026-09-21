package users

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// TokenID is a SHA-256 lookup fingerprint of a randomly generated 256-bit
// credential. It is never accepted as a credential; bcrypt verifies the match.
type User struct {
	Name      string    `json:"name"`
	Token     string    `json:"token"`
	TokenID   string    `json:"token_id,omitempty"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

type legacyScan struct {
	offset int
	at     time.Time
}

// Manager publishes immutable snapshots. Every disk mutation re-reads under an
// OS file lock shared by all managers/processes, then atomically replaces JSON.
type Manager struct {
	path       string
	users      []User
	mu         sync.RWMutex
	raw        []byte
	info       os.FileInfo
	index      map[string]User
	legacy     []User
	scans      map[string]legacyScan
	upgrading  map[string]bool
	legacySlot chan struct{}
}

func New(path string) *Manager {
	m := &Manager{path: path, legacySlot: make(chan struct{}, 1), scans: map[string]legacyScan{}, upgrading: map[string]bool{}}
	if err := m.Load(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "[proxy] warning: could not load %s: %v\n", path, err)
	}
	return m
}

func readUsers(path string) ([]User, []byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var users []User
	if err := json.Unmarshal(raw, &users); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if users == nil {
		return nil, nil, fmt.Errorf("%s must contain a user array", path)
	}
	return users, raw, nil
}

func (m *Manager) publish(users []User, raw []byte) {
	m.users, m.raw = users, raw
	m.info, _ = os.Stat(m.path)
	m.index = map[string]User{}
	m.legacy = nil
	m.scans = map[string]legacyScan{}
	for _, u := range users {
		if !u.Active {
			continue
		}
		id := u.TokenID
		if id == "" && isSHA256Hash(u.Token) {
			id = u.Token
		}
		if id != "" {
			m.index[id] = u
		} else if isBcryptHash(u.Token) {
			m.legacy = append(m.legacy, u)
		}
	}
}

func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	unlock, err := lockStore(m.path)
	if err != nil {
		return err
	}
	defer unlock()
	users, raw, err := readUsers(m.path)
	if err != nil {
		return err
	}
	migrated := false
	for i := range users {
		if strings.HasPrefix(users[i].Token, tokenPrefix) {
			plain := users[i].Token
			hashed, err := HashTokenBcrypt(plain)
			if err != nil {
				return err
			}
			users[i].Token, users[i].TokenID = hashed, HashToken(plain)
			migrated = true
		}
	}
	if migrated {
		if err := writeUsers(m.path, users); err != nil {
			return err
		}
		raw, err = os.ReadFile(m.path)
		if err != nil {
			return err
		}
	}
	m.publish(users, raw)
	return nil
}

// Save is an optimistic snapshot write, kept for callers that edit snapshots.
// A stale manager must reload instead of overwriting intervening changes.
func (m *Manager) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	unlock, err := lockStore(m.path)
	if err != nil {
		return err
	}
	defer unlock()
	_, raw, err := readUsers(m.path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if !bytes.Equal(raw, m.raw) {
		return fmt.Errorf("users changed on disk; reload before saving")
	}
	if err := writeUsers(m.path, m.users); err != nil {
		return err
	}
	raw, err = os.ReadFile(m.path)
	if err != nil {
		return err
	}
	m.publish(m.users, raw)
	return nil
}

func (m *Manager) mutate(change func(*[]User) (bool, error)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	unlock, err := lockStore(m.path)
	if err != nil {
		return err
	}
	defer unlock()
	users, raw, err := readUsers(m.path)
	if errors.Is(err, fs.ErrNotExist) && m.raw == nil {
		users = []User{}
	} else if err != nil {
		return err
	}
	changed, err := change(&users)
	if err != nil {
		return err
	}
	if changed {
		if err := writeUsers(m.path, users); err != nil {
			return err
		}
		raw, err = os.ReadFile(m.path)
		if err != nil {
			return err
		}
	}
	m.publish(users, raw)
	return nil
}

// Save writes the current user list to the JSON file atomically: a temp file
// in the same directory is written, fsynced, then renamed over the target.
// On any failure the temp file is removed so a partial users.json never ends
// up in place.
func writeUsers(path string, users []User) error {
	data, err := json.MarshalIndent(users, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".users-*.json.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

func (m *Manager) Add(name string) (string, error) {
	tok, err := GenerateToken()
	if err != nil {
		return "", err
	}
	hashed, err := HashTokenBcrypt(tok)
	if err != nil {
		return "", err
	}
	err = m.mutate(func(list *[]User) (bool, error) {
		for _, u := range *list {
			if u.Name == name {
				return false, fmt.Errorf("user %q already exists", name)
			}
		}
		*list = append(*list, User{Name: name, Token: hashed, TokenID: HashToken(tok), Active: true, CreatedAt: time.Now().UTC()})
		return true, nil
	})
	if err != nil {
		return "", err
	}
	return tok, nil
}

func (m *Manager) setActive(name string, active bool) error {
	return m.mutate(func(list *[]User) (bool, error) {
		for i := range *list {
			if (*list)[i].Name == name {
				(*list)[i].Active = active
				return true, nil
			}
		}
		return false, fmt.Errorf("user %q not found", name)
	})
}

func (m *Manager) Revoke(name string) error  { return m.setActive(name, false) }
func (m *Manager) Restore(name string) error { return m.setActive(name, true) }

// ErrAuthBusy tells the HTTP layer to return 429 with Retry-After. Old bcrypt
// records cannot be indexed until their plaintext is seen. Their first login
// is scanned in bounded slices; retries resume, never rescan the full store.
var ErrAuthBusy = errors.New("authentication budget exhausted; retry")

const maxLegacyChecks = 4
const maxLegacyScans = 128

// refresh notices atomic replacements before auth, including revoke by another
// process. Read errors fail closed; no last-known-good snapshot grants access.
func (m *Manager) refresh() error {
	info, err := os.Stat(m.path)
	if err != nil {
		return err
	}
	m.mu.RLock()
	old := m.info
	unchanged := old != nil && os.SameFile(old, info) && old.ModTime().Equal(info.ModTime()) && old.Size() == info.Size()
	m.mu.RUnlock()
	if unchanged {
		return nil
	}
	return m.Load()
}

func (m *Manager) Authenticate(token string) (*User, error) {
	if token == "" || len(token) > 72 {
		return nil, nil
	}
	if err := m.refresh(); err != nil {
		return nil, err
	}
	id := HashToken(token)
	m.mu.RLock()
	candidate, indexed := m.index[id]
	legacy := m.legacy // immutable until the next published snapshot
	scan := m.scans[id]
	m.mu.RUnlock()
	if indexed {
		if ok, upgrade := VerifyToken(token, candidate.Token); ok {
			if upgrade {
				m.scheduleUpgrade(candidate, token)
			}
			return m.currentMatch(candidate, token)
		}
		return nil, nil
	}
	if len(legacy) == 0 {
		return nil, nil
	}
	select {
	case m.legacySlot <- struct{}{}:
		defer func() { <-m.legacySlot }()
	default:
		return nil, ErrAuthBusy
	}
	offset := scan.offset
	if time.Since(scan.at) > time.Minute || offset >= len(legacy) {
		offset = 0
	}
	end := min(offset+maxLegacyChecks, len(legacy))
	for _, candidate := range legacy[offset:end] {
		if ok, _ := VerifyToken(token, candidate.Token); ok {
			// Persist only this fingerprint; no rehash is needed for bcrypt.
			err := m.mutate(func(list *[]User) (bool, error) {
				for i := range *list {
					u := &(*list)[i]
					if u.Name == candidate.Name && u.Token == candidate.Token {
						u.TokenID = id
						return true, nil
					}
				}
				return false, nil
			})
			if err != nil {
				return nil, err
			}
			return m.currentMatch(candidate, token)
		}
	}
	m.mu.Lock()
	// Bounded memory even when attackers rotate random tokens.
	if len(m.scans) >= maxLegacyScans {
		m.scans = map[string]legacyScan{}
	}
	if end < len(legacy) {
		m.scans[id] = legacyScan{offset: end, at: time.Now()}
	} else {
		delete(m.scans, id)
	}
	m.mu.Unlock()
	if end < len(legacy) {
		return nil, ErrAuthBusy
	}
	return nil, nil
}

func (m *Manager) currentMatch(candidate User, token string) (*User, error) {
	// Re-check after expensive work, without holding a reader lock over bcrypt.
	if err := m.refresh(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	current, ok := m.index[HashToken(token)]
	if ok && current.Active && current.Name == candidate.Name && (current.Token == candidate.Token || isSHA256Hash(candidate.Token)) {
		return &current, nil
	}
	return nil, nil
}

func (m *Manager) FindByToken(token string) *User { u, _ := m.Authenticate(token); return u }

func (m *Manager) scheduleUpgrade(user User, plain string) {
	m.mu.Lock()
	if m.upgrading[user.Name] {
		m.mu.Unlock()
		return
	}
	m.upgrading[user.Name] = true
	m.mu.Unlock()
	go func() {
		defer func() { m.mu.Lock(); delete(m.upgrading, user.Name); m.mu.Unlock() }()
		m.upgradeToBcrypt(user.Name, user.Token, plain)
	}()
}

func (m *Manager) upgradeToBcrypt(name, expectedOld, plaintext string) {
	newHash, err := HashTokenBcrypt(plaintext)
	if err != nil {
		return
	}
	err = m.mutate(func(list *[]User) (bool, error) {
		for i := range *list {
			u := &(*list)[i]
			if u.Name == name && u.Token == expectedOld {
				u.Token, u.TokenID = newHash, HashToken(plaintext)
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[proxy] warning: token upgrade not persisted: %v\n", err)
	}
}

func (m *Manager) List() []User {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]User{}, m.users...)
}

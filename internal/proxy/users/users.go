package users

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"sync"
	"time"
)

// User represents a registered user with an API token.
type User struct {
	Name      string    `json:"name"`
	Token     string    `json:"token"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

// Manager handles CRUD operations on a list of users persisted to a JSON file.
type Manager struct {
	path  string
	users []User
	mu    sync.RWMutex
}

// New creates a Manager for the given file path and attempts to load existing data.
// A missing file is treated as a fresh install; any other load error is logged to stderr
// so a corrupted users.json doesn't silently present as an empty user list.
func New(path string) *Manager {
	m := &Manager{path: path}
	if err := m.Load(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "[proxy] warning: could not load %s: %v\n", path, err)
	}
	return m
}

// Load reads users from the JSON file on disk. Plaintext tokens (sk-ai- prefix,
// ever written by pre-v0.6.0 builds) are migrated to bcrypt on the spot so
// they never remain on disk in recoverable form. Legacy SHA-256 hashes from
// v0.6.0/v0.6.1 are left as-is here and are upgraded lazily on first successful
// auth by FindByToken.
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, err := os.ReadFile(m.path)
	if err != nil {
		return err
	}
	var users []User
	if err := json.Unmarshal(data, &users); err != nil {
		return fmt.Errorf("parse %s: %w", m.path, err)
	}
	migrated := false
	for i := range users {
		if strings.HasPrefix(users[i].Token, tokenPrefix) {
			hashed, hashErr := HashTokenBcrypt(users[i].Token)
			if hashErr != nil {
				// bcrypt should not fail on valid input; fall back to SHA-256
				// so plaintext is never left on disk even if bcrypt is broken.
				hashed = HashToken(users[i].Token)
			}
			users[i].Token = hashed
			migrated = true
		}
	}
	m.users = users
	if migrated {
		// Self-healing: next Load will re-migrate if Save fails, so don't fail the whole Load.
		if err := m.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "[proxy] warning: token migration not persisted: %v\n", err)
		}
	}
	return nil
}

// Save writes the current user list to the JSON file.
func (m *Manager) Save() error {
	data, err := json.MarshalIndent(m.users, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, append(data, '\n'), 0600)
}

// Add creates a new user with a random token and persists the change.
// The token is stored as a bcrypt hash; the raw token is returned to the caller.
func (m *Manager) Add(name string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.Name == name {
			return "", fmt.Errorf("user %q already exists", name)
		}
	}
	tok, err := GenerateToken()
	if err != nil {
		return "", err
	}
	hashed, err := HashTokenBcrypt(tok)
	if err != nil {
		return "", err
	}
	u := User{Name: name, Token: hashed, Active: true, CreatedAt: time.Now().UTC()}
	m.users = append(m.users, u)
	if err := m.Save(); err != nil {
		return "", fmt.Errorf("save: %w", err)
	}
	return tok, nil
}

// Revoke deactivates a user by name.
func (m *Manager) Revoke(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.users {
		if m.users[i].Name == name {
			m.users[i].Active = false
			return m.Save()
		}
	}
	return fmt.Errorf("user %q not found", name)
}

// Restore reactivates a previously revoked user by name.
func (m *Manager) Restore(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.users {
		if m.users[i].Name == name {
			m.users[i].Active = true
			return m.Save()
		}
	}
	return fmt.Errorf("user %q not found", name)
}

// FindByToken walks all active users and verifies the given plaintext against
// each stored hash (either bcrypt or legacy SHA-256). On a SHA-256 match the
// stored hash is upgraded to bcrypt asynchronously so auth-path latency stays
// low on subsequent requests.
//
// Returns a defensive copy so callers can't race on slice reallocation.
func (m *Manager) FindByToken(token string) *User {
	m.mu.RLock()
	var (
		matched       User
		matchName     string
		matchedStored string
		upgrade       bool
		found         bool
	)
	for i := range m.users {
		if !m.users[i].Active {
			continue
		}
		stored := m.users[i].Token
		ok, needUpgrade := VerifyToken(token, stored)
		if !ok {
			continue
		}
		matched = m.users[i]
		matchName = m.users[i].Name
		matchedStored = stored
		upgrade = needUpgrade
		found = true
		break
	}
	m.mu.RUnlock()
	if !found {
		return nil
	}
	if upgrade {
		go m.upgradeToBcrypt(matchName, matchedStored, token)
	}
	return &matched
}

// upgradeToBcrypt replaces the SHA-256 hash of the named user with a bcrypt
// hash of the given plaintext, then persists. Safe against concurrent requests
// for the same token: if another goroutine already upgraded (stored differs
// from expectedOld) we bail without writing.
func (m *Manager) upgradeToBcrypt(name, expectedOld, plaintext string) {
	newHash, err := HashTokenBcrypt(plaintext)
	if err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.users {
		if m.users[i].Name != name {
			continue
		}
		if m.users[i].Token != expectedOld {
			return // already upgraded, or user was revoked and re-created
		}
		m.users[i].Token = newHash
		if err := m.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "[proxy] warning: bcrypt upgrade for %s not persisted: %v\n", name, err)
			m.users[i].Token = expectedOld // keep memory in sync with disk
		}
		return
	}
}

// List returns a copy of all users.
func (m *Manager) List() []User {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]User, len(m.users))
	copy(result, m.users)
	return result
}

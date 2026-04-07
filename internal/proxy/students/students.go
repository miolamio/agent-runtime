package students

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Student represents a registered user with an API token.
type Student struct {
	Name      string    `json:"name"`
	Token     string    `json:"token"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

// Manager handles CRUD operations on a list of users persisted to a JSON file.
type Manager struct {
	path     string
	students []Student
	mu       sync.RWMutex
}

// New creates a Manager for the given file path and attempts to load existing data.
func New(path string) *Manager {
	m := &Manager{path: path}
	m.Load()
	return m
}

// Load reads users from the JSON file on disk. Plaintext tokens (sk-ai- prefix)
// are automatically migrated to SHA-256 hashes.
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, err := os.ReadFile(m.path)
	if err != nil {
		return err
	}
	var students []Student
	if err := json.Unmarshal(data, &students); err != nil {
		return fmt.Errorf("parse %s: %w", m.path, err)
	}
	migrated := false
	for i := range students {
		if strings.HasPrefix(students[i].Token, tokenPrefix) {
			students[i].Token = HashToken(students[i].Token)
			migrated = true
		}
	}
	m.students = students
	if migrated {
		m.Save()
	}
	return nil
}

// Save writes the current user list to the JSON file.
func (m *Manager) Save() error {
	data, err := json.MarshalIndent(m.students, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, append(data, '\n'), 0600)
}

// Add creates a new user with a random token and persists the change.
// The token is stored as a SHA-256 hash; the raw token is returned to the caller.
func (m *Manager) Add(name string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.students {
		if s.Name == name {
			return "", fmt.Errorf("user %q already exists", name)
		}
	}
	tok, err := GenerateToken()
	if err != nil {
		return "", err
	}
	hashed := HashToken(tok)
	s := Student{Name: name, Token: hashed, Active: true, CreatedAt: time.Now().UTC()}
	m.students = append(m.students, s)
	if err := m.Save(); err != nil {
		return "", fmt.Errorf("save: %w", err)
	}
	return tok, nil
}

// Revoke deactivates a user by name.
func (m *Manager) Revoke(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.students {
		if m.students[i].Name == name {
			m.students[i].Active = false
			return m.Save()
		}
	}
	return fmt.Errorf("user %q not found", name)
}

// Restore reactivates a previously revoked user by name.
func (m *Manager) Restore(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.students {
		if m.students[i].Name == name {
			m.students[i].Active = true
			return m.Save()
		}
	}
	return fmt.Errorf("user %q not found", name)
}

// FindByToken hashes the incoming token and performs a constant-time comparison
// against all stored hashes. Returns nil if no active match is found.
func (m *Manager) FindByToken(token string) *Student {
	m.mu.RLock()
	defer m.mu.RUnlock()
	hashed := HashToken(token)
	hashedBytes := []byte(hashed)
	for i := range m.students {
		if m.students[i].Active && subtle.ConstantTimeCompare([]byte(m.students[i].Token), hashedBytes) == 1 {
			return &m.students[i]
		}
	}
	return nil
}

// List returns a copy of all users.
func (m *Manager) List() []Student {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]Student, len(m.students))
	copy(result, m.students)
	return result
}

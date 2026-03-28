package students

import (
	"encoding/json"
	"fmt"
	"os"
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
	byToken  map[string]*Student
	mu       sync.RWMutex
}

// New creates a Manager for the given file path and attempts to load existing data.
func New(path string) *Manager {
	m := &Manager{path: path, byToken: make(map[string]*Student)}
	m.Load()
	return m
}

// Load reads users from the JSON file on disk.
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
	m.students = students
	m.byToken = make(map[string]*Student, len(students))
	for i := range m.students {
		m.byToken[m.students[i].Token] = &m.students[i]
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
// Returns an error if a user with the same name already exists.
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
	s := Student{Name: name, Token: tok, Active: true, CreatedAt: time.Now().UTC()}
	m.students = append(m.students, s)
	m.byToken[tok] = &m.students[len(m.students)-1]
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

// FindByToken returns a pointer to the user with the given token, or nil.
func (m *Manager) FindByToken(token string) *Student {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.byToken[token]
}

// List returns a copy of all users.
func (m *Manager) List() []Student {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]Student, len(m.students))
	copy(result, m.students)
	return result
}

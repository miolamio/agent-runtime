# airun v0.6.0 Full Roadmap

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix all critical bugs, harden security, clean up architecture, implement missing advertised features, and bring test coverage to the core packages.

**Architecture:** The codebase is a Go CLI (`cmd/airun/main.go`) dispatching to `internal/` packages. Single external dep: `gopkg.in/yaml.v3`. The proxy subsystem (`internal/proxy/`) is the most complex component. All changes preserve the existing package boundaries and dependency DAG.

**Tech Stack:** Go 1.25+, standard library, `gopkg.in/yaml.v3`, Docker CLI

---

## File Map

### Files to modify

| File | Changes |
|------|---------|
| `internal/proxy/students/students.go` | Fix dangling pointer; add SHA256 hashing |
| `internal/proxy/students/token.go` | Add `HashToken` helper |
| `internal/proxy/handler.go` | Add `MaxBytesReader`; pass student via context; hardcode forward path |
| `internal/proxy/forward.go` | Accept explicit path parameter |
| `internal/proxy/server.go` | Fix `Init` error handling; fix `StudentList` panic; default `127.0.0.1` |
| `internal/proxy/ratelimit.go` | Key on user name; add stale bucket eviction |
| `internal/proxy/config.go` | Default listen to `127.0.0.1:8080` |
| `internal/proxy/connect.go` | Check `rand.Read` error; check `http.NewRequest` error |
| `internal/runner/runner.go` | Implement `--loop`; extract shared docker/history logic; warn on unused plugins |
| `internal/config/config.go` | Remove dead `isMinimax`; add Anthropic provider |
| `internal/envfile/envfile.go` | Use private temp dir instead of `/tmp` |
| `internal/history/history.go` | Fix file permissions to 0600; propagate errors |
| `internal/keys/keys.go` | Check `http.NewRequest` error; check `UpdateEnvKey` errors |
| `internal/keys/providers.go` | Add Anthropic provider entry |
| `internal/profile/profile.go` | Guard `user.Current()` nil return |
| `internal/setup/setup.go` | Fix profile path; replace `goto`; check write errors |
| `scripts/setup.sh` | Fix `*.env.example` -> `*.yaml` |

### Files to create (tests)

| File | Tests for |
|------|-----------|
| `internal/config/config_test.go` | `NormalizeProvider`, `ContainerEnvWithModel`, `loadEnvFile` |
| `internal/envfile/envfile_test.go` | `Write` permissions, `Cleanup` validation |
| `internal/history/history_test.go` | `Save`, `FormatTable`, `NewRunDir` |
| `internal/proxy/students/students_concurrent_test.go` | Concurrent `Add`/`FindByToken` |

---

## Phase 1: Critical Bug Fixes

### Task 1: Fix dangling pointer in students.Manager.Add

The `byToken` map stores pointers into a slice. When `append` reallocates the backing array, all stored pointers become dangling. This corrupts memory or crashes the proxy on the second+ user addition.

**Acceptance criteria:**
- Adding 100 users sequentially does not corrupt any `FindByToken` lookup
- Concurrent `Add` + `FindByToken` from multiple goroutines does not race or panic
- Existing tests pass unchanged

**Files:**
- Modify: `internal/proxy/students/students.go:63-83`
- Test: `internal/proxy/students/students_test.go` (existing)

- [ ] **Step 1: Write a test that exposes the bug**

Add to `internal/proxy/students/students_test.go`:

```go
func TestAddManyUsersNoCorruption(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "students.json")
	os.WriteFile(path, []byte("[]"), 0600)
	mgr := New(path)

	tokens := make([]string, 50)
	for i := 0; i < 50; i++ {
		tok, err := mgr.Add(fmt.Sprintf("user%d", i))
		if err != nil {
			t.Fatalf("Add user%d: %v", i, err)
		}
		tokens[i] = tok
	}

	// Verify every token still resolves correctly
	for i, tok := range tokens {
		s := mgr.FindByToken(tok)
		if s == nil {
			t.Fatalf("FindByToken returned nil for user%d (token %s)", i, tok[:10]+"...")
		}
		expected := fmt.Sprintf("user%d", i)
		if s.Name != expected {
			t.Errorf("FindByToken returned name %q, want %q", s.Name, expected)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/codegeek/src/agent-runtime && go test ./internal/proxy/students/ -run TestAddManyUsersNoCorruption -v`
Expected: FAIL with nil pointer or wrong name (due to dangling pointers after slice reallocation)

- [ ] **Step 3: Fix Add() to rebuild byToken map after append**

In `internal/proxy/students/students.go`, replace lines 78-79:

```go
// OLD (lines 78-79):
m.students = append(m.students, s)
m.byToken[tok] = &m.students[len(m.students)-1]

// NEW:
m.students = append(m.students, s)
m.rebuildIndex()
```

Add the `rebuildIndex` helper method after the `Save` method (after line 61):

```go
// rebuildIndex reconstructs the byToken map from the students slice.
// Must be called with m.mu held.
func (m *Manager) rebuildIndex() {
	m.byToken = make(map[string]*Student, len(m.students))
	for i := range m.students {
		m.byToken[m.students[i].Token] = &m.students[i]
	}
}
```

Also update `Load()` to use the same helper. Replace lines 47-50:

```go
// OLD (lines 47-50):
m.byToken = make(map[string]*Student, len(students))
for i := range m.students {
    m.byToken[m.students[i].Token] = &m.students[i]
}

// NEW:
m.rebuildIndex()
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/codegeek/src/agent-runtime && go test ./internal/proxy/students/ -run TestAddManyUsersNoCorruption -v`
Expected: PASS

- [ ] **Step 5: Run all existing tests**

Run: `cd /Users/codegeek/src/agent-runtime && go test ./internal/proxy/students/ -v`
Expected: All PASS

- [ ] **Step 6: Write concurrent access test**

Create `internal/proxy/students/students_concurrent_test.go`:

```go
package students

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentAddAndFind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "students.json")
	os.WriteFile(path, []byte("[]"), 0600)
	mgr := New(path)

	// Pre-add some users
	var tokens []string
	for i := 0; i < 10; i++ {
		tok, err := mgr.Add(fmt.Sprintf("pre%d", i))
		if err != nil {
			t.Fatalf("pre-add: %v", err)
		}
		tokens = append(tokens, tok)
	}

	var wg sync.WaitGroup

	// Concurrent readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(tok string) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				s := mgr.FindByToken(tok)
				if s == nil {
					t.Errorf("FindByToken returned nil for known token")
					return
				}
			}
		}(tokens[i])
	}

	// Concurrent writers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				mgr.Add(fmt.Sprintf("concurrent_%d_%d", idx, j))
			}
		}(i)
	}

	wg.Wait()
}
```

- [ ] **Step 7: Run concurrent test with race detector**

Run: `cd /Users/codegeek/src/agent-runtime && go test -race ./internal/proxy/students/ -run TestConcurrentAddAndFind -v`
Expected: PASS with no data races

- [ ] **Step 8: Commit**

```bash
cd /Users/codegeek/src/agent-runtime
git add internal/proxy/students/students.go internal/proxy/students/students_test.go internal/proxy/students/students_concurrent_test.go
git commit -m "fix: rebuild byToken index after slice append to prevent dangling pointers

Add() stored pointers into the students slice in the byToken map. When
append() reallocated the backing array, all existing pointers became
invalid. Extract rebuildIndex() and call it after every slice mutation.

Add tests for 50-user sequential add and concurrent read/write."
```

---

### Task 2: Fix --loop flag (currently a no-op)

`RunOpts.Loop` and `RunOpts.MaxLoops` are parsed from CLI flags but never read in `runDocker()`. The `claude` CLI supports `--max-turns` for autonomous looping. Currently every run is one-shot.

**Acceptance criteria:**
- `airun --loop --max-loops 10 "prompt"` passes `--max-turns 10` to the `claude` command inside the container
- Default `--max-loops` (5) works when only `--loop` is specified
- Non-loop runs are unaffected

**Files:**
- Modify: `internal/runner/runner.go:118,151-154`

- [ ] **Step 1: Verify the bug — Loop/MaxLoops never used**

Run: `cd /Users/codegeek/src/agent-runtime && grep -n "Loop\|MaxLoops" internal/runner/runner.go`
Expected: Only struct definition and no reads of these fields in `runDocker`/`runDockerWithExport`

- [ ] **Step 2: Pass Loop and MaxLoops through to runDocker**

In `internal/runner/runner.go`, modify the call at line 118 to pass the full opts:

```go
// OLD (line 118):
return runDocker(cfg, RunOpts{Prompt: opts.Prompt, Mount: mount, Profile: opts.Profile, NoState: opts.NoState}, provider, model, extraVolumes)

// NEW:
return runDocker(cfg, RunOpts{Prompt: opts.Prompt, Mount: mount, Profile: opts.Profile, NoState: opts.NoState, Loop: opts.Loop, MaxLoops: opts.MaxLoops}, provider, model, extraVolumes)
```

- [ ] **Step 3: Add loop args to the claude command in runDocker**

In `internal/runner/runner.go`, replace lines 151-154:

```go
// OLD (lines 151-154):
// Claude Code command (non-interactive only)
if !opts.Interactive {
    args = append(args, "claude", "-p", opts.Prompt, "--dangerously-skip-permissions")
}

// NEW:
// Claude Code command (non-interactive only)
if !opts.Interactive {
    args = append(args, "claude", "-p", opts.Prompt, "--dangerously-skip-permissions")
    if opts.Loop && opts.MaxLoops > 0 {
        args = append(args, "--max-turns", fmt.Sprintf("%d", opts.MaxLoops))
    }
}
```

Also add the same to `runDockerWithExport` at line 221:

```go
// OLD (line 221):
createArgs = append(createArgs, imageName, "claude", "-p", opts.Prompt, "--dangerously-skip-permissions")

// NEW:
createArgs = append(createArgs, imageName, "claude", "-p", opts.Prompt, "--dangerously-skip-permissions")
if opts.Loop && opts.MaxLoops > 0 {
    createArgs = append(createArgs, "--max-turns", fmt.Sprintf("%d", opts.MaxLoops))
}
```

- [ ] **Step 4: Verify build succeeds**

Run: `cd /Users/codegeek/src/agent-runtime && go build ./cmd/airun/`
Expected: Build succeeds

- [ ] **Step 5: Commit**

```bash
cd /Users/codegeek/src/agent-runtime
git add internal/runner/runner.go
git commit -m "fix: implement --loop flag by passing --max-turns to claude CLI

Loop and MaxLoops fields were parsed but never read. Now when --loop is
set, --max-turns N is appended to the claude command in both runDocker
and runDockerWithExport."
```

---

### Task 3: Fix SHA256 token hashing (README claims hashing, code stores plaintext)

Tokens in `students.json` are stored as raw plaintext. README and CLAUDE.md claim SHA256 hashing. This is both a documentation lie and a security risk: anyone who reads `students.json` gets all tokens.

**Acceptance criteria:**
- `students.json` stores `sha256(token)` hex digest, never the raw token
- `FindByToken` hashes the incoming token and compares against stored hashes
- Comparison uses `crypto/subtle.ConstantTimeCompare`
- Token is shown to admin exactly once on `Add()`, then only the hash is persisted
- Existing `students.json` files with plaintext tokens are auto-migrated on `Load()`
- All existing proxy and student tests pass (updated as needed)

**Files:**
- Modify: `internal/proxy/students/token.go`
- Modify: `internal/proxy/students/students.go`
- Test: `internal/proxy/students/students_test.go`
- Test: `internal/proxy/students/token_test.go`

- [ ] **Step 1: Add HashToken to token.go**

Add to `internal/proxy/students/token.go`:

```go
import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// HashToken returns the SHA-256 hex digest of a token.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
```

- [ ] **Step 2: Write test for HashToken**

Add to `internal/proxy/students/token_test.go`:

```go
func TestHashToken(t *testing.T) {
	tok := "sk-ai-abcdef1234567890"
	h1 := HashToken(tok)
	h2 := HashToken(tok)
	if h1 != h2 {
		t.Error("HashToken not deterministic")
	}
	if len(h1) != 64 {
		t.Errorf("hash length = %d, want 64", len(h1))
	}
	if HashToken("different") == h1 {
		t.Error("different inputs produce same hash")
	}
}
```

Run: `cd /Users/codegeek/src/agent-runtime && go test ./internal/proxy/students/ -run TestHashToken -v`
Expected: PASS

- [ ] **Step 3: Update Student struct — add TokenHash field**

In `internal/proxy/students/students.go`, update the struct:

```go
type Student struct {
	Name      string    `json:"name"`
	Token     string    `json:"token"`      // stored as SHA-256 hash; raw token only returned once on Add()
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}
```

Note: We keep the JSON field name `"token"` but store the hash in it. This way existing JSON structure is preserved.

- [ ] **Step 4: Update Add() to store hash, return raw token**

Replace `Add` method in `internal/proxy/students/students.go`:

```go
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
	m.rebuildIndex()
	if err := m.Save(); err != nil {
		return "", fmt.Errorf("save: %w", err)
	}
	return tok, nil // raw token returned to caller; only hash persisted
}
```

- [ ] **Step 5: Update FindByToken to hash the incoming token**

Replace `FindByToken` in `internal/proxy/students/students.go`:

```go
import (
	"crypto/subtle"
	// ... existing imports
)

func (m *Manager) FindByToken(token string) *Student {
	m.mu.RLock()
	defer m.mu.RUnlock()
	hashed := HashToken(token)
	// Constant-time lookup: iterate all students
	for i := range m.students {
		if subtle.ConstantTimeCompare([]byte(m.students[i].Token), []byte(hashed)) == 1 {
			return &m.students[i]
		}
	}
	return nil
}
```

Remove the `byToken` map entirely since we now do constant-time comparison. Update `rebuildIndex` accordingly:

```go
// rebuildIndex is no longer needed for byToken but kept as a no-op
// for forward compatibility. The constant-time scan replaces map lookup.
func (m *Manager) rebuildIndex() {
	// Intentionally empty — FindByToken uses constant-time scan now
}
```

Update `Manager` struct to remove `byToken`:

```go
type Manager struct {
	path     string
	students []Student
	mu       sync.RWMutex
}

func New(path string) *Manager {
	m := &Manager{path: path}
	m.Load()
	return m
}
```

Update `Load()` to remove byToken rebuild:

```go
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
	// Auto-migrate plaintext tokens to hashed
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
```

Add `"strings"` to imports.

- [ ] **Step 6: Update existing tests**

The existing `TestAdd` and `TestFindByToken` tests call `Add` then `FindByToken` with the returned raw token. This still works because `FindByToken` now hashes the raw token and compares with the stored hash. The test for `TestPersistence` loads from disk and re-checks — the tokens on disk are now hashes, so `FindByToken` with the raw token will hash it and match.

One test needs updating: `TestFindByTokenInactive` — it accesses `mgr.students[0].Token` directly, which is now a hash. The test should use the raw token returned by `Add` instead.

Update in `students_test.go`:

```go
func TestFindByTokenInactive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "students.json")
	os.WriteFile(path, []byte("[]"), 0600)
	mgr := New(path)
	rawToken, _ := mgr.Add("TestUser")
	mgr.Revoke("TestUser")
	s := mgr.FindByToken(rawToken)
	if s != nil {
		t.Error("expected nil for inactive user")
	}
}
```

- [ ] **Step 7: Run all tests**

Run: `cd /Users/codegeek/src/agent-runtime && go test ./internal/proxy/... -v`
Expected: All PASS

- [ ] **Step 8: Fix CLAUDE.md reference**

In `/Users/codegeek/src/agent-runtime/CLAUDE.md`, the "Proxy System" section says "Token auth: SHA256-hashed tokens in students.json" — this is now true, no change needed. Remove "Token format" from Gotchas section since tokens are now hashed on disk.

- [ ] **Step 9: Commit**

```bash
cd /Users/codegeek/src/agent-runtime
git add internal/proxy/students/
git commit -m "security: hash tokens with SHA-256 before storing in students.json

Tokens were stored as plaintext despite documentation claiming SHA-256.
Now Add() stores only the hash; FindByToken() hashes the incoming token
and uses constant-time comparison. Existing plaintext tokens in
students.json are auto-migrated on Load()."
```

---

### Task 4: Add MaxBytesReader to proxy handler

Any authenticated user can send a multi-GB request body, causing OOM. `io.ReadAll(r.Body)` reads without limit.

**Acceptance criteria:**
- Request bodies over 10 MB are rejected with HTTP 413
- Normal requests (< 10 MB) work unchanged
- Existing handler tests pass

**Files:**
- Modify: `internal/proxy/handler.go:80-84`
- Test: `internal/proxy/handler_test.go`

- [ ] **Step 1: Write failing test**

Add to `internal/proxy/handler_test.go`:

```go
func TestMessagesBodyTooLarge(t *testing.T) {
	cfg := &ProxyConfig{
		Listen:    ":0",
		UserAgent: "test",
		Providers: map[string]ProviderEntry{
			"test": {BaseURL: "http://localhost", APIKey: "key", Models: []string{"m1"}},
		},
	}
	dir := t.TempDir()
	sp := filepath.Join(dir, "students.json")
	os.WriteFile(sp, []byte("[]"), 0600)
	mgr := students.New(sp)
	tok, _ := mgr.Add("BigUser")

	h := NewHandler(cfg, mgr)

	// 11 MB body
	body := make([]byte, 11<<20)
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("x-api-key", tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/codegeek/src/agent-runtime && go test ./internal/proxy/ -run TestMessagesBodyTooLarge -v`
Expected: FAIL (currently returns 400 or reads the whole body)

- [ ] **Step 3: Add MaxBytesReader**

In `internal/proxy/handler.go`, add a constant and modify `handleMessages`:

```go
const maxRequestBodySize = 10 << 20 // 10 MB

func (h *Handler) handleMessages(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		if err.Error() == "http: request body too large" {
			jsonError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		jsonError(w, http.StatusBadRequest, "cannot read body")
		return
	}
	// ... rest unchanged
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/codegeek/src/agent-runtime && go test ./internal/proxy/ -run TestMessagesBodyTooLarge -v`
Expected: PASS

- [ ] **Step 5: Run all proxy tests**

Run: `cd /Users/codegeek/src/agent-runtime && go test ./internal/proxy/... -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
cd /Users/codegeek/src/agent-runtime
git add internal/proxy/handler.go internal/proxy/handler_test.go
git commit -m "security: limit proxy request body to 10 MB to prevent OOM

io.ReadAll without a size limit allowed any authenticated user to
exhaust server memory. Add http.MaxBytesReader with a 10 MB cap."
```

---

### Task 5: Fix proxy default listen address

`proxy init` generates `listen: ":8080"` which binds on all interfaces. Combined with no TLS, this exposes the proxy to the public internet on cloud VMs.

**Acceptance criteria:**
- Default listen address in generated `proxy.yaml` is `127.0.0.1:8080`
- Default in `LoadProxyConfig` fallback is also `127.0.0.1:8080`
- `--port` flag still works (overrides only port, binds all interfaces as before for explicit use)

**Files:**
- Modify: `internal/proxy/server.go:65`
- Modify: `internal/proxy/config.go:31,36`

- [ ] **Step 1: Update default in config.go**

In `internal/proxy/config.go`, change lines 31 and 35-37:

```go
// OLD:
cfg := &ProxyConfig{Listen: ":8080", UserAgent: defaultUserAgent}
// ...
if cfg.Listen == "" {
    cfg.Listen = ":8080"
}

// NEW:
cfg := &ProxyConfig{Listen: "127.0.0.1:8080", UserAgent: defaultUserAgent}
// ...
if cfg.Listen == "" {
    cfg.Listen = "127.0.0.1:8080"
}
```

- [ ] **Step 2: Update template in server.go**

In `internal/proxy/server.go`, change line 65:

```go
// OLD:
listen: ":8080"

// NEW:
listen: "127.0.0.1:8080"
```

- [ ] **Step 3: Run existing tests**

Run: `cd /Users/codegeek/src/agent-runtime && go test ./internal/proxy/ -v`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
cd /Users/codegeek/src/agent-runtime
git add internal/proxy/server.go internal/proxy/config.go
git commit -m "security: default proxy listen to 127.0.0.1:8080 instead of all interfaces

Prevents accidental public exposure when proxy runs without a reverse
proxy. Users deploying with nginx can change to 0.0.0.0 explicitly."
```

---

### Task 6: Fix nil-deref on malformed URL in connect.go and keys.go

`http.NewRequest` returns an error on malformed URLs, but the error is discarded. The next line dereferences `req` which is `nil`.

**Acceptance criteria:**
- Malformed proxy URLs return a clear error instead of panic
- Both `connect.go:fetchModels` and `keys.go:FetchRemoteModels` are fixed

**Files:**
- Modify: `internal/proxy/connect.go:263`
- Modify: `internal/keys/keys.go:381`

- [ ] **Step 1: Fix connect.go**

In `internal/proxy/connect.go`, replace lines 262-264:

```go
// OLD:
req, _ := http.NewRequest("GET", url, nil)
req.Header.Set("x-api-key", apiKey)

// NEW:
req, err := http.NewRequest("GET", url, nil)
if err != nil {
    return nil, fmt.Errorf("invalid proxy URL: %w", err)
}
req.Header.Set("x-api-key", apiKey)
```

- [ ] **Step 2: Fix keys.go**

In `internal/keys/keys.go`, replace lines 381-382:

```go
// OLD:
req, _ := http.NewRequest("GET", url, nil)
req.Header.Set("x-api-key", apiKey)

// NEW:
req, err := http.NewRequest("GET", url, nil)
if err != nil {
    return nil, fmt.Errorf("invalid proxy URL: %w", err)
}
req.Header.Set("x-api-key", apiKey)
```

- [ ] **Step 3: Verify build**

Run: `cd /Users/codegeek/src/agent-runtime && go build ./...`
Expected: Build succeeds

- [ ] **Step 4: Commit**

```bash
cd /Users/codegeek/src/agent-runtime
git add internal/proxy/connect.go internal/keys/keys.go
git commit -m "fix: check http.NewRequest error to prevent nil-deref on malformed URLs"
```

---

### Task 7: Fix StudentList panic on short/corrupt tokens

`server.go:127` does `s.Token[:10]` without length check. A corrupt `students.json` with a short token panics.

**Acceptance criteria:**
- Short tokens are displayed without panic
- Normal-length tokens display as before

**Files:**
- Modify: `internal/proxy/server.go:127`

- [ ] **Step 1: Add length guard**

In `internal/proxy/server.go`, replace line 127:

```go
// OLD:
masked := s.Token[:10] + "..." + s.Token[len(s.Token)-4:]

// NEW:
masked := s.Token
if len(masked) >= 14 {
    masked = s.Token[:10] + "..." + s.Token[len(s.Token)-4:]
}
```

Note: After Task 3 (SHA256 hashing), `s.Token` will be a 64-char hex hash. This guard protects against any future format changes or corruption.

- [ ] **Step 2: Verify build**

Run: `cd /Users/codegeek/src/agent-runtime && go build ./...`

- [ ] **Step 3: Commit**

```bash
cd /Users/codegeek/src/agent-runtime
git add internal/proxy/server.go
git commit -m "fix: guard StudentList against short tokens to prevent panic"
```

---

## Phase 2: Security Hardening

### Task 8: Fix history file permissions

History files containing prompts and outputs are written with 0644 (world-readable).

**Acceptance criteria:**
- History dirs created with 0700
- All history files written with 0600
- Errors from `os.MkdirAll` and `os.WriteFile` are logged to stderr (not fatal)

**Files:**
- Modify: `internal/history/history.go:30-38`

- [ ] **Step 1: Fix Save()**

Replace `Save` in `internal/history/history.go`:

```go
func Save(rec RunRecord, output string) error {
	dir := rec.RunDir
	if err := os.MkdirAll(dir, 0700); err != nil {
		fmt.Fprintf(os.Stderr, "[airun] warning: cannot create history dir: %v\n", err)
		return err
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	for name, content := range map[string][]byte{
		"meta.json":  data,
		"prompt.txt": []byte(rec.Prompt),
		"output.txt": []byte(output),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0600); err != nil {
			fmt.Fprintf(os.Stderr, "[airun] warning: cannot write %s: %v\n", name, err)
		}
	}
	return nil
}
```

- [ ] **Step 2: Verify build and tests**

Run: `cd /Users/codegeek/src/agent-runtime && go build ./... && go test ./...`
Expected: All pass

- [ ] **Step 3: Commit**

```bash
cd /Users/codegeek/src/agent-runtime
git add internal/history/history.go
git commit -m "security: write history files with 0600 and dirs with 0700

History contains prompts and agent output which may include sensitive
data. Previously written world-readable (0644/0755). Also propagate
write errors to stderr instead of silently discarding."
```

---

### Task 9: Fix envfile temp directory

`envfile.Write` creates credential files in `/tmp` (world-traversable).

**Acceptance criteria:**
- Temp env files are created in `~/.airun/tmp/` with 0700 parent dir
- Cleanup still validates the `.airun-` prefix
- Existing functionality unchanged

**Files:**
- Modify: `internal/envfile/envfile.go`

- [ ] **Step 1: Use private temp dir**

Replace `Write` in `internal/envfile/envfile.go`:

```go
import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

func privateTmpDir() string {
	usr, err := user.Current()
	if err != nil {
		return os.TempDir() // fallback
	}
	dir := filepath.Join(usr.HomeDir, ".airun", "tmp")
	os.MkdirAll(dir, 0700)
	return dir
}

func Write(envVars []string) (string, error) {
	f, err := os.CreateTemp(privateTmpDir(), ".airun-*.env")
	if err != nil {
		return "", fmt.Errorf("cannot create temp env file: %w", err)
	}
	defer f.Close()
	if err := os.Chmod(f.Name(), 0600); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("cannot chmod env file: %w", err)
	}
	for _, env := range envVars {
		if _, err := fmt.Fprintln(f, env); err != nil {
			os.Remove(f.Name())
			return "", fmt.Errorf("cannot write env file: %w", err)
		}
	}
	return f.Name(), nil
}
```

- [ ] **Step 2: Verify build**

Run: `cd /Users/codegeek/src/agent-runtime && go build ./...`

- [ ] **Step 3: Commit**

```bash
cd /Users/codegeek/src/agent-runtime
git add internal/envfile/envfile.go
git commit -m "security: write temp env files to ~/.airun/tmp/ instead of /tmp

Credential-bearing temp files were created in the world-traversable
/tmp directory. Use a private per-user directory with 0700 permissions."
```

---

### Task 10: Fix rand.Read error in connect.go

**Files:**
- Modify: `internal/proxy/connect.go:167-169`

- [ ] **Step 1: Check rand.Read error**

In `internal/proxy/connect.go`, replace lines 167-169:

```go
// OLD:
b := make([]byte, 32)
rand.Read(b)
cj["userID"] = hex.EncodeToString(b)

// NEW:
b := make([]byte, 32)
if _, err := rand.Read(b); err != nil {
    return fmt.Errorf("generate userID: %w", err)
}
cj["userID"] = hex.EncodeToString(b)
```

- [ ] **Step 2: Verify build**

Run: `cd /Users/codegeek/src/agent-runtime && go build ./...`

- [ ] **Step 3: Commit**

```bash
cd /Users/codegeek/src/agent-runtime
git add internal/proxy/connect.go
git commit -m "fix: check crypto/rand.Read error in connect.go"
```

---

### Task 11: Fix proxy Init error handling

`proxy init` silently discards `os.WriteFile` errors — user sees "Created:" but files may not exist.

**Files:**
- Modify: `internal/proxy/server.go:89-94`

- [ ] **Step 1: Check WriteFile errors**

In `internal/proxy/server.go`, replace lines 90-94:

```go
// OLD:
os.WriteFile(configPath, []byte(template), 0600)
fmt.Printf("  Created: %s\n", configPath)

os.WriteFile(studentsPath, []byte("[]\n"), 0600)
fmt.Printf("  Created: %s\n", studentsPath)

// NEW:
if err := os.WriteFile(configPath, []byte(template), 0600); err != nil {
    return fmt.Errorf("write %s: %w", configPath, err)
}
fmt.Printf("  Created: %s\n", configPath)

if err := os.WriteFile(studentsPath, []byte("[]\n"), 0600); err != nil {
    return fmt.Errorf("write %s: %w", studentsPath, err)
}
fmt.Printf("  Created: %s\n", studentsPath)
```

- [ ] **Step 2: Verify build**

Run: `cd /Users/codegeek/src/agent-runtime && go build ./...`

- [ ] **Step 3: Commit**

```bash
cd /Users/codegeek/src/agent-runtime
git add internal/proxy/server.go
git commit -m "fix: check os.WriteFile errors in proxy Init"
```

---

### Task 12: Rate limiter — key on user name, add eviction

Rate limiter keys on raw token string. Unbounded memory growth. Token rotation bypasses limits.

**Acceptance criteria:**
- Rate limiter keyed on user name, not token
- Stale buckets (no requests for 5 minutes) are evicted
- `Allow` takes `userKey string` (caller passes student name)
- Existing rate limit behavior unchanged

**Files:**
- Modify: `internal/proxy/ratelimit.go`
- Modify: `internal/proxy/handler.go:55` (pass student name to Allow)

- [ ] **Step 1: Update ratelimit.go**

Replace entire `internal/proxy/ratelimit.go`:

```go
package proxy

import (
	"sync"
	"time"
)

const (
	rateLimitWindow = time.Minute
	bucketEvictAge  = 5 * time.Minute
)

type bucket struct {
	times    []time.Time
	lastSeen time.Time
}

type RateLimiter struct {
	rpm     int
	buckets map[string]*bucket
	mu      sync.Mutex
}

func NewRateLimiter(rpm int) *RateLimiter {
	return &RateLimiter{rpm: rpm, buckets: make(map[string]*bucket)}
}

func (r *RateLimiter) Allow(userKey string) bool {
	if r.rpm <= 0 {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-rateLimitWindow)
	b, ok := r.buckets[userKey]
	if !ok {
		b = &bucket{}
		r.buckets[userKey] = b
	}
	b.lastSeen = now
	valid := b.times[:0]
	for _, t := range b.times {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	b.times = valid
	if len(b.times) >= r.rpm {
		return false
	}
	b.times = append(b.times, now)

	// Lazy eviction: purge stale buckets periodically
	if len(r.buckets) > 100 {
		for k, bk := range r.buckets {
			if now.Sub(bk.lastSeen) > bucketEvictAge {
				delete(r.buckets, k)
			}
		}
	}
	return true
}
```

- [ ] **Step 2: Update handler.go to pass student name**

In `internal/proxy/handler.go`, the `ServeHTTP` method already has `student` at line 50. Change the `Allow` call at line 55:

```go
// OLD:
if !h.limiter.Allow(token) {

// NEW:
if !h.limiter.Allow(student.Name) {
```

- [ ] **Step 3: Run all proxy tests**

Run: `cd /Users/codegeek/src/agent-runtime && go test ./internal/proxy/... -v`
Expected: All PASS (ratelimit_test.go may need `Allow` signature update — the test passes `userToken` string, which now represents a user name; semantically equivalent for the test)

- [ ] **Step 4: Commit**

```bash
cd /Users/codegeek/src/agent-runtime
git add internal/proxy/ratelimit.go internal/proxy/handler.go
git commit -m "security: key rate limiter on user name, add stale bucket eviction

Rate limiter was keyed on raw token, allowing limit bypass via token
rotation and causing unbounded memory growth. Now keyed on student name.
Stale buckets (5min inactive) evicted when map exceeds 100 entries."
```

---

## Phase 3: Architecture Cleanup

### Task 13: Remove dead code and fix trivial issues

**Acceptance criteria:**
- `isMinimax()` removed
- `profile.go` guards `user.Current()` nil return
- `setup.go` replaces `goto` with conditional

**Files:**
- Modify: `internal/config/config.go:167-169`
- Modify: `internal/profile/profile.go:22,43,62`
- Modify: `internal/setup/setup.go:31-33`

- [ ] **Step 1: Remove isMinimax**

Delete lines 167-169 in `internal/config/config.go`:

```go
// DELETE:
func (c *Config) isMinimax() bool {
	return NormalizeProvider(c.Provider) == "minimax"
}
```

- [ ] **Step 2: Guard user.Current() in profile.go**

In `internal/profile/profile.go`, update all three call sites:

```go
// Line 22 — Load()
func Load(name string) (*Profile, error) {
	usr, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}
	// ... rest unchanged

// Line 43 — List()
func List() ([]string, error) {
	usr, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}
	// ... rest unchanged

// Line 62 — SkillPaths()
func (p *Profile) SkillPaths() []string {
	usr, err := user.Current()
	if err != nil {
		return nil
	}
	// ... rest unchanged
```

- [ ] **Step 3: Replace goto in setup.go**

In `internal/setup/setup.go`, replace lines 29-33 and 115:

```go
// OLD (lines 29-33):
		if strings.TrimSpace(strings.ToLower(answer)) != "y" {
			fmt.Println("  Keeping existing config.")
			goto dirs
		}
// ...
// line 115:
dirs:

// NEW (lines 29-33):
		if strings.TrimSpace(strings.ToLower(answer)) != "y" {
			fmt.Println("  Keeping existing config.")
			isNew = false // skip key config block
		}
```

Wrap the key-configuration block (lines 35-113) in `if isNew || <reconfigure>`:

```go
	shouldConfigure := isNew
	if !isNew {
		// already handled above — reconfigure only if answer was "y"
		// Check if we did NOT skip
		shouldConfigure = strings.TrimSpace(strings.ToLower(answer)) == "y"
	}

	if shouldConfigure {
		// ... entire key config block (lines 35-113)
	}

	// Directories (was after 'dirs:' label)
```

Actually, simpler approach — just use a boolean:

```go
func Run() error {
	usr, _ := user.Current()
	home := usr.HomeDir
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Agent Runtime — Setup")
	fmt.Println()

	envFile := filepath.Join(home, ".airun.env")
	configureKeys := true
	if _, err := os.Stat(envFile); err == nil {
		fmt.Printf("  Config exists: %s\n", envFile)
		fmt.Print("  Reconfigure keys? [y/N] ")
		answer, _ := reader.ReadString('\n')
		configureKeys = strings.TrimSpace(strings.ToLower(answer)) == "y"
		if !configureKeys {
			fmt.Println("  Keeping existing config.")
		}
	}

	if configureKeys {
		// ... existing key config block unchanged (lines 37-113)
	}

	// Directories — rest unchanged from line 117
```

- [ ] **Step 4: Verify build and tests**

Run: `cd /Users/codegeek/src/agent-runtime && go build ./... && go test ./...`

- [ ] **Step 5: Commit**

```bash
cd /Users/codegeek/src/agent-runtime
git add internal/config/config.go internal/profile/profile.go internal/setup/setup.go
git commit -m "cleanup: remove dead isMinimax, guard user.Current, replace goto in setup"
```

---

### Task 14: Warn when profile lists plugins (currently silently ignored)

`Profile.Plugins` is parsed from YAML but never consumed in `profileMounts`.

**Acceptance criteria:**
- Warning printed to stderr when a profile has non-empty `Plugins`
- No behavior change otherwise

**Files:**
- Modify: `internal/runner/runner.go:271` (inside `profileMounts`)

- [ ] **Step 1: Add warning**

In `internal/runner/runner.go`, add at the beginning of `profileMounts`:

```go
func profileMounts(p *profile.Profile) (volumes []string, settingsPath string, err error) {
	if len(p.Plugins) > 0 {
		fmt.Fprintf(os.Stderr, "[airun] warning: profile plugins are not yet supported (ignored: %s)\n",
			strings.Join(p.Plugins, ", "))
	}
	// ... rest unchanged
```

- [ ] **Step 2: Verify build**

Run: `cd /Users/codegeek/src/agent-runtime && go build ./...`

- [ ] **Step 3: Commit**

```bash
cd /Users/codegeek/src/agent-runtime
git add internal/runner/runner.go
git commit -m "fix: warn when profile plugins are specified but not yet supported"
```

---

### Task 15: Deduplicate fetchModels

Both `proxy/connect.go:261` and `keys/keys.go:379` implement the same `GET /v1/models` call.

**Acceptance criteria:**
- `proxy/connect.go` calls `keys.FetchRemoteModels` instead of its own `fetchModels`
- Internal `fetchModels` in `connect.go` is deleted
- Behavior unchanged

**Files:**
- Modify: `internal/proxy/connect.go`

- [ ] **Step 1: Replace connect.go's fetchModels with keys.FetchRemoteModels**

In `internal/proxy/connect.go`, add import:

```go
import (
    // ... existing
    "github.com/miolamio/agent-runtime/internal/keys"
)
```

Replace the call at line 44:

```go
// OLD:
models, err := fetchModels(proxyURL, token)

// NEW:
models, err := keys.FetchRemoteModels(proxyURL, token)
```

Delete the entire `fetchModels` function (lines 261-297).

- [ ] **Step 2: Verify build**

Run: `cd /Users/codegeek/src/agent-runtime && go build ./...`

- [ ] **Step 3: Run all tests**

Run: `cd /Users/codegeek/src/agent-runtime && go test ./...`

- [ ] **Step 4: Commit**

```bash
cd /Users/codegeek/src/agent-runtime
git add internal/proxy/connect.go
git commit -m "refactor: deduplicate fetchModels — use keys.FetchRemoteModels"
```

---

### Task 16: Pass authenticated student via request context

`handleMessages` re-extracts the token and re-calls `FindByToken` — redundant double lock acquisition per request.

**Acceptance criteria:**
- Student is stored in request context by `ServeHTTP`
- `handleMessages` retrieves student from context (no second `FindByToken` call)
- Logging still shows student name

**Files:**
- Modify: `internal/proxy/handler.go`

- [ ] **Step 1: Add context key and helpers**

Add to `internal/proxy/handler.go` after the imports:

```go
type contextKey string

const studentContextKey contextKey = "student"

func withStudent(r *http.Request, s *students.Student) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), studentContextKey, s))
}

func studentFromContext(r *http.Request) *students.Student {
	s, _ := r.Context().Value(studentContextKey).(*students.Student)
	return s
}
```

Add `"context"` to imports.

- [ ] **Step 2: Store student in context in ServeHTTP**

In `ServeHTTP`, after the `student` lookup (line 50-54), pass it via context:

```go
// After line 54 (rate limit check), before line 59:
r = withStudent(r, student)
h.mux.ServeHTTP(w, r)
```

- [ ] **Step 3: Simplify handleMessages**

Replace lines 108-119 in `handleMessages`:

```go
// OLD (lines 108-119):
// Extract token the same way as ServeHTTP (x-api-key or Bearer)
token := r.Header.Get("x-api-key")
if token == "" {
    if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
        token = strings.TrimPrefix(auth, "Bearer ")
    }
}
student := h.students.FindByToken(token)
name := "unknown"
if student != nil {
    name = student.Name
}

// NEW:
student := studentFromContext(r)
name := "unknown"
if student != nil {
    name = student.Name
}
token := r.Header.Get("x-api-key")
```

The `token` variable is still needed for the masked log line below. Simplify the mask:

```go
if token == "" {
    if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
        token = strings.TrimPrefix(auth, "Bearer ")
    }
}
```

Actually, keep the token extraction for the log mask only. The student lookup is gone.

- [ ] **Step 4: Run all proxy tests**

Run: `cd /Users/codegeek/src/agent-runtime && go test ./internal/proxy/... -v`

- [ ] **Step 5: Commit**

```bash
cd /Users/codegeek/src/agent-runtime
git add internal/proxy/handler.go
git commit -m "refactor: pass authenticated student via request context

Eliminates redundant FindByToken call and double RWMutex lock
acquisition on every /v1/messages request."
```

---

## Phase 4: Missing Features

### Task 17: Add Anthropic as first-party provider

Most users have direct Anthropic API keys. Currently only third-party proxies are supported.

**Acceptance criteria:**
- `airun keys add anthropic` guides user to get an Anthropic API key
- `airun --provider anthropic "prompt"` routes to `api.anthropic.com`
- Provider aliases: `a`, `anthropic`
- Default model: `claude-sonnet-4-6-20250514`

**Files:**
- Modify: `internal/keys/providers.go`
- Modify: `internal/config/config.go`

- [ ] **Step 1: Add provider to providers.go**

In `internal/keys/providers.go`, add entry to `providers` slice (before the `remote` entry):

```go
{
    ID:          "anthropic",
    Name:        "Anthropic",
    RegisterURL: "https://console.anthropic.com",
    Steps: []string{
        "Go to https://console.anthropic.com",
        "Sign up / Sign in",
        "Navigate to API Keys -> Create Key",
        "Copy the key (starts with sk-ant-)",
    },
    BaseURL:    "https://api.anthropic.com",
    Model:      "claude-sonnet-4-6-20250514",
    EnvKey:     "ANTHROPIC_API_KEY",
    EnvBaseURL: "ANTHROPIC_BASE_URL_DIRECT",
    EnvModel:   "ANTHROPIC_MODEL",
},
```

Update `ProviderByAlias`:

```go
case "a", "anthropic":
    idx = 3
case "r", "remote":
    idx = 4
```

- [ ] **Step 2: Add config fields and env loading**

In `internal/config/config.go`, add fields to `Config`:

```go
// Anthropic (direct)
AnthropicAPIKey  string
AnthropicBaseURL string
AnthropicModel   string
```

Add defaults:

```go
AnthropicBaseURL: "https://api.anthropic.com",
AnthropicModel:   "claude-sonnet-4-6-20250514",
```

Add cases in `loadEnvFile`:

```go
case "ANTHROPIC_API_KEY":
    c.AnthropicAPIKey = val
case "ANTHROPIC_BASE_URL_DIRECT":
    c.AnthropicBaseURL = val
case "ANTHROPIC_MODEL":
    c.AnthropicModel = val
```

Add to `NormalizeProvider`:

```go
case "a", "anthropic":
    return "anthropic"
```

Add to `ContainerEnvWithModel`:

```go
case "anthropic":
    baseURL = c.AnthropicBaseURL
    apiKey = c.AnthropicAPIKey
    model = c.AnthropicModel
```

Add to `Show()`:

```go
Anthropic: %s (key: %s)
// ...
c.AnthropicModel, masked(c.AnthropicAPIKey),
```

- [ ] **Step 3: Add to runner.go model resolution**

In `internal/runner/runner.go`, add case at line 72:

```go
case "anthropic":
    model = cfg.AnthropicModel
```

- [ ] **Step 4: Verify build**

Run: `cd /Users/codegeek/src/agent-runtime && go build ./... && go test ./...`

- [ ] **Step 5: Commit**

```bash
cd /Users/codegeek/src/agent-runtime
git add internal/keys/providers.go internal/config/config.go internal/runner/runner.go
git commit -m "feat: add Anthropic as first-party provider

Aliases: a, anthropic. Default model: claude-sonnet-4-6-20250514.
API endpoint: api.anthropic.com. Env key: ANTHROPIC_API_KEY."
```

---

### Task 18: Fix airun init profile path

`setup.go:134` uses relative path `configs/profiles` which fails when running the installed binary from `~/.local/bin/`.

**Acceptance criteria:**
- Profile templates are found regardless of working directory
- Falls back gracefully with a message when templates are unavailable

**Files:**
- Modify: `internal/setup/setup.go:134`

- [ ] **Step 1: Resolve path relative to executable**

In `internal/setup/setup.go`, replace line 134:

```go
// OLD:
srcProfiles := "configs/profiles"

// NEW:
// Try relative to executable first, then relative to cwd
srcProfiles := "configs/profiles"
if exe, err := os.Executable(); err == nil {
    candidate := filepath.Join(filepath.Dir(exe), "..", "configs", "profiles")
    if _, err := os.Stat(candidate); err == nil {
        srcProfiles = candidate
    }
    // Also try sibling to repo root (when exe is in bin/)
    candidate = filepath.Join(filepath.Dir(exe), "..", "configs", "profiles")
    if _, err := os.Stat(candidate); err == nil {
        srcProfiles = candidate
    }
}
```

- [ ] **Step 2: Verify build**

Run: `cd /Users/codegeek/src/agent-runtime && go build ./...`

- [ ] **Step 3: Commit**

```bash
cd /Users/codegeek/src/agent-runtime
git add internal/setup/setup.go
git commit -m "fix: resolve profile templates relative to executable in airun init"
```

---

### Task 19: Fix setup.sh broken profile copy

`scripts/setup.sh:18` copies `*.env.example` but profile files are `*.yaml`.

**Acceptance criteria:**
- The glob matches actual profile files

**Files:**
- Modify: `scripts/setup.sh`

- [ ] **Step 1: Read and fix the script**

In `scripts/setup.sh`, find the line with `*.env.example` and replace with `*.yaml`:

```bash
# OLD:
cp -n "$PROJECT_DIR"/configs/profiles/*.env.example ~/airun-profiles/

# NEW:
cp -n "$PROJECT_DIR"/configs/profiles/*.yaml ~/airun-profiles/ 2>/dev/null || true
```

- [ ] **Step 2: Commit**

```bash
cd /Users/codegeek/src/agent-runtime
git add scripts/setup.sh
git commit -m "fix: setup.sh copies *.yaml profiles instead of nonexistent *.env.example"
```

---

### Task 20: Mount agents dir from config

`config.AgentsDir` is set to `~/airun-agents/` but never mounted into containers. This is a one-line fix that unlocks Claude Code agent definitions.

**Acceptance criteria:**
- `~/airun-agents/` is mounted read-only at `/home/claude/.claude/agents/` in the container
- Only mounted if the directory exists

**Files:**
- Modify: `internal/runner/runner.go` (in `runDocker`, before `args = append(args, imageName)`)

- [ ] **Step 1: Add agents mount**

In `internal/runner/runner.go`, add before `args = append(args, imageName)` (line 149):

```go
// Mount agents directory if it exists
if info, err := os.Stat(cfg.AgentsDir); err == nil && info.IsDir() {
    args = append(args, "-v", cfg.AgentsDir+":/home/claude/.claude/agents:ro")
}
```

Add the same to `runDockerWithExport` before `createArgs = append(createArgs, imageName, ...)` (line 221):

```go
if info, err := os.Stat(cfg.AgentsDir); err == nil && info.IsDir() {
    createArgs = append(createArgs, "-v", cfg.AgentsDir+":/home/claude/.claude/agents:ro")
}
```

- [ ] **Step 2: Verify build**

Run: `cd /Users/codegeek/src/agent-runtime && go build ./...`

- [ ] **Step 3: Commit**

```bash
cd /Users/codegeek/src/agent-runtime
git add internal/runner/runner.go
git commit -m "feat: mount ~/airun-agents/ into containers as Claude Code agents"
```

---

## Phase 5: Test Coverage

### Task 21: Add tests for internal/config

**Acceptance criteria:**
- `NormalizeProvider` tested for all aliases including edge cases
- `ContainerEnvWithModel` tested for each provider
- Kimi-specific `ENABLE_TOOL_SEARCH=false` verified
- Empty provider defaults to `zai`

**Files:**
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Write config_test.go**

Create `internal/config/config_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeProvider(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"z", "zai"}, {"zai", "zai"}, {"", "zai"},
		{"m", "minimax"}, {"mm", "minimax"}, {"minimax", "minimax"},
		{"k", "kimi"}, {"kimi", "kimi"},
		{"a", "anthropic"}, {"anthropic", "anthropic"},
		{"r", "remote"}, {"remote", "remote"},
		{"Z", "zai"}, {"ZAI", "zai"}, {"MINIMAX", "minimax"},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		got := NormalizeProvider(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeProvider(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestContainerEnvWithModel_ZAI(t *testing.T) {
	cfg := &Config{
		Provider:   "zai",
		ZaiBaseURL: "https://api.z.ai/api/anthropic",
		ZaiAPIKey:  "sk-test-zai",
		ZaiModel:   "glm-5.1",
		APITimeout: "3000000",
		DisableTraffic: "1",
	}
	env := cfg.ContainerEnvWithModel("zai", "")
	assertEnvContains(t, env, "ANTHROPIC_BASE_URL=https://api.z.ai/api/anthropic")
	assertEnvContains(t, env, "ANTHROPIC_AUTH_TOKEN=sk-test-zai")
	assertEnvContains(t, env, "ANTHROPIC_DEFAULT_SONNET_MODEL=glm-5.1")
}

func TestContainerEnvWithModel_Kimi(t *testing.T) {
	cfg := &Config{
		KimiBaseURL: "https://api.kimi.com/coding/",
		KimiAPIKey:  "sk-test-kimi",
		KimiModel:   "kimi-k2.5",
		APITimeout:  "3000000",
		DisableTraffic: "1",
	}
	env := cfg.ContainerEnvWithModel("kimi", "")
	assertEnvContains(t, env, "ENABLE_TOOL_SEARCH=false")
}

func TestContainerEnvWithModel_Override(t *testing.T) {
	cfg := &Config{
		ZaiBaseURL: "https://api.z.ai/api/anthropic",
		ZaiAPIKey:  "sk-test",
		ZaiModel:   "glm-5.1",
		APITimeout: "3000000",
		DisableTraffic: "1",
	}
	env := cfg.ContainerEnvWithModel("zai", "glm-4.7")
	assertEnvContains(t, env, "ANTHROPIC_DEFAULT_SONNET_MODEL=glm-4.7")
}

func TestLoadEnvFile(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".airun.env")
	content := `ARUN_WORKSPACE=/tmp/ws
ARUN_MODE=bind
ARUN_PROVIDER=minimax
ZAI_API_KEY=sk-zai-test
ZAI_BASE_URL=https://custom.z.ai/api
MINIMAX_API_KEY=mm-test
KIMI_BASE_URL=https://kimi.custom/coding/
`
	os.WriteFile(envFile, []byte(content), 0600)

	cfg := &Config{}
	if err := cfg.loadEnvFile(envFile); err != nil {
		t.Fatalf("loadEnvFile: %v", err)
	}
	if cfg.Workspace != "/tmp/ws" {
		t.Errorf("Workspace = %q, want /tmp/ws", cfg.Workspace)
	}
	if cfg.Mode != "bind" {
		t.Errorf("Mode = %q, want bind", cfg.Mode)
	}
	if cfg.Provider != "minimax" {
		t.Errorf("Provider = %q, want minimax", cfg.Provider)
	}
	if cfg.ZaiAPIKey != "sk-zai-test" {
		t.Errorf("ZaiAPIKey = %q, want sk-zai-test", cfg.ZaiAPIKey)
	}
	if cfg.MinimaxAPIKey != "mm-test" {
		t.Errorf("MinimaxAPIKey = %q, want mm-test", cfg.MinimaxAPIKey)
	}
}

func TestLoadEnvFile_EqualsInValue(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".airun.env")
	os.WriteFile(envFile, []byte("ZAI_BASE_URL=https://api.z.ai/api?foo=bar\n"), 0600)

	cfg := &Config{}
	cfg.loadEnvFile(envFile)
	if cfg.ZaiBaseURL != "https://api.z.ai/api?foo=bar" {
		t.Errorf("URL with = not parsed correctly: %q", cfg.ZaiBaseURL)
	}
}

func assertEnvContains(t *testing.T, env []string, want string) {
	t.Helper()
	for _, e := range env {
		if e == want {
			return
		}
	}
	t.Errorf("env does not contain %q\ngot: %s", want, strings.Join(env, "\n"))
}
```

- [ ] **Step 2: Run tests**

Run: `cd /Users/codegeek/src/agent-runtime && go test ./internal/config/ -v`
Expected: All PASS

- [ ] **Step 3: Commit**

```bash
cd /Users/codegeek/src/agent-runtime
git add internal/config/config_test.go
git commit -m "test: add unit tests for config package

Cover NormalizeProvider aliases, ContainerEnvWithModel per provider,
Kimi ENABLE_TOOL_SEARCH, model override, env file parsing with
equals-in-value edge case."
```

---

### Task 22: Add tests for internal/envfile

**Acceptance criteria:**
- File permissions verified as 0600
- Cleanup validates prefix
- Content integrity checked

**Files:**
- Create: `internal/envfile/envfile_test.go`

- [ ] **Step 1: Write envfile_test.go**

Create `internal/envfile/envfile_test.go`:

```go
package envfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWrite_Permissions(t *testing.T) {
	path, err := Write([]string{"FOO=bar"})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	defer Cleanup(path)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("permissions = %o, want 0600", info.Mode().Perm())
	}
}

func TestWrite_Content(t *testing.T) {
	envVars := []string{"KEY1=val1", "KEY2=val2", "KEY3=val=with=equals"}
	path, err := Write(envVars)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	defer Cleanup(path)

	data, _ := os.ReadFile(path)
	content := string(data)
	for _, env := range envVars {
		if !strings.Contains(content, env) {
			t.Errorf("file missing %q", env)
		}
	}
}

func TestCleanup_ValidPrefix(t *testing.T) {
	path, err := Write([]string{"X=1"})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	Cleanup(path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file still exists after Cleanup")
	}
}

func TestCleanup_WrongPrefix(t *testing.T) {
	f, _ := os.CreateTemp(os.TempDir(), "not-airun-*.txt")
	f.Close()
	path := f.Name()
	defer os.Remove(path)

	Cleanup(path)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("Cleanup deleted file without .airun- prefix")
	}
}

func TestCleanup_EmptyPath(t *testing.T) {
	Cleanup("") // should not panic
}

func TestMaskLog(t *testing.T) {
	path := "/some/long/path/.airun-abc123.env"
	got := MaskLog(path)
	want := ".airun-abc123.env"
	if got != want {
		t.Errorf("MaskLog = %q, want %q", got, want)
	}
}

func TestWrite_InPrivateDir(t *testing.T) {
	path, err := Write([]string{"SECRET=value"})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	defer Cleanup(path)

	dir := filepath.Dir(path)
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	// Parent should be 0700 (our private dir) or at least not world-readable
	perm := info.Mode().Perm()
	if perm&0077 != 0 && strings.Contains(dir, ".airun") {
		t.Errorf("private temp dir permissions = %o, want 0700", perm)
	}
}
```

- [ ] **Step 2: Run tests**

Run: `cd /Users/codegeek/src/agent-runtime && go test ./internal/envfile/ -v`
Expected: All PASS

- [ ] **Step 3: Commit**

```bash
cd /Users/codegeek/src/agent-runtime
git add internal/envfile/envfile_test.go
git commit -m "test: add unit tests for envfile package

Cover file permissions, content integrity, cleanup prefix validation,
empty path safety, and private temp directory."
```

---

### Task 23: Add tests for internal/history

**Acceptance criteria:**
- `Save` writes 3 files with correct content
- `FormatTable` truncation works
- `FormatTable` handles empty list

**Files:**
- Create: `internal/history/history_test.go`

- [ ] **Step 1: Write history_test.go**

Create `internal/history/history_test.go`:

```go
package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSave(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, "test-run")

	rec := RunRecord{
		Timestamp:  "2026-04-07_12-00-00",
		Profile:    "dev",
		Provider:   "zai",
		Model:      "glm-5.1",
		Prompt:     "test prompt",
		DurationMs: 1500,
		ExitCode:   0,
		RunDir:     runDir,
	}
	if err := Save(rec, "test output"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Check meta.json
	metaData, err := os.ReadFile(filepath.Join(runDir, "meta.json"))
	if err != nil {
		t.Fatalf("read meta.json: %v", err)
	}
	var loaded RunRecord
	if err := json.Unmarshal(metaData, &loaded); err != nil {
		t.Fatalf("unmarshal meta.json: %v", err)
	}
	if loaded.Provider != "zai" {
		t.Errorf("meta provider = %q, want zai", loaded.Provider)
	}

	// Check prompt.txt
	promptData, _ := os.ReadFile(filepath.Join(runDir, "prompt.txt"))
	if string(promptData) != "test prompt" {
		t.Errorf("prompt.txt = %q", string(promptData))
	}

	// Check output.txt
	outputData, _ := os.ReadFile(filepath.Join(runDir, "output.txt"))
	if string(outputData) != "test output" {
		t.Errorf("output.txt = %q", string(outputData))
	}

	// Check permissions
	info, _ := os.Stat(filepath.Join(runDir, "meta.json"))
	if info.Mode().Perm() != 0600 {
		t.Errorf("meta.json permissions = %o, want 0600", info.Mode().Perm())
	}
}

func TestFormatTable_Truncation(t *testing.T) {
	rec := RunRecord{
		Timestamp: "2026-04-07_12-00-00",
		Prompt:    "This is a very long prompt that should be truncated at forty characters total",
		Profile:   "dev",
		Provider:  "zai",
	}
	table := FormatTable([]RunRecord{rec})
	if !strings.Contains(table, "...") {
		t.Error("long prompt not truncated")
	}
}

func TestFormatTable_Empty(t *testing.T) {
	table := FormatTable(nil)
	if !strings.Contains(table, "TIME") {
		t.Error("empty table missing header")
	}
	// Should have header + separator only
	lines := strings.Split(strings.TrimSpace(table), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines (header + separator), got %d", len(lines))
	}
}

func TestFormatTable_ExitCode(t *testing.T) {
	records := []RunRecord{
		{Timestamp: "2026-04-07_12-00-00", ExitCode: 0, Prompt: "ok", Profile: "d", Provider: "z"},
		{Timestamp: "2026-04-07_12-01-00", ExitCode: 1, Prompt: "fail", Profile: "d", Provider: "z"},
	}
	table := FormatTable(records)
	lines := strings.Split(strings.TrimSpace(table), "\n")
	if !strings.Contains(lines[2], "ok") {
		t.Error("exit 0 should show 'ok'")
	}
	if !strings.Contains(lines[3], "fail") {
		t.Error("exit 1 should show 'fail'")
	}
}
```

- [ ] **Step 2: Run tests**

Run: `cd /Users/codegeek/src/agent-runtime && go test ./internal/history/ -v`
Expected: All PASS

- [ ] **Step 3: Commit**

```bash
cd /Users/codegeek/src/agent-runtime
git add internal/history/history_test.go
git commit -m "test: add unit tests for history package

Cover Save (3 files, permissions), FormatTable truncation, empty list,
and exit code display."
```

---

### Task 24: Add missing proxy handler and forward tests

Critical security gaps: no test for Authorization header stripping, rate limiting at handler level, or revoked user rejection.

**Acceptance criteria:**
- Test verifies Authorization header is NOT forwarded upstream
- Test verifies rate-limited user gets 429
- Test verifies revoked user gets 401
- Test verifies malformed body gets 400

**Files:**
- Modify: `internal/proxy/forward_test.go`
- Modify: `internal/proxy/handler_test.go`

- [ ] **Step 1: Add forward_test.go — Authorization header stripping**

Add to `internal/proxy/forward_test.go`:

```go
func TestForwardRequest_AuthorizationStripped(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("Authorization header leaked to upstream: %s", auth)
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"ok": true}`))
	}))
	defer upstream.Close()

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer sk-ai-student-token-should-not-leak")
	req.Header.Set("x-api-key", "original-key")
	rec := httptest.NewRecorder()

	ForwardRequest(rec, req, upstream.URL, "real-provider-key", "test-agent")

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}
```

- [ ] **Step 2: Add handler_test.go — rate limiting and revoked user**

Add to `internal/proxy/handler_test.go`:

```go
func TestRateLimitedUser(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"msg_1","type":"message","model":"m1","content":[{"type":"text","text":"ok"}]}`))
	}))
	defer upstream.Close()

	cfg := &ProxyConfig{
		RPM:       1,
		UserAgent: "test",
		Providers: map[string]ProviderEntry{
			"test": {BaseURL: upstream.URL, APIKey: "key", Models: []string{"m1"}},
		},
	}
	dir := t.TempDir()
	sp := filepath.Join(dir, "students.json")
	os.WriteFile(sp, []byte("[]"), 0600)
	mgr := students.New(sp)
	tok, _ := mgr.Add("RateLimited")

	h := NewHandler(cfg, mgr)

	body := `{"model":"m1","messages":[{"role":"user","content":"hi"}]}`

	// First request — should succeed
	req1 := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req1.Header.Set("x-api-key", tok)
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req1)
	if rec1.Code != 200 {
		t.Fatalf("first request: status = %d, want 200", rec1.Code)
	}

	// Second request — should be rate limited
	req2 := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req2.Header.Set("x-api-key", tok)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 429 {
		t.Errorf("second request: status = %d, want 429", rec2.Code)
	}
}

func TestRevokedUserRejected(t *testing.T) {
	cfg := &ProxyConfig{
		UserAgent: "test",
		Providers: map[string]ProviderEntry{
			"test": {BaseURL: "http://localhost", APIKey: "key", Models: []string{"m1"}},
		},
	}
	dir := t.TempDir()
	sp := filepath.Join(dir, "students.json")
	os.WriteFile(sp, []byte("[]"), 0600)
	mgr := students.New(sp)
	tok, _ := mgr.Add("Revoked")
	mgr.Revoke("Revoked")

	h := NewHandler(cfg, mgr)
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("x-api-key", tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 401 {
		t.Errorf("revoked user: status = %d, want 401", rec.Code)
	}
}
```

- [ ] **Step 3: Run all proxy tests**

Run: `cd /Users/codegeek/src/agent-runtime && go test ./internal/proxy/... -v`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
cd /Users/codegeek/src/agent-runtime
git add internal/proxy/forward_test.go internal/proxy/handler_test.go
git commit -m "test: add security-critical proxy tests

Cover Authorization header stripping in forward, rate limiting at
handler level (429), and revoked user rejection (401)."
```

---

## Dependency Order

```
Phase 1 (Critical):  Task 1 → 2 → 3 → 4 → 5 → 6 → 7  (sequential, each builds on stable base)
Phase 2 (Security):  Task 8, 9, 10, 11 (independent)  →  Task 12 (depends on handler context from Phase 3)
Phase 3 (Cleanup):   Task 13 → 14 → 15 → 16
Phase 4 (Features):  Task 17, 18, 19, 20 (independent)
Phase 5 (Tests):     Task 21, 22, 23 (independent)  →  Task 24 (after Phase 1-3 changes)
```

Tasks within a phase marked "independent" can be executed in parallel by separate agents.

---

## Summary of Acceptance Criteria (End State)

| Area | Requirement |
|------|-------------|
| **P0 bugs** | No dangling pointers in students Manager; `--loop` passes `--max-turns` to claude; tokens SHA256-hashed |
| **Security** | Proxy listens on localhost by default; request body capped at 10MB; history files 0600; temp envfiles in private dir; rate limiter keyed on user name with eviction |
| **Error handling** | No silently discarded `os.WriteFile`/`http.NewRequest` errors; `StudentList` no panic on short tokens |
| **Architecture** | No dead code; no duplicated `fetchModels`; student in request context; profile plugin warning |
| **Features** | Anthropic as first-party provider; agents dir mounted; setup profile path fixed; setup.sh fixed |
| **Tests** | `config`, `envfile`, `history` packages have tests; proxy forward/handler have security-critical tests; all tests pass with `-race` |
| **All tests** | `go test -race ./...` passes with zero failures |

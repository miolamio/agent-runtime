# airun v0.6.0 — Changelog & Remaining Work

> Date: 2026-04-08
> Status: Shipped, Docker image built, testing in progress

## What was done (14 commits)

### Full codebase review (5 parallel agents)

Ran 5 specialized review agents in parallel:
- **bugs-reviewer** — found 12 code issues including P0 memory corruption
- **security-reviewer** — found 10 vulnerabilities including 2 critical
- **arch-reviewer** — found 7 architecture problems, 13 design violations
- **test-reviewer** — assessed coverage gaps, proposed 30+ concrete tests
- **functionality-reviewer** — found 7 broken/missing advertised features

Results synthesized into a prioritized roadmap of 30 tasks across 5 phases.
Full plan: `docs/superpowers/plans/2026-04-07-airun-roadmap.md`

### Critical bug fixes

| Fix | Commit | Details |
|-----|--------|---------|
| Dangling pointer in `byToken` map | `0984ac1` | `students.Manager.Add()` stored pointers into a slice. After slice reallocation, all map entries became dangling. Extracted `rebuildIndex()` method. |
| `--loop` flag was a no-op | `0984ac1` | `Loop`/`MaxLoops` were parsed but never read. Now pass `--max-turns N` to claude CLI. |
| `ARUN_MODE=snapshot` not implemented | `ef25a5d` | Config loaded `Mode` but runner always did bind mount. Implemented full snapshot lifecycle: `docker create` → `docker cp` → `docker start` → `docker rm`. |

### Security fixes

| Fix | Commit | Details |
|-----|--------|---------|
| SHA-256 token hashing | `5f79f82` | Tokens were plaintext (README claimed SHA-256). Now: hash on store, constant-time compare via `crypto/subtle`, auto-migrate existing plaintext tokens. |
| MaxBytesReader (10 MB) | `0984ac1` | `io.ReadAll(r.Body)` without limit → OOM DoS. Added `http.MaxBytesReader`. |
| Default listen `127.0.0.1:8080` | `0984ac1` | Proxy listened on all interfaces. Changed to localhost. |
| TLS support for proxy | `21192a5` | Added `tls_cert`/`tls_key` fields to ProxyConfig, conditional `ListenAndServeTLS`. |
| Hardcode `/v1/messages` forward path | `21192a5` | `r.URL.Path` was passed unsanitized to upstream URL. Now hardcoded. |
| History files 0600 | `6ad841a` | Prompts/output were world-readable (0644). Changed to 0600/0700. |
| Private temp dir | `6ad841a` | Credential temp files in `/tmp` → `~/.airun/tmp/` (0700). |
| Rate limiter: key on user name | `6ad841a` | Was keyed on raw token (rotation bypass, unbounded growth). Now keyed on student name with stale bucket eviction. |
| `rand.Read` error check | `6ad841a` | `crypto/rand.Read` return value was ignored in connect.go. |
| Nil-deref on malformed URL | `0984ac1` | `http.NewRequest` error ignored in connect.go and keys.go. |
| Panic on short tokens | `0984ac1` | `StudentList` sliced tokens without length check. |
| Proxy Init error handling | `6ad841a` | `os.WriteFile` errors silently discarded. |

### Architecture & refactoring

| Change | Commit | Details |
|--------|--------|---------|
| Path consolidation `~/.airun/` | `fb1dfd0` | All configs moved from scattered `~/` paths into `~/.airun/`. Auto-migration on first run. |
| Dedup `fetchModels` | `4d65833` | Removed private `fetchModels` from connect.go, uses `keys.FetchRemoteModels`. |
| Student in request context | `4d65833` | Eliminated redundant `FindByToken` + double RWMutex lock per request. |
| Dead code removal | `6ad841a` | `isMinimax()` (zero callers), `goto` in setup.go, unguarded `user.Current()`. |
| Plugin injection | `0ca33c2` | Profile plugins now installed via generated post-init.sh. |
| yq → Python3 | `0ca33c2` | `init-container.sh` no longer depends on yq. |

### Features

| Feature | Commit | Details |
|---------|--------|---------|
| Anthropic as provider | `304af11` | Aliases: `a`, `anthropic`. Model: `claude-sonnet-4-6-20250514`. |
| Agents dir mounting | `304af11` | `~/.airun/agents/` mounted RO into containers. |
| Setup fixes | `304af11` | `setup.sh` glob fix, init profile path relative to executable. |
| Per-profile state volumes | `79aeba4` | Each profile gets `airun-state-{profile}` Docker volume. |
| Browser display (noVNC + CDP) | `79aeba4` | `--browser vnc\|cdp\|both`. Xvfb + x11vnc + noVNC in Dockerfile. Ports 6080/9222. |
| Profiles: research, dev, ceo | `79aeba4` | Three production profiles with tailored plugins/skills. |

### Tests

| Area | Commit | Count |
|------|--------|-------|
| config: NormalizeProvider, ContainerEnvWithModel, loadEnvFile | `d253e9b` | 7 |
| envfile: permissions, cleanup, content | `d253e9b` | 6 |
| history: Save, FormatTable | `d253e9b` | 4 |
| proxy: auth header stripping | `d253e9b` | 1 |
| proxy: rate limit 429, revoked user 401, body 413 | `d253e9b` + `0984ac1` | 3 |
| students: corruption, concurrent race, HashToken | `0984ac1` + `5f79f82` | 4 |
| **Total new tests** | | **25** |

Test packages with tests: 2 → 6. `go test -race ./...` green.

---

## Current state & testing notes

**Build:** Docker image `agent-runtime:latest` built successfully (2026-04-08).

**Usage:**
```bash
# Rebuild binary after code changes (always needed!)
go build -o bin/airun ./cmd/airun/

# Copy profile templates to ~/.airun/profiles/ (one-time)
cp configs/profiles/*.yaml ~/.airun/profiles/

# Run with profile
./bin/airun shell -p dev
./bin/airun shell -p research --browser vnc    # noVNC at http://localhost:6080
./bin/airun shell -p ceo

# Run without profile (default state volume)
./bin/airun shell
```

**Known issues found during testing:**
- Profile YAML templates are in `configs/profiles/` but `airun init` may fail to copy them when running from `~/.local/bin/` (partial fix applied — resolves relative to executable, but edge cases remain)
- noVNC browser viewing needs manual verification (Xvfb + x11vnc + noVNC chain in container)
- Profile plugins install via `post-init.sh` — depends on Claude Code `claude plugin install` being available in container at startup

**Per-profile state volumes:**
```
airun-state-research   # ~/.claude inside container for research profile
airun-state-dev        # ~/.claude inside container for dev profile
airun-state-ceo        # ~/.claude inside container for ceo profile
airun-claude-state     # default (no profile specified)
```

**Metrics:**
- 15 commits total on main
- 30 files changed, ~3500 lines added
- 25 new tests, 6 packages covered
- `go test -race ./...` green

---

## Remaining work (not started)

### Architecture

- [ ] **Deduplicate `runDocker`/`runDockerWithExport`/`runDockerSnapshot`** — 3 functions share 80% of logic (env file creation, docker args, history recording). Extract `buildDockerArgs()` and `recordRun()` helpers. ~3 hours.
- [ ] **Image name constant** — `"agent-runtime:latest"` hardcoded in 2-3 places. Extract to `const imageName`. 15 min.
- [ ] **Commands dir mounting** — `~/.airun/commands/` exists but isn't mounted. Same pattern as agents mount. 15 min.

### Proxy improvements

- [ ] **Auto-reload users on file change** — Replace SIGHUP with `fsnotify` watcher on `students.json`. ~1 day.
- [ ] **Per-user daily usage limits/quotas** — Extend rate limiter with daily counter + persistence to disk. 2-3 days.
- [ ] **`errors.As` for MaxBytesError** — Replace brittle `err.Error()` string match with `errors.As(&http.MaxBytesError{})`. 10 min. (Codex review recommendation)

### Features

- [ ] **Windows native support** — ACL permissions instead of chmod, PowerShell profile, path normalization. Large effort.
- [ ] **Container init from profile** — Auto-install npm/pip packages from profile YAML on container start. 1-2 days.
- [ ] **Proxy connect TLS validation** — `connect-proxy.sh` and `proxy connect` should validate TLS certificates. 1 hour.

### Profiles & Docker

- [ ] **Profile-specific CLAUDE.md injection** — Mount a per-profile CLAUDE.md into container to give Claude context about the profile's purpose. 1 hour.
- [ ] **Profile browser auto-detect** — If a profile includes playwright-cli plugin, auto-set `--browser vnc` without requiring the flag. 30 min.
- [ ] **noVNC password** — Currently VNC is passwordless. Add optional VNC password from profile or env. 30 min.
- [ ] **Rebuild only if needed** — Check image timestamp vs Dockerfile mtime, skip rebuild if unchanged. 1 hour.
- [ ] **gstack in CEO profile** — Currently commented out (macOS only). Explore running gstack's Bun binary via QEMU user-mode emulation in container, or just document as host-only. Investigation.

### Testing

- [ ] **`internal/runner/` tests** — Most complex package, zero tests. Test `stateVolumeForProfile`, `ParseAgentSpec`, snapshot mode arg building. 1 day.
- [ ] **`internal/profile/` tests** — YAML loading, skill path resolution, missing file handling. 2 hours.
- [ ] **Integration test** — End-to-end: build image, run container with mock provider, verify output. 1 day.
- [ ] **Proxy TLS test** — Test `Serve()` with self-signed cert. 1 hour.

### Documentation

- [ ] **Update README.md** — Reflect new profile system, per-profile state, browser display, ~/.airun/ paths, Anthropic provider.
- [ ] **Update CLAUDE.md** — Add new features (snapshot mode, per-profile state, browser flag, Anthropic).
- [ ] **Profile documentation** — How to create custom profiles, available plugins/skills reference.

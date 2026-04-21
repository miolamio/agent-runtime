# airun v0.6.1 — Changelog

> Date: 2026-04-21
> Status: Local main is 19 commits ahead of origin (ready to tag and push)

## Summary

Hardening release. No new user-facing features — focus on closing Go-level blockers surfaced during v0.6.0 review, refactoring `internal/runner` into testable pieces, and standing up a proper end-to-end test harness so regressions can't slip silently anymore.

**Metrics:**
- 19 commits on `main` (not yet on origin)
- `internal/runner/runner.go`: 500 → 439 lines (helpers extracted, duplication removed)
- 25 new unit tests in `internal/runner/`
- 42 new bash-driven e2e tests in `test/e2e/` (41 pass + 1 network-gated skip)
- `bash test/e2e/run-all.sh`: ~8.7s full run including `go build` preflight
- `go test ./...` green, `go test -race ./...` green

---

## Phase 1 — Go blockers, cleanup, unit tests (10 commits)

### Fixes

| Fix | Commit | Details |
|-----|--------|---------|
| `user.Current()` nil-panic risk | `60513f9` | 3 call sites (`cmd/airun`, `setup`, `history`) replaced with `os.UserHomeDir()` which propagates errors and honours `$HOME` on Unix. |
| Cleanup errors silently ignored | `6dc6d4b` | 4 `docker rm` cleanups in runner + `state reset` now log errors. `state reset` also distinguishes "no volume existed" from a real daemon error. |
| Finish HOME-override migration | `eeb61ad` | `config`, `profile`, `envfile` moved from `user.Current()` → `os.UserHomeDir()`. Unblocks `$HOME`-based test isolation (required by e2e suite). |
| `os.IsNotExist` → `errors.Is(err, fs.ErrNotExist)` | `14be0e1` | 7 call sites in 4 files (proxy/server, proxy/connect, envfile, envfile_test). Go 1.13+ idiom — handles wrapped errors correctly. |
| Centralize image tag, drop stale comments | `418943b` | Hardcoded `"agent-runtime:latest"` → exported `runner.ImageName` constant. Removed `// Fix 2:` comments left over from earlier review passes. |

### Runner refactor (5 commits)

`internal/runner/runner.go` had three very similar top-level functions (`runDocker`, `runDockerSnapshot`, `runDockerWithExport`) sharing ~80% of their logic. Split in small reviewable steps so each commit keeps the full suite green:

| Commit | Extracted |
|--------|-----------|
| `cb797a9` | `cleanupContainer(name)` helper |
| `8a135d5` | `appendClaudeCmd(args, opts)` builds the claude CLI portion of docker args |
| `b937518` | `appendStateAndExtras(args, cfg, opts, extraVolumes)` handles state volume, extra mounts, browser flags |
| `4d54955` | `recordHistoryEntry(opts, provider, model, start, exitCode, output)` |
| `71d0b6c` | **Unified** snapshot/export flows into `runContainerCreate(copyIn, copyOut, namePrefix)`. Workspace `-v` logic moved up to the `Run()` dispatcher. `runner.go`: 500 → 439 lines. |

### Unit tests — `internal/runner/` (commit `090a276`)

First tests ever for the most complex package. 25 test cases:

| Function | Cases |
|----------|-------|
| `stateVolumeForProfile` | 3 (default, with profile, empty profile) |
| `appendClaudeCmd` | 6 (prompt, loop, max-turns, appends onto existing slice, escaping) |
| `appendStateAndExtras` | 10 (NoState, extra volumes, agents dir, browser matrix: vnc/cdp/both/none) |
| `ParseAgentSpec` | 6 (well-formed, trimming, empty-name rejection, embedded colons) |

---

## Phase 2 — End-to-end test harness (10 commits)

Before v0.6.1 the project had 0 integration tests. All coverage was package-level unit tests.

### Design

Two patterns, same harness:

1. **Offline shim-based** (used by 40..70, 80, 95, 99 groups) — fake `docker` binary in `$PATH` logs every invocation and captures short-lived bind-mount source files *before* airun deletes them. Tests airun's orchestration end-to-end in milliseconds.
2. **Live-stack** (30-run-basic, opt-in via `--with-network`) — real container, real `docker`, real GLM-5.1. Uses host `~/.airun/config.env` + real ZAI key. Short prompts only, no loop mode. GLM-5.1 only by policy — other providers gated behind `--include-non-glm`.

**HOME isolation** is the pivot. `mk_test_home` creates a tmpdir with a minimal `.airun/config.env`. Because all airun code now uses `os.UserHomeDir()` (which honours `$HOME`), each test runs against a throwaway fake home — no risk of mutating the user's real config.

### Structure

```
test/e2e/
├── lib/
│   ├── harness.sh   # assert_*, die, skip, on_exit chain (reverse-order cleanup)
│   ├── env.sh       # AIRUN_BIN, AIRUN_REPO_ROOT, sources config.env
│   ├── docker.sh    # docker_available, image_exists, wait_port, cleanup helpers
│   ├── skip.sh      # skip_if_no_docker / _image / _airun_config / _zai_key,
│   │                # skip_unless_network, skip_unless_non_glm
│   └── home.sh      # mk_test_home, mk_test_profile, install_docker_shim
├── run-all.sh       # TAP orchestrator, flags: --group, --only, --with-network,
│                    #                         --include-non-glm, --fast, --no-build
└── 00-prereq/ 10-cli/ 20-keys/ 30-run-basic/ 40-profiles/
    50-workspace/ 60-state/ 70-parallel/ 80-browser/
    90-proxy/ 95-history-monitor/ 99-errors/
```

Exit codes: **0** = PASS, **77** = SKIP, anything else = FAIL. Output: TAP to stdout, per-test logs to `test/e2e/.logs/<group>/<test>.log` (gitignored).

### Coverage (42 tests)

| Commit | Group | Count | What it covers |
|--------|-------|-------|----------------|
| `902d6a2` | scaffold | — | `lib/`, `run-all.sh`, README, first self-check |
| `664f2c0` | 00-prereq, 10-cli, 20-keys | 12 | airun binary present, `--help`, `--version`, `keys list` doesn't mutate config |
| `4d2b1a3` | 50-workspace, 60-state, 70-parallel | 9 | bind vs snapshot mount, output export, state volume per profile, `--no-state`, `state reset`, parallel agents force NoState, bad agent spec rejected |
| `75f7d57` | 40-profiles | 5 | provider override, skill mount, plugin post-init script captured, settings.json seeded, missing profile error |
| `47dd7e6` | 30-run-basic | 1 | live GLM-5.1 round-trip (opt-in via `--with-network`) |
| `9d69f5f` | 80-browser | 4 | `--browser none` / `vnc` / `cdp` / `both` produce expected port mappings |
| `5ba8cff` | 90-proxy | 5 | `proxy init`, `init` twice is idempotent, user add+list, duplicate rejected, revoke/restore. **Pins security invariant: plaintext token NEVER lands in students.json — only SHA-256 hash.** |
| `1de133f` | 95-history-monitor, 99-errors | 5 | history empty / after-run, `airun status` invokes `docker ps`, invalid remote model rejected before docker call, `keys remove` of missing key is graceful |

### Run modes

```sh
bash test/e2e/run-all.sh                  # default — offline, ~8.7s
bash test/e2e/run-all.sh --group 90-proxy # one group
bash test/e2e/run-all.sh --only proxy-init
bash test/e2e/run-all.sh --with-network   # + live GLM-5.1 tests
bash test/e2e/run-all.sh --fast           # bail on first fail
bash test/e2e/run-all.sh --no-build       # skip `go build` preflight
```

---

## Commit list

```
1de133f test(e2e): history, monitor, and error-path coverage (5 tests)
5ba8cff test(e2e): proxy init + user lifecycle coverage (5 tests)
9d69f5f test(e2e): browser flag coverage (4 tests)
47dd7e6 test(e2e): GLM-5.1 basic-run test (opt-in via --with-network)
75f7d57 test(e2e): profile coverage (5 tests) — skills, plugins, settings, provider
4d2b1a3 test(e2e): workspace / state / parallel coverage via docker shim (9 tests)
eeb61ad refactor: finish user.Current() → os.UserHomeDir() migration
664f2c0 test(e2e): offline prereq / cli / keys coverage (12 tests)
902d6a2 test(e2e): harness skeleton — lib/, run-all.sh, README, first self-check
418943b refactor: centralize agent-runtime image tag, drop stale Fix 2 comments
14be0e1 refactor: migrate os.IsNotExist(err) → errors.Is(err, fs.ErrNotExist)
090a276 test(runner): unit tests for helpers and ParseAgentSpec
71d0b6c refactor(runner): unify snapshot and export flows into runContainerCreate
4d54955 refactor(runner): extract recordHistoryEntry helper
b937518 refactor(runner): extract appendStateAndExtras helper
8a135d5 refactor(runner): extract appendClaudeCmd helper
cb797a9 refactor(runner): extract cleanupContainer helper
6dc6d4b fix(runner): surface cleanup errors instead of silently ignoring them
60513f9 fix: replace user.Current() with os.UserHomeDir() to avoid nil panics
```

---

## Not in this release (deferred to v0.7.x / Phase 3)

- **Profile → proper skill sets** — tailoring which skills go with research / dev / ceo profiles
- **Profile-plugin activation implementation** — spec exists at `docs/superpowers/specs/2026-04-08-profile-plugin-activation-design.md` (commit `357e31f`)
- **Full SIGHUP reload in proxy** — currently reloads `students.json` only; `proxy.yaml` still requires restart
- **bcrypt token migration with transparent upgrade** — currently SHA-256 (already a security improvement over plaintext in v0.6.0; bcrypt is the next step)
- **Fix `dev.yaml` / `text.yaml` profiles** — reference plugins that don't exist yet
- **Minimal CI** — `go build && go test && golangci-lint run` on push
- **`krrkt` skill nesting cleanup** — `.claude/skills/krrkt/krrkt/` double-nesting (lives outside this repo)

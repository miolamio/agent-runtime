# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

Agent Runtime — Docker-based infrastructure for running Claude Code agents in isolated containers with multi-provider model routing.

- CLI: `airun` v0.7.0 (`cmd/airun/main.go`, no third-party CLI framework — plain `flag`)
- Module: `github.com/miolamio/agent-runtime`
- External deps: `gopkg.in/yaml.v3` (config/profile), `golang.org/x/crypto` (bcrypt for proxy tokens)
- Spec: `.development/specification.md` (Russian-language; describes the broader AUTOMATICA system; `airun` is "layer 2", the container runtime)
- The `.claude/skills/` tree is the *source* for skills shipped to users — but as of v0.7.0 those skills are delivered to containers via Claude Code marketplace plugins, not filesystem mounts (see "Skills delivery" below).

## Build, Test, Run

```bash
# Build (Go 1.25+; module pinned via go.mod)
go build -o bin/airun ./cmd/airun/

# Cross-compile for Linux (proxy server deployment)
GOOS=linux GOARCH=amd64 go build -o bin/airun-linux ./cmd/airun/

# Unit tests (run with -race in CI)
go test ./...
go test -race ./...

# Single package / single test
go test ./internal/proxy/...
go test ./internal/proxy/ -run TestForwardRequest -v

# Lint (CI uses golangci-lint v2.11.4; config in .golangci.yml)
golangci-lint run

# Build Docker image
docker build -t agent-runtime:latest docker/

# Force Claude Code reinstall in image (busts a build-arg cache key)
docker build --build-arg CLAUDE_BUST_CACHE=$(date +%s) -t agent-runtime:latest docker/

# e2e tests — bash harness, see test/e2e/README.md
test/e2e/run-all.sh                     # offline-safe (uses docker shim)
test/e2e/run-all.sh --with-network      # include real provider calls (GLM-5.1 only by default)
test/e2e/run-all.sh --group 90-proxy    # one group
test/e2e/run-all.sh --only cli/version  # one file by substring
```

CI (`.github/workflows/ci.yml`) runs: `go build`, `go vet`, `go test -race ./...`, `bash test/e2e/run-all.sh --no-build`, and `golangci-lint`.

Lint config (`.golangci.yml`): `errcheck`, `govet`, `ineffassign`, `staticcheck`, `unused`. `errcheck` excludes the usual close/print/encode noise; `_test.go` files are exempt from `errcheck`.

Unit tests live in: `config/`, `envfile/`, `history/`, `keys/`, `proxy/` (+ `proxy/users/`), `runner/`. The `cmd/`, `monitor/`, `prereq/`, `setup/` packages are not unit-tested (covered by e2e).

## Architecture

```
cmd/airun/main.go               # plain `flag` dispatcher; const version = "0.7.0"
  ├── config       loads ~/.airun/config.env, resolves provider/model, generates container env
  ├── runner       docker run/create, volume mounts, parallel agents, plugin filtering
  │     └── config, envfile, history, profile
  ├── keys         API key add/remove/test/list/default (writes ~/.airun/config.env)
  │     └── config, envfile, keys/providers, keys/validate
  ├── proxy        HTTP proxy server with per-user auth + SIGHUP reload
  │     └── proxy/users  (bcrypt-hashed tokens, JSON persistence)
  ├── setup        interactive `airun init` wizard
  │     └── config, keys
  ├── history      run history + logs in ~/.airun/runs/
  ├── monitor      `docker ps` wrapper for `airun --status`
  └── prereq       checks Docker daemon + image presence (`airun --check`)
```

### CLI surface

Top-level: `--version`, `--help`, `--status`, `--check`, `init`, `rebuild [--no-cache] [--fresh]`, `state {info|reset}`, `shell`, `history`, `keys {list|add|remove|test|default|model}`, `proxy {connect|disconnect|init|serve|user {add|list|revoke|restore|import|export}}`. The default form `airun [flags] "<prompt>"` runs an agent.

### Provider routing

All providers expose the Anthropic Messages API natively. `internal/config` normalizes provider names and emits container env:

- **Aliases** (`NormalizeProvider`): `z`/`zai`, `m`/`mm`/`minimax`, `k`/`kimi`, `r`/`remote`, `anthropic` (direct).
- **Resolution order**: CLI flag `--provider` → profile YAML (`provider:`) → `~/.airun/config.env` (`ARUN_PROVIDER`) → default (`zai`).
- **Container env**: provider-specific keys collapse to `ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_DEFAULT_SONNET_MODEL`. `ContainerEnvWithModel` adds `ENABLE_TOOL_SEARCH=false` for Kimi.

### Docker container lifecycle

Each `airun` run creates an ephemeral container.

- **Mount modes**: `snapshot` (copies workspace into container) vs `bind` (live host mount).
- **State volume**: `airun-claude-state` Docker volume persists Claude Code state across runs; disable with `--no-state`.
- **Build ID tracking**: `docker/entrypoint.sh` compares `/etc/airun-build-id` (baked into image) with `~/.claude/.image-build-id` (in state volume) and warns on mismatch — catches stale state after image rebuilds.
- **Parallel agents force `NoState: true`** to prevent concurrent writes from corrupting the shared state volume.
- **Entrypoint** seeds Claude Code settings, bypasses onboarding, strips `[credential]` sections from host git config before copying it in, and registers marketplaces via the `claude` CLI (settings mount is RW so plugin metadata can persist).

### Skills delivery (v0.7.0 breaking change)

Skills are **no longer mounted from `~/airun-skills/` or any host filesystem path**. They ship as Claude Code plugins from marketplaces, activated per-profile via `plugins:` in profile YAML.

- `runner.basePlugins` lists plugins pre-installed at image build time (by `docker/seed-plugins.sh`); profile-declared plugins are filtered through `filterBasePlugins` to drop these and the remainder is passed to the container as `AIRUN_PLUGINS=<comma-separated>`.
- The container entrypoint reads `AIRUN_PLUGINS` and activates those plugins into `installed_plugins.json`.
- Profiles still parse a legacy `skills:` field — but it is ignored with a one-time stderr warning telling the user to use `plugins:` instead.

### Proxy system

`internal/proxy/` — HTTP proxy that lets admins share model access without sharing API keys. The most complex subsystem.

- Config: `~/.airun/proxy.yaml` (providers, RPM, user_agent) + `~/.airun/users.json` (user records). Legacy `~/.airun/students.json` and `~/students.json` are auto-migrated on first read.
- **Token storage**: bcrypt hashes (`HashTokenBcrypt`). Pre-v0.6.0 SHA-256 hashes are accepted for verification and **transparently upgraded to bcrypt on the auth path** (asynchronous persist; auth-path latency unaffected). New users always get bcrypt.
- **Auth**: checks `x-api-key` first, falls back to `Authorization: Bearer <token>`.
- **Rate limiting**: per-user RPM. `RPM=0` means *unlimited* (intentional — not "disabled").
- **SIGHUP reload**: reloads `proxy.yaml` (providers, RPM, user_agent) AND `users.json` without restarting; in-flight requests keep their old config.
- The proxy only rewrites `x-api-key` and `User-Agent`; everything else passes through to the upstream provider unchanged.

## Gotchas

- **Config path moved** — `~/.airun/config.env` (not `~/.airun.env`). `config.Load()` migrates the old file on first run; do not write to the old path.
- **Profile location** — `~/.airun/profiles/<name>.yaml`. Templates ship in `configs/profiles/` (default, dev, ceo, research, text).
- **Skills delivery** — see above. Anything that mounts a filesystem path for skills is wrong post-v0.7.0.
- **Parallel agents force `NoState: true`** — prevents Docker volume corruption from concurrent writes to the shared state volume.
- **Kimi-specific env var** — `ContainerEnvWithModel()` automatically adds `ENABLE_TOOL_SEARCH=false` for Kimi.
- **Proxy token format** — `sk-ai-` prefix + 32 bytes random hex = 70 chars total; tests assert this length.
- **Forward client timeout** — 5 minutes for proxied requests; 15 seconds for key validation calls.
- **Entrypoint credential filtering** — `docker/entrypoint.sh` strips `[credential]` sections from host git config before copying into container.
- **Connect-proxy scripts** — `scripts/connect-proxy.{sh,ps1}` configure host Claude Code to use the proxy and mark `settings.json` with `_airunManaged` so disconnect can revert cleanly. Don't reformat that flag.
- **`airun.skill`** at the repo root is a packaged zip bundle (the `airun` skill exported for distribution), not source — edit `.claude/skills/airun/` instead.

## Key Domain Types

```go
// internal/runner — main run config
type RunOpts struct {
    Prompt, Provider, Profile, Model, Name, Mount, Output string
    Loop, Interactive, NoState bool
    MaxLoops int
}

// internal/runner — parallel agent spec ("name:prompt" format)
type AgentSpec struct{ Name, Prompt string }

// internal/profile — YAML profile (Skills field is legacy/ignored as of v0.7.0)
type Profile struct {
    Name, Description, Provider string
    Plugins  []string
    Settings map[string]any
}

// internal/proxy/users — user record (Token is a bcrypt or legacy SHA-256 hash)
type User struct {
    Name string; Token string; Active bool; CreatedAt time.Time
}

// internal/proxy — parsed ~/.airun/proxy.yaml
type ProxyConfig struct {
    Listen, UserAgent string; RPM int; Providers map[string]Provider
}
```

## Test Patterns

- **Unit**: `t.TempDir()` for file-based isolation; `httptest.NewServer()` for upstream provider mocks; `httptest.NewRecorder()` for handler-level assertions. No shared fixtures — each test builds its own state.
- **Validation mocks** (`internal/keys/validate_test.go`) mock the Anthropic Messages API response (`{id, type, model, content: [{type, text}]}`) and assert exact headers (`x-api-key`, `anthropic-version: 2023-06-01`).
- **Concurrency**: `users_concurrent_test.go` exercises parallel auth + bcrypt upgrade; run with `-race`.
- **e2e** (`test/e2e/`): bash, organized by group (`00-prereq`, `10-cli`, …, `99-errors`). Exit codes: `0` PASS, `77` SKIP, anything else FAIL. Helpers in `test/e2e/lib/`:
  - `harness.sh` (sets `set -euo pipefail`, `on_exit` cleanups, assertions)
  - `env.sh`, `home.sh` (HOME isolation — tests must not touch real `~/.airun/` or `~/.claude/`)
  - `docker.sh` (docker shim for offline runs)
  - `skip.sh` — `skip_unless_network` (gates real API calls behind `--with-network`); `skip_unless_non_glm` (containerized provider tests run only against `zai/glm-5.1` unless `--include-non-glm`)
  - Per-test logs land in `test/e2e/.logs/<group>/<test>.log` (gitignored); TAP summary in stdout.

## Skills Source Layout

Skills sources live in `.claude/skills/<name>/`:

- `SKILL.md` — required, YAML frontmatter (`name`, `description`) + procedural body. The `description` field is what determines when Claude triggers the skill.
- `scripts/`, `references/`, `assets/` / `templates/` — supporting material.

Design rules: SKILL.md body < 500 lines; reference files > 100 lines should include a TOC; description must spell out trigger conditions (keywords, file types, user phrases).

Scaffold a new skill with the `skill-creator` skill or directly:
```bash
python .claude/skills/skill-creator/scripts/init_skill.py <skill-name> --path <output-dir>
```

## Filesystem Layout

- `~/.airun/config.env` — central config: API keys, default provider, workspace (chmod 600). Migrated from legacy `~/.airun.env` on first run.
- `~/.airun/profiles/*.yaml` — workload profiles (provider, plugins, settings).
- `~/.airun/proxy.yaml`, `~/.airun/users.json` — proxy server state.
- `~/.airun/agents/` — agent definitions (migrated from legacy `~/airun-agents/`).
- `~/.airun/runs/` — run history with logs and metadata.
- `configs/airun.env.example`, `configs/init/init.yaml.example`, `configs/profiles/*.yaml`, `configs/router/config.json.example` — repo-side templates.

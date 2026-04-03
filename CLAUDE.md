# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

Two things in one repo:

1. **Skills Library** — Self-contained skills in `.claude/skills/` for artifact generation, Office formats, design, translation, and browser automation.
2. **Agent Runtime** — Docker-based infrastructure for running Claude Code agents in isolated containers with multi-provider model routing. CLI: `airun`. Spec: `.development/specification.md`

## Build, Test, Run

```bash
# Build (requires Go 1.25+)
go build -o bin/airun ./cmd/airun/

# Run all tests
go test ./...

# Run tests for a specific package
go test ./internal/proxy/...
go test ./internal/keys/...

# Run a single test
go test ./internal/proxy/ -run TestForwardRequest

# Verbose test output
go test -v ./internal/proxy/...

# Build Docker image
docker build -t agent-runtime:latest docker/

# Force Claude Code reinstall in image
docker build --build-arg CLAUDE_BUST_CACHE=$(date +%s) -t agent-runtime:latest docker/

# Cross-compile for Linux (proxy server deployment)
GOOS=linux GOARCH=amd64 go build -o bin/airun-linux ./cmd/airun/
```

No Makefile, CI pipeline, or linter config exists. Tests use standard `go test`. Only `internal/proxy/` and `internal/keys/` have test files — core packages (`runner`, `config`, `profile`, `history`) are untested.

## Architecture

The CLI (`cmd/airun/main.go`) uses `flag` (no third-party CLI framework) and dispatches to internal packages. Single external dependency: `gopkg.in/yaml.v3`.

### Package Dependency Flow

```
cmd/airun/main.go
  ├── config     ← loads ~/.airun.env, resolves provider/model
  ├── runner     ← docker run/create, volume mounts, parallel execution
  │     └── config, envfile, history, profile
  ├── keys       ← API key add/remove/test/list/default
  │     └── config
  ├── proxy      ← HTTP proxy server with per-user auth
  │     └── proxy/students (token generation, user CRUD)
  ├── setup      ← interactive init wizard
  │     └── config, keys
  ├── history    ← run history to ~/.airun/runs/
  ├── monitor    ← docker ps wrapper
  └── prereq     ← checks Docker availability
```

### Provider Routing

All providers expose the Anthropic Messages API natively. The config package normalizes provider names and generates container env vars:

- **Aliases**: `z`/`zai`, `m`/`mm`/`minimax`, `k`/`kimi`, `r`/`remote`
- **Resolution order**: CLI flag `--provider` → profile YAML → `~/.airun.env` → default (`zai`)
- **Container env**: Provider-specific keys become `ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_DEFAULT_SONNET_MODEL`

### Docker Container Lifecycle

Each `airun` run creates an ephemeral container. Key behaviors:

- **Mount modes**: `snapshot` (copies workspace into container) vs `bind` (live mount of host directory)
- **State volume**: Optional `airun-claude-state` Docker volume persists Claude Code state across runs. Disabled with `--no-state`.
- **Build ID tracking**: Entrypoint warns if image was rebuilt since state volume was created (catches stale caches)
- **Parallel agents force `NoState: true`** to avoid Docker volume corruption from concurrent writes
- **Entrypoint** (`docker/entrypoint.sh`): Seeds Claude Code settings, bypasses onboarding, filters `[credential]` sections from host git config, seeds plugin metadata from build-time JSON

### Proxy System

The most complex subsystem (`internal/proxy/`). An HTTP proxy that lets admins share model access without sharing API keys.

- Config: `~/proxy.yaml` (providers, rate limits) + `~/students.json` (user tokens)
- Token auth: SHA256-hashed tokens in students.json
- Per-user rate limiting (configurable RPM)
- SIGHUP reload (update users without restart)
- Only replaces `x-api-key` and `User-Agent` headers; everything else passes through unchanged

## Skill Structure

Each skill lives in `.claude/skills/<name>/` and follows this pattern:

- `SKILL.md` — Required. YAML frontmatter (`name`, `description`) + procedural instructions. The `description` field determines when the skill triggers.
- `scripts/` — Python/JS/Bash utilities for deterministic operations
- `references/` — On-demand documentation, schemas, API guides
- `assets/` or `templates/` — Media, templates, boilerplate

Use the `skill-creator` skill to scaffold new skills:
```bash
python .claude/skills/skill-creator/scripts/init_skill.py <skill-name> --path <output-dir>
```

### Key Design Principles

- **SKILL.md body < 500 lines**; reference files > 100 lines should include a table of contents
- **Scripts** solve operations that are fragile, repeated, or require deterministic behavior
- **Description field** must clearly state trigger conditions (keywords, file types, user phrases)

## Configuration

- `~/.airun.env` — Central config: API keys, default provider, workspace (chmod 600)
- `~/airun-profiles/*.yaml` — Workload profiles (provider, skills, plugins, settings)
- `~/airun-skills/` — Skills mounted into containers (RO)
- `~/.airun/runs/` — Run history with logs and metadata
- `configs/airun.env.example` — Template for the config file
- `configs/profiles/` — Shipped profile templates: default, dev, text

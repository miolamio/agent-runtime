# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

Two things in one repo:

1. **Skills Library** — 20 self-contained skills in `.claude/skills/` for artifact generation, Office formats, design, translation, and browser automation.
2. **Agent Runtime** — Docker-based infrastructure for running Claude Code agents in isolated containers with multi-provider model routing. CLI: `airun`. Spec: `.development/specification.md`

## Skill Structure

Each skill lives in `.claude/skills/<name>/` and follows this pattern:

- `SKILL.md` — Required. YAML frontmatter (`name`, `description`) + procedural instructions. The `description` field determines when the skill triggers.
- `scripts/` — Python/JS/Bash utilities for deterministic operations (PDF analysis, Office validation, etc.)
- `references/` — On-demand documentation, schemas, API guides
- `assets/` or `templates/` — Media, templates, boilerplate used in outputs

Skills are independent and self-contained. Claude reads SKILL.md and executes its instructions; scripts run via Bash/Node when needed.

## Creating / Modifying Skills

Use the `skill-creator` skill, which provides the canonical framework:

```bash
# Scaffold a new skill
python .claude/skills/skill-creator/scripts/init_skill.py <skill-name> --path <output-dir>

# Validate and package
python .claude/skills/skill-creator/scripts/package_skill.py <path/to/skill-folder> ./dist
```

Detailed workflow patterns: `.claude/skills/skill-creator/references/workflows.md`
Output formatting patterns: `.claude/skills/skill-creator/references/output-patterns.md`

## Key Design Principles

- **Concise**: Context window is shared — only include essential information in SKILL.md
- **Progressive disclosure**: Metadata loads first, then SKILL.md body, then references/scripts on demand
- **SKILL.md body < 500 lines**; reference files > 100 lines should include a table of contents
- **Scripts** solve operations that are fragile, repeated, or require deterministic behavior
- **Description field** must clearly state trigger conditions (keywords, file types, user phrases)

## Skills by Category

**Document processing**: docx, pptx, pdf, xlsx — Office format creation/editing via Python/JS libraries
**Design**: canvas-design, frontend-design, algorithmic-art, theme-factory, brand-guidelines
**Development**: mcp-builder, web-artifacts-builder, webapp-testing
**Language**: en-ru-translator-adv, ru-editor, krrkt
**Content**: doc-coauthoring, internal-comms, skill-creator
**Automation**: playwright-cli, slack-gif-creator

## Agent Runtime — Build and Run

```bash
# Build airun CLI
go build -o bin/airun ./cmd/airun/

# First-time setup (creates ~/.airun.env, dirs, profiles, installs binary)
./bin/airun init

# Check prerequisites
./bin/airun --check

# Run agent task (from any directory)
airun "prompt"
airun -p dev "prompt"             # with profile
airun --provider mm "prompt"      # with specific provider

# Interactive Claude Code session (mounts current dir)
airun shell
airun shell -p dev
airun shell --provider mm

# Parallel agents
airun --parallel --agent "a1:task1" --agent "a2:task2"

# Autonomous loop
airun --loop --max-loops 5 "prompt"

# Export artifacts from container
airun --output ./results "generate report"

# Status, history, rebuild
airun --status
airun history
airun rebuild

# Key management
airun keys list                   # show configured keys
airun keys add kimi               # add key with guide + validation
airun keys remove minimax         # remove provider key
airun keys test                   # validate all keys
airun keys test kimi              # validate specific key
airun keys default kimi           # change default provider

# Proxy server (admin-side)
airun proxy init                         # create proxy.yaml + students.json
airun proxy serve                        # start proxy server
airun proxy serve --port 9090            # custom port
airun proxy user add "Ivanov"            # create user → token
airun proxy user list                    # show all users
airun proxy user revoke "Ivanov"         # deactivate user
airun proxy user restore "Ivanov"        # reactivate user
airun proxy user import list.txt         # bulk import
airun proxy user export                  # export name + token pairs

# Remote proxy (user-side)
airun keys add remote                    # connect to proxy with URL + token
```

## Agent Runtime — Configuration

- `~/.airun.env` — Central config: API keys, default provider, workspace
- `~/airun-profiles/*.yaml` — Workload profiles (dev, text, etc.)
- `~/airun-skills/` — Skills mounted into containers (RO)
- `~/.airun/runs/` — Run history with logs and metadata

## Agent Runtime — Project Structure

- `cmd/airun/` — Go CLI entry point
- `internal/` — config, envfile, history, monitor, prereq, profile, runner, setup
- `docker/` — Dockerfile, entrypoint.sh, default settings
- `configs/profiles/` — YAML profile templates (dev, text, default)
- `configs/router/` — Claude Code Router config (optional)
- `scripts/` — setup.sh, init-container.sh
- `examples/` — Sample skills, agents, commands
- `.development/` — Specification and development logs

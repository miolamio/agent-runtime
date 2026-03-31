---
name: airun
description: "Agent Runtime CLI for running Claude Code agents in isolated Docker containers with multi-provider model routing. Use when working with airun commands, Docker container management, provider/model configuration (Z.AI, Kimi, MiniMax), proxy setup, profiles, key management, or connect-proxy scripts. Triggers on: airun, agent-runtime, docker container agent, proxy serve, proxy connect, provider routing, airun shell, airun rebuild, airun keys, airun proxy, airun state."
---

# Agent Runtime (airun)

CLI tool that runs Claude Code agents inside Docker containers with multi-provider model routing.

## Quick Reference

```bash
# Run agent task
airun "prompt"
airun -p dev "prompt"
airun --provider kimi "prompt"
airun --model glm-5.1 "prompt"

# Interactive session
airun shell
airun shell -p dev
airun shell --provider mm

# Parallel agents
airun --parallel --agent "a1:task1" --agent "a2:task2"

# Autonomous loop
airun --loop --max-loops 5 "prompt"

# Export results
airun --output ./results "prompt"

# Key management
airun keys list
airun keys add <provider>
airun keys test [provider]
airun keys default <provider>
airun keys model <model>

# Docker image
airun rebuild
airun rebuild --fresh
airun rebuild --no-cache

# Persistent state
airun state info
airun state reset

# System
airun --check
airun --status
airun history
```

## Providers

| Provider | Aliases | Default Model | Base URL |
|----------|---------|---------------|----------|
| Z.AI | z, zai | glm-5.1 | api.z.ai/api/anthropic |
| MiniMax | m, mm, minimax | MiniMax-M2.7 | api.minimax.io/anthropic |
| Kimi | k, kimi | kimi-k2.5 | api.kimi.com/coding/ |
| Remote | r, remote | configurable | configurable |

Provider priority: `--provider` flag > profile provider > config default.

Model priority: `--model` flag > config default for provider.

## Profiles

YAML files at `~/airun-profiles/`. Activate with `-p <name>`.

Each profile bundles: skills (mounted RO), plugins, settings, optional provider override.

- **default** — basic agent, no extra skills
- **dev** — webapp-testing, superpowers, pyright-lsp, context7
- **text** — ru-editor, en-ru-translator-adv, krrkt (provider: MiniMax)

## Proxy

For detailed proxy admin and user management guide, see [references/proxy-guide.md](references/proxy-guide.md).

Admin quick start:
```bash
airun proxy init && airun proxy serve
airun proxy user add "Name"    # prints sk-ai-... token
```

User connection (three methods):
```bash
airun proxy connect <url> <token>                  # with airun
curl -fsSL .../connect-proxy.sh | bash -s -- <url> <token>  # without airun
airun proxy disconnect                             # cleanup
```

## Docker Container

Image `agent-runtime:latest` includes: Claude Code CLI, Node.js 22, Playwright + Chromium, 20 skills, 3 plugins (context7, skill-creator, superpowers), Debian Bookworm.

Named volume `airun-claude-state` persists `~/.claude` between runs. Use `airun state reset` after rebuilding image.

## Configuration

Central config: `~/.airun.env` (chmod 600). Key variables: `ARUN_PROVIDER`, `ZAI_API_KEY`, `ZAI_MODEL`, `MINIMAX_API_KEY`, `KIMI_API_KEY`, `REMOTE_BASE_URL`, `REMOTE_API_KEY`, `API_TIMEOUT_MS`.

For the full CLI reference, see [references/cli-reference.md](references/cli-reference.md).

## Project Structure

```
cmd/airun/          — Go CLI entry point
internal/           — config, envfile, history, keys, monitor, prereq, profile, proxy, runner, setup
docker/             — Dockerfile, entrypoint.sh, seed-plugins.sh
configs/profiles/   — YAML profile templates (default, dev, text)
scripts/            — connect-proxy.sh, connect-proxy.ps1
.claude/skills/     — bundled skills
```

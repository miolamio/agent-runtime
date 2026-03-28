#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

log() { echo "[setup] $*"; }

log "Creating host directories..."
mkdir -p ~/airun-skills
mkdir -p ~/airun-agents
mkdir -p ~/airun-commands
mkdir -p ~/airun-profiles
mkdir -p ~/airun-init
mkdir -p ~/.claude-code-router

if [[ -d "$PROJECT_DIR/configs/profiles" ]]; then
  log "Copying profile examples to ~/airun-profiles/"
  cp -n "$PROJECT_DIR"/configs/profiles/*.env.example ~/airun-profiles/ 2>/dev/null || true
fi

if [[ -d "$PROJECT_DIR/examples/skills" ]]; then
  log "Copying example skills to ~/airun-skills/"
  cp -rn "$PROJECT_DIR"/examples/skills/* ~/airun-skills/ 2>/dev/null || true
fi

if [[ -d "$PROJECT_DIR/examples/agents" ]]; then
  log "Copying example agents to ~/airun-agents/"
  cp -n "$PROJECT_DIR"/examples/agents/* ~/airun-agents/ 2>/dev/null || true
fi

if [[ -d "$PROJECT_DIR/examples/commands" ]]; then
  log "Copying example commands to ~/airun-commands/"
  cp -n "$PROJECT_DIR"/examples/commands/* ~/airun-commands/ 2>/dev/null || true
fi

if [[ -f "$PROJECT_DIR/configs/router/config.json.example" ]]; then
  if [[ ! -f ~/.claude-code-router/config.json ]]; then
    log "Copying Router config example to ~/.claude-code-router/"
    cp "$PROJECT_DIR/configs/router/config.json.example" ~/.claude-code-router/config.json
    log "Edit ~/.claude-code-router/config.json with your API keys!"
  else
    log "Router config already exists at ~/.claude-code-router/config.json — skipping"
  fi
fi

if [[ -f "$PROJECT_DIR/configs/init/init.yaml.example" ]]; then
  cp -n "$PROJECT_DIR/configs/init/init.yaml.example" ~/airun-init/init.yaml 2>/dev/null || true
fi

log "Setup complete!"
log ""
log "Next steps:"
log "  1. Edit ~/.claude-code-router/config.json with your API keys"
log "  2. Copy ~/airun-profiles/*.env.example to *.env and fill in keys"
log "  3. Start Router: ccr start"
log "  4. Test: airun 'Hello, what model are you?'"

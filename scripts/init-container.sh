#!/usr/bin/env bash
set -euo pipefail

# Agent Runtime Container Init Script
# Runs as post-init inside the container.
# Reads manifest from /home/user/.airun/init.yaml (mounted or embedded).

MANIFEST="${ARUN_INIT_MANIFEST:-/home/user/.airun/init.yaml}"
SKILLS_DIR="/home/user/.claude/skills"
AGENTS_DIR="/home/user/.claude/agents"
COMMANDS_DIR="/home/user/.claude/commands"

log() { echo "[airun-init] $*"; }

if [[ ! -f "$MANIFEST" ]]; then
  log "No init manifest found at $MANIFEST — skipping custom init"
  exit 0
fi

log "Reading manifest: $MANIFEST"

if command -v yq &>/dev/null; then
  PACKAGES=$(yq -r '.npm_packages[]? // empty' "$MANIFEST" 2>/dev/null)
  if [[ -n "$PACKAGES" ]]; then
    log "Installing npm packages: $PACKAGES"
    npm install -g $PACKAGES
  fi

  PIP_PACKAGES=$(yq -r '.pip_packages[]? // empty' "$MANIFEST" 2>/dev/null)
  if [[ -n "$PIP_PACKAGES" ]]; then
    log "Installing pip packages: $PIP_PACKAGES"
    pip install $PIP_PACKAGES
  fi

  SKILL_REPOS=$(yq -r '.skill_repos[]? // empty' "$MANIFEST" 2>/dev/null)
  for repo in $SKILL_REPOS; do
    REPO_NAME=$(basename "$repo" .git)
    if [[ ! -d "$SKILLS_DIR/$REPO_NAME" ]]; then
      log "Cloning skill repo: $repo"
      git clone --depth 1 "$repo" "$SKILLS_DIR/$REPO_NAME" 2>/dev/null || true
    fi
  done

  INIT_SKILLS_DIR="/tmp/init-skills"
  if [[ -d "$INIT_SKILLS_DIR" ]]; then
    log "Copying embedded skills from $INIT_SKILLS_DIR"
    cp -r "$INIT_SKILLS_DIR"/* "$SKILLS_DIR/" 2>/dev/null || true
  fi
else
  log "yq not found — skipping manifest parsing (install yq for full init support)"
fi

log "Container init complete"

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

# yaml_list_values extracts values from a simple YAML list key.
# Usage: yaml_list_values <file> <key>
# Example: yaml_list_values init.yaml npm_packages
#   Given:
#     npm_packages:
#       - typescript
#       - eslint
#   Outputs:
#     typescript
#     eslint
yaml_list_values() {
  local file="$1" key="$2"
  python3 -c "
import sys, re
text = open(sys.argv[1]).read()
# Match the key followed by list items (indented '- value' lines)
pattern = r'^' + re.escape(sys.argv[2]) + r':\s*\n((?:[ \t]+-[ \t]+.+\n)*)'
m = re.search(pattern, text, re.MULTILINE)
if m:
    for line in m.group(1).strip().splitlines():
        val = re.sub(r'^[ \t]*-[ \t]+', '', line).strip()
        # Strip optional quotes
        if len(val) >= 2 and val[0] == val[-1] and val[0] in ('\"', \"'\"):
            val = val[1:-1]
        if val:
            print(val)
" "$file" "$key" 2>/dev/null || true
}

PACKAGES=$(yaml_list_values "$MANIFEST" npm_packages)
if [[ -n "$PACKAGES" ]]; then
  log "Installing npm packages: $PACKAGES"
  npm install -g $PACKAGES
fi

PIP_PACKAGES=$(yaml_list_values "$MANIFEST" pip_packages)
if [[ -n "$PIP_PACKAGES" ]]; then
  log "Installing pip packages: $PIP_PACKAGES"
  pip install $PIP_PACKAGES
fi

SKILL_REPOS=$(yaml_list_values "$MANIFEST" skill_repos)
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

log "Container init complete"

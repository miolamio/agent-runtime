#!/bin/bash
set -e

# Seed Claude Code plugins into the container at build time.
# Clones marketplace repos and extracts plugin files into the cache structure
# that Claude Code expects. JSON configs are generated at runtime by entrypoint.sh.

USERNAME="${1:-claude}"
HOME_DIR="/home/${USERNAME}"
PLUGINS_DIR="${HOME_DIR}/.claude/plugins"
CACHE_DIR="${PLUGINS_DIR}/cache"
MKT_DIR="${PLUGINS_DIR}/marketplaces"

log() { echo "[seed-plugins] $*"; }

mkdir -p "$CACHE_DIR" "$MKT_DIR"

# ── 1. Clone claude-plugins-official marketplace ──
log "Cloning claude-plugins-official marketplace..."
git clone --depth 1 https://github.com/anthropics/claude-plugins-official.git \
    "${MKT_DIR}/claude-plugins-official"
CPO_SHA=$(git -C "${MKT_DIR}/claude-plugins-official" rev-parse --short=12 HEAD)
log "claude-plugins-official at ${CPO_SHA}"

# ── 2. Extract skill-creator plugin (the only base plugin) ──
# Everything else is opt-in per profile via `plugins:` (AIRUN_PLUGINS).
log "Extracting skill-creator plugin..."
SC_CACHE="${CACHE_DIR}/claude-plugins-official/skill-creator/${CPO_SHA}"
mkdir -p "$SC_CACHE"
cp -r "${MKT_DIR}/claude-plugins-official/plugins/skill-creator/"* "$SC_CACHE/" 2>/dev/null || true
cp -r "${MKT_DIR}/claude-plugins-official/plugins/skill-creator/.claude-plugin" "$SC_CACHE/" 2>/dev/null || true

# ── 3. Clone miolamio/agent-skills marketplace ──
# Registered at runtime as a local marketplace; its skills (ru-editor,
# en-ru-translator-adv, krrkt, …) are opt-in plugins, NOT copied into
# ~/.claude/skills.
log "Cloning miolamio-agent-skills marketplace..."
git clone --depth 1 https://github.com/miolamio/agent-skills.git \
    "${MKT_DIR}/miolamio-agent-skills"
MAS_SHA=$(git -C "${MKT_DIR}/miolamio-agent-skills" rev-parse --short=12 HEAD)

# NOTE: anthropic-agent-skills is deliberately NOT cloned here. Claude Code
# >=2.1.x reserves that marketplace name and only accepts it from the anthropics
# GitHub source, so entrypoint.sh registers it at runtime with
# `claude plugin marketplace add anthropics/skills`. A build-time clone would be
# dead weight the CLI never reads.

# ── 4. Save metadata for entrypoint.sh to generate JSON configs ──
cat > "${PLUGINS_DIR}/.seed-metadata.json" <<METAEOF
{
  "cpo_sha": "${CPO_SHA}",
  "mas_sha": "${MAS_SHA}",
  "seeded_at": "$(date -u +%Y-%m-%dT%H:%M:%S.000Z)"
}
METAEOF

# ── 5. Fix ownership ──
chown -R "${USERNAME}:${USERNAME}" "${PLUGINS_DIR}"

log "Done: skill-creator + miolamio-agent-skills marketplace (anthropic-agent-skills registers at runtime)"

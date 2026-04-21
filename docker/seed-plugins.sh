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

# ── 2. Extract context7 plugin (MCP server) ──
log "Extracting context7 plugin..."
CTX_CACHE="${CACHE_DIR}/claude-plugins-official/context7/${CPO_SHA}"
mkdir -p "$CTX_CACHE"
cp -r "${MKT_DIR}/claude-plugins-official/external_plugins/context7/"* "$CTX_CACHE/" 2>/dev/null || true
# context7 is an external plugin — needs .claude-plugin/plugin.json and .mcp.json
mkdir -p "$CTX_CACHE/.claude-plugin"
cat > "$CTX_CACHE/.claude-plugin/plugin.json" <<'PJEOF'
{
  "name": "context7",
  "description": "Upstash Context7 MCP server for up-to-date documentation lookup. Pull version-specific documentation and code examples directly from source repositories into your LLM context.",
  "author": { "name": "Upstash" }
}
PJEOF
cat > "$CTX_CACHE/.mcp.json" <<'MCPEOF'
{
  "context7": {
    "command": "npx",
    "args": ["-y", "@upstash/context7-mcp"]
  }
}
MCPEOF

# ── 3. Extract skill-creator plugin ──
log "Extracting skill-creator plugin..."
SC_CACHE="${CACHE_DIR}/claude-plugins-official/skill-creator/${CPO_SHA}"
mkdir -p "$SC_CACHE"
cp -r "${MKT_DIR}/claude-plugins-official/plugins/skill-creator/"* "$SC_CACHE/" 2>/dev/null || true
cp -r "${MKT_DIR}/claude-plugins-official/plugins/skill-creator/.claude-plugin" "$SC_CACHE/" 2>/dev/null || true

# ── 4. Clone superpowers plugin (external repo) ──
log "Cloning superpowers plugin..."
git clone --depth 1 https://github.com/obra/superpowers.git /tmp/superpowers
SP_VER=$(jq -r '.version // "latest"' /tmp/superpowers/package.json 2>/dev/null || echo "latest")
SP_SHA=$(git -C /tmp/superpowers rev-parse --short=12 HEAD)
SP_CACHE="${CACHE_DIR}/claude-plugins-official/superpowers/${SP_VER}"
mkdir -p "$SP_CACHE"
# Copy everything except .git
cd /tmp/superpowers && find . -mindepth 1 -maxdepth 1 -not -name '.git' -exec cp -a {} "$SP_CACHE/" \;
cd / && rm -rf /tmp/superpowers
log "superpowers v${SP_VER} at ${SP_SHA}"

# ── 5. Clone miolamio/agent-skills marketplace ──
log "Cloning miolamio-agent-skills marketplace..."
git clone --depth 1 https://github.com/miolamio/agent-skills.git \
    "${MKT_DIR}/miolamio-agent-skills"
MAS_SHA=$(git -C "${MKT_DIR}/miolamio-agent-skills" rev-parse --short=12 HEAD)

# Also copy skills to ~/.claude/skills/ for direct access
mkdir -p "${HOME_DIR}/.claude/skills"
cp -r "${MKT_DIR}/miolamio-agent-skills/skills/"* "${HOME_DIR}/.claude/skills/" 2>/dev/null || true

# ── 6. Clone anthropic-agent-skills marketplace ──
# Carries bundle plugins like example-skills (webapp-testing, internal-comms,
# doc-coauthoring, …) and document-skills (xlsx/docx/pptx/pdf).
log "Cloning anthropic-agent-skills marketplace..."
git clone --depth 1 https://github.com/anthropics/skills.git \
    "${MKT_DIR}/anthropic-agent-skills"
AAS_SHA=$(git -C "${MKT_DIR}/anthropic-agent-skills" rev-parse --short=12 HEAD)

# ── 7. Save metadata for entrypoint.sh to generate JSON configs ──
cat > "${PLUGINS_DIR}/.seed-metadata.json" <<METAEOF
{
  "cpo_sha": "${CPO_SHA}",
  "sp_ver": "${SP_VER}",
  "sp_sha": "${SP_SHA}",
  "mas_sha": "${MAS_SHA}",
  "aas_sha": "${AAS_SHA}",
  "seeded_at": "$(date -u +%Y-%m-%dT%H:%M:%S.000Z)"
}
METAEOF

# ── 8. Fix ownership ──
chown -R "${USERNAME}:${USERNAME}" "${PLUGINS_DIR}" "${HOME_DIR}/.claude/skills"

log "Done: context7, skill-creator, superpowers + miolamio-agent-skills + anthropic-agent-skills"

#!/bin/bash
set -e

_USER="${ARUN_USER:-claude}"
_HOME="/home/${_USER}"

# ── SSH known hosts (generated at build time via ssh-keyscan) ──
mkdir -p "${_HOME}/.ssh"
chmod 700 "${_HOME}/.ssh"
if [ -f /etc/ssh/ssh_known_hosts ]; then
    cp /etc/ssh/ssh_known_hosts "${_HOME}/.ssh/known_hosts"
fi
chown -R "${_USER}:${_USER}" "${_HOME}/.ssh"
chmod 600 "${_HOME}/.ssh/known_hosts" 2>/dev/null || true

# ── Git config from host (filter out credential.helper) ──
if [ -f /tmp/host-gitconfig ]; then
    awk '
        /^\[credential/ { skip=1; next }
        /^\[/ { skip=0 }
        !skip { print }
    ' /tmp/host-gitconfig > "${_HOME}/.gitconfig" 2>/dev/null || true
    chown "${_USER}:${_USER}" "${_HOME}/.gitconfig" 2>/dev/null || true
fi

# ── Seed Claude Code settings ──
INIT_DIR="${_HOME}/.claude-init"
CONFIG_DIR="${_HOME}/.claude"
if [ -d "$INIT_DIR" ]; then
    # Settings: merge init defaults with mounted overrides
    if [ ! -f "$CONFIG_DIR/settings.json" ]; then
        cp "$INIT_DIR/settings.json" "$CONFIG_DIR/settings.json" 2>/dev/null || true
    fi
fi

# ── Seed plugin JSON configs (from build-time metadata) ──
PLUGINS_DIR="${_HOME}/.claude/plugins"
SEED_META="${PLUGINS_DIR}/.seed-metadata.json"
if [ -f "$SEED_META" ] && [ ! -f "$PLUGINS_DIR/installed_plugins.json" ]; then
    CPO_SHA=$(jq -r '.cpo_sha' "$SEED_META")
    SP_VER=$(jq -r '.sp_ver' "$SEED_META")
    SP_SHA=$(jq -r '.sp_sha' "$SEED_META")
    SEEDED_AT=$(jq -r '.seeded_at' "$SEED_META")

    cat > "$PLUGINS_DIR/known_marketplaces.json" <<KMEOF
{
  "claude-plugins-official": {
    "source": { "source": "github", "repo": "anthropics/claude-plugins-official" },
    "installLocation": "${PLUGINS_DIR}/marketplaces/claude-plugins-official",
    "lastUpdated": "${SEEDED_AT}"
  },
  "miolamio-agent-skills": {
    "source": { "source": "github", "repo": "miolamio/agent-skills" },
    "installLocation": "${PLUGINS_DIR}/marketplaces/miolamio-agent-skills",
    "lastUpdated": "${SEEDED_AT}"
  }
}
KMEOF

    cat > "$PLUGINS_DIR/installed_plugins.json" <<IPEOF
{
  "version": 2,
  "plugins": {
    "context7@claude-plugins-official": [
      {
        "scope": "user",
        "installPath": "${PLUGINS_DIR}/cache/claude-plugins-official/context7/${CPO_SHA}",
        "version": "${CPO_SHA}",
        "installedAt": "${SEEDED_AT}",
        "lastUpdated": "${SEEDED_AT}"
      }
    ],
    "skill-creator@claude-plugins-official": [
      {
        "scope": "user",
        "installPath": "${PLUGINS_DIR}/cache/claude-plugins-official/skill-creator/${CPO_SHA}",
        "version": "${CPO_SHA}",
        "installedAt": "${SEEDED_AT}",
        "lastUpdated": "${SEEDED_AT}"
      }
    ],
    "superpowers@claude-plugins-official": [
      {
        "scope": "user",
        "installPath": "${PLUGINS_DIR}/cache/claude-plugins-official/superpowers/${SP_VER}",
        "version": "${SP_VER}",
        "installedAt": "${SEEDED_AT}",
        "lastUpdated": "${SEEDED_AT}",
        "gitCommitSha": "${SP_SHA}"
      }
    ]
  }
}
IPEOF

    chown "${_USER}:${_USER}" "$PLUGINS_DIR/known_marketplaces.json" "$PLUGINS_DIR/installed_plugins.json"
    echo "[airun] plugins: context7, skill-creator, superpowers seeded" >&2
fi

# ── Post-init script (for profile-specific setup) ──
if [ -x "${_HOME}/.airun/post-init.sh" ]; then
    gosu "$_USER" "${_HOME}/.airun/post-init.sh" || true
fi

# ── Skip Claude Code login / onboarding prompt ──
# Claude Code re-triggers onboarding when lastOnboardingVersion < installed version.
# We must set lastOnboardingVersion >= the installed version.
#
# Detect version from the npm package metadata (avoids running `claude` which
# could itself trigger the onboarding flow before .claude.json exists).
CLAUDE_JSON="${_HOME}/.claude.json"
INSTALLED_VER=$(jq -r '.version // empty' "${_HOME}/.claude/local/package.json" 2>/dev/null \
    || jq -r '.version // empty' "${_HOME}/.local/lib/node_modules/@anthropic-ai/claude-code/package.json" 2>/dev/null \
    || echo "")
# Fallback: run claude --version (safe because we write .claude.json immediately after)
if [ -z "$INSTALLED_VER" ]; then
    INSTALLED_VER=$(gosu "$_USER" claude --version 2>/dev/null | head -1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' || echo "99.0.0")
fi

# Always regenerate — the file lives in the container's ephemeral layer.
USER_ID=$(od -An -tx1 -N32 /dev/urandom | tr -d ' \n')
cat > "$CLAUDE_JSON" <<CJEOF
{
  "numStartups": 184,
  "autoUpdaterStatus": "disabled",
  "userID": "${USER_ID}",
  "hasCompletedOnboarding": true,
  "hasTrustDialogAccepted": true,
  "lastOnboardingVersion": "${INSTALLED_VER}",
  "projects": {}
}
CJEOF
chown "${_USER}:${_USER}" "$CLAUDE_JSON"
echo "[airun] claude.json: onboarding=${INSTALLED_VER}" >&2

# ── Ready signal ──
echo "[airun] ready ts=$(date +%s)" >&2

# ── Drop to non-root user and exec ──
if [ "${1#-}" != "${1}" ] || [ -z "$(command -v "${1}" 2>/dev/null)" ]; then
    set -- claude "$@"
fi

exec gosu "$_USER" "$@"

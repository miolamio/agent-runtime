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

# ── Post-init script (for profile-specific setup) ──
if [ -x "${_HOME}/.airun/post-init.sh" ]; then
    gosu "$_USER" "${_HOME}/.airun/post-init.sh" || true
fi

# ── Skip Claude Code login prompt ──
CLAUDE_JSON="${_HOME}/.claude.json"
if [ ! -f "$CLAUDE_JSON" ]; then
    echo '{"hasCompletedOnboarding":true}' > "$CLAUDE_JSON"
    chown "${_USER}:${_USER}" "$CLAUDE_JSON"
fi

# ── Ready signal ──
echo "[airun] ready ts=$(date +%s)" >&2

# ── Drop to non-root user and exec ──
if [ "${1#-}" != "${1}" ] || [ -z "$(command -v "${1}" 2>/dev/null)" ]; then
    set -- claude "$@"
fi

exec gosu "$_USER" "$@"

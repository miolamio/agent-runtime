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

# ── Track image build vs volume state ──
IMAGE_BUILD_ID_FILE="/etc/airun-build-id"
VOLUME_BUILD_ID_FILE="${CONFIG_DIR}/.image-build-id"
if [ -f "$IMAGE_BUILD_ID_FILE" ]; then
    IMAGE_BUILD_ID=$(cat "$IMAGE_BUILD_ID_FILE")
    if [ -f "$VOLUME_BUILD_ID_FILE" ]; then
        VOLUME_BUILD_ID=$(cat "$VOLUME_BUILD_ID_FILE")
        if [ "$IMAGE_BUILD_ID" != "$VOLUME_BUILD_ID" ]; then
            echo "[airun] warning: image updated (build ${IMAGE_BUILD_ID}) since state volume was created (build ${VOLUME_BUILD_ID})" >&2
            echo "[airun] hint: run 'airun state reset' to re-seed from updated image" >&2
        fi
    else
        echo "$IMAGE_BUILD_ID" > "$VOLUME_BUILD_ID_FILE"
        chown "${_USER}:${_USER}" "$VOLUME_BUILD_ID_FILE"
    fi
fi

# ── Register plugin marketplaces on first run ──
# claude CLI stores registrations in ~/.claude/settings.json under
# extraKnownMarketplaces. Pre-cloned local paths are accepted for
# miolamio/anthropic-agent-skills; the "claude-plugins-official" name is
# reserved and must be registered via the anthropics GitHub source.
#
# A sentinel avoids re-running on warm starts when the state volume
# persists ~/.claude/.
PLUGINS_DIR="${_HOME}/.claude/plugins"
SENTINEL="${_HOME}/.claude/.airun-marketplaces-registered"
if [ ! -f "$SENTINEL" ]; then
    echo "[airun] registering plugin marketplaces (first run)…" >&2
    gosu "$_USER" claude plugin marketplace add anthropics/claude-plugins-official >/dev/null 2>&1 || \
        echo "[airun] warning: failed to register claude-plugins-official" >&2
    if [ -d "${PLUGINS_DIR}/marketplaces/miolamio-agent-skills" ]; then
        gosu "$_USER" claude plugin marketplace add "${PLUGINS_DIR}/marketplaces/miolamio-agent-skills" >/dev/null 2>&1 || \
            echo "[airun] warning: failed to register miolamio-agent-skills" >&2
    fi
    if [ -d "${PLUGINS_DIR}/marketplaces/anthropic-agent-skills" ]; then
        gosu "$_USER" claude plugin marketplace add "${PLUGINS_DIR}/marketplaces/anthropic-agent-skills" >/dev/null 2>&1 || \
            echo "[airun] warning: failed to register anthropic-agent-skills" >&2
    fi

    # Install base plugins (superpowers, context7, skill-creator).
    for _base in context7@claude-plugins-official skill-creator@claude-plugins-official superpowers@claude-plugins-official; do
        gosu "$_USER" claude plugin install "${_base}" >/dev/null 2>&1 || \
            echo "[airun] warning: base plugin install failed: ${_base}" >&2
    done

    mkdir -p "$(dirname "$SENTINEL")"
    touch "$SENTINEL"
    chown "${_USER}:${_USER}" "$SENTINEL"
    echo "[airun] marketplaces registered + base plugins installed" >&2
fi

# ── Profile-specific plugins (comma-separated name@marketplace list) ──
# Invoked with the single-argument "name@marketplace" form that claude CLI
# supports. Failures are non-fatal: we tolerate transient marketplace issues
# rather than block the entire run.
if [ -n "${AIRUN_PLUGINS:-}" ]; then
    echo "[airun] profile plugins: ${AIRUN_PLUGINS}" >&2
    IFS=',' read -ra _plugins <<< "${AIRUN_PLUGINS}"
    for _plugin in "${_plugins[@]}"; do
        gosu "$_USER" claude plugin install "${_plugin}" >/dev/null 2>&1 || \
            echo "[airun] warning: plugin install failed: ${_plugin}" >&2
    done
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

# ── Browser display (VNC / CDP) ──
if [ "${AIRUN_BROWSER}" = "vnc" ] || [ "${AIRUN_BROWSER}" = "both" ]; then
    echo "[airun] Starting virtual display + VNC + noVNC..." >&2
    export DISPLAY=:99
    Xvfb :99 -screen 0 1920x1080x24 -ac &>/dev/null &
    sleep 0.5
    x11vnc -display :99 -forever -shared -nopw -rfbport 5900 &>/dev/null &
    /opt/noVNC/utils/novnc_proxy --vnc localhost:5900 --listen 6080 &>/dev/null &
    echo "[airun] noVNC available at http://localhost:6080" >&2
fi

if [ "${AIRUN_BROWSER}" = "cdp" ] || [ "${AIRUN_BROWSER}" = "both" ]; then
    echo "[airun] CDP remote debugging enabled on port 9222" >&2
    export PLAYWRIGHT_CHROMIUM_ARGS="--remote-debugging-port=9222 --remote-debugging-address=0.0.0.0"
fi

# Ensure Playwright can find browsers (set in Dockerfile ENV too, but be explicit)
export PLAYWRIGHT_BROWSERS_PATH=/home/${_USER}/.cache/ms-playwright

# ── Drop to non-root user and exec ──
if [ "${1#-}" != "${1}" ] || [ -z "$(command -v "${1}" 2>/dev/null)" ]; then
    set -- claude "$@"
fi

exec gosu "$_USER" "$@"

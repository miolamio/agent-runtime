#!/bin/bash
set -e

_USER="${AUTOMATICA_USER:-claude}"
_HOME="/home/${_USER}"

# ── SSH known hosts (GitHub, GitLab, Bitbucket) ──
mkdir -p "${_HOME}/.ssh"
chmod 700 "${_HOME}/.ssh"
cat > "${_HOME}/.ssh/known_hosts" << 'KNOWN_HOSTS'
github.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl
github.com ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBEmKSENjQEezOmxkZMy7opKgwFB9nkt5YRrYMjNuG5N87uRgg6CLrbo5wAdT/y6v0mKV0U2w0WZ2YB/++Tpockg=
gitlab.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAfuCHKVTjquxvt6CM6tdG4SLp1Btn/nOeHHE5UOzRdf
gitlab.com ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBFSMqzJeV9rUzU4kWitGjeR4PWSa29SPqJ1fVkhtj3Hw9xjLVXVYrU9QlYWrOLXBpQ6KWjbjTDTdDkoohFzgbEY=
bitbucket.org ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIIazEu89wgQZ4bqs3d63QSMzYVa0MuJ2e2gKTKqu+UUO
bitbucket.org ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBPIQmuzMBuKdWeF4+a2sjSSpBK0iqitSQ+5BM9KhpexuGt20JpTVM7u5BDZngncgrqDMbWdxMWWOGtZ9UgbqgZE=
KNOWN_HOSTS
chown -R "${_USER}:${_USER}" "${_HOME}/.ssh"
chmod 600 "${_HOME}/.ssh/known_hosts"

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
if [ -x "${_HOME}/.automatica/post-init.sh" ]; then
    gosu "$_USER" "${_HOME}/.automatica/post-init.sh" || true
fi

# ── Ready signal ──
echo "[arun] ready ts=$(date +%s)" >&2

# ── Drop to non-root user and exec ──
if [ "${1#-}" != "${1}" ] || [ -z "$(command -v "${1}" 2>/dev/null)" ]; then
    set -- claude "$@"
fi

exec gosu "$_USER" "$@"

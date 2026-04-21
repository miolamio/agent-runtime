#!/usr/bin/env bash
# Without a profile, airun must mount the default `airun-claude-state` volume
# (the shared one) — not a profile-suffixed variant.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/skip.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"

install_docker_shim "$th"

PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" "ping" >/dev/null 2>&1 || true

log=$(cat "$DOCKER_SHIM_LOG")
assert_contains "$log" "airun-claude-state:/home/claude/.claude" \
    "default (unprofiled) state volume must be mounted"
assert_not_contains "$log" "airun-state-" \
    "no profile-suffixed volume when no profile is set"

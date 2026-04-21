#!/usr/bin/env bash
# Asking for a profile that doesn't exist on disk must fail early — before
# airun touches docker — with an actionable error.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/skip.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"

set +e
out=$(PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" -p nonexistent "ping" 2>&1)
ec=$?
set -e

assert_exit_code "1" "$ec" "missing profile fails airun"
assert_contains "$out" "profile"           "error mentions profile"
assert_contains "$out" "not found"         "error uses 'not found' wording"

# The failure must happen before any docker invocation.
if [[ -s "$DOCKER_SHIM_LOG" ]]; then
    die "docker shim was invoked before profile lookup failed:\n$(cat "$DOCKER_SHIM_LOG")"
fi

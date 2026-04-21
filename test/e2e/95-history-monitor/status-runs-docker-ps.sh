#!/usr/bin/env bash
# `airun --status` is a thin wrapper around `docker ps` with an ancestor
# filter. Verify airun invokes docker ps with the right flags.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"

set +e
PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" --status >/dev/null 2>&1
ec=$?
set -e
assert_exit_code "0" "$ec" "--status exits 0 when docker ps is happy"

log=$(cat "$DOCKER_SHIM_LOG")
assert_contains "$log" "ps --filter ancestor=agent-runtime" \
    "docker ps runs with the agent-runtime ancestor filter"

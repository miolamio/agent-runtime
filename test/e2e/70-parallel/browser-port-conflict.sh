#!/usr/bin/env bash
# Fixed browser host ports must be rejected before starting competing workers.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"
for mode in vnc cdp both; do
    set +e
    out=$(PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" --parallel --browser "$mode" \
        --agent 'first:inspect first' --agent 'second:inspect second' 2>&1)
    ec=$?
    set -e
    assert_exit_code 1 "$ec" "$mode rejects competing browser ports"
    assert_contains "$out" 'host ports are fixed' 'error explains the conflict'
    [[ ! -s "$DOCKER_SHIM_LOG" ]] || die 'conflicting workers reached Docker'
done
PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" --parallel --browser cdp \
    --agent 'single:inspect browser' >/dev/null 2>&1
assert_contains "$(cat "$DOCKER_SHIM_LOG")" '127.0.0.1:9222:9222' \
    'one worker can retain the existing browser port'

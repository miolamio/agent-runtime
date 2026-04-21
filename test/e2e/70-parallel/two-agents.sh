#!/usr/bin/env bash
# `airun --parallel --agent a:X --agent b:Y` must fire two docker invocations
# whose prompts differ (X vs Y). NoState is forced in parallel mode, so neither
# invocation should mount a state volume.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/skip.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"

PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" \
    --parallel \
    --agent "alice:do X" \
    --agent "bob:do Y" \
    >/dev/null 2>&1 || true

log=$(cat "$DOCKER_SHIM_LOG")
assert_contains "$log" "do X" "alice's prompt reached docker"
assert_contains "$log" "do Y" "bob's prompt reached docker"
assert_not_contains "$log" "airun-state" "parallel mode forces NoState — no state volume"

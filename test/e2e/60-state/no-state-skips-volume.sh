#!/usr/bin/env bash
# `airun --no-state` must NOT include the state volume in the docker invocation.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/skip.sh"
source "${E2E_LIB}/home.sh"

tag=$(unique_test_tag)
th=$(mk_test_home)
on_exit "rm -rf '$th'"

install_docker_shim "$th"
mk_test_profile "$th" "$tag"

PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" --no-state -p "$tag" "ping" >/dev/null 2>&1 || true

log=$(cat "$DOCKER_SHIM_LOG")
assert_not_contains "$log" "airun-state" "no state volume should be mounted"
assert_not_contains "$log" "airun-claude-state" "no default state volume either"

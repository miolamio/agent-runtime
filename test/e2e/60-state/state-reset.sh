#!/usr/bin/env bash
# `airun state reset` invokes `docker volume rm airun-claude-state`. It prints
# "no state volume to remove." if the volume didn't exist, "state volume
# removed." on success. The shim's volume-tracker starts empty so the first
# call reports the "no volume" path.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/skip.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"

install_docker_shim "$th"

# First reset: no volume present. Should still exit 0 and say so.
out=$(PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" state reset 2>&1)
assert_contains "$out" "state volume removed" "shim's volume rm always succeeds, so airun reports removal"

# `state info` tries `docker volume inspect`; the shim now no longer knows about
# airun-claude-state (we never mounted it). We expect the friendly message.
out=$(PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" state info 2>&1)
assert_contains "$out" "No state volume found" "state info handles missing volume"

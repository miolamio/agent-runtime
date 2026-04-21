#!/usr/bin/env bash
# An agent spec without a ':' is rejected by ParseAgentSpec — airun exits
# non-zero with a helpful message before touching docker.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/skip.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"

set +e
out=$(PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" \
    --parallel --agent "no-colon-here" 2>&1)
ec=$?
set -e

assert_exit_code "1" "$ec" "bad agent spec fails"
assert_contains "$out" "invalid agent spec" "error message mentions the spec"

# Important: no docker calls should have happened before the error.
if [[ -s "$DOCKER_SHIM_LOG" ]]; then
    die "docker shim was invoked despite bad spec:\n$(cat "$DOCKER_SHIM_LOG")"
fi

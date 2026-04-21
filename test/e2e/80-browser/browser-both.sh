#!/usr/bin/env bash
# --browser both must publish both ports (6080, 9222) and set AIRUN_BROWSER=both.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"

PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" --browser both "ping" >/dev/null 2>&1 || true

log=$(cat "$DOCKER_SHIM_LOG")
assert_contains "$log" "AIRUN_BROWSER=both" "browser env var set"
assert_contains "$log" "6080:6080"          "VNC port published"
assert_contains "$log" "9222:9222"          "CDP port published"

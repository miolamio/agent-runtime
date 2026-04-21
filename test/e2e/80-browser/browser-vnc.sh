#!/usr/bin/env bash
# --browser vnc must set AIRUN_BROWSER=vnc and publish the noVNC port 6080,
# but NOT the CDP port 9222.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"

PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" --browser vnc "ping" >/dev/null 2>&1 || true

log=$(cat "$DOCKER_SHIM_LOG")
assert_contains "$log" "AIRUN_BROWSER=vnc" "browser env var set"
assert_contains "$log" "6080:6080"         "VNC port published"
assert_not_contains "$log" "9222:9222"     "CDP port NOT published for vnc mode"

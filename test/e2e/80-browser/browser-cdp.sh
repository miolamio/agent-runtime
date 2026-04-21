#!/usr/bin/env bash
# --browser cdp must set AIRUN_BROWSER=cdp and publish the Chromium DevTools
# port 9222, but NOT the noVNC port 6080.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"

PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" --browser cdp "ping" >/dev/null 2>&1 || true

log=$(cat "$DOCKER_SHIM_LOG")
assert_contains "$log" "AIRUN_BROWSER=cdp" "browser env var set"
assert_contains "$log" "9222:9222"         "CDP port published"
assert_not_contains "$log" "6080:6080"     "VNC port NOT published for cdp mode"

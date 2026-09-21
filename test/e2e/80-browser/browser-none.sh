#!/usr/bin/env bash
# Without --browser, airun must not emit AIRUN_BROWSER or any -p port mapping.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"

PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" "ping" >/dev/null 2>&1 || true

log=$(cat "$DOCKER_SHIM_LOG")
assert_not_contains "$log" "AIRUN_BROWSER" "no browser env by default"
assert_not_contains "$log" "127.0.0.1:6080:6080"     "no VNC port by default"
assert_not_contains "$log" "127.0.0.1:9222:9222"     "no CDP port by default"

assert_not_contains "$log" "-p 6080:6080" "no public VNC mapping"
assert_not_contains "$log" "-p 9222:9222" "no public CDP mapping"

#!/usr/bin/env bash
# Bind mode (default when ARUN_MODE=bind) must pass `-v <mount>:/workspace`
# to `docker run`. airun resolves the mount to cwd when --mount is not set
# and the profile doesn't override it.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/skip.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"

# config.env from mk_test_home defaults to ARUN_MODE=bind.
workdir="$th/work"
mkdir -p "$workdir"
cd "$workdir"
PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" "ping" >/dev/null 2>&1 || true

log=$(cat "$DOCKER_SHIM_LOG")
assert_contains "$log" "run --rm"     "bind mode uses \`docker run --rm\`, not create"
assert_contains "$log" "${workdir}:/workspace" "cwd is bind-mounted as /workspace"

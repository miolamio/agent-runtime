#!/usr/bin/env bash
# Running airun with -p <profile> instructs docker to mount a per-profile state
# volume named "airun-state-<profile>". Uses the docker shim so the test runs
# offline — we only need to verify airun's argument construction.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/skip.sh"
source "${E2E_LIB}/home.sh"

tag=$(unique_test_tag)
th=$(mk_test_home)
on_exit "rm -rf '$th'"

install_docker_shim "$th"
mk_test_profile "$th" "$tag"

# airun is expected to return 0 because the shim reports success for every
# docker subcommand.
set +e
PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" -p "$tag" "ping" >/dev/null 2>&1
ec=$?
set -e
assert_exit_code "0" "$ec" "airun with shimmed docker"

# Expect a `docker run --rm ...` or `docker create ...` invocation that
# mounts the per-profile state volume. Snapshot mode is the default so we
# match either `create` or `run`.
log=$(cat "$DOCKER_SHIM_LOG")
assert_contains "$log" "airun-state-${tag}:/home/claude/.claude" \
    "per-profile state volume mount present in docker invocation"

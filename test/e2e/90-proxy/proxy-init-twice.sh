#!/usr/bin/env bash
# Running `proxy init` twice on the same HOME must fail loud instead of
# silently clobbering an existing proxy.yaml.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"

HOME="$th" "$AIRUN_BIN" proxy init >/dev/null
assert_file_exists "$th/.airun/proxy.yaml"

set +e
out=$(HOME="$th" "$AIRUN_BIN" proxy init 2>&1)
ec=$?
set -e
assert_exit_code "1" "$ec" "second init fails"
assert_contains "$out" "already exists" "error names the collision"

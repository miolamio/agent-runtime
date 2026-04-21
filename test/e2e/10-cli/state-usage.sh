#!/usr/bin/env bash
# `airun state` without a subcommand prints its own usage and exits 1;
# unknown state subcommand is a hard error.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"

set +e
out=$("$AIRUN_BIN" state 2>&1); ec=$?
set -e
assert_exit_code "1" "$ec" "bare state exits 1"
assert_contains "$out" "state <info|reset>" "usage mentions info|reset"

set +e
out=$("$AIRUN_BIN" state bogus 2>&1); ec=$?
set -e
assert_exit_code "1" "$ec" "unknown state subcommand exits 1"
assert_contains "$out" "Unknown state subcommand" "helpful error"

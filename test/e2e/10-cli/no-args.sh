#!/usr/bin/env bash
# `airun` with no args prints usage on stdout and exits with code 1.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"

set +e
out=$("$AIRUN_BIN" 2>&1)
ec=$?
set -e
assert_exit_code "1" "$ec" "bare airun exits 1"
assert_contains "$out" "Usage:" "no-args prints usage"

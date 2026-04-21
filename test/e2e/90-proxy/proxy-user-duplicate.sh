#!/usr/bin/env bash
# Adding a user with a name that already exists must fail (the store key is
# the name; silently replacing would invalidate the old token).
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"

HOME="$th" "$AIRUN_BIN" proxy init >/dev/null
HOME="$th" "$AIRUN_BIN" proxy user add carol >/dev/null

set +e
out=$(HOME="$th" "$AIRUN_BIN" proxy user add carol 2>&1)
ec=$?
set -e
assert_exit_code "1" "$ec" "duplicate add fails"
assert_contains "$out" "already exists" "error mentions already-exists"

#!/usr/bin/env bash
# Fresh HOME with no runs yet — `airun history` should report that gracefully,
# not spit a stack trace or fail.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"

out=$(HOME="$th" "$AIRUN_BIN" history 2>&1)
ec=$?
assert_exit_code "0" "$ec" "history exits 0 on empty"
# Either "No run history yet." (dir missing) or "No runs yet." (dir empty) is
# acceptable — both are user-facing "nothing here" messages.
if [[ "$out" != *"No run"* ]]; then
    die "expected an empty-history message, got:\n$out"
fi

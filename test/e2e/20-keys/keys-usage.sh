#!/usr/bin/env bash
# `airun keys` without subcommand, and each subcommand missing its required
# argument, must print a usage hint and exit 1 without touching the config.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/skip.sh"

skip_if_no_airun_config

set +e
out=$("$AIRUN_BIN" keys 2>&1); ec=$?
set -e
assert_exit_code "1" "$ec" "bare keys exits 1"
assert_contains "$out" "keys <list|add|remove|test|default>" "lists subcommands"

for sub in add remove default model; do
    set +e
    out=$("$AIRUN_BIN" keys "$sub" 2>&1); ec=$?
    set -e
    assert_exit_code "1" "$ec" "keys $sub without arg exits 1"
    assert_contains "$out" "Usage:" "keys $sub missing-arg shows usage"
done

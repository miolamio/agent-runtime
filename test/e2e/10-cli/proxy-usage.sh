#!/usr/bin/env bash
# `airun proxy` without subcommand shows its usage; unknown proxy subcommand fails.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"

set +e
out=$("$AIRUN_BIN" proxy 2>&1); ec=$?
set -e
assert_exit_code "1" "$ec" "bare proxy exits 1"
assert_contains "$out" "init|serve|user|connect|disconnect" "usage lists proxy subcmds"

set +e
out=$("$AIRUN_BIN" proxy bogus 2>&1); ec=$?
set -e
assert_exit_code "1" "$ec" "unknown proxy subcommand exits 1"
assert_contains "$out" "Unknown proxy subcommand" "helpful error"

#!/usr/bin/env bash
# Sanity: harness lib sources cleanly and the basic assertions behave.
# Lives in 00-prereq so it fails loud if the harness itself is broken before
# any real test runs.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"

assert_eq "1" "1" "equal literals"
assert_contains "hello world" "llo wo" "substring present"
assert_not_contains "hello world" "xyz" "substring absent"

# Resolution of AIRUN_REPO_ROOT should land on the repo that contains go.mod.
assert_file_exists "${AIRUN_REPO_ROOT}/go.mod" "repo root detection"

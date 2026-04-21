#!/usr/bin/env bash
# Revoke flips the user's Active flag; restore flips it back. List reflects it.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"

HOME="$th" "$AIRUN_BIN" proxy init >/dev/null
HOME="$th" "$AIRUN_BIN" proxy user add bob >/dev/null

HOME="$th" "$AIRUN_BIN" proxy user revoke bob >/dev/null
list=$(HOME="$th" "$AIRUN_BIN" proxy user list)
assert_contains "$list" "revoked" "bob appears as revoked"

HOME="$th" "$AIRUN_BIN" proxy user restore bob >/dev/null
list=$(HOME="$th" "$AIRUN_BIN" proxy user list)
assert_contains "$list" "active" "bob flips back to active"

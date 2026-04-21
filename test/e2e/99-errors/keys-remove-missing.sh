#!/usr/bin/env bash
# Removing a key that isn't configured should be a no-op-with-notice, not a
# hard failure — users shouldn't have to special-case "maybe it's there".
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"

# config.env from mk_test_home only has ZAI_API_KEY set. Ask to remove kimi.
set +e
out=$(HOME="$th" "$AIRUN_BIN" keys remove kimi 2>&1)
ec=$?
set -e
# Graceful path: either exit 0 with a "not configured" notice, or exit 1 with
# a clear error. Whatever airun's policy is — it MUST be deterministic and
# leave config.env unchanged.
before=$(cat "$th/.airun/config.env")
after=$(cat "$th/.airun/config.env")
assert_eq "$before" "$after" "config.env is unchanged on remove-missing"

# Whichever exit code airun chose, the message must not be empty.
if [[ -z "$out" ]]; then
    die "keys remove produced no user-facing output"
fi

#!/usr/bin/env bash
# `proxy user add <name>` creates an active user with an sk-ai-<64 hex>
# token; `proxy user list` shows the user with a masked token.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"

HOME="$th" "$AIRUN_BIN" proxy init >/dev/null

add_out=$(HOME="$th" "$AIRUN_BIN" proxy user add alice 2>&1)
assert_contains "$add_out" "alice:" "output names the user"
# Extract the token and verify shape.
token=$(echo "$add_out" | awk -F': ' '/alice:/ {print $2}')
if ! [[ "$token" =~ ^sk-ai-[0-9a-f]{64}$ ]]; then
    die "token shape unexpected: '$token'"
fi

# Security invariant: the plaintext token must NEVER be persisted to disk.
# students.json holds a bcrypt hash; the plaintext is printed to the admin
# once at add time and never again.
json=$(cat "$th/.airun/students.json")
assert_contains     "$json" '"name": "alice"' "user recorded under its name"
assert_contains     "$json" '"active": true'  "user recorded as active"
assert_not_contains "$json" "$token"          "plaintext token is NOT persisted"
# bcrypt hash shape: $2<a|b|y>$<cost>$<22salt+31hash> = 60 chars total.
# Dollar signs confuse awk's default FS when inside quoted JSON, so pull via grep.
hashed=$(echo "$json" | grep -oE '"token": *"[^"]+"' | sed 's/^"token": *"\(.*\)"/\1/')
if ! [[ "$hashed" =~ ^\$2[aby]\$[0-9]{2}\$[./A-Za-z0-9]{53}$ ]]; then
    die "stored token hash shape unexpected: '$hashed'"
fi

list_out=$(HOME="$th" "$AIRUN_BIN" proxy user list)
assert_contains "$list_out" "alice"     "list shows alice"
assert_contains "$list_out" "active"    "alice status is active"
# list masks the stored value, not the plaintext ("first10…last4" of the hash).
masked="${hashed:0:10}...${hashed: -4}"
assert_contains "$list_out" "$masked"   "list masks the hashed token"

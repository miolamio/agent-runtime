#!/usr/bin/env bash
# Settings travel in the secret-free read-only manifest. The container owns
# the private effective settings file; it must not write to a host file.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/skip.sh"
source "${E2E_LIB}/home.sh"

tag=$(unique_test_tag)
th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"

mk_test_profile "$th" "$tag" "settings:
  autoApproveToolUse: true
  favouriteColor: blue"

PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" --profile "$tag" "ping" >/dev/null 2>&1 || true

log=$(cat "$DOCKER_SHIM_LOG")
assert_contains "$log" "/run/airun/profile.json:ro" \
    "profile manifest is read-only"
assert_not_contains "$log" "/home/claude/.claude/settings.json" \
    "effective settings are not mounted from the host"

snap="$DOCKER_SHIM_CAPTURE/profile.json"
assert_file_exists "$snap" "shim captured the normalized profile"
content=$(cat "$snap")
# The Go side marshals a map with yaml.v3 → any; json.Marshal preserves types
# but map key order is non-deterministic, so grep for substrings instead.
assert_contains "$content" '"autoApproveToolUse":true' "settings contains the bool flag"
assert_contains "$content" '"favouriteColor":"blue"'   "settings contains the string value"
assert_contains "$content" '"version":1' "manifest contract is versioned"
assert_contains "$content" "\"profile_key\":\"$tag\"" "canonical selector is retained"

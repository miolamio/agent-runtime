#!/usr/bin/env bash
# A profile with a `settings:` block must marshal it to JSON and mount the
# resulting file as /home/claude/.claude/settings.json read-only.
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

PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" -p "$tag" "ping" >/dev/null 2>&1 || true

log=$(cat "$DOCKER_SHIM_LOG")
assert_contains "$log" "/home/claude/.claude/settings.json:ro" \
    "settings.json is bind-mounted read-only"

snap="$DOCKER_SHIM_CAPTURE/settings.json"
assert_file_exists "$snap" "shim captured the rendered settings.json"
content=$(cat "$snap")
# The Go side marshals a map with yaml.v3 → any; json.Marshal preserves types
# but map key order is non-deterministic, so grep for substrings instead.
assert_contains "$content" '"autoApproveToolUse":true' "settings contains the bool flag"
assert_contains "$content" '"favouriteColor":"blue"'   "settings contains the string value"

#!/usr/bin/env bash
# All plugin references flow through the manifest. The adapter owns exact
# baseline deduplication and must see conflicting marketplace identities.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/skip.sh"
source "${E2E_LIB}/home.sh"

tag=$(unique_test_tag)
th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"

mk_test_profile "$th" "$tag" "plugins:
  - superpowers
  - context7
  - skill-creator
  - frontend-design@market1
  - playwright-cli@miolamio-agent-skills"

PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" --profile "$tag" "ping" >/dev/null 2>&1

# Post-init script era is over — the shim must NOT see such a bind mount.
log=$(cat "$DOCKER_SHIM_LOG")
assert_not_contains "$log" "post-init.sh" \
    "post-init.sh is no longer mounted"

# The env file points to the manifest; it does not carry a second plugin list.
env_snapshot="$DOCKER_SHIM_CAPTURE/env-file.env"
assert_file_exists "$env_snapshot" "shim captured the env file airun generated"
env_contents=$(cat "$env_snapshot")
assert_contains "$env_contents" \
    "AIRUN_PROFILE_MANIFEST=/run/airun/profile.json" \
    "env file identifies declarative input"
assert_not_contains "$env_contents" "AIRUN_PLUGINS=" "old imperative plugin input is absent"
manifest=$(cat "$DOCKER_SHIM_CAPTURE/profile.json")
assert_contains "$manifest" '"native_plugins":["superpowers","context7","skill-creator","frontend-design@market1","playwright-cli@miolamio-agent-skills"]' \
    "adapter receives all exact native references"

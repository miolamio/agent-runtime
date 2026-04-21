#!/usr/bin/env bash
# Profile plugins flow to the container via AIRUN_PLUGINS in the env file.
# Base plugins (superpowers, context7, skill-creator) are pre-seeded at image
# build time and must be filtered out by the runner — only "extras" should
# appear in AIRUN_PLUGINS.
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

out=$(PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" -p "$tag" "ping" 2>&1 || true)

# Runner logs the filtered list (base plugins dropped).
assert_contains "$out" \
    "extra plugins: frontend-design@market1, playwright-cli@miolamio-agent-skills" \
    "runner announces only non-base plugins"

# Post-init script era is over — the shim must NOT see such a bind mount.
log=$(cat "$DOCKER_SHIM_LOG")
assert_not_contains "$log" "post-init.sh" \
    "post-init.sh is no longer mounted"

# AIRUN_PLUGINS must land in the env file airun passes via --env-file.
env_snapshot="$DOCKER_SHIM_CAPTURE/env-file.env"
assert_file_exists "$env_snapshot" "shim captured the env file airun generated"
env_contents=$(cat "$env_snapshot")
assert_contains "$env_contents" \
    "AIRUN_PLUGINS=frontend-design@market1,playwright-cli@miolamio-agent-skills" \
    "env file carries the filtered plugin list"

# Base plugins must NOT appear inside AIRUN_PLUGINS.
plugins_line=$(grep '^AIRUN_PLUGINS=' "$env_snapshot")
assert_not_contains "$plugins_line" "superpowers" \
    "superpowers filtered out (pre-seeded at build time)"
assert_not_contains "$plugins_line" "context7" \
    "context7 filtered out (pre-seeded at build time)"
assert_not_contains "$plugins_line" "skill-creator" \
    "skill-creator filtered out (pre-seeded at build time)"

#!/usr/bin/env bash
# Profile plugins must be rendered into a post-init script that airun mounts
# into the container. Each plugin becomes a `claude plugin install <spec>` line.
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
  - frontend-design@market1"

out=$(PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" -p "$tag" "ping" 2>&1 || true)

# airun logs the plugin list once on startup.
assert_contains "$out" "plugins: superpowers, frontend-design@market1" \
    "airun announces the plugin list"

log=$(cat "$DOCKER_SHIM_LOG")
assert_contains "$log" "/home/claude/.airun/post-init.sh:ro" \
    "post-init.sh is bind-mounted into the container"

# The shim captured the rendered script under captured_mounts/post-init.sh.
script="$DOCKER_SHIM_CAPTURE/post-init.sh"
assert_file_exists "$script" "shim captured the plugin script"
content=$(cat "$script")
assert_contains "$content" "claude plugin install superpowers" \
    "simple plugin rendered without marketplace"
assert_contains "$content" "claude plugin install frontend-design --marketplace market1" \
    "name@marketplace split rendered correctly"

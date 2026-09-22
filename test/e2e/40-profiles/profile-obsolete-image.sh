#!/usr/bin/env bash
# A valid profile cannot launch or update using an image without manifest v1.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"
mk_test_profile "$th" reviewer 'settings: {agent: code-reviewer}'

for mode in normal shell update; do
    case "$mode" in
        normal) args=(--profile reviewer ping) ;;
        shell) args=(shell --profile reviewer) ;;
        update) args=(profile update reviewer) ;;
    esac
    : > "$DOCKER_SHIM_LOG"
    set +e
    out=$(PATH="$th/bin:$PATH" HOME="$th" DOCKER_SHIM_MANIFEST_VERSION=0 \
        "$AIRUN_BIN" "${args[@]}" 2>&1)
    ec=$?
    set -e
    assert_exit_code 1 "$ec" "$mode rejects incompatible image"
    assert_contains "$out" 'profile manifest version 1' 'error names required contract'
    assert_contains "$out" 'airun rebuild' 'error provides repair command'
    log=$(cat "$DOCKER_SHIM_LOG")
    assert_contains "$log" 'image inspect' 'image capability was checked'
    if grep -Eq '^(run|create|start|cp) ' "$DOCKER_SHIM_LOG"; then
        die "$mode attempted a container launch after image incompatibility"
    fi
done

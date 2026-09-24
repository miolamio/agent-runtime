#!/usr/bin/env bash
# The short profile alias remains compatible while guiding callers to --profile.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"
mk_test_profile "$th" alias

for mode in normal shell; do
    args=(-p alias)
    if [[ "$mode" == shell ]]; then
        args=(shell "${args[@]}")
    else
        args+=(ping)
    fi
    out=$(PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" "${args[@]}" 2>&1)
    assert_contains "$out" '[airun] warning: -p is deprecated; use --profile' "$mode warns about the short alias"
    manifest=$(cat "$DOCKER_SHIM_CAPTURE/profile.json")
    assert_contains "$manifest" '"profile_key":"alias"' "$mode selects the requested profile"
done
: > "$DOCKER_SHIM_LOG"

out=$(PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" --profile alias ping 2>&1)
assert_not_contains "$out" 'deprecated' 'long profile option does not warn'
: > "$DOCKER_SHIM_LOG"

for usage in missing-action missing-name unknown-action extra-name; do
    case "$usage" in
        missing-action) args=(profile) ;;
        missing-name) args=(profile update) ;;
        unknown-action) args=(profile inspect reviewer) ;;
        extra-name) args=(profile update reviewer extra) ;;
    esac
    set +e
    out=$(PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" "${args[@]}" 2>&1)
    ec=$?
    set -e
    assert_exit_code 1 "$ec" "$usage fails"
    assert_contains "$out" 'airun profile update NAME' "$usage explains update syntax"
done
[[ ! -s "$DOCKER_SHIM_LOG" ]] || die "invalid CLI input reached Docker"

out=$(HOME="$th" "$AIRUN_BIN" --help)
assert_contains "$out" '--profile' 'help documents long option'
assert_contains "$out" 'airun profile update reviewer' 'help documents explicit update'
assert_contains "$out" '-p               Deprecated alias for --profile' 'help documents the transition'
assert_not_contains "$out" 'airun -p ' 'examples remove profile short alias'

#!/usr/bin/env bash
# Profile selection has one long option; update requires exactly one selector.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"

for mode in normal shell; do
    args=(-p unused)
    if [[ "$mode" == shell ]]; then
        args=(shell "${args[@]}")
    else
        args+=(ping)
    fi
    set +e
    out=$(PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" "${args[@]}" 2>&1)
    ec=$?
    set -e
    assert_exit_code 2 "$ec" "$mode rejects removed profile alias"
    assert_contains "$out" 'flag provided but not defined: -p' "$mode identifies removed alias"
done

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
assert_not_contains "$out" '-p, --profile' 'help removes profile short alias'
assert_not_contains "$out" 'airun -p ' 'examples remove profile short alias'

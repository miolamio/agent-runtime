#!/usr/bin/env bash
# Schema errors and missing/unsafe MCP values fail before any Docker operation.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"
unset AIRUN_E2E_MISSING_TOKEN

for scenario in unknown-field invalid-reference missing-credential unsafe-credential; do
    case "$scenario" in
        unknown-field)
            source_yaml='components: {hooks: []}'
            field='components.hooks'
            ;;
        invalid-reference)
            source_yaml='components: {agents: [{id: tools/item, env: {TOKEN: HOST}}]}'
            field='components.agents[0].env'
            ;;
        missing-credential|unsafe-credential)
            source_yaml='components: {mcps: [{id: test/component, env: {TOKEN: AIRUN_E2E_MISSING_TOKEN}}]}'
            field='components.mcps[0].env.TOKEN'
            ;;
    esac
    mk_test_profile "$th" invalid "$source_yaml"
    if [[ "$scenario" == unsafe-credential ]]; then
        export AIRUN_E2E_MISSING_TOKEN=$'synthetic-unsafe-canary\nANTHROPIC_AUTH_TOKEN=injected'
    fi
    for mode in normal shell update; do
        case "$mode" in
            normal) args=(--profile invalid ping) ;;
            shell) args=(shell --profile invalid) ;;
            update) args=(profile update invalid) ;;
        esac
        set +e
        out=$(PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" "${args[@]}" 2>&1)
        ec=$?
        set -e
        assert_exit_code 1 "$ec" "$scenario fails for $mode"
        assert_contains "$out" "$field" "$scenario identifies its field"
        assert_not_contains "$out" 'synthetic-unsafe-canary' 'unsafe credential is not printed'
        assert_not_contains "$out" 'ANTHROPIC_AUTH_TOKEN=injected' 'injected line is not printed'
        [[ ! -s "$DOCKER_SHIM_LOG" ]] || die "$scenario reached Docker during $mode"
    done
done

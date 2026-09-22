#!/usr/bin/env bash
# Historical state and host agents are inputs separate from active configuration.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"
cat > "$th/.airun/profiles/reviewer.yaml" <<'PROFILE'
name: Human-readable review role
settings: {agent: code-reviewer}
components: {agents: [development-tools/code-reviewer]}
PROFILE
printf 'host agent sentinel\n' > "$th/.airun/agents/host-reviewer.md"

PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" --profile reviewer ping >/dev/null 2>&1
log=$(cat "$DOCKER_SHIM_LOG")
assert_contains "$log" 'airun-state-reviewer:/var/lib/airun/state' 'state identity uses selector, not display name'
assert_contains "$log" 'AIRUN_PROFILE_STATE=/var/lib/airun/state' 'historical input is declared'
assert_contains "$log" 'airun-components-cache:/var/lib/airun/components' 'artifact cache has a separate lifetime'
assert_contains "$log" "$th/.airun/agents:/run/airun/host-agents:ro" 'host agents are a read-only input'
assert_contains "$log" 'AIRUN_HOST_AGENTS=/run/airun/host-agents' 'host-agent input is declared'
assert_not_contains "$log" ':/home/claude/.claude' 'active configuration is not mounted from historical state'
assert_eq 'host agent sentinel' "$(cat "$th/.airun/agents/host-reviewer.md")" 'host agent stays unchanged'

# State reset must leave the independent component cache alone.
: > "$DOCKER_SHIM_LOG"
PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" state reset >/dev/null 2>&1
log=$(cat "$DOCKER_SHIM_LOG")
assert_contains "$log" 'volume rm airun-claude-state' 'legacy reset target remains supported'
assert_not_contains "$log" 'airun-components-cache' 'reset does not clear retained components'

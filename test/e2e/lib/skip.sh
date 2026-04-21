#!/usr/bin/env bash
# skip.sh — guard helpers. Each one calls `skip "..."` (exit 77) when its
# precondition isn't met. Source after harness.sh and env.sh.
set -euo pipefail

skip_if_no_docker() {
    if ! docker info >/dev/null 2>&1; then
        skip "docker daemon is not reachable"
    fi
}

skip_if_no_image() {
    local tag="${1:-agent-runtime:latest}"
    if ! docker image inspect "$tag" >/dev/null 2>&1; then
        skip "image ${tag} not built — run \`airun rebuild\`"
    fi
}

skip_if_no_airun_config() {
    if [[ -z "${AIRUN_CONFIG_ENV:-}" ]]; then
        skip "no ~/.airun/config.env — run \`airun init\` or set AIRUN_TEST_ENV"
    fi
}

skip_if_no_zai_key() {
    if [[ -z "${ZAI_API_KEY:-}" ]]; then
        skip "ZAI_API_KEY not configured — container tests require Z.AI"
    fi
}

skip_if_no_tty() {
    if [[ ! -t 0 ]]; then
        skip "stdin is not a TTY"
    fi
}

# skip_unless_network: tests that actually call a model provider (burn quota)
# run only when the orchestrator exported E2E_WITH_NETWORK=1.
skip_unless_network() {
    if [[ "${E2E_WITH_NETWORK:-0}" != "1" ]]; then
        skip "requires --with-network (would call live API)"
    fi
}

# skip_unless_non_glm: tests for non-default providers. Container tests outside
# zai/glm-5.1 are opt-in per project feedback (GLM-5.1 is the sanctioned model).
skip_unless_non_glm() {
    if [[ "${E2E_INCLUDE_NON_GLM:-0}" != "1" ]]; then
        skip "non-GLM provider test — opt in with --include-non-glm"
    fi
}

#!/usr/bin/env bash
# When RemoteModels is configured (a whitelist) and the user asks for a model
# that isn't on it, airun must reject before any docker call — not pass it
# through to the proxy and learn later.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"

# Configure the remote provider with a known model list.
cat >> "$th/.airun/config.env" <<'EOF'
ARUN_PROVIDER=remote
REMOTE_BASE_URL=http://127.0.0.1:1
REMOTE_API_KEY=sk-ai-fake
REMOTE_MODELS=glm-5.1,kimi-k2.5
REMOTE_DEFAULT_MODEL=glm-5.1
EOF

set +e
out=$(PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" \
    --provider remote --model bogus-model-7 "ping" 2>&1)
ec=$?
set -e
assert_exit_code "1" "$ec" "unknown remote model fails"
assert_contains "$out" "not available on remote proxy" "helpful error text"
assert_contains "$out" "glm-5.1,kimi-k2.5"             "error lists available models"

if [[ -s "$DOCKER_SHIM_LOG" ]]; then
    die "docker was invoked despite invalid-model rejection:\n$(cat "$DOCKER_SHIM_LOG")"
fi

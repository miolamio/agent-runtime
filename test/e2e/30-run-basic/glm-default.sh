#!/usr/bin/env bash
# Full-stack smoke test: bring up the real agent-runtime container against
# the user's real ZAI_API_KEY, run one short prompt on GLM-5.1, expect exit 0.
#
# This is the ONLY container test we run by default; it's network-gated and
# --no-state so it won't mutate the user's state volume. Anything more
# ambitious belongs either here (with --with-network) or behind separate
# opt-in groups to avoid burning provider quota.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/docker.sh"
source "${E2E_LIB}/skip.sh"

skip_unless_network
skip_if_no_docker
skip_if_no_image "agent-runtime:latest"
skip_if_no_airun_config
skip_if_no_zai_key

# Leave HOME alone — we want the real config.env and real key. --no-state
# gives us ephemeral isolation so no state volume is touched.
out=$("$AIRUN_BIN" --no-state "Reply with exactly the word: OK" 2>&1)
ec=$?
assert_exit_code "0" "$ec" "airun GLM-5.1 run exits 0"
# Model output can be verbose and wrapped; we just check that the container
# reported provider=zai and model=glm-5.1 somewhere in its banner so we know
# routing worked.
assert_contains "$out" "provider=zai"   "airun banner reports provider=zai"
assert_contains "$out" "model=glm-5.1"  "airun banner reports model=glm-5.1"
# The container emits a "[airun] done in …" summary on success.
assert_contains "$out" "done in"        "airun printed the done-in summary"

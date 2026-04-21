#!/usr/bin/env bash
# ~/.airun/config.env (or $AIRUN_TEST_ENV) is present and readable. Tests that
# need a provider key will further gate on the specific key being set.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/skip.sh"

skip_if_no_airun_config

assert_file_exists "$AIRUN_CONFIG_ENV"
# Required for Z.AI-backed container tests (90% of our coverage).
if [[ -z "${ZAI_API_KEY:-}" ]]; then
    log_warn "ZAI_API_KEY not set in $AIRUN_CONFIG_ENV — network-gated tests will skip"
fi

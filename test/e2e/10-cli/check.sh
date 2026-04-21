#!/usr/bin/env bash
# `airun --check` prints prerequisites + config summary. It never fails loud —
# it's diagnostic — but its output must mention the config path we load from.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/skip.sh"

skip_if_no_airun_config

out=$("$AIRUN_BIN" --check 2>&1)
assert_contains "$out" "Prerequisites:"       "shows prereq block"
assert_contains "$out" "Config (~/.airun/config.env)" "shows config block label"
assert_contains "$out" "Env file:"           "shows env file path line"

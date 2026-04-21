#!/usr/bin/env bash
# `airun --help` / `-h` print usage, list the main subcommands, and exit 0.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"

for flag in --help -h; do
    out=$("$AIRUN_BIN" "$flag")
    assert_contains "$out" "Usage:"          "$flag shows Usage:"
    assert_contains "$out" "airun shell"     "$flag mentions shell subcommand"
    assert_contains "$out" "airun keys"      "$flag mentions keys subcommand"
    assert_contains "$out" "airun proxy"     "$flag mentions proxy subcommand"
    assert_contains "$out" "GLM-5.1"         "$flag advertises GLM-5.1"
done

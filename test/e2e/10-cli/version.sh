#!/usr/bin/env bash
# `airun --version` and `airun -v` both exit 0 and print "airun X.Y.Z".
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"

for flag in --version -v; do
    out=$("$AIRUN_BIN" "$flag")
    assert_contains "$out" "airun" "$flag output mentions airun"
    if ! [[ "$out" =~ airun\ [0-9]+\.[0-9]+\.[0-9]+ ]]; then
        die "'$flag' output has no X.Y.Z version: '$out'"
    fi
done

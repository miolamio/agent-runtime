#!/usr/bin/env bash
# airun binary exists, is executable, and reports a sane --version line.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"

assert_file_exists "$AIRUN_BIN" "airun binary"
if [[ ! -x "$AIRUN_BIN" ]]; then
    die "airun binary is not executable: $AIRUN_BIN"
fi

out=$("$AIRUN_BIN" --version)
assert_contains "$out" "airun" "version output"
# version looks like "airun X.Y.Z"
if ! [[ "$out" =~ ^airun\ [0-9]+\.[0-9]+\.[0-9]+ ]]; then
    die "unexpected --version format: '$out'"
fi

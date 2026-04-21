#!/usr/bin/env bash
# `airun keys list` (and its `ls` alias) must exit 0 and list the configured
# providers. This is a read-only operation — it never mutates config.env.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/skip.sh"

skip_if_no_airun_config

# Snapshot config.env mtime so we can assert `list` didn't touch it.
mtime_before=$(stat -f '%m' "$AIRUN_CONFIG_ENV" 2>/dev/null || stat -c '%Y' "$AIRUN_CONFIG_ENV")

for sub in list ls; do
    out=$("$AIRUN_BIN" keys "$sub")
    # At minimum the command should mention at least one provider name. We can't
    # assume any specific provider is configured on the host, so check for the
    # fixed header-like tokens the list command prints.
    assert_contains "$out" "Provider" "keys $sub output has header"
done

mtime_after=$(stat -f '%m' "$AIRUN_CONFIG_ENV" 2>/dev/null || stat -c '%Y' "$AIRUN_CONFIG_ENV")
assert_eq "$mtime_before" "$mtime_after" "keys list must not touch config.env"

#!/usr/bin/env bash
# After airun completes a non-interactive run (shimmed docker), a history
# record must land under ~/.airun/runs/ and `airun history` must show it.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"

PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" "say hi from history test" >/dev/null 2>&1

# At least one run dir must exist with a meta.json inside.
runs="$th/.airun/runs"
assert_file_exists "$runs" "runs dir created"
metas=$(find "$runs" -name 'meta.json' 2>/dev/null | head -1)
if [[ -z "$metas" ]]; then
    die "no meta.json under $runs"
fi
# Meta must include our prompt.
grep -q "say hi from history test" "$metas" \
    || die "meta.json missing prompt text"

# `airun history` must render the row: prompt prefix, TIME/PROFILE header.
out=$(HOME="$th" "$AIRUN_BIN" history)
assert_contains "$out" "TIME"        "history renders the header"
assert_contains "$out" "say hi from" "history shows our prompt prefix"

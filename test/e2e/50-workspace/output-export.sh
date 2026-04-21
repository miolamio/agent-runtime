#!/usr/bin/env bash
# `airun --output <dir>` routes through the export lifecycle: docker create +
# start + `docker cp container:/workspace/. <dir>` + rm, using an airun-export-
# container name.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/skip.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"

out="$th/export"
workdir="$th/work"
mkdir -p "$workdir"
cd "$workdir"

PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" --output "$out" "ping" >/dev/null 2>&1 || true

log=$(cat "$DOCKER_SHIM_LOG")
assert_contains "$log" "create --name airun-export-" "export path uses airun-export-* names"
assert_contains "$log" "cp airun-export-"            "docker cp copies workspace out"
assert_contains "$log" ":/workspace/. ${out}"        "docker cp target is the --output dir"
# mkdir -p happens before cp, so the output dir must exist afterward.
assert_file_exists "$out" "output dir was created"

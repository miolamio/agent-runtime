#!/usr/bin/env bash
# Snapshot mode uses `docker create` + `docker cp <mount>/. container:/workspace`
# + `docker start -a` + `docker rm` — NOT a direct `docker run -v`.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/skip.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"

# Switch to snapshot mode.
sed -i.bak 's/^ARUN_MODE=.*/ARUN_MODE=snapshot/' "$th/.airun/config.env"
rm -f "$th/.airun/config.env.bak"

workdir="$th/work"
mkdir -p "$workdir"
echo "hello" > "$workdir/file.txt"
cd "$workdir"

PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" "ping" >/dev/null 2>&1 || true

log=$(cat "$DOCKER_SHIM_LOG")
assert_contains "$log" "create --name airun-snap-" "snapshot path names container airun-snap-*"
assert_contains "$log" "cp ${workdir}/. airun-snap-" "docker cp copies workspace into container"
assert_contains "$log" "start -a airun-snap-"       "docker start -a launches the container"
assert_contains "$log" "rm airun-snap-"             "snapshot cleanup removes the container"
# Snapshot mode must NOT bind-mount the workspace via -v.
assert_not_contains "$log" "-v ${workdir}:/workspace" "no bind mount in snapshot mode"

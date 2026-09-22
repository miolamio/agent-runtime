#!/usr/bin/env bash
# Explicit updates carry only preparation inputs and propagate container failure.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"
mk_test_profile "$th" reviewer 'settings: {agent: code-reviewer}
components: {agents: [development-tools/code-reviewer]}'

out=$(PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" profile update reviewer 2>&1)
assert_contains "$out" 'updated profile=reviewer' 'successful update is reported'
log=$(cat "$DOCKER_SHIM_LOG")
assert_contains "$log" 'run --rm --name airun-profile-update-' 'update uses a preparation container'
assert_contains "$log" '/run/airun/profile.json:ro' 'update receives manifest'
assert_contains "$log" 'airun-components-cache:/var/lib/airun/components' 'update uses retained artifact cache'
assert_not_contains "$log" ':/workspace' 'update attaches no workspace'
assert_not_contains "$log" ':/var/lib/airun/state' 'update attaches no historical state'
assert_not_contains "$log" ':/home/claude/.claude' 'update attaches no active configuration'
assert_not_contains "$log" ' claude -p ' 'update launches no agent'
[[ ! -d "$th/.airun/runs" ]] || die "preparation-only update recorded an agent session"

python3 - "$DOCKER_SHIM_CAPTURE" <<'PY'
import json, pathlib, sys
capture = pathlib.Path(sys.argv[1])
manifest = json.loads((capture / "profile.json").read_text())
assert manifest["profile_key"] == "reviewer"
assert manifest["settings"]["agent"] == "code-reviewer"
env = dict(line.split("=", 1) for line in (capture / "env-file.env").read_text().splitlines())
assert env == {
    "AIRUN_PROFILE_MANIFEST": "/run/airun/profile.json",
    "AIRUN_PROFILE_ACTION": "update",
}, "update received unrelated environment or provider credentials"
PY

set +e
out=$(PATH="$th/bin:$PATH" HOME="$th" DOCKER_SHIM_RUN_EXIT_CODE=47 \
    "$AIRUN_BIN" profile update reviewer 2>&1)
ec=$?
set -e
assert_exit_code 1 "$ec" 'preparation failure reaches CLI exit status'
assert_contains "$out" 'update profile "reviewer"' 'failure identifies addressed profile'
assert_contains "$out" 'exit status 47' 'underlying preparation exit is retained'
assert_not_contains "$out" 'updated profile=' 'failed update does not announce success'

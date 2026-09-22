#!/usr/bin/env bash
# Each workspace lifecycle receives the same role, settings, profile and model.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/home.sh"

for mode in bind snapshot shell-bind shell-snapshot export-bind export-snapshot; do
    th=$(mk_test_home)
    on_exit "rm -rf '$th'"
    install_docker_shim "$th"
    work="$th/workspace"
    mkdir -p "$work"
    printf 'host workspace sentinel\n' > "$work/sentinel.txt"
    mk_test_profile "$th" reviewer 'provider: kimi
settings: {agent: code-reviewer, effortLevel: high}
components: {agents: [development-tools/code-reviewer]}'
    cat >> "$th/.airun/config.env" <<'CONFIG'
KIMI_API_KEY=synthetic-kimi-key
KIMI_BASE_URL=http://127.0.0.1:1
KIMI_MODEL=profile-default-model
CONFIG
    runtime_mode=bind
    [[ "$mode" == *snapshot* ]] && runtime_mode=snapshot
    printf 'ARUN_MODE=%s\n' "$runtime_mode" >> "$th/.airun/config.env"
    args=(--profile reviewer --model explicit-model)
    case "$mode" in
        shell-*) args=(shell "${args[@]}" --mount "$work") ;;
        export-*) args+=(--output "$th/export" ping) ;;
        *) args+=(ping) ;;
    esac
    profile_before=$(cksum < "$th/.airun/profiles/reviewer.yaml")
    (
        cd "$work"
        PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" "${args[@]}"
    ) >"$th/output.log" 2>&1

    python3 - "$DOCKER_SHIM_CAPTURE" <<'PY'
import json, pathlib, sys
capture = pathlib.Path(sys.argv[1])
manifest = json.loads((capture / "profile.json").read_text())
assert manifest["version"] == 1
assert manifest["profile_key"] == "reviewer"
assert manifest["settings"] == {"agent": "code-reviewer", "effortLevel": "high"}
assert manifest["components"]["agents"] == [{"id": "development-tools/code-reviewer"}]
env = dict(line.split("=", 1) for line in (capture / "env-file.env").read_text().splitlines())
assert env["AIRUN_PROFILE_MANIFEST"] == "/run/airun/profile.json"
assert env["ANTHROPIC_AUTH_TOKEN"] == "synthetic-kimi-key", "profile provider was not selected"
for tier in ("SONNET", "OPUS", "HAIKU"):
    assert env["ANTHROPIC_DEFAULT_" + tier + "_MODEL"] == "explicit-model", "CLI model was lost"
PY
    log=$(cat "$DOCKER_SHIM_LOG")
    assert_contains "$log" '/run/airun/profile.json:ro' "$mode mounts immutable profile input"
    if [[ "$runtime_mode" == snapshot ]]; then
        assert_contains "$log" "cp $work/. " "$mode copies workspace into container"
        assert_not_contains "$log" "$work:/workspace" "$mode has no workspace bind"
    else
        assert_contains "$log" "$work:/workspace" "$mode binds selected workspace"
    fi
    if [[ "$mode" == shell-* ]]; then
        assert_not_contains "$log" ' claude -p ' "$mode leaves interactive startup to entrypoint"
        assert_contains "$log" '-it' "$mode remains interactive"
    else
        assert_contains "$log" 'claude -p ping --dangerously-skip-permissions' "$mode retains Claude headless flag"
    fi
    if [[ "$mode" == export-* ]]; then
        assert_contains "$log" ":/workspace/. $th/export" "$mode exports results"
    fi
    assert_eq "$profile_before" "$(cksum < "$th/.airun/profiles/reviewer.yaml")" "$mode leaves host profile unchanged"
    assert_eq 'host workspace sentinel' "$(cat "$work/sentinel.txt")" "$mode leaves host input unchanged"
    [[ ! -e "$work/.claude" ]] || die "$mode created configuration in the host workspace"
done

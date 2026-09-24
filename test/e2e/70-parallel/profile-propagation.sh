#!/usr/bin/env bash
# Both parallel workers receive the selected role, settings, model and cache.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"
mk_test_profile "$th" reviewer 'provider: kimi
settings: {agent: code-reviewer, effortLevel: high}
components: {agents: [development-tools/code-reviewer]}'
cat >> "$th/.airun/config.env" <<'CONFIG'
KIMI_API_KEY=synthetic-parallel-key
KIMI_BASE_URL=http://127.0.0.1:1
KIMI_MODEL=profile-default-model
CONFIG

PATH="$th/bin:$PATH" HOME="$th" "$AIRUN_BIN" \
    --profile reviewer --model parallel-model --parallel \
    --agent 'alice:review first' --agent 'bob:review second' \
    >"$th/output.log" 2>&1

python3 - "$DOCKER_SHIM_CAPTURE" "$DOCKER_SHIM_LOG" <<'PY'
import json, pathlib, sys
capture, log = map(pathlib.Path, sys.argv[1:])
manifests = list(capture.glob("*/profile.json"))
assert len(manifests) == 2, "both parallel inputs must be captured separately"
assert len({path.parent.name for path in manifests}) == 2, "workers shared a container identity"
for path in manifests:
    manifest = json.loads(path.read_text())
    assert manifest["profile_key"] == "reviewer"
    assert manifest["settings"] == {"agent": "code-reviewer", "effortLevel": "high"}
    assert manifest["components"]["agents"] == [{"id": "development-tools/code-reviewer"}]
    env = dict(line.split("=", 1) for line in (path.parent / "env-file.env").read_text().splitlines())
    assert env["ANTHROPIC_AUTH_TOKEN"] == "synthetic-parallel-key", "worker lost profile provider"
    assert env["ANTHROPIC_DEFAULT_SONNET_MODEL"] == "parallel-model", "worker lost CLI model"
    assert env["AIRUN_PROFILE_MANIFEST"] == "/run/airun/profile.json"
launches = [line for line in log.read_text().splitlines() if line.startswith(("run ", "create "))]
assert len(launches) == 2, "parallel launch did not start exactly two workers"
for line in launches:
    assert "/run/airun/profile.json:ro" in line
    assert "airun-components-cache:/var/lib/airun/components" in line
    assert "airun-state-" not in line, "parallel worker restored shared session state"
assert any("claude -p review first " in line for line in launches)
assert any("claude -p review second " in line for line in launches)
PY

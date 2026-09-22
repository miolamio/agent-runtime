#!/usr/bin/env bash
# Two components share a target name while transport values remain private.
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/home.sh"

th=$(mk_test_home)
on_exit "rm -rf '$th'"
install_docker_shim "$th"
mk_test_profile "$th" reviewer 'settings:
  agent: code-reviewer
components:
  agents: [development-tools/code-reviewer]
  mcps:
    - id: test/first
      env: {TOKEN: AIRUN_E2E_FIRST_TOKEN}
    - id: test/second
      env: {TOKEN: AIRUN_E2E_SECOND_TOKEN}'

PATH="$th/bin:$PATH" HOME="$th" \
    AIRUN_E2E_FIRST_TOKEN=synthetic-first-canary \
    AIRUN_E2E_SECOND_TOKEN=synthetic-second-canary \
    AIRUN_E2E_UNREFERENCED_TOKEN=synthetic-unused-canary \
    "$AIRUN_BIN" --profile reviewer ping >"$th/output.log" 2>&1

python3 - "$DOCKER_SHIM_CAPTURE" "$DOCKER_SHIM_LOG" "$th" <<'PY'
import json, pathlib, stat, sys
capture, log, home = map(pathlib.Path, sys.argv[1:])
manifest_text = (capture / "profile.json").read_text()
manifest = json.loads(manifest_text)
assert manifest["version"] == 1
assert manifest["profile_key"] == "reviewer"
assert manifest["settings"]["agent"] == "code-reviewer"
assert manifest["native_plugins"] == []
assert all(isinstance(refs, list) for refs in manifest["components"].values())
refs = manifest["components"]["mcps"]
assert [ref["id"] for ref in refs] == ["test/first", "test/second"]
aliases = [ref["env"]["TOKEN"] for ref in refs]
assert len(set(aliases)) == 2
assert all(alias.startswith("AIRUN_COMPONENT_ENV_") for alias in aliases)
env_file = capture / "env-file.env"
entries = dict(line.split("=", 1) for line in env_file.read_text().splitlines())
assert entries[aliases[0]] == "synthetic-first-canary", "first child binding changed"
assert entries[aliases[1]] == "synthetic-second-canary", "second child binding changed"
assert "TOKEN" not in entries, "component target became a global variable"
assert entries["ANTHROPIC_AUTH_TOKEN"] == "e2e-placeholder-invalid-key", "provider credential was overwritten"
assert stat.S_IMODE(env_file.stat().st_mode) == 0o600, "environment file is not private"
assert "synthetic-unused-canary" not in env_file.read_text(), "unnamed host value was forwarded"
public = manifest_text + log.read_text() + (home / "output.log").read_text()
for forbidden in ("synthetic-first-canary", "synthetic-second-canary", "synthetic-unused-canary"):
    assert forbidden not in public, "credential appeared in public manifest or logs"
for host_name in ("AIRUN_E2E_FIRST_TOKEN", "AIRUN_E2E_SECOND_TOKEN"):
    assert host_name not in manifest_text, "source variable name entered manifest"
assert not list((home / ".airun" / "tmp").iterdir()), "temporary transport files survived launch"
PY

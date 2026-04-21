# airun e2e tests

Bash-driven end-to-end tests covering the airun CLI, Docker lifecycle,
profiles, state volumes, parallel agents, browser display, and proxy.

## Running

```sh
test/e2e/run-all.sh                     # everything that's safe offline
test/e2e/run-all.sh --group 00-prereq   # one group
test/e2e/run-all.sh --only cli/version  # single file by substring
test/e2e/run-all.sh --with-network      # include tests that hit real APIs
test/e2e/run-all.sh --include-non-glm   # include non-zai/glm-5.1 providers
test/e2e/run-all.sh --fast              # bail on first failure
```

Per-test logs land in `test/e2e/.logs/<group>/<test>.log` (gitignored);
TAP summary in stdout and in `.logs/summary.tap`.

## Writing a test

Each file is a self-contained bash script:

```bash
#!/usr/bin/env bash
# Short description (one sentence is enough).
source "${E2E_LIB}/harness.sh"
source "${E2E_LIB}/env.sh"
source "${E2E_LIB}/skip.sh"

skip_if_no_airun_config

out=$("$AIRUN_BIN" --version)
assert_contains "$out" "airun"
```

### Exit codes

- **0** — PASS
- **77** — SKIP (use the `skip "reason"` helper)
- **anything else** — FAIL (use `die "msg"` or let an assertion abort)

### Safe defaults

- `set -euo pipefail` is enabled by `harness.sh`.
- Register cleanups with `on_exit "command…"` — they run in reverse order on
  any exit path.

### Network gating

Tests that make real API calls must call `skip_unless_network` first. They
only run with `--with-network`, so normal dev loops don't burn provider
quotas.

### GLM-5.1 only

Container runs targeting providers other than `zai/glm-5.1` must call
`skip_unless_non_glm`. The project policy is to exercise containers only
against GLM-5.1 unless opted into explicitly with `--include-non-glm`.

## Groups

| Group | Focus |
|---|---|
| 00-prereq | docker daemon, image built, config present |
| 10-cli | `--version`, `--help`, exit codes, unknown flags |
| 20-keys | `airun keys list|add|remove|default` |
| 30-run-basic | `airun "prompt"` against GLM-5.1 (requires --with-network) |
| 40-profiles | default/dev/research/ceo/text mounts and env |
| 50-workspace | snapshot vs bind, export dir, missing workspace |
| 60-state | volume creation, persistence, --no-state, per-profile volumes |
| 70-parallel | `--parallel --agent` success and NoState enforcement |
| 80-browser | `--browser vnc|cdp|both` port + env |
| 90-proxy | `airun proxy init|serve|user…` flows, SIGHUP reload, TLS |
| 95-history-monitor | `airun history`, `airun --status` |
| 99-errors | broken daemon, bad key, unknown provider |

## Environment knobs

| Var | Meaning |
|---|---|
| `AIRUN_TEST_ENV` | Path to alternate `config.env` (defaults to `~/.airun/config.env`) |
| `AIRUN_BIN` | Path to airun binary (defaults to `bin/airun` in the repo) |
| `E2E_WITH_NETWORK` | Set to `1` by `--with-network` flag |
| `E2E_INCLUDE_NON_GLM` | Set to `1` by `--include-non-glm` flag |

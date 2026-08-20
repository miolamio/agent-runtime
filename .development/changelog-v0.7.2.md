# airun v0.7.2 — Changelog

> Date: 2026-08-20
> Status: released

## Summary

Maintenance release. Moves the default Z.AI model to **glm-5.3**, refreshes
dependencies and CI actions, drops a dead build-time clone from the Docker
image, and fixes version/model drift in the docs and config templates.

## Changes

- **Default Z.AI model `glm-5.2` → `glm-5.3`.** z.ai already answers `glm-5.2`
  requests with the 5.3 generation — a plain Messages call for `glm-5.2` comes
  back with `"model":"glm-5.3"` — so the old label was cosmetic. Direct
  `glm-5.3` access is verified on the Anthropic endpoint
  (`https://api.z.ai/api/anthropic`), both without a `thinking` block (plain
  text reply) and with one (`thinking` content blocks returned). GLM-5.3 keeps
  the 1M context / 128K output envelope and, per Z.AI, is substantially
  stronger on long-horizon coding work.

  Caveat worth knowing: Z.AI documents GLM-5.3 as *always* reasoning, with
  effort levels `low`/`high`/`max` and no way to disable it. On a GLM Coding
  Plan key a request without `thinking` can be rejected with error 1210
  (*"This model always engages in thinking and cannot be disabled"*). Standard
  API keys are unaffected — plain requests succeed. Users who hit 1210 should
  pin `ZAI_MODEL=glm-4.7` in `~/.airun/config.env`.

  The bump covers the `internal/config` default, `airun init`, the
  `internal/keys` provider default, the proxy connect default-model preference,
  CLI help text, e2e assertions and the GLM-only test policy, README +
  CLAUDE.md, the `airun` skill docs, the architecture diagram, and the
  `connect-proxy` / `test-proxy` scripts. `glm-4.7` and the haiku tier
  (`GLM-4.5-Air`) are unchanged.

- **deps: `golang.org/x/crypto` v0.54.0 → v0.55.0.**

- **ci: refresh pinned actions.** `actions/checkout` v4 → v7,
  `actions/setup-go` v5 → v7, `golangci/golangci-lint-action` v6 → v9, and the
  pinned `golangci-lint` v2.11.4 → v2.13.0. These pins had drifted several
  major versions behind.

- **docker: drop the dead `anthropic-agent-skills` build-time clone.** Since
  v0.7.1 the entrypoint registers that marketplace from GitHub at runtime
  (`claude plugin marketplace add anthropics/skills`), because Claude Code
  ≥2.1.x reserves the name and rejects local clone paths. The clone
  `seed-plugins.sh` still performed was never read by the CLI, so it was pure
  image weight. `aas_sha` is gone from `.seed-metadata.json` (nothing consumed
  it) and `PLUGINS_BUST_CACHE` moves 2 → 3 to force the seed layer to re-run.
  Note this did not shrink the image on net: dropping the clone was more than
  offset by the newer Claude Code and Node that came with the same rebuild
  (2.58 GB → 2.64 GB).

- **docs/config drift.** `CLAUDE.md` still described the CLI as v0.7.0;
  `configs/airun.env.example` shipped `ZAI_MODEL=glm-4.7`, contradicting the
  code default; `configs/router/config.json.example` listed only `glm-4.7` for
  the zai provider. All three now match reality.

## Verification

- `go build` / `go vet` / `go test -race ./...` green — 107 tests, 12 packages.
- Offline e2e (`test/e2e/run-all.sh --no-build`): 41 pass / 0 fail / 1 skip
  (the skip is network-gated).
- Live GLM-5.3 probes against `https://api.z.ai/api/anthropic`: plain request
  → HTTP 200 `"model":"glm-5.3"`; `thinking`-enabled request → HTTP 200 with
  `thinking` content blocks; bogus model → `1214 modelCode: does not exist`
  (control).
- Image rebuilt from scratch (`--no-cache`, busted `CLAUDE_BUST_CACHE`):
  Claude Code 2.1.207 → **2.1.237**, Node 24.18.0 → **24.19.0**;
  `marketplaces/` now holds only `claude-plugins-official` and
  `miolamio-agent-skills`, and `.seed-metadata.json` has no `aas_sha`.
- Live `airun --no-state -p dev "Reply with exactly: OK"` on the rebuilt image:
  `provider=zai model=glm-5.3`, marketplaces + base + profile plugins install
  with **zero warnings** — including `example-skills@anthropic-agent-skills`,
  which proves the runtime GitHub registration fully replaces the removed
  build-time clone. `OK`, exit 0, 26.1 s.

## Known issues (not fixed here)

- **GitHub Actions CI has been red since before v0.7.0 for non-code reasons.**
  Every job dies in ~3 s with `steps_count: 0` — the runner never starts, which
  points at billing/quota rather than the build. The local equivalents of both
  CI jobs pass.

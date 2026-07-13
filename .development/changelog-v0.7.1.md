# airun v0.7.1 — Changelog

> Date: 2026-07-13
> Status: ready to tag

## Summary

Patch release. Bumps the default Z.AI model to **glm-5.2** and fixes plugin
marketplace registration that broke against Claude Code ≥2.1.x.

## Changes

- **Default Z.AI model `glm-5.1` → `glm-5.2`.** z.ai already serves `glm-5.1`
  requests with the 5.2 generation, and direct `glm-5.2` access is verified
  (Anthropic endpoint `https://api.z.ai/api/anthropic`, HTTP 200,
  `model: glm-5.2`). The bump covers `internal/config` default, the
  `airun init` setup template, `internal/keys` provider default, the proxy
  connect default-model preference, CLI help text, e2e assertions + the
  GLM-only test policy, README + CLAUDE.md, the `airun` skill docs, the
  architecture diagram label, and the `connect-proxy` / `test-proxy`
  scripts. The haiku tier (`GLM-4.5-Air`) and `glm-4.7` are unchanged.

- **fix(entrypoint): register `anthropic-agent-skills` from its GitHub source.**
  Claude Code ≥2.1.x reserves the `anthropic-agent-skills` marketplace name and
  only accepts it from a GitHub source in the `anthropics` org — registering the
  build-time local clone now fails with *"The name 'anthropic-agent-skills' is
  reserved …"*, which cascaded into `example-skills@anthropic-agent-skills`
  install failures on profiles like `dev`/`research`/`ceo`. The entrypoint now
  runs `claude plugin marketplace add anthropics/skills`, matching how
  `claude-plugins-official` is already registered.

## Verification

- `go build` / `go vet` / `go test -race ./...` green — 107 tests, 12 packages.
- Offline e2e (`test/e2e/run-all.sh`): 41 pass / 0 fail / 1 skip (network-gated).
- Live `airun --no-state -p dev "Reply with exactly: OK"` on the rebuilt image:
  `provider=zai model=glm-5.2`, marketplaces + base + profile plugins all install
  with **zero warnings**, `OK`, exit 0.

## Known follow-up

- `docker/seed-plugins.sh` still clones `anthropics/skills` at build time, but the
  entrypoint now re-registers it from GitHub at first run, so that build-time clone
  is dead weight — a candidate for removal to shrink the image.

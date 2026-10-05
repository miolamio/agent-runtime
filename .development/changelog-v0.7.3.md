# airun v0.7.3 — Changelog

> Date: 2026-10-05
> Status: released

## Summary

Maintenance release. Moves the toolchain to Go 1.27, the Docker image to
Debian 13 (trixie), and refreshes the default model IDs of every provider
except Z.AI's flagship (`glm-5.3` is still current).

## Changes

- **Go 1.25 → 1.27.** `go` directive `1.25.0` → `1.27.0` (1.25 is out of
  support). CI reads the version from `go.mod`.
- **deps: `golang.org/x/crypto` v0.55.0 → v0.57.0.**
- **ci: `golangci-lint` v2.13.0 → v2.14.0.** Actions were already on their
  latest majors (checkout v7, setup-go v7, golangci-lint-action v9).
- **docker: `buildpack-deps:bookworm-scm` → `trixie-scm`.** Debian 13 renamed
  four Chromium/Playwright runtime libraries in the t64 transition:
  `libatk1.0-0t64`, `libatk-bridge2.0-0t64`, `libcups2t64`, `libasound2t64`.
  Node stays on 24 LTS (NodeSource `nodistro` repo works on trixie).
- **docker: git-delta 0.19.2 → 0.20.1.**
- **Default model IDs.** Each new ID was checked against the provider's
  `/v1/models` list and, where the account allowed it, a live Messages call:
  - Z.AI haiku tier: `GLM-4.5-Air` → `glm-5.3-flash` (live 200).
  - MiniMax: `MiniMax-M2.7` → `MiniMax-M3` (listed in `/v1/models`; live call
    blocked by account balance).
  - Kimi: `kimi-k2.5` → `kimi-for-coding` (live 200). The Kimi coding
    endpoint echoes back *any* model string — including nonexistent ones —
    so `kimi-k2.5` was silently served by the endpoint default. Only IDs from
    `/v1/models` are trustworthy there.
  - Anthropic: `claude-sonnet-4-6-20250514` → `claude-sonnet-5-5`. The old ID
    combined the Sonnet 4.6 name with the Sonnet 4 date stamp and is not a
    valid model.

  Explicit `*_MODEL` values in an existing `~/.airun/config.env` still
  override these defaults; users who pinned `KIMI_MODEL=kimi-k2.5` should
  drop or update that line.

## Verification

- `go build`, `go vet`, `go test -race ./...` green; golangci-lint v2.14.0: 0 issues.
- Offline e2e: 43 pass / 0 fail / 1 skip.
- Image build (trixie, arm64): 2.28 GB; Debian 13, node v24.21.0,
  Claude Code 2.1.289, delta 0.20.1. Headless Chromium via Playwright launches.
- Real one-shot run `--provider zai`: exit 0, model `glm-5.3`, no marketplace
  or plugin warnings. Claude Code 2.1.289 logs a cosmetic
  `unrecognized_model glm-5.3[1m]` line (suffix added by Claude Code itself).

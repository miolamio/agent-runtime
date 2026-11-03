# airun v0.7.4 — Changelog

> Date: 2026-11-03
> Status: released

## Summary

Maintenance release: Go toolchain patch, Node 26 LTS in the image, pinned
image tooling, and Kimi's default moved to K3.1 Pro.

## Changes

- **Go toolchain go1.27.3.** `go.mod` keeps `go 1.27.0` as the minimum and adds
  `toolchain go1.27.3`; `actions/setup-go@v7` honours it, so CI builds with the
  patched toolchain.
- **deps: `golang.org/x/crypto` v0.57.0 → v0.58.0.**
- **ci: `golangci-lint` v2.14.0 → v2.15.0.** No new default linters; 0 issues.
- **docker: Node 24 → 26.** Node 26 "Lithium" became LTS on 2026-10-28.
- **docker: pinned previously floating tools.** `@playwright/cli@0.1.23`
  (`PLAYWRIGHT_CLI_VERSION`), noVNC v1.7.0 and websockify v0.13.0 (were cloned
  at HEAD).
- **Kimi default `kimi-for-coding` → `k3.1-pro`.** `kimi-for-coding` serves
  K2.8 Preview; `k3.1-pro` (K3.1 Pro, released 2026-10-23) is listed in the
  coding endpoint's `/v1/models`. It always reasons and rejects disabled
  thinking — airun never disables it. Requires a Kimi Code Moderato/Plus plan
  or higher; on lower plans set `KIMI_MODEL=kimi-for-coding`.
- **docs:** `airun` skill and architecture diagram no longer claim Node 22 /
  Debian Bookworm.

Unchanged: Z.AI `glm-5.3` / `glm-5.3-flash`, Anthropic `claude-sonnet-5-5`
(still current). MiniMax stays on `MiniMax-M3`: `MiniMax-M3.1-Flash` is listed
in `/v1/models` but could not be verified (account balance 402). An
unconfirmed tracker reports M3 retiring 2026-11-18 — re-check before then.

## Verification

- `go build`, `go vet`, `go test -race ./...` (146 pass); golangci-lint v2.15.0: 0 issues.
- Offline e2e: 43 pass / 0 fail / 1 skip.
- Image (`--no-cache`, arm64): 2.43 GB; Debian 13, Node 26.13.0, npm 11.20.0,
  Claude Code 2.1.317, delta 0.20.1, playwright-cli 0.1.23, Chromium 157.
  Headless Chromium renders under Node 26.
- Real runs: `--provider zai` → OK, exit 0; `--provider kimi --model k3.1-pro`
  with a shell tool call → correct output, exit 0.

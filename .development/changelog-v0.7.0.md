# airun v0.7.0 — Changelog

> Date: 2026-07-13
> Status: ready to tag (all commits on `origin/main`)

## Summary

**Breaking release.** Skills stop being a filesystem concern. The `skills:` field in
profile YAML is gone and the `~/.airun/skills/` host bind-mount is removed entirely —
skills now ship through the plugin/marketplace system, either baked into the image at
build time or activated per-profile on container start.

The proxy subsystem also matured: the `students` package was renamed to `users` (with
transparent `students.json → users.json` migration), tokens moved from SHA-256 to bcrypt
with on-the-fly upgrade, `proxy.yaml` hot-reloads on SIGHUP, and the user store now writes
atomically to survive a crash mid-save. First CI pipeline (`go build`/`vet`/`test -race` +
`golangci-lint`) landed in this cycle as well.

## Breaking changes

1. **`skills:` removed from profile schema.** Listing the field still loads the profile
   (legacy YAML is not rejected), but the runner emits a one-shot stderr warning and
   ignores the contents. Migrate to `plugins:` with a `name@marketplace` reference.
2. **`~/.airun/skills/` bind-mount deleted.** Custom skills under that directory must be
   repackaged as a plugin and published to a marketplace (private or public), then
   referenced from a profile.
3. **`Config.SkillsDir` removed** from `internal/config`; the `~/airun-skills → ~/.airun/skills`
   migration shim is gone.
4. **`examples/skills/` directory removed.** The one skill that lived there
   (`claude-code-best-practices`) was not published to any marketplace.
5. **Proxy `students` package renamed to `users`** (`internal/proxy/users`). On-disk store
   moved `~/.airun/students.json → ~/.airun/users.json`; legacy `~/.airun/students.json` and
   `~/students.json` are auto-migrated on first read, so no operator action is required.

## Features

- **SIGHUP reloads `proxy.yaml`.** `Handler.config` is now an `atomic.Pointer[ProxyConfig]`
  and `RateLimiter` grows `SetRPM`; providers, rate limit, and user-agent hot-swap without
  dropping in-flight requests. Listen address and TLS cert/key cannot be hot-swapped — the
  reload warns and keeps the current value (restart required). The reload also re-reads
  `users.json`.
- **bcrypt token hashing with transparent SHA-256 upgrade.** New users always get bcrypt
  (`HashTokenBcrypt`). Pre-v0.6.0 SHA-256 hashes still verify and are transparently upgraded
  to bcrypt on the auth path (asynchronous persist; auth latency unaffected).
- **Atomic `users.json` writes.** The store now writes to a temp file and renames into
  place, so a crash mid-save can no longer truncate or corrupt the user database.
- **Profile plugin activation via `AIRUN_PLUGINS`.** Profile-declared plugins are filtered
  against the image's base set (`filterBasePlugins`) and the remainder is passed to the
  container as a comma-separated `AIRUN_PLUGINS`; the entrypoint activates them into
  `installed_plugins.json`.
- **anthropic-agent-skills marketplace baked into the image.** `seed-plugins.sh` clones
  `anthropics/skills` alongside the existing marketplaces; the entrypoint registers it via
  the `claude` CLI (settings mount is RW so plugin metadata persists). Profiles can reference
  bundle plugins like `example-skills@anthropic-agent-skills` and
  `document-skills@anthropic-agent-skills` (xlsx, docx, pptx, pdf).

## Fixes / cleanup

- **Entrypoint marketplace registration hardened** — marketplaces are now registered through
  the `claude` CLI with a read-write settings mount so plugin metadata survives, instead of
  hand-editing JSON.
- **errcheck cleanup** — previously-ignored error returns surfaced by `errcheck` are now
  handled or explicitly logged.
- `setup.sh` creates the consolidated `~/.airun/{profiles,agents,commands}` paths instead of
  the legacy `~/airun-*` directories.
- Legacy `skills:` profiles get a clear one-shot warning pointing operators at the plugin
  mechanism.

## CI

- **First CI pipeline** (`.github/workflows/ci.yml`): `go build`, `go vet`,
  `go test -race ./...`, `bash test/e2e/run-all.sh --no-build`, and `golangci-lint`.
  Lint config in `.golangci.yml` (`errcheck`, `govet`, `ineffassign`, `staticcheck`,
  `unused`). CI reads the Go version straight from `go.mod` (`go-version-file`).

## Dependencies / build

- `golang.org/x/crypto` **v0.50.0 → v0.54.0** (direct dep; home of the bcrypt token hashing).
- Docker image: Node.js LTS **22 → 24**, git-delta **0.18.2 → 0.19.2**.
- `go build`, `go vet`, and `go test -race ./...` green (107 tests across 12 packages).

## Tests

- New `internal/proxy/reload_test.go` covers atomic config swap and RPM hot-swap;
  `TestRateLimiterSetRPM` covers 0→n→0→n transitions.
- `internal/proxy/users/users_concurrent_test.go` exercises parallel auth + bcrypt upgrade
  under `-race`.
- e2e `40-profiles/profile-skills-mounted.sh` replaced by `profile-skills-deprecated.sh`,
  which asserts the deprecation warning and confirms no mount happens.

## Shipped profiles (`configs/profiles/`)

| Profile  | Plugins |
|----------|---------|
| default  | — (base set only) |
| dev      | `security-guidance`, `playwright`, `frontend-design` (all `@claude-plugins-official`), `example-skills@anthropic-agent-skills` |
| text     | — (ru-editor, en-ru-translator-adv, krrkt are baked into the image) |
| research | `playwright@claude-plugins-official`, `example-skills@anthropic-agent-skills`, `document-skills@anthropic-agent-skills` |
| ceo      | `example-skills@anthropic-agent-skills` |

## Commits (on top of v0.6.1)

```
beec7d8 feat(proxy): atomic users.json write to survive crash mid-save
3f0e5e4 refactor(proxy)!: rename students package to users + migrate JSON store
d6867e3 fix(entrypoint): register marketplaces via claude CLI, RW settings mount
9067508 feat!: skills delivered via plugins only (breaking)
30972e0 feat(proxy): SIGHUP reloads proxy.yaml (providers, rpm, user_agent)
2d80449 feat(runner): profile plugin activation via AIRUN_PLUGINS env var
8c8bb36 feat(proxy): bcrypt token hashing with transparent SHA-256 upgrade
3be226e ci: add golangci-lint config and GitHub Actions workflow
d4eb500 fix: surface previously-ignored errors flagged by errcheck
```

Plus the release commit bumping `cmd/airun/main.go` to `0.7.0`, refreshing this changelog,
and the dependency/Docker version bumps above.

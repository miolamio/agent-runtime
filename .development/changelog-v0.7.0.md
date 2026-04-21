# airun v0.7.0 — Changelog

> Date: 2026-04-21
> Status: draft — pending commit + tag

## Summary

**Breaking release.** The `skills:` field in profile YAML is gone and the `~/.airun/skills/` host bind-mount is removed entirely. Skills now ship through the plugin/marketplace system — either baked into the image at build time (miolamio-agent-skills) or installed on container start via `claude plugin install` from a known marketplace.

Also in this release: full SIGHUP reload for `proxy.yaml` (providers, rpm, user_agent), not just `students.json`; `anthropic-agent-skills` marketplace added to the image so document/example bundles are reachable without extra configuration.

## Breaking changes

1. **`skills:` removed from profile schema.** Listing the field still loads the profile (legacy yaml is not rejected), but the runner emits a warning on stderr and ignores the contents. Migrate to `plugins:` with a `name@marketplace` reference.
2. **`~/.airun/skills/` bind-mount deleted.** Any user who had custom skills under that directory must repackage them as a plugin and publish to a marketplace (private or public), then reference it from a profile.
3. **`Config.SkillsDir` removed** from `internal/config`. The `~/airun-skills → ~/.airun/skills` migration shim is also gone.
4. **`examples/skills/` directory removed.** The one skill that lived there (`claude-code-best-practices`) was not published to any marketplace.

## Features

- **SIGHUP reloads proxy.yaml.** `Handler.config` is now an `atomic.Pointer[ProxyConfig]` and `RateLimiter` grows `SetRPM`; providers, rate limit, and user-agent hot-swap without drop. Listen address and TLS cert/key cannot be hot-swapped — change warns and keeps the current value (restart required).
- **anthropic-agent-skills marketplace baked into image.** `seed-plugins.sh` clones `anthropics/skills` alongside the existing marketplaces; `entrypoint.sh` registers it in `known_marketplaces.json`. Profiles can now reference bundle plugins like `example-skills@anthropic-agent-skills` (webapp-testing, doc-coauthoring, internal-comms, …) and `document-skills@anthropic-agent-skills` (xlsx, docx, pptx, pdf).

## Fixes / cleanup

- `setup.sh` updated to create `~/.airun/{profiles,agents,commands}` (consolidated paths) instead of the legacy `~/airun-*` directories.
- Legacy `skills:` profiles get a clear one-shot warning pointing operators at the new mechanism.

## Tests

- New `internal/proxy/reload_test.go` covers atomic config swap and RPM hot-swap.
- New `TestRateLimiterSetRPM` covering 0→n→0→n transitions.
- e2e `40-profiles/profile-skills-mounted.sh` removed; replaced by `profile-skills-deprecated.sh` that asserts the warning and confirms no mount still happens.
- Full race suite green, `golangci-lint run ./...` 0 issues.

## Shipped profiles (`configs/profiles/`)

| Profile  | Plugins |
|----------|---------|
| default  | — (base set only) |
| dev      | `security-guidance`, `playwright`, `frontend-design` (all `@claude-plugins-official`), `example-skills@anthropic-agent-skills` |
| text     | — (ru-editor, en-ru-translator-adv, krrkt are baked into the image) |
| research | `playwright@claude-plugins-official`, `example-skills@anthropic-agent-skills`, `document-skills@anthropic-agent-skills` |
| ceo      | `example-skills@anthropic-agent-skills` |

## Commits (on top of v0.6.1)

- `30972e0` — `feat(proxy): SIGHUP reloads proxy.yaml (providers, rpm, user_agent)`
- `2d80449` — `feat(runner): profile plugin activation via AIRUN_PLUGINS env var` (pre-0.7.0 phase 3 work)
- `8c8bb36` — `feat(proxy): bcrypt token hashing with transparent SHA-256 upgrade` (pre-0.7.0 phase 3 work)
- (forthcoming) — skills removal + marketplace seed + profile rewrite + version bump

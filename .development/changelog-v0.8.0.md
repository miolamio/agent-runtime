# airun v0.8.0 — Changelog

> Date: 2026-11-04
> Status: released

## Summary

**Breaking default change.** A container now enables no plugins and no extra
settings by default — `skill-creator` is the only base plugin. Everything else
is opt-in per profile via `plugins:`.

## Changes

- **Base plugins: superpowers + context7 + skill-creator → skill-creator only.**
  `seed-plugins.sh` seeds only skill-creator; the entrypoint installs only it;
  `runner.basePlugins` is `[skill-creator]`. context7's MCP server is no longer
  started by default.
- **miolamio/agent-skills no longer copied into `~/.claude/skills`.** Its
  marketplace (`miolamio-agent-skills`) stays registered so profiles can opt
  in. Marketplace registrations themselves enable nothing.
- **`configs/profiles/default.yaml` is empty:** no `provider:` (the flag /
  `ARUN_PROVIDER` applies) and no `settings:`.
- **Profiles that relied on implicit base plugins now list them:** `dev` and
  `research` add `superpowers@claude-plugins-official` and
  `context7@claude-plugins-official`; `text` lists `ru-editor`,
  `en-ru-translator-adv`, `krrkt` `@miolamio-agent-skills`.
- **One-time state-volume migration.** Existing `airun-claude-state` volumes
  kept the old defaults (installed by the old entrypoint, seeded from the old
  image). On first start of the new image, volumes that carry the old sentinel
  but no `~/.claude/.airun-defaults-v2` marker get superpowers and context7
  uninstalled and the six former miolamio skill dirs removed — except anything
  named in the current `AIRUN_PLUGINS`. No other skill dirs are touched. Runs
  once per volume.
- `PLUGINS_BUST_CACHE` 3 → 4.
- docs: README, CLAUDE.md, `airun` skill; `cli-reference.md` also dropped
  stale pre-0.7 paths (`~/.airun.env`, `~/airun-profiles/`, `~/airun-skills/`).

## Upgrade notes

- Profiles in `~/.airun/profiles/` are copies made by `airun init` and are not
  overwritten. Re-copy the templates from `configs/profiles/` to get the
  explicit plugin lists; old copies may reference
  `superpowers@superpowers-marketplace`, which is no longer registered.
- Known upstream issue: `ru-editor` installs but does not load — its
  `SKILL.md` in miolamio/agent-skills has an HTML comment before the YAML
  frontmatter.

## Verification

- `go test -race ./...`, golangci-lint v2.15.0 (0 issues), offline e2e 43/0/1.
- Fresh volume, no profile: only `skill-creator` plus Claude Code built-ins.
- `-p dev`, `-p text`: listed plugins install without warnings.
- Migration tested on scratch volumes (old-style, with `AIRUN_PLUGINS`
  exemptions, fresh, second run is a no-op) and on a real upgraded volume.

# Profile-Based Plugin Activation

## Problem

Currently, `installed_plugins.json` in the entrypoint hardcodes context7 + skill-creator + superpowers for ALL profiles. Profile-specific plugins are delivered via a mounted `post-init.sh` script generated on the host — this adds temp file management in the runner and couples the host to the container's plugin install mechanism.

## Decision

Hybrid approach: base plugins pre-installed at build time (fast start), profile-specific plugins installed at container startup via `claude plugin install` (always fresh). Runner passes plugin list as a single env var `AIRUN_PLUGINS` inside the existing env file — no CLI bloat, no temp files.

## Design

### Data Flow

```
Runner (Go)                              Container
───────────                              ─────────
profile.Load()
  ↓
plugins: [superpowers@sp, context7@cpo,
          playwright-cli@mio]
  ↓
filter out base plugins
  ↓
AIRUN_PLUGINS=playwright-cli@mio         entrypoint.sh
  (written into env file)                  ↓
  ↓                                      1. Generate installed_plugins.json
docker run --env-file ...                    with base 3 (always)
                                         2. If AIRUN_PLUGINS non-empty:
                                            parse and run claude plugin install
                                            for each entry
                                         3. exec claude
```

### Base Plugins (always active)

Installed at image build time by `seed-plugins.sh`, activated by entrypoint into `installed_plugins.json`:

- `context7@claude-plugins-official`
- `skill-creator@claude-plugins-official`
- `superpowers` (from obra/superpowers)

The miolamio-agent-skills marketplace is also cloned at build time and skills are copied to `~/.claude/skills/`.

### AIRUN_PLUGINS Format

Comma-separated list of `name@marketplace` entries:

```
AIRUN_PLUGINS=playwright-cli@miolamio-agent-skills,security-guidance@claude-plugins-official
```

- Empty or unset means no extra plugins (only base)
- Base plugins are filtered out by the runner even if listed in the profile

### Profile Matrix

| Profile  | Base (installed_plugins.json)           | AIRUN_PLUGINS                                                |
|----------|----------------------------------------|--------------------------------------------------------------|
| default  | superpowers, context7, skill-creator   | _(empty)_                                                    |
| dev      | superpowers, context7, skill-creator   | security-guidance@cpo, playwright-cli@mio                    |
| research | superpowers, context7, skill-creator   | notebooklm-mcp@mio, playwright-cli@mio                      |
| ceo      | superpowers, context7, skill-creator   | _(empty)_                                                    |
| text     | superpowers, context7, skill-creator   | ru-editor@mio, en-ru-translator-adv@mio                     |

## Changes

### 1. `internal/runner/runner.go`

- Remove `generatePluginScript()` function
- Remove temp file creation and mount of `post-init.sh`
- In `profileMounts()` (or wherever env vars are assembled): extract profile plugins, filter out base set (`superpowers`, `context7`, `skill-creator`), join remainder as comma-separated `AIRUN_PLUGINS=...` and add to the env file
- Clean up deferred temp file removal for plugin script

### 2. `docker/entrypoint.sh`

- Existing seed-plugin section (generates `installed_plugins.json` with base 3) stays unchanged
- New section after seed: if `AIRUN_PLUGINS` is non-empty, split by comma, for each entry parse `name@marketplace` and run `claude plugin install {name} --marketplace {marketplace} 2>/dev/null || true`
- Remove the existing `post-init.sh` execution block (no longer mounted)

### 3. No Changes

- `docker/seed-plugins.sh` — unchanged (base cache at build time)
- `docker/Dockerfile` — unchanged
- `configs/profiles/*.yaml` — unchanged (format already correct)
- `internal/profile/profile.go` — unchanged (already parses plugins field)

## Base Plugin Filter

Runner uses a hardcoded set to filter:

```go
var basePlugins = map[string]bool{
    "superpowers": true,
    "context7":    true,
    "skill-creator": true,
}
```

Plugin name is extracted by splitting on `@` and taking the first part.

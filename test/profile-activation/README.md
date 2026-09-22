# Real Claude profile activation acceptance

```sh
bash test/profile-activation/run.sh
# Optional: AIRUN_TEST_IMAGE=another-compatible-image bash test/profile-activation/run.sh
```

The default image is `agent-runtime:profiles-dev`, containing Claude Code
2.1.278 and the pinned component installer. The test binds current production
adapter/supervisor code read-only into a disposable container and runs as UID
1001 with `--network none`. A loopback HTTP server supplies synthetic Anthropic
responses; no host credentials, user state, named volumes, external model calls,
or changes to `agent-runtime:latest` are involved.

The test checks behavior of the actual Claude CLI:

- Production preparation runs the installed `claude-code-templates` 1.29.6
  binary for an agent and a skill. A test-only fetch preload supplies controlled
  GitHub responses; actual argument parsing, recursive downloads, source
  verification, and publication must preserve the nested resource bytes.
- A selected agent's unique body reaches the model request's system prompt.
- Three real stdio MCP children execute tools and verify different credentials
  bound to the same `TOKEN` target, returning only success markers.
- The third child begins as an `npx` reference. Production `provisionNpm` runs
  actual npm against a loopback registry and package tarball; Claude executes
  the retained binary. Then the registry is closed and npm's download cache
  is moved away. A warm session must execute the same binary with unchanged
  runtime inventory/receipt and no second npm invocation.
- The actual `Skill` tool expands catalog skill, command, and native plugin
  skill bodies into subsequent model requests.
- A native plugin installed from a local marketplace runs its session hook.
- A mod loaded from the adapter's skills directory executes a function hook.
- Removing the selection removes role, skills, commands, plugins, MCP tools and
  mod callbacks despite a warm cache; re-adding restores them without another
  catalog lookup or changed resolution generation.
- Two sequential sessions retain actual Claude transcripts across profile edits.
  Actual `--resume` restores an earlier session and sends its prior conversation.
- Two simultaneous sessions reach a response barrier together. Executed function
  hooks report different private config paths and imported history; both new
  transcripts survive completion.
- Synthetic credential values stay out of retained artifacts/receipts and model
  request content.

Real layers: `profile-start.mjs`, `component-adapter.mjs`, pinned installer
agent/skill operations, npm package provisioning and retained execution,
source inventory and artifact verification, locks/cache publication, native local
marketplace installation/validation, Claude's settings/plugin/agent loading,
MCP protocol, tool execution, function hooks, and history import/merge.

Fixture layers: source catalog inventory/bytes, controlled installer HTTP
responses, installer replacement for the remaining activation fixtures, npm
registry/package, minimal baseline, local MCP child, and predetermined model
responses. Predetermined responses test execution and configuration, not model
reasoning. Live catalog downloads, shipped baseline content, Go/CLI transport,
explicit remote updates, and upstream catalog compatibility are covered by
separate tests. Catalog plugins have no supported upstream entry and are not
invented for this test.

`installer-acceptance.mjs` leaves production `installCatalog` in place and
wraps only the command runner's environment with the fetch preload. Other
catalog activation fixtures retain their injected installer in `fixtures.mjs`.
The npm wrapper only supplies an isolated registry/cache environment; production
`provisionNpm`, actual npm and the installed MCP binary all run. Native
installation also remains production code. Function-hook syntax
follows Anthropic's [published plugin declarations](https://github.com/anthropics/claude-code/blob/main/mods/types/claude-code.d.ts).
All artifacts and captured requests stay inside the disposable container.

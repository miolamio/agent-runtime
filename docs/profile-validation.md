# Container agent profile validation

Validated on 2026-09-22 against the feature branch and a separately built
`agent-runtime:profiles-dev` image, with Claude Code 2.1.278 and the published
`claude-code-templates` npm package 1.29.6. The existing `agent-runtime:latest`
image, user credentials and state volumes were not changed.

## Automated checks

- `go test -race ./...`, `go vet ./...`, and golangci-lint 2.13.0 pass.
- `AIRUN_TEST_ENV=/dev/null bash test/e2e/run-all.sh`: 52 pass, one live provider
  test skipped. Profile coverage includes shell/headless, bind/snapshot/export,
  parallel inputs, update-only execution and error propagation.
- All 73 Linux non-root Node tests pass (56 adapter, 17 history). They cover
  schema handoff, artifact verification, profile transactions, MCP aliases,
  resource collisions and history merging.
- `bash test/profile-activation/run.sh` exercises the real CLI without external
  networking: main-role prompt, three MCP children with distinct credentials,
  skills/commands, native plugin skills/hooks, mod function hooks, removal and
  restoration, native transcript persistence/resume and simultaneous sessions.
  Its [README](../test/profile-activation/README.md) identifies each real and
  fixture layer. CI runs this acceptance after building the compatible image.
- The same acceptance invokes the actual pinned catalog installer with
  controlled upstream responses and verifies nested skill resources. It also
  provisions an MCP package using actual npm against a local registry, executes
  its tool, then repeats with the registry unavailable and npm's download cache
  removed. Retained bytes and receipts stay unchanged without reinstalling.
- `bash test/profile-entrypoint/run.sh` checks the production root entrypoint,
  fresh root-owned cache/state volumes, non-root Claude execution, actual
  reviewer system prompt, saved native transcripts and no-profile permission
  defaults. Its [README](../test/profile-entrypoint/README.md) describes the
  loopback model and cached-agent fixture. CI runs this test too.

Independent blind, edge-case and verification reviews produced 18 findings
(15 distinct issues). All were addressed with regression coverage, including
resource modes, invocation collisions, failed-transaction cleanup, update-only
validation, interpreter prerequisites, image defaults and history scheduling.
No review findings were deferred.

## Production catalog smoke

A preparation-only container, running as the image's non-root user, resolved
and verified this actual upstream selection into an isolated temporary cache:

| Kind | Reference |
| --- | --- |
| Agent | `development-tools/code-reviewer` |
| Skill | `ai-maestro/docs-search` |
| Command | `analysis/supply-chain-audit` |
| MCP | `integration/github-integration` |
| Mod | `productivity/npm-to-pnpm-rewriter` |
| Native plugin | `frontend-design@claude-plugins-official` |

The manifest selected `settings.agent: code-reviewer`, repeated the baseline
`context7` reference, and supplied a synthetic GitHub token through its transport
alias. The production image baseline, upstream installer, source inventories,
GitHub/native plugin download, npm MCP dependency provisioning and verification
all ran without fixture replacements. Preparation exited successfully. A second
container with the same selection/cache and `--network none` prepared successfully
and reached Claude 2.1.278, demonstrating retained reuse without downloads. This
smoke checks preparation; behavioral activation is covered separately above.

Two upstream compatibility details were verified and handled:

- Published installer 1.29.6 reports an older CLI version banner (1.29.4).
  Validation therefore reads the outer installed npm package metadata.
- Claude accepts command bodies with unquoted bracket/pipe argument hints that
  strict YAML rejects. Commands retain exact verified source bytes and use the
  native metadata loader; required agent/skill fields remain validated.

## Limits

No external model API or real GitHub MCP request was made. Remote service
availability and model judgment are outside these tests. Catalog plugins have no
supported installable entry. Unsupported native source formats, missing runtime
prerequisites and unavailable catalog selections fail before readiness rather
than publishing a partial environment.

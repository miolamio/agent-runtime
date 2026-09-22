# Container agent profile validation

Validated on 2026-09-22 against the feature branch and a separately built
`agent-runtime:profiles-dev` image, with Claude Code 2.1.278 and the published
`claude-code-templates` npm package 1.29.6. Automated acceptance uses isolated
volumes. The separately authorized live check below uses the installed CLI,
existing provider configuration and retained reviewer state.

## Automated checks

- `go test -race ./...`, `go vet ./...`, and golangci-lint 2.13.0 pass.
- `AIRUN_TEST_ENV=/dev/null bash test/e2e/run-all.sh`: 52 pass, one live provider
  test skipped. Profile coverage includes shell/headless, bind/snapshot/export,
  parallel inputs, update-only execution and error propagation.
- All 121 Linux non-root Node tests pass (104 adapter, 17 history).
  These cover schema handoff, artifact verification,
  profile transactions, MCP aliases, resource collisions and history merging.
- Complete equal plain skills/commands deduplicate across baseline, catalog and
  repository sources. Adapter regressions check every selected catalog receipt,
  nested command identities, byte/inventory/executable differences, supported
  internal symlinks, unsafe colliding links, unchanged source files and published
  generations, warm reuse and removal. Repository-owned equality cannot bypass
  retained-artifact verification or conceal a mod collision. Unrelated repository
  resources remain outside equivalence checks; updates defer workspace checks.
- Correction regressions also cover wrapper directory conflicts before omission,
  retained repository execute permissions (`0755` versus `0645`), three-source
  diagnostic provenance, FIFO rejection before reads and unrelated command
  sibling names. Baseline skill aliases, nested resource links and command file
  or directory aliases retain their dependencies privately when a target is
  omitted. Private dependencies preserve their own contained links; removing one
  invocation reached through a directory alias preserves its other commands.
  Empty directory dependencies survive final publication, and parent-directory
  links retain valid relative targets. Source snapshots include exact modes and
  directory entries without following links.
- `bash test/profile-activation/run.sh` exercises the real CLI without external
  networking: main-role prompt, three MCP children with distinct credentials,
  skills/commands, native plugin skills/hooks, mod function hooks, removal and
  restoration, native transcript persistence/resume and simultaneous sessions.
  Its [README](../test/profile-activation/README.md) identifies each real and
  fixture layer. CI runs this acceptance after building the compatible image.
- The native acceptance also expands identical baseline/catalog/repository skill
  and command bodies through the actual `Skill` tool, then invokes baseline
  aliases and uses native `Read` to access their private resources. It verifies
  that discoverable private duplicates are absent, empty directory targets are
  reachable, and exact source modes/directories plus retained generation,
  payload and runtime contents remain unchanged. Changing only a nested skill
  resource then fails before a native
  session hook or model request, reporting both source paths and the catalog ID
  without printing resource contents. Its existing removal, resume and
  concurrency phases continue to pass after the duplicate fixture is removed.
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

Initial implementation blind, edge-case and verification reviews produced 18 findings
(15 distinct issues). All were addressed with regression coverage, including
resource modes, invocation collisions, failed-transaction cleanup, update-only
validation, interpreter prerequisites, image defaults and history scheduling.
No review findings were deferred. The subsequent equivalence correction also
received independent reviews; confirmed collision/link defects and acceptance
gaps were fixed and rerun, while unsupported claims and two negligible
performance suggestions were rejected during triage.

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

## External provider check

The installed `airun --profile reviewer` completed a real review through Z.AI
in a clean checkout, with the `code-reviewer` main role, exit status zero and a
saved native Claude transcript. Its private configuration was removed after
history persistence. The previous installed binary and image were preserved
before installing the feature.

A read-only preparation check against the original working tree found different
local and image versions of `en-ru-translator-adv` and `ru-editor`. The corrected
adapter reports the competing source paths and stops before readiness. Choosing
which local versions to retain remains a local configuration decision; neither
version was silently replaced.

At the user's request, the next live acceptance targeted the `youtube-cli`
repository instead. The installed reviewer inspected actual commit `6ecb086`
against its parent, with 75 existing project tests passing beforehand. It
completed through Z.AI / `glm-5.3` in 400.6 seconds with exit status zero and two
findings, both independently reproduced through the project's real Python/CLI
environment. Inside the generic image, Python development dependencies were
absent, so the reviewer used explicitly disclosed stdlib probes instead of
claiming to run pytest. The snapshot left all 61 compared repository files and
the original Git status unchanged.

The retained reviewer generation and receipt hash stayed unchanged. A fresh
container then ran the native Claude `--resume` command for the saved session;
without being given the original identifier again, the external model recalled
it and the leading finding in one turn. This verifies native conversation
restoration; it does not add a host `airun --resume` option. Automated native
resume remains covered above.

## Limits

No real GitHub MCP request was made. The external provider checks demonstrate
successful runs; continued remote availability and model judgment are outside
the automated acceptance guarantees. Catalog plugins have no
supported installable entry. Unsupported native source formats, missing runtime
prerequisites and unavailable catalog selections fail before readiness rather
than publishing a partial environment.

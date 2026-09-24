# ART-23: baseline startup and history persistence measurements

Measured on 2026-09-24 with the existing `agent-runtime:latest` image
`sha256:e2ceda99e264f0759e14d5b84d4b3c67fba256a12ce8e4a4df8ff9afd3a832e5`
(Claude Code 2.1.278, Node 24.21.0, OrbStack 29.4.0, macOS arm64 host).
The image's `profile-start.mjs` and `component-adapter.mjs` hashes differ from
this checkout, so these numbers describe the installed image, not an exact
build of commit `ae1a8fd`. Rebuilds were avoided with only about 10 GiB free.

Run `node benchmarks/art23-profile-startup.mjs --large-mib 1024 --reps 5`.
The script creates uniquely named Docker volumes, synthetic JSONL history,
and an empty-component profile. It runs with `--network none`, no provider key,
and `/bin/true` as the agent command. It measures host monotonic time from
starting `docker run` until `[airun] ready` appears on stderr. Direct stage
timings use the same image's `claude --version`, `importHistory`, and `prepare`
functions. A separate call measures `mergeHistory` with unchanged history and
with one new complete JSONL record in every transcript. All temporary volumes
and files are removed on exit; no existing state volume is mounted.

| Scenario | Time |
| --- | ---: |
| First run without state | 1.257 s |
| Warm run without state, median of 5 | 1.118 s (1.038–1.212 s) |
| Warm run with ~1 MiB state, median of 5 | 1.135 s (0.857–1.347 s) |
| Warm run with 1 GiB state, 2 samples | 5.237 s, 6.111 s |

For the ~1 MiB state, `claude --version` took 5.8 ms, `importHistory` 29.5 ms,
and the profile adapter 856.4 ms in a direct stage run. The adapter therefore
dominates ordinary profile startup. Its warm path verifies the image baseline
(about 30 MiB across 1,265 files), activates baked components, copies the
active view into a private config, and verifies that view again. These are
candidate operations to time separately after the ART-22 GC changes land.

For 1 GiB state, `importHistory` took 2.097 s and the adapter 2.526 s in one
direct run. The adapter number includes disk contention from the preceding
large import, so it is not an isolated adapter CPU cost. The 1 GiB consists of
four 256 MiB JSONL files; this reflects an aggregate history tree and stays
below Node's 536,870,888-byte maximum string length per file.

| One `mergeHistory` call | ~1 MiB | 1 GiB aggregate |
| --- | ---: | ---: |
| No content change | 16.3 ms | 1.288 s |
| One appended record per JSONL file | 25.0 ms | 15.260 s |
| Peak process RSS during changed merge | 59 MiB | 2,382 MiB |

The changed-JSONL path dominates persistence work. It reads both active and
retained files fully, converts each to a UTF-8 string, splits them into records,
builds a `Set`, joins all records, and atomically rewrites the full result.
The 10-second persistence tick can therefore spend longer than its interval
on a 1 GiB changed history; ticks coalesce, but a final save still waits for
the in-flight operation. Even an unchanged snapshot reads and hashes the full
1 GiB. Any optimization must preserve exact-record deduplication, concurrent
session additions, and complete-record handling on partial writes.

The script's JSON output is in `art23-results.json`. The benchmark did not
run a live Claude/provider session, measure catalog installs, or use private
user history. Only synthetic state was used. After the run the ART-23 volumes
were absent; observed host free space was about 7.4 GiB, down from about
9.6 GiB before the run. No Docker prune or user-data cleanup was performed.

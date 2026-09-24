# ART-23: measured profile startup and history persistence

Measured on 2026-09-24 with real `agent-runtime` images on OrbStack 29.4.0,
macOS arm64, Node 24.21.0 and Claude Code 2.1.278. The before image was built
from commit `5293555` (ART-22 GC), image ID
`sha256:b060f29d59ad2b8ca0e85136a30c2ad65b866699974bbbfaddee3055bd19afd2`.
The optimized image ID was
`sha256:056685143d61a37e423bdc4de3465bc6beced7fc801026cbc9b3c3a5841ea89f`.
Both image script hashes matched the corresponding source recorded in
`art23-before.json` and `art23-after-final.json` at measurement time; the
source differences between those images affect ART-23 only. The ART-23 logic
was subsequently rebased without changes onto `50f9a65`. That ART-22 fixture
cache GC fix is not in either paired comparison image and changes the final
adapter script hash.

Reproduce with `node benchmarks/art23-profile-startup.mjs --image
agent-runtime:art23-after --compare-image agent-runtime:art23-before --output
art23-after-final.json --large-mib 1024 --reps 3` after building the image
tags from the two commits. The script uses an empty-component profile, the
baked image baseline, no provider credentials or network, and `/bin/true` in
place of a live agent. It measures host monotonic time from launching
`docker run` to `[airun] ready`. It creates labelled private Docker volumes,
generates synthetic history and removes those volumes on exit. No user state
volume is mounted or pruned.

| Operation | Before | After |
| --- | ---: | ---: |
| Warm profile, no state (median of 3) | 1.044 s | 0.638 s |
| Warm profile, 1 MiB state (median of 3) | 1.020 s | 0.610 s |
| Typical adapter stage, direct | 798 ms | 435 ms |
| 1 GiB unchanged `mergeHistory`, direct | 753 ms | 48 ms |
| 1 GiB, one appended record in each of 4 files, direct | 14.492 s | 9.066 s |
| Peak process RSS during changed merge | 2,202 MiB | 1,489 MiB |

The warm typical profile reaches readiness in 0.610 s in this fixture. Thus
its added time over a no-profile launch is necessarily below 0.610 s, well
within the 3 s target, although this benchmark does not directly measure the
no-profile path. The adapter dominated ordinary startup. Caching successful
baseline verification by image build ID and receipt avoids rescanning the
immutable image baseline on each warm run. The private config still receives
its normal checks.

After the rebase, a smoke run on an exact final image built from `50f9a65` plus
this patch (`sha256:5aa3948ad3263172ad5f2264a7c79661707407d1f8c5cc65492876da03e0cfd8`)
measured a 0.670 s median for warm startup with 1 MiB state (three samples:
0.701, 0.662, 0.670 s). Both installed script hashes matched the final source.
Its raw output is `art23-final-smoke.json`. This smoke run is not part of the
1 GiB paired comparison and used only 1 MiB of generated history.

For 1 GiB state, startup does not show a reliable improvement. Standalone
before samples were 6.394 and 4.031 s; after samples were 4.397 and 3.767 s.
An alternating run on the **same state and cache volumes** produced after
5.042, 6.131, 3.758 s (median 5.042 s) and before 4.353, 4.664, 4.476 s
(median 4.476 s). This is a measured regression in the paired median amid
substantial I/O variation; do not infer a large-state startup speedup from the
standalone samples. In direct stage runs, importing the 1 GiB history took
1.696 s before and 1.882 s after, and the adapter took 1.756 s before and
1.322 s after. These stages contend with the Docker VM's page cache and
storage, so they are diagnostic rather than additive predictions of readiness.

The unchanged-history improvement uses inode, size, and nanosecond mtime and
ctime to avoid rereading a private snapshot whose bytes have not changed.
When a JSONL file has changed, an append shortcut runs only when the imported
prefix and retained bytes still match their original digest, the retained
file has no duplicate lines, and the new suffix is complete and at most
1 MiB. It deduplicates by exact bytes and atomically publishes the retained
file plus novel suffix. Native rewrites, incomplete tails, concurrent retained
edits, and already-duplicated retained files fall back to the existing full
merge. The changed merge still reads the full history and peaks near 1.5 GiB
RSS, so it is not a bounded-memory solution for larger histories.

The initial installed-image baseline is in `ART-23-baseline.md` and
`art23-results.json`; it predates the exact ART-22 comparison above. This
benchmark does not measure catalog installation, network access or a live
Claude session.

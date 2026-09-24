#!/usr/bin/env node
// Explicit maintenance for the shared component cache and retained recovery
// snapshots. Successful sessions remove their private directories themselves.
import { collectCache } from './component-adapter.mjs';
import { cleanRecovery } from './profile-start.mjs';

const [cache, state] = process.argv.slice(2);
if (!cache || !cache.startsWith('/') || (state && !state.startsWith('/'))) {
  console.error('[airun] usage: profile-clean CACHE [STATE]');
  process.exitCode = 2;
} else {
  try {
    let collected = false;
    if (process.env.AIRUN_CACHE_GC_SKIP === '1') console.error('[airun] warning: component cache GC deferred while a legacy container may be active');
    else { await collectCache(cache); collected = true; }
    const removed = state ? await cleanRecovery(state) : [];
    for (const directory of removed) console.error(`[airun] removed recoverable session: ${directory}`);
    console.error(`[airun] component cache ${collected ? 'collected' : 'unchanged'}; removed ${removed.length} recoverable session(s)`);
  } catch (error) {
    console.error(`[airun] profile cleanup failed: ${error.message}`);
    process.exitCode = 1;
  }
}

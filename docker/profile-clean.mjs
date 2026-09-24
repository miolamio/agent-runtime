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
    await collectCache(cache);
    const removed = state ? await cleanRecovery(state) : [];
    for (const directory of removed) console.error(`[airun] removed recoverable session: ${directory}`);
    console.error(`[airun] component cache collected; removed ${removed.length} recoverable session(s)`);
  } catch (error) {
    console.error(`[airun] profile cleanup failed: ${error.message}`);
    process.exitCode = 1;
  }
}

#!/usr/bin/env node
// Per-container configuration and supervised, allowlisted session persistence.
import { spawn, spawnSync } from 'node:child_process';
import { createHash, randomUUID } from 'node:crypto';
import * as fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

export const HISTORY_PATHS = ['history.jsonl', 'projects', 'file-history', 'plans', 'tasks', 'todos', 'paste-cache', 'image-cache', 'uploads'];
const ACTIVATION_ENV = new Set(['CLAUDE_CODE_ENABLE_FUNCTION_HOOKS', 'CLAUDE_CODE_PLUGIN_SEED_DIR']);
const digest = data => createHash('sha256').update(data).digest('hex');
const missing = error => error.code === 'ENOENT';
const signalExitCode = signal => os.constants.signals[signal] ? 128 + os.constants.signals[signal] : 1;

// Coalesce timer ticks while a whole-history snapshot is running. Shutdown
// drains that one snapshot and performs exactly one final, settled save.
export function historyPersistence(snapshot, onError) {
  let active;
  let stopped = false;
  let finished;
  return {
    tick() {
      if (stopped || active) return;
      active = Promise.resolve().then(() => snapshot(false)).catch(onError).finally(() => { active = undefined; });
    },
    finish() {
      if (!finished) {
        stopped = true;
        finished = Promise.resolve(active).then(() => snapshot(true));
      }
      return finished;
    },
    async stop() { stopped = true; await active; },
  };
}

async function stat(file) {
  try { return await fs.lstat(file); } catch (error) { if (missing(error)) return null; throw error; }
}

// A native rewrite can briefly remove a path between enumeration and open.
// Retry that case, then defer it to the next snapshot rather than poison all
// future persistence. Other I/O errors still require recovery.
export async function readHistoryFile(file, readFile = fs.readFile) {
  for (let attempt = 0; attempt < 3; attempt++) {
    try { return await readFile(file); } catch (error) {
      if (!missing(error)) throw error;
      if (attempt < 2) await new Promise(resolve => setTimeout(resolve, 10));
    }
  }
  return null;
}

async function historyFiles(root) {
  const files = [];
  async function visit(relative) {
    const file = path.join(root, relative);
    const info = await stat(file);
    if (!info) return;
    if (info.isSymbolicLink()) throw new Error(`session history contains a symlink: ${relative}`);
    if (info.isDirectory()) {
      let entries;
      try { entries = await fs.readdir(file); } catch (error) { if (missing(error)) return; throw error; }
      for (const name of entries.sort()) await visit(path.join(relative, name));
    } else if (info.isFile()) files.push(relative);
    else throw new Error(`session history is not a regular file: ${relative}`);
  }
  for (const relative of HISTORY_PATHS) await visit(relative);
  return files;
}

async function atomicWrite(root, relative, data) {
  const parts = relative.split(path.sep);
  let parent = root;
  for (const name of parts.slice(0, -1)) {
    parent = path.join(parent, name);
    const info = await stat(parent);
    if (info && !info.isDirectory()) throw new Error(`session history directory is unsafe: ${relative}`);
    await fs.mkdir(parent, { recursive: true, mode: 0o700 });
  }
  const target = path.join(root, relative);
  const info = await stat(target);
  if (info && !info.isFile()) throw new Error(`session history destination is unsafe: ${relative}`);
  const temporary = `${target}.airun-${randomUUID()}.tmp`;
  await fs.writeFile(temporary, data, { flag: 'wx', mode: 0o600 });
  await fs.rename(temporary, target);
}

// flock's child holds the lock until its stdin closes; process/container failure
// releases the kernel lock without stale PID files or cross-container PID checks.
export async function withHistoryLock(state, operation) {
  await fs.mkdir(state, { recursive: true, mode: 0o700 });
  const lockPath = path.join(state, '.airun-history.lock');
  const info = await stat(lockPath);
  if (info && !info.isFile()) throw new Error('session history lock is not a regular file');
  const holder = spawn('flock', ['--exclusive', lockPath, process.execPath, '-e', 'process.stdout.write("locked\\n");process.stdin.resume()'], { stdio: ['pipe', 'pipe', 'pipe'] });
  holder.stderr.resume();
  holder.stdin.on('error', () => {});
  const closed = new Promise(resolve => holder.once('close', resolve));
  try {
    await new Promise((resolve, reject) => {
      let output = '';
      holder.once('error', error => reject(error.code === 'ENOENT'
        ? new Error('flock is required for session history persistence', { cause: error })
        : error));
      holder.once('close', code => reject(new Error(`session history lock failed (${code})`)));
      holder.stdout.on('data', chunk => { output += chunk; if (output.includes('locked\n')) resolve(); });
    });
    return await operation();
  } finally {
    holder.stdin.end();
    await closed;
  }
}

export async function importHistory(state, config) {
  const original = new Map();
  if (!state) return original;
  await withHistoryLock(state, async () => {
    for (const relative of await historyFiles(state)) {
      const data = await readHistoryFile(path.join(state, relative));
      if (data === null) continue;
      await atomicWrite(config, relative, data);
      original.set(relative, digest(data));
    }
  });
  return original;
}

// Never propagate deletions from a private snapshot: another session can still
// need that history. JSONL records merge by exact bytes, including after native
// atomic rewrites; independent additions survive either session's exit order.
export async function mergeHistory(state, config, original, { final = false } = {}) {
  if (!state) return;
  await withHistoryLock(state, async () => {
    const unfinished = [];
    for (const relative of await historyFiles(config)) {
      const data = await readHistoryFile(path.join(config, relative));
      if (data === null) continue;
      const currentHash = digest(data);
      if (original.get(relative) === currentHash) continue;
      const target = path.join(state, relative);
      const targetInfo = await stat(target);
      if (targetInfo && !targetInfo.isFile()) throw new Error(`session history destination is unsafe: ${relative}`);
      const retained = targetInfo ? await readHistoryFile(target) : null;
      let pending = false;
      if (relative.endsWith('.jsonl')) {
        const current = completeRecords(data, final);
        const saved = completeRecords(retained ?? Buffer.alloc(0), true);
        if (saved.pending) throw new Error(`retained session history has an incomplete JSONL record: ${relative}`);
        pending = current.pending;
        if (pending) unfinished.push(relative);
        const lines = new Set([...saved.data.toString('utf8').split('\n'), ...current.data.toString('utf8').split('\n')].filter(Boolean));
        if (lines.size) await atomicWrite(state, relative, Buffer.from([...lines].join('\n') + '\n'));
      } else if (!retained || digest(retained) === original.get(relative) || digest(retained) === currentHash) {
        await atomicWrite(state, relative, data);
      } else {
        // Non-transcript files (for example a shared memory note) may be
        // rewritten concurrently. Preserve both rather than lose either edit.
        await atomicWrite(state, `${relative}.airun-conflict-${currentHash.slice(0, 16)}`, data);
      }
      // A partially written suffix has not been persisted. Keep it eligible for
      // the next snapshot even when the writer has not changed the file yet.
      if (!pending) original.set(relative, currentHash);
    }
    if (final && unfinished.length) throw new Error(`session history has an incomplete JSONL record: ${unfinished.join(', ')}`);
  });
}

function completeRecords(data, final) {
  const boundary = data.lastIndexOf(10) + 1;
  const complete = data.subarray(0, boundary);
  const tail = data.subarray(boundary);
  if (!tail.toString('utf8').trim()) return { data: complete, pending: false };
  // A valid final record need not end in a newline once the process has exited.
  // While it is running, only a newline proves that its append is complete.
  if (final) {
    try { JSON.parse(tail.toString('utf8')); return { data: Buffer.concat([data, Buffer.from('\n')]), pending: false }; } catch {}
  }
  return { data: complete, pending: true };
}

export function activationEnvironment(value) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('invalid preparation environment');
  const result = {};
  for (const [key, setting] of Object.entries(value)) {
    if (!ACTIVATION_ENV.has(key) || typeof setting !== 'string' || setting.includes('\0')) throw new Error(`unsupported preparation environment key: ${key}`);
    if (key === 'CLAUDE_CODE_ENABLE_FUNCTION_HOOKS' && setting !== '1') throw new Error('invalid function-hooks activation');
    if (key === 'CLAUDE_CODE_PLUGIN_SEED_DIR' && !path.isAbsolute(setting)) throw new Error('plugin seed must be an absolute path');
    result[key] = setting;
  }
  return result;
}

export async function startProfile(argv, options = {}) {
  const environment = options.env ?? process.env;
  const action = environment.AIRUN_PROFILE_ACTION || 'prepare';
  if (!['prepare', 'update'].includes(action)) throw new Error('invalid profile preparation action');
  if (!environment.AIRUN_PROFILE_MANIFEST || !environment.AIRUN_COMPONENT_CACHE) throw new Error('profile manifest and component cache are required');
  if (process.getuid?.() === 0 && !options.allowRoot) throw new Error('profile preparation must run as the non-root container user');
  const state = action === 'prepare' ? environment.AIRUN_PROFILE_STATE : undefined;
  // Unique directories remain private even though a state-enabled launch puts
  // them on the retained volume. Failed saves survive docker --rm for recovery;
  // no later launch imports or reuses a previous configuration directory.
  const runRoot = options.runRoot ?? path.join(state || os.homedir(), '.airun-runs');
  const rootInfo = await stat(runRoot);
  if (rootInfo && !rootInfo.isDirectory()) throw new Error('private configuration directory is not a regular directory');
  await fs.mkdir(runRoot, { recursive: true, mode: 0o700 });
  const config = await fs.mkdtemp(path.join(runRoot, 'config-'));
  const childEnv = { ...environment, CLAUDE_CONFIG_DIR: config, DISABLE_AUTOUPDATER: '1' };
  let original = new Map();
  let child;
  let interrupted;
  const forward = signal => { interrupted = signal; child?.kill(signal); };
  const sigint = () => forward('SIGINT');
  const sigterm = () => forward('SIGTERM');
  process.on('SIGINT', sigint);
  process.on('SIGTERM', sigterm);
  async function run(command, args, env) {
    if (interrupted) return signalExitCode(interrupted);
    child = spawn(command, args, { env, stdio: 'inherit' });
    return await new Promise((resolve, reject) => {
      child.once('error', reject);
      child.once('exit', (code, signal) => { child = undefined; resolve(code ?? signalExitCode(signal)); });
    });
  }
  let persistence;
  let persistenceError;
  let timer;
  let sessionStarted = false;
  let preserveForRecovery = false;
  try {
    const version = spawnSync('claude', ['--version'], { env: childEnv, encoding: 'utf8' });
    const installedVersion = version.stdout?.match(/\b\d+\.\d+\.\d+\b/)?.[0];
    if (version.status !== 0 || !installedVersion) throw new Error('cannot determine installed Claude Code version');
    await fs.writeFile(path.join(config, '.claude.json'), JSON.stringify({
      numStartups: 1, autoUpdaterStatus: 'disabled', userID: randomUUID(),
      hasCompletedOnboarding: true, hasTrustDialogAccepted: true,
      lastOnboardingVersion: installedVersion, projects: {},
    }), { mode: 0o600 });
    original = await importHistory(state, config);
    const prepared = await run(process.execPath, [options.adapterPath ?? '/usr/local/lib/airun/component-adapter.mjs',
      '--manifest', environment.AIRUN_PROFILE_MANIFEST, '--cache', environment.AIRUN_COMPONENT_CACHE,
      '--config', config, '--baseline', options.baselinePath ?? '/opt/airun/profile-baseline', '--action', action], childEnv);
    if (prepared !== 0 || action === 'update') return prepared;
    const launch = JSON.parse(await fs.readFile(path.join(config, 'airun-launch.json'), 'utf8'));
    if (!Array.isArray(launch.args) || launch.args.some(arg => typeof arg !== 'string' || arg.includes('\0'))) throw new Error('invalid preparation launch arguments');
    Object.assign(childEnv, activationEnvironment(launch.env));
    let command = argv[0] || 'claude';
    let args = argv.slice(1);
    if (command.startsWith('-')) { args = argv; command = 'claude'; }
    if (path.basename(command) === 'claude') args.push(...launch.args);
    else if (launch.args.length) throw new Error('profile activation arguments require a Claude Code invocation');
    if (interrupted) return signalExitCode(interrupted);
    console.error(`[airun] ready ts=${Math.floor(Date.now() / 1000)}`);
    if (state) {
      persistence = historyPersistence(async final => {
        await mergeHistory(state, config, original, { final });
        persistenceError = undefined;
      }, error => {
          if (persistenceError?.message !== error.message) console.error(`[airun] session history snapshot deferred: ${error.message}`);
          persistenceError = error;
      });
      timer = setInterval(() => persistence.tick(), 10000);
      timer.unref();
    }
    sessionStarted = true;
    const code = await run(command, args, childEnv);
    if (timer) clearInterval(timer);
    await persistence?.finish();
    return code;
  } catch (error) {
    if (state && sessionStarted) {
      preserveForRecovery = true;
      throw new Error(`${error.message}; private session history preserved for recovery at ${config}`, { cause: error });
    }
    throw error;
  } finally {
    if (timer) clearInterval(timer);
    await persistence?.stop();
    process.off('SIGINT', sigint);
    process.off('SIGTERM', sigterm);
    if (!preserveForRecovery) await fs.rm(config, { recursive: true, force: true });
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  startProfile(process.argv.slice(2)).then(code => { process.exitCode = code; }).catch(error => {
    console.error(`[airun] profile startup failed: ${error.message}`);
    process.exitCode = 1;
  });
}

import test from 'node:test';
import assert from 'node:assert/strict';
import * as fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { spawn, spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { importHistory, mergeHistory, readHistoryFile, activationEnvironment, historyPersistence } from './profile-start.mjs';

const flockAvailable = !spawnSync('flock', [], { stdio: 'ignore' }).error;
const testWithFlock = (name, run) => test(name, { skip: flockAvailable ? false : 'flock is not available in PATH' }, run);

async function fixture(t) {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'airun-private-config-test-'));
  t.after(() => fs.rm(root, { recursive: true, force: true }));
  async function write(relative, data) {
    const target = path.join(root, relative);
    await fs.mkdir(path.dirname(target), { recursive: true });
    await fs.writeFile(target, data);
    return target;
  }
  return { root, write };
}

test('missing flock reports the session history prerequisite clearly', async t => {
  const { root, write } = await fixture(t);
  const emptyBin = path.join(root, 'empty-bin');
  await fs.mkdir(emptyBin);
  const source = fileURLToPath(new URL('./profile-start.mjs', import.meta.url));
  const wrapper = await write('missing-flock.mjs', `import { withHistoryLock } from ${JSON.stringify(source)};
withHistoryLock(${JSON.stringify(path.join(root, 'state'))}, async () => { throw new Error('lock was not acquired'); })
  .catch(error => { console.error(error.message); process.exitCode = 1; });
`);
  const result = await launch(wrapper, { ...process.env, PATH: emptyBin });
  assert.equal(result.code, 1, result.stderr);
  assert.match(result.stderr, /flock is required for session history persistence/);
  assert.doesNotMatch(result.stderr, /spawn flock ENOENT|lock was not acquired/);
});

testWithFlock('imports retained session files and excludes all old discovery/configuration', async t => {
  const { root, write } = await fixture(t);
  await write('state/history.jsonl', '{"prompt":"old"}\n');
  await write('state/projects/-workspace/session.jsonl', '{"message":"saved"}\n');
  for (const stale of ['settings.json', '.claude.json', '.config.json', 'agents/removed.md', 'skills/removed/SKILL.md', 'plugins/installed_plugins.json']) await write(`state/${stale}`, 'stale');
  const active = path.join(root, 'active');
  await fs.mkdir(active);
  const original = await importHistory(path.join(root, 'state'), active);
  assert.equal(original.size, 2);
  assert.deepEqual((await fs.readdir(active)).sort(), ['history.jsonl', 'projects']);
  assert.equal((await fs.lstat(path.join(active, 'history.jsonl'))).isFile(), true);
  assert.equal(await fs.readFile(path.join(active, 'projects/-workspace/session.jsonl'), 'utf8'), '{"message":"saved"}\n');
});

testWithFlock('two private snapshots merge concurrent appends and inode replacements without loss', async t => {
  const { root, write } = await fixture(t);
  const state = path.join(root, 'state');
  await write('state/history.jsonl', '{"prompt":"original"}\n');
  await write('state/projects/-workspace/session.jsonl', '{"message":"original"}\n');
  const first = path.join(root, 'first');
  const second = path.join(root, 'second');
  await Promise.all([fs.mkdir(first), fs.mkdir(second)]);
  const [a, b] = await Promise.all([importHistory(state, first), importHistory(state, second)]);
  const firstInode = (await fs.stat(path.join(first, 'history.jsonl'))).ino;
  await write('first/replacement.tmp', '{"prompt":"first"}\n');
  await fs.rename(path.join(first, 'replacement.tmp'), path.join(first, 'history.jsonl'));
  assert.notEqual((await fs.stat(path.join(first, 'history.jsonl'))).ino, firstInode);
  await fs.appendFile(path.join(second, 'history.jsonl'), '{"prompt":"second"}\n');
  await fs.appendFile(path.join(first, 'projects/-workspace/session.jsonl'), '{"message":"first"}\n');
  await fs.appendFile(path.join(second, 'projects/-workspace/session.jsonl'), '{"message":"second"}\n');
  await Promise.all([mergeHistory(state, first, a), mergeHistory(state, second, b)]);
  const prompts = (await fs.readFile(path.join(state, 'history.jsonl'), 'utf8')).trim().split('\n').map(JSON.parse).map(x => x.prompt).sort();
  assert.deepEqual(prompts, ['first', 'original', 'second']);
  const messages = (await fs.readFile(path.join(state, 'projects/-workspace/session.jsonl'), 'utf8')).trim().split('\n').map(JSON.parse).map(x => x.message).sort();
  assert.deepEqual(messages, ['first', 'original', 'second']);
  assert.equal(await fs.readFile(path.join(first, 'history.jsonl'), 'utf8'), '{"prompt":"first"}\n');
  await mergeHistory(state, second, b);
  assert.equal((await fs.readFile(path.join(state, 'history.jsonl'), 'utf8')).trim().split('\n').length, 3);
});

testWithFlock('rejects symlinks rather than import or overwrite files outside session state', async t => {
  const { root, write } = await fixture(t);
  await write('outside', 'preserved');
  await fs.mkdir(path.join(root, 'state'));
  await fs.mkdir(path.join(root, 'active'));
  await fs.symlink(path.join(root, 'outside'), path.join(root, 'state/history.jsonl'));
  await assert.rejects(importHistory(path.join(root, 'state'), path.join(root, 'active')), /symlink/);
  assert.equal(await fs.readFile(path.join(root, 'outside'), 'utf8'), 'preserved');
});

testWithFlock('periodic snapshots publish complete records only and pick up the completed tail later', async t => {
  const { root, write } = await fixture(t);
  const state = path.join(root, 'state');
  const active = path.join(root, 'active');
  await write('state/history.jsonl', '{"prompt":"saved"}\n');
  await fs.mkdir(active);
  const original = await importHistory(state, active);
  await write('active/history.jsonl', '{"prompt":"saved"}\n{"prompt":"complete"}\n{"prompt":"par');
  await mergeHistory(state, active, original);
  assert.equal(await fs.readFile(path.join(state, 'history.jsonl'), 'utf8'), '{"prompt":"saved"}\n{"prompt":"complete"}\n');
  await fs.appendFile(path.join(active, 'history.jsonl'), 'tial"}\n');
  await mergeHistory(state, active, original);
  assert.equal(await fs.readFile(path.join(state, 'history.jsonl'), 'utf8'), '{"prompt":"saved"}\n{"prompt":"complete"}\n{"prompt":"partial"}\n');
  // An all-partial new transcript must not create a corrupt retained file.
  await write('active/projects/-workspace/new.jsonl', '{"message":');
  await mergeHistory(state, active, original);
  await assert.rejects(fs.stat(path.join(state, 'projects/-workspace/new.jsonl')), { code: 'ENOENT' });
  await assert.rejects(mergeHistory(state, active, original, { final: true }), /incomplete JSONL record/);
});

testWithFlock('final snapshot accepts a complete last JSON record without a newline', async t => {
  const { root, write } = await fixture(t);
  await write('active/history.jsonl', '{"prompt":"complete"}');
  await mergeHistory(path.join(root, 'state'), path.join(root, 'active'), new Map(), { final: true });
  assert.equal(await fs.readFile(path.join(root, 'state/history.jsonl'), 'utf8'), '{"prompt":"complete"}\n');
});

test('history reads retry transient disappearance and leave permanently removed files for a later snapshot', async t => {
  const { root, write } = await fixture(t);
  const file = await write('replacement.jsonl', '{"prompt":"replacement"}\n');
  let attempts = 0;
  const result = await readHistoryFile(file, async target => {
    if (++attempts === 1) throw Object.assign(new Error('native rewrite'), { code: 'ENOENT' });
    return fs.readFile(target);
  });
  assert.equal(attempts, 2);
  assert.equal(result.toString(), '{"prompt":"replacement"}\n');
  assert.equal(await readHistoryFile(path.join(root, 'removed.jsonl')), null);
  await assert.rejects(readHistoryFile(file, async () => { throw Object.assign(new Error('denied'), { code: 'EACCES' }); }), /denied/);
});

test('no-state does not read or publish session data', async t => {
  const { root, write } = await fixture(t);
  await write('active/history.jsonl', 'ephemeral\n');
  const original = await importHistory(undefined, path.join(root, 'active'));
  await mergeHistory(undefined, path.join(root, 'active'), original);
  assert.equal(original.size, 0);
  assert.deepEqual(await fs.readdir(root), ['active']);
});

test('activation environment cannot replace launcher, provider, or config values', () => {
  for (const key of ['PATH', 'HOME', 'ANTHROPIC_API_KEY', 'CLAUDE_CONFIG_DIR', 'AIRUN_COMPONENT_ENV_0001']) assert.throws(() => activationEnvironment({ [key]: 'bad' }), /unsupported/);
  assert.throws(() => activationEnvironment({ CLAUDE_CODE_ENABLE_FUNCTION_HOOKS: '0' }), /invalid/);
  assert.throws(() => activationEnvironment({ CLAUDE_CODE_PLUGIN_SEED_DIR: 'relative' }), /absolute/);
  assert.deepEqual(activationEnvironment({ CLAUDE_CODE_ENABLE_FUNCTION_HOOKS: '1', CLAUDE_CODE_PLUGIN_SEED_DIR: '/tmp/private seed' }), { CLAUDE_CODE_ENABLE_FUNCTION_HOOKS: '1', CLAUDE_CODE_PLUGIN_SEED_DIR: '/tmp/private seed' });
});

test('slow persistence coalesces timer ticks and shutdown drains one snapshot plus one final save', async () => {
  const calls = [];
  let release;
  const slow = new Promise(resolve => { release = resolve; });
  const persistence = historyPersistence(async final => { calls.push(final); if (!final) await slow; }, error => { throw error; });
  persistence.tick();
  await Promise.resolve();
  assert.deepEqual(calls, [false]);
  for (let tick = 0; tick < 100; tick++) persistence.tick();
  const stopped = persistence.finish();
  persistence.tick();
  await Promise.resolve();
  assert.deepEqual(calls, [false], 'final save must await the active snapshot');
  release();
  await stopped;
  await persistence.finish();
  await persistence.stop();
  assert.deepEqual(calls, [false, true], 'queued ticks must not turn into a backlog of full-history copies');
});

async function startupFixture(t) {
  const f = await fixture(t);
  const { root, write } = f;
  const binary = await write('bin/claude', `#!/usr/bin/env node
import fs from 'node:fs';
import path from 'node:path';
if (process.argv.includes('--version')) { console.log('2.1.278 (Claude Code)'); process.exit(0); }
const config=process.env.CLAUDE_CONFIG_DIR;
fs.writeFileSync(process.env.TEST_REPORT, JSON.stringify({uid:process.getuid(),config,args:process.argv.slice(2),onboarding:JSON.parse(fs.readFileSync(path.join(config,'.claude.json'))),flag:process.env.CLAUDE_CODE_ENABLE_FUNCTION_HOOKS}));
fs.writeFileSync(path.join(config,'history.jsonl'), '{"prompt":"new"}\\n');
if (process.env.TEST_INCOMPLETE_TAIL) fs.appendFileSync(path.join(config,'history.jsonl'), '{"prompt":');
if (process.env.TEST_BREAK_PERSISTENCE) fs.mkdirSync(path.join(process.env.AIRUN_PROFILE_STATE,'history.jsonl'));
if (process.env.TEST_CHILD_SIGKILL) process.kill(process.pid,'SIGKILL');
if (process.env.TEST_WAIT) setInterval(()=>{},1000);
`);
  await fs.chmod(binary, 0o755);
  await write('manifest.json', '{}');
  const adapterPath = await write('adapter.mjs', `import fs from 'node:fs';import path from 'node:path';
const args=process.argv;const config=args[args.indexOf('--config')+1];
if(process.getuid()===0) throw Error('root preparation');
const onboarding=JSON.parse(fs.readFileSync(path.join(config,'.claude.json')));if(onboarding.lastOnboardingVersion!=='2.1.278')throw Error('wrong onboarding');
if(process.env.TEST_PREPARE_FAILURE) process.exit(12);
fs.writeFileSync(path.join(config,'airun-launch.json'),JSON.stringify({args:['--agent','reviewer with spaces'],env:{CLAUDE_CODE_ENABLE_FUNCTION_HOOKS:'1'}}));
`);
  const source = fileURLToPath(new URL('./profile-start.mjs', import.meta.url));
  const wrapper = await write('wrapper.mjs', `import {startProfile} from ${JSON.stringify(source)};
startProfile(['claude','-p','literal $(do-not-run)'],{adapterPath:${JSON.stringify(adapterPath)}}).then(code=>{process.exitCode=code;}).catch(error=>{console.error(error.message);process.exitCode=1;});
`);
  const env = { ...process.env, HOME: root, PATH: path.join(root, 'bin') + ':' + process.env.PATH, AIRUN_PROFILE_MANIFEST: path.join(root, 'manifest.json'), AIRUN_COMPONENT_CACHE: path.join(root, 'cache'), AIRUN_PROFILE_STATE: path.join(root, 'state'), TEST_REPORT: path.join(root, 'report.json') };
  return { ...f, wrapper, env };
}

async function launch(wrapper, env) {
  const child = spawn(process.execPath, [wrapper], { env, stdio: ['ignore', 'pipe', 'pipe'] });
  let stdout = '', stderr = '';
  child.stdout.on('data', value => { stdout += value; });
  child.stderr.on('data', value => { stderr += value; });
  const code = await new Promise((resolve, reject) => { child.once('error', reject); child.once('close', resolve); });
  return { code, stdout, stderr };
}

testWithFlock('supervised launch initializes correct onboarding, preserves arguments and history as non-root', async t => {
  const { root, wrapper, env, write } = await startupFixture(t);
  await write('state/history.jsonl', '{"prompt":"old"}\n');
  const result = await launch(wrapper, env);
  assert.equal(result.code, 0, result.stderr);
  assert.match(result.stderr, /\[airun\] ready ts=/);
  const report = JSON.parse(await fs.readFile(env.TEST_REPORT));
  assert.notEqual(report.uid, 0);
  assert.equal(report.onboarding.hasCompletedOnboarding, true);
  assert.deepEqual(report.args, ['-p', 'literal $(do-not-run)', '--agent', 'reviewer with spaces']);
  assert.equal(report.flag, '1');
  assert.ok(report.config.startsWith(path.join(root, 'state/.airun-runs/config-')));
  assert.deepEqual(await fs.readdir(path.join(root, 'state/.airun-runs')), []);
  assert.match(await fs.readFile(path.join(root, 'state/history.jsonl'), 'utf8'), /old.*\n.*new/s);
});

test('update exits without readiness, agent execution, or history import', async t => {
  const { root, wrapper, env } = await startupFixture(t);
  const result = await launch(wrapper, { ...env, AIRUN_PROFILE_ACTION: 'update' });
  assert.equal(result.code, 0, result.stderr);
  assert.doesNotMatch(result.stderr, /\[airun\] ready/);
  await assert.rejects(fs.stat(env.TEST_REPORT), { code: 'ENOENT' });
  await assert.rejects(fs.stat(path.join(root, 'state')), { code: 'ENOENT' });
});

testWithFlock('preparation failure exits before readiness or agent execution', async t => {
  const { wrapper, env } = await startupFixture(t);
  const result = await launch(wrapper, { ...env, TEST_PREPARE_FAILURE: '1' });
  assert.equal(result.code, 12, result.stderr);
  assert.doesNotMatch(result.stderr, /\[airun\] ready/);
  await assert.rejects(fs.stat(env.TEST_REPORT), { code: 'ENOENT' });
});

testWithFlock('failed final persistence keeps a private recovery directory on the retained volume', async t => {
  const { root, wrapper, env } = await startupFixture(t);
  const result = await launch(wrapper, { ...env, TEST_BREAK_PERSISTENCE: '1' });
  assert.equal(result.code, 1, result.stderr);
  const report = JSON.parse(await fs.readFile(env.TEST_REPORT));
  assert.ok(report.config.startsWith(path.join(root, 'state/.airun-runs/config-')));
  assert.match(result.stderr, /preserved for recovery at /);
  assert.ok(result.stderr.includes(report.config));
  assert.equal(await fs.readFile(path.join(report.config, 'history.jsonl'), 'utf8'), '{"prompt":"new"}\n');
  // A new launch must not import this failed run's settings or partial state.
  await fs.rm(path.join(root, 'state/history.jsonl'), { recursive: true });
  const next = path.join(root, 'next');
  await fs.mkdir(next);
  await importHistory(path.join(root, 'state'), next);
  assert.deepEqual(await fs.readdir(next), []);
});

test('stale state cannot redirect private configuration into a host-input directory', async t => {
  const { root, wrapper, env, write } = await startupFixture(t);
  await write('host-agents/agent.md', 'unchanged');
  await fs.mkdir(path.join(root, 'state'));
  await fs.symlink(path.join(root, 'host-agents'), path.join(root, 'state/.airun-runs'));
  const before = await fs.stat(path.join(root, 'host-agents/agent.md'));
  const result = await launch(wrapper, env);
  assert.equal(result.code, 1, result.stderr);
  assert.doesNotMatch(result.stderr, /\[airun\] ready/);
  assert.deepEqual(await fs.readdir(path.join(root, 'host-agents')), ['agent.md']);
  assert.equal(await fs.readFile(path.join(root, 'host-agents/agent.md'), 'utf8'), 'unchanged');
  assert.equal((await fs.stat(path.join(root, 'host-agents/agent.md'))).mode, before.mode);
});

testWithFlock('incomplete tail at process exit retains exact bytes for recovery without corrupting saved JSONL', async t => {
  const { root, wrapper, env } = await startupFixture(t);
  const result = await launch(wrapper, { ...env, TEST_INCOMPLETE_TAIL: '1' });
  assert.equal(result.code, 1, result.stderr);
  assert.match(result.stderr, /incomplete JSONL record.*preserved for recovery/);
  const report = JSON.parse(await fs.readFile(env.TEST_REPORT));
  assert.equal(await fs.readFile(path.join(report.config, 'history.jsonl'), 'utf8'), '{"prompt":"new"}\n{"prompt":');
  assert.equal(await fs.readFile(path.join(root, 'state/history.jsonl'), 'utf8'), '{"prompt":"new"}\n');
});

testWithFlock('SIGTERM reaches the agent and retained history is saved before supervisor exits', async t => {
  const { root, wrapper, env } = await startupFixture(t);
  const child = spawn(process.execPath, [wrapper], { env: { ...env, TEST_WAIT: '1' }, stdio: ['ignore', 'pipe', 'pipe'] });
  t.after(() => child.kill('SIGKILL'));
  let stderr = '';
  child.stderr.on('data', data => { stderr += data; });
  const closed = new Promise((resolve, reject) => { child.once('error', reject); child.once('close', resolve); });
  let ready = false;
  for (let attempt = 0; attempt < 100; attempt++) {
    try { await fs.stat(env.TEST_REPORT); ready = true; break; } catch (error) { if (error.code !== 'ENOENT') throw error; }
    await new Promise(resolve => setTimeout(resolve, 20));
  }
  assert.equal(ready, true, stderr);
  child.kill('SIGTERM');
  assert.equal(await closed, 143, stderr);
  assert.equal(await fs.readFile(path.join(root, 'state/history.jsonl'), 'utf8'), '{"prompt":"new"}\n');
  assert.deepEqual(await fs.readdir(path.join(root, 'state/.airun-runs')), []);
});

testWithFlock('child SIGKILL returns conventional exit 137 while the supervisor saves history', async t => {
  const { root, wrapper, env } = await startupFixture(t);
  const result = await launch(wrapper, { ...env, TEST_CHILD_SIGKILL: '1' });
  assert.equal(result.code, 137, result.stderr);
  assert.equal(await fs.readFile(path.join(root, 'state/history.jsonl'), 'utf8'), '{"prompt":"new"}\n');
  assert.deepEqual(await fs.readdir(path.join(root, 'state/.airun-runs')), []);
});

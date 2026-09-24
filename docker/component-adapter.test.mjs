import test from 'node:test';
import assert from 'node:assert/strict';
import * as fs from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import { createHash } from 'node:crypto';
import { spawn, spawnSync } from 'node:child_process';
import { prepare, inventory, renderMCP, checkRuntime, verifyCatalogOutput, expectedInventory, installerPackageVersion, verifyNativeSource, withProfileLock, validateManifest } from './component-adapter.mjs';

const sha = bytes => createHash('sha256').update(bytes).digest('hex');
const gitSha = bytes => createHash('sha1').update(`blob ${Buffer.byteLength(bytes)}\0`).update(bytes).digest('hex');
const deferred = () => { let resolve; const promise = new Promise(r => { resolve = r; }); return { promise, resolve }; };
const keys = ['agents', 'skills', 'commands', 'mcps', 'mods', 'plugins'];
const manifest = (components = {}, profile = 'reviewer') => ({ version: 1, profile_key: profile, settings: {}, native_plugins: [], components: Object.fromEntries(keys.map(k => [k, (components[k] ?? []).map(v => typeof v === 'string' ? { id: v } : v)])) });
async function put(root, relative, bytes) { const file = path.join(root, relative); await fs.mkdir(path.dirname(file), { recursive: true }); await fs.writeFile(file, bytes); }
async function json(root, relative, value) { await put(root, relative, JSON.stringify(value)); }
async function sourceSnapshot(root) {
  const entries = [];
  async function visit(relative = '') {
    const file = path.join(root, relative), stat = await fs.lstat(file);
    const entry = { path: relative || '.', mode: stat.mode & 0o7777 };
    if (stat.isSymbolicLink()) entries.push({ ...entry, type: 'link', target: await fs.readlink(file) });
    else if (stat.isDirectory()) {
      entries.push({ ...entry, type: 'directory' });
      for (const name of (await fs.readdir(file)).sort()) await visit(path.join(relative, name));
    } else if (stat.isFile()) entries.push({ ...entry, type: 'file', size: stat.size, sha256: sha(await fs.readFile(file)) });
    else entries.push({ ...entry, type: 'special' });
  }
  await visit();
  return entries;
}
async function baselineReceipt(root) {
  const files = (await inventory(root, { links: true })).filter(f => f.path !== 'inventory.json');
  await json(root, 'inventory.json', { version: 1, files: await Promise.all(files.map(async f => f.symlink !== undefined ? { path: f.path, symlink: f.symlink } : { path: f.path, size: f.size, sha256: f.sha256, mode: (await fs.stat(path.join(root, f.path))).mode & 0o777 })) });
}
function yaml(text) {
  return Object.fromEntries(text.split('\n').filter(line => line.includes(':')).map(line => { const n = line.indexOf(':'); return [line.slice(0, n), line.slice(n + 1).trim().replace(/^['"]|['"]$/g, '')]; }));
}
const agent = name => `---\nname: ${name}\ndescription: Reviews code\n---\nReview carefully.\n`;
const skill = '---\nname: helper\ndescription: A useful fixture\n---\nUse references/data.txt.\n';

async function fixture(t) {
  const root = await fs.realpath(await fs.mkdtemp(path.join(os.tmpdir(), 'airun-components-test-')));
  t.after(() => fs.rm(root, { recursive: true, force: true }));
  const baseline = path.join(root, 'baseline');
  await json(baseline, 'settings.json', { effortLevel: 'medium', permissions: { defaultMode: 'bypassPermissions' } });
  await json(baseline, 'baseline.json', { version: 1, native_plugins: [] });
  await baselineReceipt(baseline);
  const source = {
    'cli-tool/components/agents/development-tools/code-reviewer.md': Buffer.from(agent('code-reviewer')),
    'cli-tool/components/agents/other/code-reviewer.md': Buffer.from(agent('different')),
    'cli-tool/components/commands/tools/check.md': Buffer.from('Check all changes.\n'),
    'cli-tool/components/skills/development/helper/SKILL.md': Buffer.from(skill),
    'cli-tool/components/skills/development/helper/references/data.txt': Buffer.from('reference v1\n'),
    'cli-tool/components/mods/testing/helper/.claude-plugin/plugin.json': Buffer.from(JSON.stringify({ name: 'helper-mod' })),
    'cli-tool/components/mods/testing/helper/hooks/hooks.json': Buffer.from(JSON.stringify({ modules: ['./main.ts'] })),
    'cli-tool/components/mods/testing/helper/hooks/main.ts': Buffer.from('export default {}\n'),
    'cli-tool/components/mcps/integration/one.json': Buffer.from(JSON.stringify({ mcpServers: { one: { command: 'node', args: ['-e', 'process.exit(0)'], description: 'removed by upstream', env: { TOKEN: '<YOUR_TOKEN>' } } } })),
    'cli-tool/components/mcps/integration/two.json': Buffer.from(JSON.stringify({ mcpServers: { two: { command: 'node', args: ['-e', 'process.exit(0)'], env: { TOKEN: '<YOUR_TOKEN>' } } } }))
  };
  const locks = new Map();
  const calls = { catalog: 0, install: 0, native: 0, runtime: 0 };
  const deps = {
    env: { AIRUN_COMPONENT_ENV_0001: 'synthetic-first', AIRUN_COMPONENT_ENV_0002: 'synthetic-second' },
    workspace: path.join(root, 'workspace'),
    parseYAML: yaml,
    claudeVersion: async () => '2.1.278',
    checkRuntime: async () => {},
    lock: async (key, action) => {
      const previous = locks.get(key) ?? Promise.resolve();
      const release = deferred(); locks.set(key, previous.then(() => release.promise));
      await previous;
      try { return await action(); } finally { release.resolve(); }
    },
    catalog: async () => {
      calls.catalog++;
      return { commit: 'a'.repeat(40), tree: Object.entries(source).map(([p, bytes]) => ({ path: p, type: 'blob', mode: '100644', sha: gitSha(bytes), size: bytes.length })) };
    },
    sourceBytes: async (_, p) => source[p],
    installCatalog: async (type, id, dir) => {
      calls.install++;
      const base = id.split('/').at(-1);
      const src = `cli-tool/components/${type}/${id}`;
      if (type === 'agents' || type === 'commands') await put(dir, `.claude/${type}/${base}.md`, source[`${src}.md`]);
      else if (type === 'mcps') {
        const data = JSON.parse(source[`${src}.json`]);
        for (const server of Object.values(data.mcpServers)) delete server.description;
        await json(dir, '.mcp.json', data);
      } else for (const [p, bytes] of Object.entries(source)) if (p.startsWith(`${src}/`)) await put(dir, `.claude/skills/${base}/${p.slice(src.length + 1)}`, bytes);
    },
    installNative: async (ref, dir) => {
      calls.native++;
      const [name, marketplace] = ref.split('@');
      await json(dir, 'payloads/0/.claude-plugin/plugin.json', { name });
      await put(dir, 'payloads/0/skills/native/SKILL.md', skill);
      await json(dir, 'native.json', { version: 1, plugins: [{ ref, name, directory: 'payloads/0', source: { source: 'github', repo: 'fixture/plugins' }, marketplace: { name: marketplace, owner: { name: 'Fixtures' }, entry: { name, source: './plugin' } } }] });
    },
    provisionNpm: async (_, name, dir) => {
      calls.runtime++;
      await put(dir, 'bin.js', '#!/usr/bin/env node\nprocess.exit(0)\n');
      await fs.chmod(path.join(dir, 'bin.js'), 0o755);
      await json(dir, 'package.json', { name });
      return path.join(dir, 'bin.js');
    }
  };
  let launch = 0;
  const options = (m, action = 'prepare') => ({ manifest: m, cache: path.join(root, 'cache'), baseline, config: path.join(root, `active-${++launch}`), action });
  return { root, baseline, source, deps, calls, options };
}

test('complete agents/skills/commands activate; role is explicit; warm startup never resolves upstream', async t => {
  const f = await fixture(t);
  const m = manifest({ agents: ['development-tools/code-reviewer'], skills: ['development/helper'], commands: ['tools/check'] });
  m.settings = { agent: 'code-reviewer', permissions: { allow: ['Read'] } };
  const o = f.options(m);
  const first = await prepare(o, f.deps);
  assert.deepEqual(first.launch.args, ['--agent', 'code-reviewer']);
  const settings = JSON.parse(await fs.readFile(path.join(o.config, 'settings.json')));
  assert.deepEqual(settings.permissions, { defaultMode: 'bypassPermissions', allow: ['Read'] });
  const warm = await prepare(f.options(m), { ...f.deps, catalog: () => assert.fail('warm network request'), installCatalog: () => assert.fail('warm install') });
  assert.deepEqual(warm.records, first.records);
  assert.equal(f.calls.install, 3);
});

test('installer pin uses outer published package metadata despite stale nested CLI banner version', async t => {
  const f = await fixture(t);
  const root = path.join(f.root, 'installed');
  await json(root, 'package.json', { name: 'claude-code-templates', version: '1.29.6', bin: { cct: 'cli-tool/bin/cli.js' } });
  await json(root, 'cli-tool/package.json', { name: 'claude-code-templates', version: '1.29.4', bin: { cct: 'bin/cli.js' } });
  await put(root, 'cli-tool/bin/cli.js', 'process.stdout.write("1.29.4")');
  assert.equal(await installerPackageVersion(path.join(root, 'cli-tool/bin/cli.js')), '1.29.6');
  await json(root, 'package.json', { name: 'claude-code-templates', version: '9.9.9', bin: { cct: 'cli-tool/bin/cli.js' } });
  assert.equal(await installerPackageVersion(path.join(root, 'cli-tool/bin/cli.js')), '9.9.9');
});

test('commands preserve native-compatible unquoted argument hints without strict YAML rewriting', async t => {
  const f = await fixture(t);
  const bytes = Buffer.from('---\nargument-hint: [version] | [entry-type] [description]\ndescription: Command fixture\n---\nRun this command.\n');
  f.source['cli-tool/components/commands/tools/check.md'] = bytes;
  const o = f.options(manifest({ commands: ['tools/check'] }));
  await prepare(o, { ...f.deps, parseYAML: () => { throw Error('strict YAML parser rejects bracket/pipe hint'); } });
  assert.deepEqual(await fs.readFile(path.join(o.config, 'commands/check.md')), bytes);
});

test('remove/re-add keeps receipts and selected bytes; immutable running views survive updates', async t => {
  const f = await fixture(t);
  const m = manifest({ skills: ['development/helper'] });
  const firstOptions = f.options(m);
  const first = await prepare(firstOptions, f.deps);
  const removedOptions = f.options(manifest());
  const removed = await prepare(removedOptions, f.deps);
  assert.deepEqual(removed.records, first.records);
  await assert.rejects(fs.stat(path.join(removedOptions.config, 'skills/helper')), { code: 'ENOENT' });
  f.source['cli-tool/components/skills/development/helper/references/data.txt'] = Buffer.from('reference v2\n');
  const restored = await prepare(f.options(m), f.deps);
  assert.deepEqual(restored.records, first.records);
  const updated = await prepare(f.options(m, 'update'), f.deps);
  assert.notEqual(updated.records['skills:development/helper'].digest, first.records['skills:development/helper'].digest);
  assert.equal(await fs.readFile(path.join(firstOptions.config, 'skills/helper/references/data.txt'), 'utf8'), 'reference v1\n');
});

test('corrupt retained artifacts fail without a fetch and explicit update repairs them', async t => {
  const f = await fixture(t);
  const m = manifest({ agents: ['development-tools/code-reviewer'] });
  const first = await prepare(f.options(m), f.deps);
  const record = first.records['agents:development-tools/code-reviewer'];
  await put(path.join(f.root, 'cache/payloads', record.digest), '.claude/agents/code-reviewer.md', 'corrupt');
  await assert.rejects(prepare(f.options(m), f.deps), /profile update reviewer to repair/);
  assert.equal(f.calls.install, 1);
  const repaired = await prepare(f.options(m, 'update'), f.deps);
  assert.equal(repaired.records['agents:development-tools/code-reviewer'].digest, record.digest);
  await prepare(f.options(m), f.deps);
});

test('missing nested file and binary corruption fail despite successful installer return', async t => {
  for (const broken of ['missing', 'binary']) await t.test(broken, async t => {
    const f = await fixture(t);
    if (broken === 'binary') f.source['cli-tool/components/skills/development/helper/references/data.bin'] = Buffer.from([0, 255, 254, 128]);
    const install = f.deps.installCatalog;
    f.deps.installCatalog = async (...args) => {
      await install(...args);
      const dir = args[2];
      if (broken === 'missing') await fs.rm(path.join(dir, '.claude/skills/helper/references/data.txt'));
      else await put(dir, '.claude/skills/helper/references/data.bin', Buffer.from([0, 255, 254, 128]).toString('utf8'));
    };
    await assert.rejects(prepare(f.options(manifest({ skills: ['development/helper'] })), f.deps), /source inventory|differs from source/);
    await assert.rejects(fs.stat(path.join(f.root, 'cache/profiles/reviewer/current.json')), { code: 'ENOENT' });
  });
});

test('partial update and invalid new selection leave the published generation unchanged', async t => {
  const f = await fixture(t);
  const m = manifest({ agents: ['development-tools/code-reviewer'], skills: ['development/helper'] });
  await prepare(f.options(m), f.deps);
  const pointerPath = path.join(f.root, 'cache/profiles/reviewer/current.json');
  const pointer = await fs.readFile(pointerPath, 'utf8');
  const install = f.deps.installCatalog;
  f.deps.installCatalog = async (type, ...args) => { if (type !== 'skills') await install(type, ...args); };
  await assert.rejects(prepare(f.options(m, 'update'), f.deps), /installed inventory/);
  await assert.rejects(prepare(f.options(manifest({ agents: ['absent/agent'] })), f.deps), /unknown catalog identity/);
  assert.equal(await fs.readFile(pointerPath, 'utf8'), pointer);
});

test('flattened names, skills/mods, main role and activation overrides are checked before publication', async t => {
  for (const [m, error] of [
    [manifest({ agents: ['development-tools/code-reviewer', 'other/code-reviewer'] }), /collision/],
    [manifest({ skills: ['development/helper'], mods: ['testing/helper'] }), /collision/],
    [{ ...manifest(), settings: { agent: 'missing' } }, /main agent is unavailable/],
    [{ ...manifest(), settings: { enabledPlugins: {} } }, /conflicts with adapter/],
    [manifest({ plugins: ['made-up/plugin'] }), /no supported catalog plugin/]
  ]) await t.test(error.source, async t => {
    const f = await fixture(t);
    await assert.rejects(prepare(f.options(m), f.deps), error);
    await assert.rejects(fs.stat(path.join(f.root, 'cache/profiles/reviewer/current.json')), { code: 'ENOENT' });
  });
});

test('profiles sharing component payloads own different resolutions; display metadata does not affect identity', async t => {
  const f = await fixture(t);
  const a = manifest({ skills: ['development/helper'] }, 'a');
  const b = manifest({ skills: ['development/helper'] }, 'b');
  const beforeA = await prepare(f.options(a), f.deps);
  const beforeB = await prepare(f.options(b), f.deps);
  assert.equal(beforeA.records['skills:development/helper'].digest, beforeB.records['skills:development/helper'].digest);
  f.source['cli-tool/components/skills/development/helper/references/data.txt'] = Buffer.from('new');
  a.name = 'Different display name';
  const afterA = await prepare(f.options(a, 'update'), f.deps);
  const afterB = await prepare(f.options(b), f.deps);
  assert.notEqual(afterA.records['skills:development/helper'].digest, afterB.records['skills:development/helper'].digest);
  assert.deepEqual(afterB.records, beforeB.records);
});

test('two concurrent first uses install once and update cannot be rolled back by ordinary preparation', async t => {
  const f = await fixture(t);
  const m = manifest({ skills: ['development/helper'] });
  const started = deferred(), proceed = deferred();
  const originalInstall = f.deps.installCatalog;
  let block = true;
  f.deps.installCatalog = async (...args) => { if (block) { block = false; started.resolve(); await proceed.promise; } return originalInstall(...args); };
  const first = prepare(f.options(m), f.deps);
  await started.promise;
  const second = prepare(f.options(m), f.deps);
  proceed.resolve();
  const [one, two] = await Promise.all([first, second]);
  assert.deepEqual(one.records, two.records); assert.equal(f.calls.install, 1);
  const updateStarted = deferred(), updateProceed = deferred();
  f.source['cli-tool/components/skills/development/helper/references/data.txt'] = Buffer.from('after-update');
  f.deps.installCatalog = async (...args) => { updateStarted.resolve(); await updateProceed.promise; return originalInstall(...args); };
  const update = prepare(f.options(m, 'update'), f.deps);
  await updateStarted.promise;
  const ordinary = prepare(f.options(m), f.deps);
  updateProceed.resolve();
  const [newVersion, ordinaryVersion] = await Promise.all([update, ordinary]);
  assert.deepEqual(ordinaryVersion.records, newVersion.records);
  assert.notDeepEqual(newVersion.records, one.records);
});

test('MCP aliases stay component-local and generated artifacts contain no synthetic values', async t => {
  const f = await fixture(t);
  const m = manifest({ mcps: [
    { id: 'integration/one', env: { TOKEN: 'AIRUN_COMPONENT_ENV_0001' } },
    { id: 'integration/two', env: { TOKEN: 'AIRUN_COMPONENT_ENV_0002' } }
  ] });
  const o = f.options(m);
  const result = await prepare(o, f.deps);
  const raw = await fs.readFile(path.join(o.config, 'airun-mcp.json'), 'utf8');
  const servers = JSON.parse(raw).mcpServers;
  assert.equal(servers.one.env.TOKEN, '${AIRUN_COMPONENT_ENV_0001}');
  assert.equal(servers.two.env.TOKEN, '${AIRUN_COMPONENT_ENV_0002}');
  assert(!raw.includes('synthetic')); assert(!JSON.stringify(result.records).includes('synthetic'));
  assert.deepEqual(result.launch.args, ['--mcp-config', path.join(o.config, 'airun-mcp.json')]);
  for (const name of ['one', 'two']) {
    const expected = name === 'one' ? 'synthetic-first' : 'synthetic-second';
    const alias = servers[name].env.TOKEN.slice(2, -1);
    const child = spawn(process.execPath, ['-e', 'process.exit(process.env.TOKEN === process.env.EXPECTED ? 0 : 1)'], { env: { TOKEN: f.deps.env[alias], EXPECTED: expected }, stdio: 'ignore' });
    assert.equal(await new Promise(resolve => child.on('exit', resolve)), 0);
  }
  const removed = f.options(manifest());
  await prepare(removed, f.deps);
  await assert.rejects(fs.stat(path.join(removed.config, 'airun-mcp.json')), { code: 'ENOENT' });
});

test('MCP missing credentials, unsupported placeholders, duplicate servers and executables fail', async t => {
  const f = await fixture(t);
  const template = { mcpServers: { x: { command: 'node', env: { TOKEN: '<YOUR_TOKEN>' } } } };
  await assert.rejects(renderMCP(template, { id: 'x', env: {} }, {}), /unresolved/);
  await assert.rejects(renderMCP(template, { id: 'x', env: { TOKEN: 'AIRUN_COMPONENT_ENV_0001' } }, {}), /missing environment binding/);
  const m = manifest({ mcps: [{ id: 'integration/one', env: { TOKEN: 'AIRUN_COMPONENT_ENV_0001' } }] });
  await assert.rejects(prepare(f.options(m), { ...f.deps, checkRuntime: async () => { throw Error('required executable unavailable'); } }), /executable unavailable/);
  const parsed = JSON.parse(f.source['cli-tool/components/mcps/integration/two.json']);
  parsed.mcpServers.one = parsed.mcpServers.two; delete parsed.mcpServers.two;
  f.source['cli-tool/components/mcps/integration/two.json'] = Buffer.from(JSON.stringify(parsed));
  m.components.mcps.push({ id: 'integration/two', env: { TOKEN: 'AIRUN_COMPONENT_ENV_0002' } });
  await assert.rejects(prepare(f.options(m), f.deps), /duplicate MCP server/);
});

test('npm MCP dependencies are retained, verified and launched without npx network resolution', async t => {
  const f = await fixture(t);
  f.source['cli-tool/components/mcps/integration/npm.json'] = Buffer.from(JSON.stringify({ mcpServers: { npm: { command: 'npx', args: ['-y', '@fixture/server', '--stdio'] } } }));
  const m = manifest({ mcps: ['integration/npm'] });
  const o = f.options(m);
  const first = await prepare(o, f.deps);
  const server = JSON.parse(await fs.readFile(path.join(o.config, 'airun-mcp.json'))).mcpServers.npm;
  assert(server.command.startsWith(path.join(f.root, 'cache/runtimes')));
  assert.deepEqual(server.args, ['--stdio']);
  await prepare(f.options(m), { ...f.deps, provisionNpm: () => assert.fail('warm runtime install') });
  await fs.writeFile(server.command, 'corrupt');
  await assert.rejects(prepare(f.options(m), f.deps), /dependencies are corrupt/);
  const updated = await prepare(f.options(m, 'update'), f.deps);
  assert.notEqual(updated.records['mcps:integration/npm'].runtimes.npm.command, first.records['mcps:integration/npm'].runtimes.npm.command);
});

test('mods require target version and retain complete hook resources', async t => {
  const f = await fixture(t);
  const m = manifest({ mods: ['testing/helper'] });
  await assert.rejects(prepare(f.options(m), { ...f.deps, claudeVersion: async () => '2.1.237' }), /2.1.259 is required/);
  const result = await prepare(f.options(m), f.deps);
  assert.equal(result.launch.env.CLAUDE_CODE_ENABLE_FUNCTION_HOOKS, '1');
  const o = f.options(manifest());
  await prepare(o, f.deps);
  await assert.rejects(fs.stat(path.join(o.config, 'skills/helper')), { code: 'ENOENT' });
});

test('native plugins use retained complete payloads, explicit activation and seed auto-update disabling', async t => {
  const f = await fixture(t);
  const m = manifest(); m.native_plugins = ['extra@fixture'];
  const o = f.options(m);
  const first = await prepare(o, f.deps);
  assert.equal(first.launch.env.CLAUDE_CODE_PLUGIN_SEED_DIR, path.join(o.config, 'airun-plugin-seed'));
  const settings = JSON.parse(await fs.readFile(path.join(o.config, 'settings.json')));
  assert.equal(settings.enabledPlugins['extra@fixture'], true);
  assert.equal(settings.extraKnownMarketplaces.fixture.autoUpdate, false);
  const known = JSON.parse(await fs.readFile(path.join(o.config, 'airun-plugin-seed/known_marketplaces.json')));
  assert.equal(known.fixture.autoUpdate, false);
  assert(!Number.isNaN(Date.parse(known.fixture.lastUpdated)));
  assert(!JSON.stringify(known).includes('/staging/'));
  await prepare(f.options(m), { ...f.deps, installNative: () => assert.fail('warm native installation') });
  assert.equal(f.calls.native, 1);
  const removed = f.options(manifest()); await prepare(removed, f.deps);
  const after = JSON.parse(await fs.readFile(path.join(removed.config, 'settings.json')));
  assert.equal(after.enabledPlugins, undefined);
});

test('native plugin verification catches omitted nested resources, independently of installer success', async t => {
  const f = await fixture(t);
  const marketplace = path.join(f.root, 'marketplace');
  const installed = path.join(f.root, 'native-output');
  const data = { '.claude-plugin/plugin.json': JSON.stringify({ name: 'example' }), 'skills/example/SKILL.md': skill, 'skills/example/data.txt': 'required resource' };
  for (const [name, bytes] of Object.entries(data)) { await put(marketplace, `plugin/${name}`, bytes); await put(installed, name, bytes); }
  await put(installed, '.in_use', '');
  await verifyNativeSource(installed, { source: './plugin' }, marketplace, { gitCommitSha: 'a'.repeat(40) });
  await fs.rm(path.join(installed, 'skills/example/data.txt'));
  await assert.rejects(verifyNativeSource(installed, { source: './plugin' }, marketplace, {}), /complete marketplace source inventory/);
});

test('root-sourced non-strict native plugins activate without plugin.json and retain their declared skills', async t => {
  const f = await fixture(t);
  const marketplace = path.join(f.root, 'marketplace');
  const installed = path.join(f.root, 'installed');
  const entries = [
    { name: 'example-skills', source: './', strict: false, skills: ['./skills/example'] },
    { name: 'document-skills', source: './', strict: false, skills: ['./skills/xlsx'] }
  ];
  await json(marketplace, '.claude-plugin/marketplace.json', { name: 'anthropic-agent-skills', owner: { name: 'Anthropic' }, plugins: entries });
  await put(marketplace, 'skills/example/SKILL.md', skill);
  await put(marketplace, 'skills/xlsx/SKILL.md', skill);
  await put(marketplace, '.git/HEAD', 'ref: refs/heads/main\n');
  await fs.cp(marketplace, installed, { recursive: true });
  await fs.rm(path.join(installed, '.git'), { recursive: true });
  await put(installed, '.in_use', '');
  await verifyNativeSource(installed, entries[0], marketplace, { gitCommitSha: 'a'.repeat(40) });
  await verifyNativeSource(installed, { ...entries[0], source: '.' }, marketplace, {});

  f.deps.installNative = async (ref, directory) => {
    const [name] = ref.split('@');
    await fs.cp(installed, path.join(directory, 'payloads/0'), { recursive: true });
    await fs.rm(path.join(directory, 'payloads/0/.in_use'));
    await json(directory, 'native.json', { version: 1, plugins: [{
      ref, name, directory: 'payloads/0', source: { source: 'github', repo: 'anthropics/skills' },
      marketplace: { name: 'anthropic-agent-skills', owner: { name: 'Anthropic' }, entry: entries.find(e => e.name === name) }
    }] });
  };
  const m = manifest();
  m.native_plugins = entries.map(entry => `${entry.name}@anthropic-agent-skills`);
  const o = f.options(m);
  const first = await prepare(o, f.deps);
  assert.equal(first.launch.env.CLAUDE_CODE_PLUGIN_SEED_DIR, path.join(o.config, 'airun-plugin-seed'));
  const settings = JSON.parse(await fs.readFile(path.join(o.config, 'settings.json')));
  const catalog = JSON.parse(await fs.readFile(path.join(o.config, 'airun-plugin-seed/marketplaces/anthropic-agent-skills/.claude-plugin/marketplace.json')));
  const known = JSON.parse(await fs.readFile(path.join(o.config, 'airun-plugin-seed/known_marketplaces.json')));
  assert.equal(known['anthropic-agent-skills'].source.source, 'directory');
  assert.equal(known['anthropic-agent-skills'].source.path, path.join(o.config, 'airun-plugin-seed/marketplaces/anthropic-agent-skills'));
  assert(!Number.isNaN(Date.parse(known['anthropic-agent-skills'].lastUpdated)));
  assert.equal(await fs.readFile(path.join(o.config, 'airun-plugin-seed/marketplaces/anthropic-agent-skills/skills/example/SKILL.md'), 'utf8'), skill);
  for (const entry of entries) {
    assert.equal(settings.enabledPlugins[`${entry.name}@anthropic-agent-skills`], true);
    assert.deepEqual(catalog.plugins.find(p => p.name === entry.name).skills, entry.skills);
  }
  await prepare(f.options(m), { ...f.deps, installNative: () => assert.fail('warm native installation') });

  await fs.rm(path.join(installed, 'skills/example/SKILL.md'));
  await assert.rejects(verifyNativeSource(installed, entries[0], marketplace, {}), /complete marketplace source inventory/);
});

test('baseline duplicate refs are harmless, distinct sources claiming baseline identity fail', async t => {
  const f = await fixture(t);
  const market = 'claude-plugins-official';
  const ref = `superpowers@${market}`;
  await json(f.baseline, 'baseline.json', { version: 1, native_plugins: [ref] });
  await json(f.baseline, 'plugins/installed_plugins.json', { version: 2, plugins: { [ref]: [{ installPath: `/home/claude/.claude/plugins/cache/${market}/superpowers/1` }] } });
  await json(f.baseline, 'plugins/known_marketplaces.json', { [market]: { source: { source: 'github', repo: 'anthropics/claude-plugins-official' } } });
  await json(f.baseline, `plugins/marketplaces/${market}/.claude-plugin/marketplace.json`, { name: market, owner: { name: 'Fixtures' }, plugins: [{ name: 'superpowers', source: './plugin' }] });
  await json(f.baseline, `plugins/cache/${market}/superpowers/1/.claude-plugin/plugin.json`, { name: 'superpowers' });
  await baselineReceipt(f.baseline);
  const m = manifest(); m.native_plugins = ['superpowers', ref, ref];
  await prepare(f.options(m), f.deps);
  assert.equal(f.calls.native, 0);
  m.native_plugins = ['superpowers@another-marketplace'];
  await assert.rejects(prepare(f.options(m), f.deps), /plugin name collision/);
});

test('baseline corruption and host-agent path/name collisions are rejected', async t => {
  const f = await fixture(t);
  await put(f.baseline, 'agents/base.md', agent('code-reviewer'));
  await baselineReceipt(f.baseline);
  const hosts = path.join(f.root, 'hosts');
  await put(hosts, 'host.md', agent('code-reviewer'));
  await assert.rejects(prepare(f.options(manifest()), { ...f.deps, env: { AIRUN_HOST_AGENTS: hosts } }), /agent identity collision/);
  await fs.writeFile(path.join(f.baseline, 'agents/base.md'), 'tampered');
  await assert.rejects(prepare(f.options(manifest()), f.deps), /image baseline changed/);
});

test('repository agent shadowing and MCP name collisions fail without modifying workspace', async t => {
  const f = await fixture(t);
  const work = f.deps.workspace;
  await put(work, '.claude/agents/reviewer.md', agent('code-reviewer'));
  await put(work, '.claude/agents/unrelated-invalid.md', 'unrelated native configuration');
  const m = manifest({ agents: ['development-tools/code-reviewer'] }); m.settings.agent = 'code-reviewer';
  const before = await sourceSnapshot(work);
  await assert.rejects(prepare(f.options(m), f.deps), /repository agent identity collision: code-reviewer/);
  assert.deepEqual(await sourceSnapshot(work), before);
  await json(work, '.mcp.json', { mcpServers: { one: { command: 'unrelated-local-server' } } });
  const mcp = manifest({ mcps: [{ id: 'integration/one', env: { TOKEN: 'AIRUN_COMPONENT_ENV_0001' } }] });
  await assert.rejects(prepare(f.options(mcp), f.deps), /duplicate MCP server one/);
  // A repository-only role remains selectable, and unrelated malformed files
  // do not become a new adapter validation requirement.
  const repoOnly = manifest(); repoOnly.settings.agent = 'code-reviewer';
  const result = await prepare(f.options(repoOnly), f.deps);
  assert.deepEqual(result.launch.args, ['--agent', 'code-reviewer']);
});

test('requested skills and commands cannot be silently shadowed by the repository', async t => {
  const f = await fixture(t);
  await put(f.deps.workspace, '.claude/skills/helper/SKILL.md', skill);
  await put(f.deps.workspace, '.claude/commands/check.md', 'Repository command');
  await assert.rejects(prepare(f.options(manifest({ skills: ['development/helper'] })), f.deps), /invocation collision.*catalog skills:development\/helper.*repository.*inventories differ/);
  await assert.rejects(prepare(f.options(manifest({ commands: ['tools/check'] })), f.deps), /invocation collision.*catalog commands:tools\/check.*repository.*inventories differ/);
});

async function copyCatalogSkill(f, target, id = 'development/helper') {
  const prefix = `cli-tool/components/skills/${id}/`;
  for (const [file, bytes] of Object.entries(f.source)) if (file.startsWith(prefix)) await put(target, `skills/${id.split('/').at(-1)}/${file.slice(prefix.length)}`, bytes);
}

test('complete equal baseline/catalog components deduplicate and retain every selected catalog identity', async t => {
  const f = await fixture(t);
  for (const [file, bytes] of Object.entries(f.source)) if (file.startsWith('cli-tool/components/skills/development/helper/')) f.source[file.replace('/development/', '/other/')] = bytes;
  f.source['cli-tool/components/commands/other/check.md'] = f.source['cli-tool/components/commands/tools/check.md'];
  await copyCatalogSkill(f, f.baseline);
  await put(f.baseline, 'commands/check.md', f.source['cli-tool/components/commands/tools/check.md']);
  await baselineReceipt(f.baseline);
  const before = await sourceSnapshot(f.baseline);
  const m = manifest({ skills: ['development/helper', 'other/helper'], commands: ['tools/check', 'other/check'] });
  const o = f.options(m);
  const first = await prepare(o, f.deps);
  assert.deepEqual((await inventory(o.config)).filter(file => /^(skills|commands)\//.test(file.path)).map(file => file.path), ['commands/check.md', 'skills/helper/references/data.txt', 'skills/helper/SKILL.md'].sort((a, b) => a.localeCompare(b)));
  assert.equal(Object.keys(first.records).length, 4);
  const pointer = path.join(f.root, 'cache/profiles/reviewer/current.json');
  const generation = await fs.readFile(pointer, 'utf8');
  const warm = await prepare(f.options(m), { ...f.deps, catalog: () => assert.fail('warm fetch'), installCatalog: () => assert.fail('warm install') });
  assert.deepEqual(warm.records, first.records);
  const removed = f.options(manifest());
  await prepare(removed, f.deps);
  assert.equal(await fs.readFile(path.join(removed.config, 'skills/helper/references/data.txt'), 'utf8'), 'reference v1\n');
  assert.equal(await fs.readFile(pointer, 'utf8'), generation);
  assert.deepEqual(await sourceSnapshot(f.baseline), before);
});

test('repository owns equal skills and commands while retained copies survive removal and repository deletion', async t => {
  const f = await fixture(t);
  await copyCatalogSkill(f, f.baseline);
  const repository = path.join(f.deps.workspace, '.claude');
  await copyCatalogSkill(f, repository);
  await put(repository, 'commands/check.md', f.source['cli-tool/components/commands/tools/check.md']);
  await baselineReceipt(f.baseline);
  const before = await sourceSnapshot(f.deps.workspace);
  const m = manifest({ skills: ['development/helper'], commands: ['tools/check'] });
  const o = f.options(m);
  const first = await prepare(o, f.deps);
  for (const file of ['skills/helper', 'commands/check.md']) await assert.rejects(fs.lstat(path.join(o.config, file)), { code: 'ENOENT' });
  const pointer = path.join(f.root, 'cache/profiles/reviewer/current.json');
  const generation = await fs.readFile(pointer, 'utf8');
  const removed = f.options(manifest());
  assert.deepEqual((await prepare(removed, f.deps)).records, first.records);
  await assert.rejects(fs.lstat(path.join(removed.config, 'skills/helper')), { code: 'ENOENT' });
  const warm = f.options(m);
  assert.deepEqual((await prepare(warm, { ...f.deps, catalog: () => assert.fail('warm fetch') })).records, first.records);
  assert.deepEqual(await sourceSnapshot(f.deps.workspace), before);
  // Test-owned deletion leaves the remaining independent managed sources active.
  await fs.rm(repository, { recursive: true });
  const restored = f.options(m);
  assert.deepEqual((await prepare(restored, f.deps)).records, first.records);
  assert.equal(await fs.readFile(path.join(restored.config, 'skills/helper/SKILL.md'), 'utf8'), skill);
  assert.equal(await fs.readFile(pointer, 'utf8'), generation);
  assert.equal(f.calls.install, 2);
});

test('nested command invocation identities and executable flags participate in equivalence', async t => {
  const f = await fixture(t);
  const repository = path.join(f.deps.workspace, '.claude');
  await put(f.baseline, 'commands/tools/nested/check.md', 'Nested command\n');
  await put(repository, 'commands/tools/nested/check.md', 'Nested command\n');
  await baselineReceipt(f.baseline);
  const o = f.options(manifest());
  await prepare(o, f.deps);
  await assert.rejects(fs.lstat(path.join(o.config, 'commands/tools/nested/check.md')), { code: 'ENOENT' });
  await fs.chmod(path.join(repository, 'commands/tools/nested/check.md'), 0o755);
  await assert.rejects(prepare(f.options(manifest()), f.deps), error => {
    assert.match(error.message, /invocation collision: tools:nested:check/);
    assert(error.message.includes(path.join(f.baseline, 'commands/tools/nested/check.md')));
    assert(error.message.includes(path.join(repository, 'commands/tools/nested/check.md')));
    return true;
  });
});

test('repository equality cannot bypass retained corruption or mod collisions', async t => {
  const f = await fixture(t);
  await copyCatalogSkill(f, path.join(f.deps.workspace, '.claude'));
  const m = manifest({ skills: ['development/helper'] });
  const first = await prepare(f.options(m), f.deps);
  await assert.rejects(prepare(f.options(manifest({ skills: ['development/helper'], mods: ['testing/helper'] })), f.deps), /mods:testing\/helper: installed directory collision/);
  const record = first.records['skills:development/helper'];
  await put(path.join(f.root, 'cache/payloads', record.digest), '.claude/skills/helper/references/data.txt', 'corrupt');
  await assert.rejects(prepare(f.options(m), { ...f.deps, catalog: () => assert.fail('corruption must not fetch') }), /retained artifact is corrupt.*profile update reviewer to repair/);
});

test('same-body skill resource differences fail with both sources and leave generation and workspace unchanged', async t => {
  for (const difference of ['changed', 'missing', 'extra', 'executable', 'missing-skill']) await t.test(difference, async t => {
    const f = await fixture(t);
    const m = manifest({ skills: ['development/helper'] });
    await prepare(f.options(m), f.deps);
    const pointer = path.join(f.root, 'cache/profiles/reviewer/current.json');
    const generation = await fs.readFile(pointer, 'utf8');
    const repository = path.join(f.deps.workspace, '.claude');
    await copyCatalogSkill(f, repository);
    const resource = path.join(repository, 'skills/helper/references/data.txt');
    if (difference === 'changed') await fs.writeFile(resource, 'synthetic-secret-resource-change');
    if (difference === 'missing') await fs.rm(resource);
    if (difference === 'extra') await put(repository, 'skills/helper/nested/extra.bin', Buffer.from([0, 255]));
    if (difference === 'executable') await fs.chmod(resource, 0o755);
    if (difference === 'missing-skill') await fs.rm(path.join(repository, 'skills/helper/SKILL.md'));
    const before = await sourceSnapshot(f.deps.workspace);
    const o = f.options(m);
    await assert.rejects(prepare(o, f.deps), error => {
      assert.match(error.message, /invocation collision: helper/);
      assert(error.message.includes('catalog skills:development/helper'));
      assert(error.message.includes('cli-tool/components/skills/development/helper'));
      assert(error.message.includes(path.join(repository, 'skills/helper')));
      assert(!error.message.includes('synthetic-secret-resource-change'));
      return true;
    });
    await assert.rejects(fs.lstat(o.config), { code: 'ENOENT' });
    assert.equal(await fs.readFile(pointer, 'utf8'), generation);
    assert.deepEqual(await sourceSnapshot(f.deps.workspace), before);
  });
});

test('different catalog or baseline versions still conflict before publication with both provenances', async t => {
  for (const source of ['catalog', 'baseline']) await t.test(source, async t => {
    const f = await fixture(t);
    const m = manifest({ skills: ['development/helper'] });
    if (source === 'catalog') {
      for (const [file, bytes] of Object.entries(f.source)) if (file.startsWith('cli-tool/components/skills/development/helper/')) f.source[file.replace('/development/', '/other/')] = bytes;
      f.source['cli-tool/components/skills/other/helper/references/data.txt'] = Buffer.from('different');
      m.components.skills.push({ id: 'other/helper' });
    } else {
      await copyCatalogSkill(f, f.baseline);
      await put(f.baseline, 'skills/helper/references/data.txt', 'different');
      await baselineReceipt(f.baseline);
    }
    await assert.rejects(prepare(f.options(m), f.deps), error => {
      assert(error.message.includes('catalog skills:development/helper'));
      assert(error.message.includes(source === 'catalog' ? 'catalog skills:other/helper' : path.join(f.baseline, 'skills/helper')));
      return /invocation collision/.test(error.message);
    });
    await assert.rejects(fs.lstat(path.join(f.root, 'cache/profiles/reviewer/current.json')), { code: 'ENOENT' });
  });
});

test('equal supported internal skill links deduplicate, but changed link targets conflict', async t => {
  const f = await fixture(t);
  const repository = path.join(f.deps.workspace, '.claude');
  for (const root of [f.baseline, repository]) {
    await put(root, 'skills/helper/SKILL.md', skill);
    await put(root, 'skills/helper/references/data.txt', 'data');
    await fs.symlink('references/data.txt', path.join(root, 'skills/helper/data-link'));
    await fs.symlink('references', path.join(root, 'skills/helper/directory-link'));
  }
  await baselineReceipt(f.baseline);
  const before = await sourceSnapshot(f.deps.workspace);
  const o = f.options(manifest());
  await prepare(o, f.deps);
  await assert.rejects(fs.lstat(path.join(o.config, 'skills/helper')), { code: 'ENOENT' });
  assert.deepEqual(await sourceSnapshot(f.deps.workspace), before);
  await fs.unlink(path.join(repository, 'skills/helper/data-link'));
  await fs.symlink('./references/data.txt', path.join(repository, 'skills/helper/data-link'));
  await assert.rejects(prepare(f.options(manifest()), f.deps), /inventories differ/);
});

test('unsafe colliding symlink roots, resources and command directories cannot hide overlaps or read external bytes', async t => {
  for (const kind of ['skill-root', 'skill-file', 'skill-directory', 'command-file', 'command-directory', 'claude-root', 'skills-root']) for (const dangling of [false, true]) await t.test(`${kind}/${dangling ? 'dangling' : 'escape'}`, async t => {
    const f = await fixture(t);
    const repository = path.join(f.deps.workspace, '.claude');
    const outside = path.join(f.root, 'external');
    await put(outside, 'SKILL.md', skill);
    await put(outside, 'secret', 'synthetic-external-secret');
    // Permission-denied external content exposes accidental dereferencing;
    // symlink metadata remains available for containment validation.
    await fs.chmod(path.join(outside, 'secret'), 0o000);
    const target = dangling ? path.join(f.root, 'absent') : outside;
    const m = manifest({ skills: ['development/helper'] });
    let link;
    if (kind === 'skill-root') link = path.join(repository, 'skills/helper');
    if (kind === 'skill-file' || kind === 'skill-directory') {
      await copyCatalogSkill(f, repository);
      link = path.join(repository, 'skills/helper', kind === 'skill-file' ? 'SKILL.md' : 'references');
      await fs.rm(link, { recursive: true });
    }
    if (kind === 'command-file') { m.components.skills = []; m.components.commands = [{ id: 'tools/check' }]; link = path.join(repository, 'commands/check.md'); }
    if (kind === 'command-directory') {
      m.components.skills = [];
      await put(f.baseline, 'commands/tools/check.md', 'command');
      await baselineReceipt(f.baseline);
      link = path.join(repository, 'commands/tools');
    }
    if (kind === 'claude-root') link = repository;
    if (kind === 'skills-root') link = path.join(repository, 'skills');
    await fs.mkdir(path.dirname(link), { recursive: true });
    await fs.symlink(kind === 'skill-file' || kind === 'command-file' ? path.join(target, 'secret') : target, link);
    const o = f.options(m);
    await assert.rejects(prepare(o, f.deps), error => {
      assert.match(error.message, /invocation collision/);
      assert(error.message.includes(repository));
      assert(!error.message.includes('synthetic-external-secret'));
      return true;
    });
    await assert.rejects(fs.lstat(o.config), { code: 'ENOENT' });
  });
});

test('unrelated repository resources stay outside equivalence checks and update defers workspace overlap', async t => {
  const f = await fixture(t);
  const repository = path.join(f.deps.workspace, '.claude');
  await put(repository, 'skills/unrelated/SKILL.md', skill);
  await fs.symlink('/unavailable/external', path.join(repository, 'skills/unrelated/resource'));
  await fs.mkdir(path.join(repository, 'commands'), { recursive: true });
  await fs.symlink('/unavailable/commands', path.join(repository, 'commands/unrelated'));
  await json(repository, 'skills/check/.claude-plugin/plugin.json', { name: 'check-plugin' });
  // A plugin wrapper does not claim a plain command's invocation namespace.
  await prepare(f.options(manifest({ commands: ['tools/check'] })), f.deps);
  const m = manifest({ skills: ['development/helper'] });
  await prepare(f.options(m), f.deps);
  await copyCatalogSkill(f, repository);
  await put(repository, 'skills/helper/references/data.txt', 'different');
  await prepare(f.options(m, 'update'), f.deps);
  await assert.rejects(prepare(f.options(m), f.deps), /invocation collision/);
});

test('wrapper directories conflict before equal plain copies can be omitted', async t => {
  for (const scenario of ['baseline-wrapper', 'catalog-wrapper', 'catalog-wrapper-and-plain', 'mod-repository', 'baseline-wrapper-repository']) await t.test(scenario, async t => {
    const f = await fixture(t);
    const repository = path.join(f.deps.workspace, '.claude');
    const m = manifest();
    await prepare(f.options(m), f.deps);
    const pointer = path.join(f.root, 'cache/profiles/reviewer/current.json');
    // Establish a published generation before attempting the invalid selection.
    await prepare(f.options(manifest({ commands: ['tools/check'] })), f.deps);
    const beforePointer = await fs.readFile(pointer, 'utf8');
    await copyCatalogSkill(f, repository);
    if (scenario.startsWith('baseline-wrapper')) {
      await json(f.baseline, 'skills/helper/.claude-plugin/plugin.json', { name: 'baseline-helper' });
      if (scenario === 'baseline-wrapper') m.components.skills.push({ id: 'development/helper' });
    } else if (scenario.startsWith('catalog-wrapper')) {
      f.source['cli-tool/components/skills/wrappers/helper/SKILL.md'] = Buffer.from(skill);
      f.source['cli-tool/components/skills/wrappers/helper/.claude-plugin/plugin.json'] = Buffer.from(JSON.stringify({ name: 'catalog-helper' }));
      m.components.skills.push({ id: 'wrappers/helper' });
      if (scenario === 'catalog-wrapper-and-plain') m.components.skills.unshift({ id: 'development/helper' });
    } else m.components.mods.push({ id: 'testing/helper' });
    await baselineReceipt(f.baseline);
    const before = await sourceSnapshot(f.deps.workspace);
    const o = f.options(m);
    await assert.rejects(prepare(o, f.deps), error => {
      assert.match(error.message, /installed directory collision/);
      assert(error.message.includes(path.join(repository, 'skills/helper')));
      assert(error.message.includes(scenario.startsWith('baseline-wrapper') ? path.join(f.baseline, 'skills/helper') : scenario === 'mod-repository' ? 'mods:testing/helper' : 'skills:wrappers/helper'));
      return true;
    });
    await assert.rejects(fs.lstat(o.config), { code: 'ENOENT' });
    assert.equal(await fs.readFile(pointer, 'utf8'), beforePointer);
    assert.deepEqual(await sourceSnapshot(f.deps.workspace), before);
  });
  const f = await fixture(t);
  await json(f.baseline, 'skills/check/.claude-plugin/plugin.json', { name: 'baseline-check' });
  await baselineReceipt(f.baseline);
  // A wrapper's physical directory name is not a plain command invocation.
  await prepare(f.options(manifest({ commands: ['tools/check'] })), f.deps);
});

test('repository executable permissions match the effective managed copy without changing receipts', async t => {
  for (const kind of ['skill', 'nested-command']) await t.test(kind, async t => {
    const f = await fixture(t);
    const repository = path.join(f.deps.workspace, '.claude');
    const relative = kind === 'skill' ? 'skills/helper/scripts/run' : 'commands/tools/check.md';
    for (const root of [f.baseline, repository]) {
      if (kind === 'skill') await put(root, 'skills/helper/SKILL.md', skill);
      await put(root, relative, '#!/bin/sh\nexit 0\n');
      await fs.chmod(path.join(root, relative), 0o755);
    }
    await baselineReceipt(f.baseline);
    const m = manifest({ agents: ['development-tools/code-reviewer'] });
    const first = await prepare(f.options(m), f.deps);
    const pointer = path.join(f.root, 'cache/profiles/reviewer/current.json');
    const generation = await fs.readFile(pointer, 'utf8');
    assert(!JSON.stringify(first.records).includes('executableMode'));
    for (const mode of [0o645, 0o744, 0o754]) {
      await fs.chmod(path.join(repository, relative), mode);
      const o = f.options(m);
      await assert.rejects(prepare(o, f.deps), /invocation collision.*inventories differ/);
      await assert.rejects(fs.lstat(o.config), { code: 'ENOENT' });
      assert.equal((await fs.stat(path.join(repository, relative))).mode & 0o777, mode);
      assert.equal(await fs.readFile(pointer, 'utf8'), generation);
    }
    await fs.chmod(path.join(repository, relative), 0o755);
    assert.deepEqual((await prepare(f.options(m), f.deps)).records, first.records);
  });
});

test('baseline skill aliases and nested incoming links retain omitted targets privately', async t => {
  const f = await fixture(t);
  const repository = path.join(f.deps.workspace, '.claude');
  await copyCatalogSkill(f, f.baseline);
  await copyCatalogSkill(f, repository);
  await fs.symlink('helper', path.join(f.baseline, 'skills/alias'));
  await put(f.baseline, 'skills/consumer/SKILL.md', skill);
  await fs.symlink('../helper/references/data.txt', path.join(f.baseline, 'skills/consumer/data'));
  await fs.symlink('../helper/references', path.join(f.baseline, 'skills/consumer/references'));
  await baselineReceipt(f.baseline);
  const before = await sourceSnapshot(f.baseline);
  const repositoryBefore = await sourceSnapshot(repository);
  const m = manifest({ skills: ['development/helper'] });
  const o = f.options(m);
  const first = await prepare(o, f.deps);
  for (const relative of ['skills/alias/SKILL.md', 'skills/consumer/data', 'skills/consumer/references/data.txt']) {
    assert.equal(await fs.readFile(path.join(o.config, relative), 'utf8'), relative.endsWith('SKILL.md') ? skill : 'reference v1\n');
    assert((await fs.realpath(path.join(o.config, relative))).startsWith(path.join(o.config, 'airun-component-resources')));
  }
  await assert.rejects(fs.lstat(path.join(o.config, 'skills/helper')), { code: 'ENOENT' });
  const pointer = path.join(f.root, 'cache/profiles/reviewer/current.json');
  const generation = await fs.readFile(pointer, 'utf8');
  for (const selection of [m, manifest()]) {
    const next = f.options(selection);
    assert.deepEqual((await prepare(next, { ...f.deps, catalog: () => assert.fail('warm fetch') })).records, first.records);
    assert.equal(await fs.readFile(path.join(next.config, 'skills/alias/SKILL.md'), 'utf8'), skill);
    await assert.rejects(fs.lstat(path.join(next.config, 'skills/helper')), { code: 'ENOENT' });
  }
  assert.equal(await fs.readFile(pointer, 'utf8'), generation);
  assert.deepEqual(await sourceSnapshot(f.baseline), before);
  assert.deepEqual(await sourceSnapshot(repository), repositoryBefore);
});

test('baseline command file and directory links survive with and without deduplicated targets', async t => {
  const f = await fixture(t);
  await put(f.baseline, 'commands/check.md', 'Check command\n');
  await put(f.baseline, 'commands/nested/one.md', 'First command\n');
  await put(f.baseline, 'commands/nested/two.md', 'Second command\n');
  await fs.symlink('check.md', path.join(f.baseline, 'commands/alias.md'));
  await fs.symlink('nested', path.join(f.baseline, 'commands/group'));
  await baselineReceipt(f.baseline);
  const before = await sourceSnapshot(f.baseline);
  const plain = f.options(manifest());
  await prepare(plain, f.deps);
  assert.equal(await fs.readFile(path.join(plain.config, 'commands/alias.md'), 'utf8'), 'Check command\n');
  assert.equal(await fs.readFile(path.join(plain.config, 'commands/group/one.md'), 'utf8'), 'First command\n');
  assert((await fs.lstat(path.join(plain.config, 'commands/group'))).isSymbolicLink());
  const repository = path.join(f.deps.workspace, '.claude');
  await put(repository, 'commands/check.md', 'Check command\n');
  await put(repository, 'commands/nested/one.md', 'First command\n');
  const aliased = f.options(manifest());
  await prepare(aliased, f.deps);
  await assert.rejects(fs.lstat(path.join(aliased.config, 'commands/nested/one.md')), { code: 'ENOENT' });
  assert.equal(await fs.readFile(path.join(aliased.config, 'commands/group/one.md'), 'utf8'), 'First command\n');
  assert((await fs.realpath(path.join(aliased.config, 'commands/group/one.md'))).startsWith(path.join(aliased.config, 'airun-component-resources')));
  await put(repository, 'commands/group/one.md', 'First command\n');
  const duplicate = f.options(manifest());
  await prepare(duplicate, f.deps);
  for (const name of ['check.md', 'nested/one.md', 'group/one.md']) await assert.rejects(fs.lstat(path.join(duplicate.config, 'commands', name)), { code: 'ENOENT' });
  assert.equal(await fs.readFile(path.join(duplicate.config, 'commands/alias.md'), 'utf8'), 'Check command\n');
  assert((await fs.realpath(path.join(duplicate.config, 'commands/alias.md'))).startsWith(path.join(duplicate.config, 'airun-component-resources')));
  assert.equal(await fs.readFile(path.join(duplicate.config, 'commands/group/two.md'), 'utf8'), 'Second command\n');
  assert.deepEqual(await sourceSnapshot(f.baseline), before);
});

test('private baseline link dependencies preserve their own contained links', async t => {
  const f = await fixture(t);
  const repository = path.join(f.deps.workspace, '.claude');
  for (const root of [f.baseline, repository]) {
    await put(root, 'skills/helper/SKILL.md', skill);
    await put(root, 'skills/helper/references/data.txt', 'linked resource\n');
    await fs.symlink('references/data.txt', path.join(root, 'skills/helper/data'));
    await fs.symlink('references', path.join(root, 'skills/helper/directory'));
  }
  await fs.symlink('helper', path.join(f.baseline, 'skills/alias'));
  await baselineReceipt(f.baseline);
  const before = await sourceSnapshot(f.baseline);
  const o = f.options(manifest());
  await prepare(o, f.deps);
  await assert.rejects(fs.lstat(path.join(o.config, 'skills/helper')), { code: 'ENOENT' });
  for (const relative of ['skills/alias/data', 'skills/alias/directory/data.txt']) {
    assert.equal(await fs.readFile(path.join(o.config, relative), 'utf8'), 'linked resource\n');
    assert((await fs.realpath(path.join(o.config, relative))).startsWith(path.join(o.config, 'airun-component-resources')));
  }
  assert.deepEqual(await sourceSnapshot(f.baseline), before);
});

test('empty private directory link targets survive final publication without changing sources or receipts', async t => {
  const f = await fixture(t);
  const repository = path.join(f.deps.workspace, '.claude');
  for (const root of [f.baseline, repository]) {
    await put(root, 'skills/helper/SKILL.md', skill);
    await fs.mkdir(path.join(root, 'skills/helper/empty'), { mode: 0o751 });
  }
  await put(f.baseline, 'skills/consumer/SKILL.md', skill);
  await fs.symlink('../helper/empty', path.join(f.baseline, 'skills/consumer/empty'));
  await baselineReceipt(f.baseline);
  const before = await sourceSnapshot(f.baseline), repositoryBefore = await sourceSnapshot(repository);
  const m = manifest({ commands: ['tools/check'] });
  const o = f.options(m);
  const first = await prepare(o, f.deps);
  const link = path.join(o.config, 'skills/consumer/empty');
  assert((await fs.lstat(link)).isSymbolicLink());
  assert((await fs.realpath(link)).startsWith(path.join(o.config, 'airun-component-resources')));
  assert.deepEqual(await fs.readdir(link), []);
  await assert.rejects(fs.lstat(path.join(o.config, 'skills/helper')), { code: 'ENOENT' });
  await inventory(o.config, { links: true });
  const warm = f.options(m);
  assert.deepEqual((await prepare(warm, f.deps)).records, first.records);
  assert.deepEqual(await fs.readdir(path.join(warm.config, 'skills/consumer/empty')), []);
  assert.deepEqual(await sourceSnapshot(f.baseline), before);
  assert.deepEqual(await sourceSnapshot(repository), repositoryBefore);
});

test('baseline links to their own parent retain a valid relative target', async t => {
  const f = await fixture(t);
  await put(f.baseline, 'skills/self/SKILL.md', skill);
  await fs.symlink('.', path.join(f.baseline, 'skills/self/link'));
  await baselineReceipt(f.baseline);
  const before = await sourceSnapshot(f.baseline);
  const o = f.options(manifest());
  await prepare(o, f.deps);
  assert.equal(await fs.readlink(path.join(o.config, 'skills/self/link')), '.');
  assert.equal(await fs.readFile(path.join(o.config, 'skills/self/link/SKILL.md'), 'utf8'), skill);
  assert.deepEqual(await sourceSnapshot(f.baseline), before);
});

test('later conflicts retain provenance from all previously equivalent selected sources', async t => {
  for (const conflict of ['repository', 'catalog']) await t.test(conflict, async t => {
    const f = await fixture(t);
    await copyCatalogSkill(f, f.baseline);
    await baselineReceipt(f.baseline);
    const m = manifest({ skills: ['development/helper'] });
    let conflictingPath;
    if (conflict === 'repository') {
      conflictingPath = path.join(f.deps.workspace, '.claude/skills/helper');
      await put(conflictingPath, 'SKILL.md', skill);
    } else {
      f.source['cli-tool/components/skills/other/helper/SKILL.md'] = Buffer.from(skill);
      m.components.skills.push({ id: 'other/helper' });
      conflictingPath = 'cli-tool/components/skills/other/helper';
    }
    await assert.rejects(prepare(f.options(m), f.deps), error => {
      for (const source of [path.join(f.baseline, 'skills/helper'), 'catalog skills:development/helper', 'cli-tool/components/skills/development/helper', conflictingPath]) assert(error.message.includes(source));
      return /invocation collision/.test(error.message);
    });
  });
});

test('command comparison ignores unrelated sibling names and rejects matching special entries before reads', async t => {
  const f = await fixture(t);
  const repository = path.join(f.deps.workspace, '.claude');
  await put(repository, 'commands/check.md', f.source['cli-tool/components/commands/tools/check.md']);
  await put(repository, 'commands/unrelated\\name', 'unrelated');
  const m = manifest({ commands: ['tools/check'] });
  const o = f.options(m);
  await prepare(o, f.deps);
  await assert.rejects(fs.lstat(path.join(o.config, 'commands/check.md')), { code: 'ENOENT' });
  const pointer = path.join(f.root, 'cache/profiles/reviewer/current.json');
  const generation = await fs.readFile(pointer, 'utf8');
  await fs.unlink(path.join(repository, 'commands/check.md'));
  const fifo = spawnSync('mkfifo', [path.join(repository, 'commands/check.md')]);
  assert.equal(fifo.status, 0, 'fixture FIFO could not be created');
  // A writer unblocks only if preparation opens the FIFO. It leaves a marker
  // before supplying bytes, so an accidental read fails without hanging tests.
  const marker = path.join(f.root, 'fifo-read');
  const writer = spawn(process.execPath, ['--input-type=module', '-e',
    'import * as fs from "node:fs/promises"; const out = await fs.open(process.argv[1], "w"); await fs.writeFile(process.argv[2], "read"); await out.write("unexpected bytes"); await out.close();',
    path.join(repository, 'commands/check.md'), marker], { stdio: 'ignore' });
  const stopped = new Promise(resolve => writer.once('close', resolve));
  t.after(() => writer.kill());
  const invalid = f.options(m);
  await assert.rejects(prepare(invalid, f.deps), error => /invocation collision/.test(error.message) && error.message.includes('catalog commands:tools/check') && error.message.includes(path.join(repository, 'commands/check.md')));
  await assert.rejects(fs.lstat(invalid.config), { code: 'ENOENT' });
  writer.kill(); await stopped;
  await assert.rejects(fs.lstat(marker), { code: 'ENOENT' });
  assert.equal(await fs.readFile(pointer, 'utf8'), generation);
  assert((await fs.lstat(path.join(repository, 'commands/check.md'))).isFIFO());
});

test('skills and commands share invocation identities across requested, baseline and repository sources', async t => {
  for (const sources of [['requested', 'requested'], ['baseline', 'requested'], ['requested', 'baseline'], ['repository', 'requested'], ['requested', 'repository'], ['baseline', 'repository'], ['repository', 'baseline']]) await t.test(sources.join('/'), async t => {
    const f = await fixture(t);
    const m = manifest();
    f.source['cli-tool/components/commands/tools/helper.md'] = Buffer.from(skill);
    if (sources[0] === 'requested') m.components.skills.push({ id: 'development/helper' });
    else await put(sources[0] === 'baseline' ? f.baseline : path.join(f.deps.workspace, '.claude'), 'skills/helper/SKILL.md', skill);
    if (sources[1] === 'requested') m.components.commands.push({ id: 'tools/helper' });
    else await put(sources[1] === 'baseline' ? f.baseline : path.join(f.deps.workspace, '.claude'), 'commands/helper.md', skill);
    await baselineReceipt(f.baseline);
    await assert.rejects(prepare(f.options(m), f.deps), error => /skill\/command invocation collision: helper/.test(error.message) && error.message.includes('skills ') && error.message.includes('commands ') && /different component kinds/.test(error.message));
    await assert.rejects(fs.stat(path.join(f.root, 'cache/profiles/reviewer/current.json')), { code: 'ENOENT' });
  });
  const f = await fixture(t);
  // Unrelated repository collisions stay the native loader's responsibility.
  await put(f.deps.workspace, '.claude/skills/unrelated/SKILL.md', skill);
  await put(f.deps.workspace, '.claude/commands/unrelated.md', 'Body');
  // Skill frontmatter name is display metadata, not the invocation identity.
  await put(f.baseline, 'skills/different/SKILL.md', skill.replace('name: helper', 'name: check'));
  await baselineReceipt(f.baseline);
  await prepare(f.options(manifest({ commands: ['tools/check'] })), f.deps);
});

test('native plugin custom agent files and directories preserve their native namespaces', async t => {
  for (const [custom, selected] of [
    ['./custom/review.md', 'extra:auditor'],
    [['./custom', './agents/default.md'], 'extra:nested:auditor'],
    [['./custom', './agents/default.md'], 'extra:default']
  ]) await t.test(selected, async t => {
    const f = await fixture(t);
    const install = f.deps.installNative;
    f.deps.installNative = async (ref, dir) => {
      await install(ref, dir);
      await json(dir, 'payloads/0/.claude-plugin/plugin.json', { name: 'extra', agents: custom });
      await put(dir, 'payloads/0/agents/default.md', agent('default'));
      await put(dir, `payloads/0/custom/${Array.isArray(custom) ? 'nested/' : ''}review.md`, agent('auditor'));
    };
    const m = manifest(); m.native_plugins = ['extra@fixture']; m.settings.agent = selected;
    const first = await prepare(f.options(m), f.deps);
    assert.deepEqual(first.launch.args, ['--agent', selected]);
    await prepare(f.options(m), { ...f.deps, installNative: () => assert.fail('warm native installation') });
  });
});

test('verified catalog scripts restore Git executable modes before capture and activation', async t => {
  const f = await fixture(t);
  const sourcePath = 'cli-tool/components/skills/development/helper/scripts/run';
  f.source[sourcePath] = Buffer.from('#!/bin/sh\nexit 0\n');
  const catalog = f.deps.catalog;
  f.deps.catalog = async () => { const value = await catalog(); value.tree.find(file => file.path === sourcePath).mode = '100755'; return value; };
  const m = manifest({ skills: ['development/helper'] });
  const o = f.options(m);
  const result = await prepare(o, f.deps);
  assert.equal((await fs.stat(path.join(o.config, 'skills/helper/scripts/run'))).mode & 0o777, 0o755);
  const record = result.records['skills:development/helper'];
  assert.equal(record.source.inventory.find(file => file.source === sourcePath).mode, '100755');
  assert.equal(record.inventory.find(file => file.path.endsWith('/scripts/run')).executable, true);
  const warm = f.options(m);
  await prepare(warm, { ...f.deps, catalog: () => assert.fail('warm network') });
  assert.equal((await fs.stat(path.join(warm.config, 'skills/helper/scripts/run'))).mode & 0o777, 0o755);
  const unverified = path.join(f.root, 'unverified');
  await put(unverified, 'run', 'wrong bytes');
  await fs.chmod(path.join(unverified, 'run'), 0o644);
  await assert.rejects(verifyCatalogOutput(unverified, [{ path: 'run', gitSha: gitSha(f.source[sourcePath]), mode: '100755' }], 'skills'), /differs from source/);
  assert.equal((await fs.stat(path.join(unverified, 'run'))).mode & 0o777, 0o644);
});

test('native marketplace and GitHub verification reject executable mode corruption', async t => {
  const f = await fixture(t);
  const market = path.join(f.root, 'marketplace'), installed = path.join(f.root, 'installed');
  const bytes = '#!/bin/sh\nexit 0\n';
  await put(market, 'plugin/run', bytes); await fs.chmod(path.join(market, 'plugin/run'), 0o755);
  await put(installed, 'run', bytes); await fs.chmod(path.join(installed, 'run'), 0o644);
  await assert.rejects(verifyNativeSource(installed, { source: './plugin' }, market, {}), /complete marketplace source inventory/);
  t.mock.method(globalThis, 'fetch', async () => ({ ok: true, json: async () => ({ tree: [{ path: 'run', type: 'blob', mode: '100755', sha: gitSha(bytes) }] }) }));
  const entry = { source: { source: 'github', repo: 'fixture/plugin' } }, registration = { gitCommitSha: 'a'.repeat(40) };
  await assert.rejects(verifyNativeSource(installed, entry, market, registration), /mode differs from source/);
  await fs.chmod(path.join(installed, 'run'), 0o755);
  await verifyNativeSource(installed, entry, market, registration);
  await verifyNativeSource(installed, { source: './plugin' }, market, {});
});

test('failed transactions remove all new npm runtimes and retain successful generations', async t => {
  const f = await fixture(t);
  const sourcePath = 'cli-tool/components/mcps/integration/npm.json';
  const template = { mcpServers: { one: { command: 'npx', args: ['-y', '@fixture/one'] } } };
  f.source[sourcePath] = Buffer.from(JSON.stringify(template));
  const m = manifest({ mcps: ['integration/npm'] });
  await prepare(f.options(m), f.deps);
  const runtimeDir = path.join(f.root, 'cache/runtimes');
  const retained = await inventory(runtimeDir);
  const pointer = path.join(f.root, 'cache/profiles/reviewer/current.json');
  const before = await fs.readFile(pointer, 'utf8');
  template.mcpServers.two = { command: 'npx', args: ['-y', '@fixture/two'] };
  f.source[sourcePath] = Buffer.from(JSON.stringify(template));
  for (const failure of ['second-provision', 'activation', 'copy']) await t.test(failure, async () => {
    const o = f.options(m, 'update');
    let count = 0;
    const deps = { ...f.deps, provisionNpm: async (...args) => { if (++count === 2 && failure === 'second-provision') throw Error('fixture provisioning failure'); return f.deps.provisionNpm(...args); } };
    if (failure === 'activation') o.manifest = { ...m, settings: { enabledPlugins: {} } };
    if (failure === 'copy') await put(o.config, 'settings.json', 'existing configuration');
    await assert.rejects(prepare(o, deps), /provisioning failure|conflicts with adapter|installed path collision/);
    assert.deepEqual(await inventory(runtimeDir), retained);
    assert.equal(await fs.readFile(pointer, 'utf8'), before);
  });
});

test('update defers workspace role and script checks while prepare requires both', async t => {
  const f = await fixture(t);
  f.source['cli-tool/components/mcps/integration/local.json'] = Buffer.from(JSON.stringify({ mcpServers: { local: { command: 'node', args: ['server.mjs'] } } }));
  const m = manifest({ mcps: ['integration/local'] }); m.settings.agent = 'repository-reviewer';
  const deps = { ...f.deps, checkRuntime: undefined };
  const updated = await prepare(f.options(m, 'update'), deps);
  assert(!updated.launch.args.includes('--agent'));
  await assert.rejects(prepare(f.options(m), deps), /runtime script is unavailable/);
  await put(f.deps.workspace, 'server.mjs', 'process.exit(0)');
  await assert.rejects(prepare(f.options(m), deps), /selected main agent is unavailable/);
  await put(f.deps.workspace, '.claude/agents/reviewer.md', agent('repository-reviewer'));
  const launched = await prepare(f.options(m), deps);
  assert(launched.launch.args.includes('repository-reviewer'));
  // An update must still reject bad selected artifacts and non-workspace dependencies.
  f.source['cli-tool/components/mcps/integration/local.json'] = Buffer.from(JSON.stringify({ mcpServers: { local: { command: 'node', args: [path.join(f.root, 'missing-outside-workspace.mjs')] } } }));
  await assert.rejects(prepare(f.options(m, 'update'), deps), /runtime script is unavailable/);
  const bad = manifest({ agents: ['development-tools/code-reviewer'] });
  f.source['cli-tool/components/agents/development-tools/code-reviewer.md'] = Buffer.from('missing frontmatter');
  await assert.rejects(prepare(f.options(bad, 'update'), deps), /missing YAML frontmatter/);
});

test('baseline verification rejects uncaptured activatable files but excludes receipt and git metadata', async t => {
  const f = await fixture(t);
  await put(f.baseline, '.git/HEAD', 'ignored checkout metadata');
  await prepare(f.options(manifest()), f.deps);
  await put(f.baseline, 'skills/uncaptured/SKILL.md', skill);
  await assert.rejects(prepare(f.options(manifest()), f.deps), /baseline inventory differs/);
});

test('MCP transport and environment shapes fail before publishing and never expose bindings', async t => {
  const f = await fixture(t);
  for (const server of [
    { type: 'http', url: '' },
    { type: 'http', url: 'file:///tmp/server' },
    { type: 'http', url: 'https://example.test', headers: { Authorization: 42 } },
    { command: 'node', args: ['-e', ''], env: { TOKEN: 42 } },
    { type: 'http', command: 'node', url: 'https://example.test' }
  ]) await t.test(JSON.stringify(server), async () => {
    f.source['cli-tool/components/mcps/integration/invalid.json'] = Buffer.from(JSON.stringify({ mcpServers: { invalid: server } }));
    await assert.rejects(prepare(f.options(manifest({ mcps: ['integration/invalid'] })), f.deps), /server (transport|URL|headers|environment)/);
    await assert.rejects(fs.stat(path.join(f.root, 'cache/profiles/reviewer/current.json')), { code: 'ENOENT' });
  });
  const bound = { id: 'bound', env: { URL: 'AIRUN_COMPONENT_ENV_0001' } };
  const valid = await renderMCP({ mcpServers: { remote: { type: 'http', url: '${URL}', headers: { Authorization: 'Bearer ${URL}' } } } }, bound, { AIRUN_COMPONENT_ENV_0001: 'https://synthetic.example.test' });
  assert.equal(valid.remote.url, '${AIRUN_COMPONENT_ENV_0001}');
  assert(!JSON.stringify(valid).includes('synthetic'));
  await assert.rejects(renderMCP({ mcpServers: { remote: { type: 'http', url: '${URL}' } } }, bound, { AIRUN_COMPONENT_ENV_0001: 'secret-invalid-value' }), error => /invalid server URL/.test(error.message) && !error.message.includes('secret-invalid-value'));
});

test('supported interpreter prerequisites are checked for both prepare and update', async t => {
  const f = await fixture(t);
  const sourcePath = 'cli-tool/components/mcps/integration/python.json';
  const m = manifest({ mcps: ['integration/python'] });
  const deps = { ...f.deps, checkRuntime: undefined };
  f.source[sourcePath] = Buffer.from(JSON.stringify({ mcpServers: { python: { command: 'python3', args: ['-m', 'airun_missing_fixture_module'] } } }));
  for (const action of ['prepare', 'update']) {
    await assert.rejects(prepare(f.options(m, action), deps), /Python module prerequisite is unavailable/);
    await assert.rejects(fs.stat(path.join(f.root, 'cache/profiles/reviewer/current.json')), { code: 'ENOENT' });
  }
  f.source[sourcePath] = Buffer.from(JSON.stringify({ mcpServers: { python: { command: 'python3', args: ['-u', '-m', 'json.tool'] } } }));
  await prepare(f.options(m), deps);
  await prepare(f.options(m, 'update'), deps);
  await put(f.deps.workspace, 'fixture.py', 'raise Exception("validation must not execute the script")');
  for (const server of [
    { command: 'python3', args: ['-u', 'fixture.py'] },
    { command: 'python3', args: ['-c', 'raise Exception("not executed")'] },
    { command: 'node', args: ['-e', 'throw Error("not executed")'] },
    { command: 'sh', args: ['-c', 'exit 99'] },
    { command: 'bash', args: ['-lc', 'exit 99'] }
  ]) await checkRuntime(server, 'fixture', { workspace: f.deps.workspace });
  await assert.rejects(checkRuntime({ command: 'python3', args: ['-u', 'absent.py'] }, 'fixture', { workspace: f.deps.workspace }), /runtime script is unavailable/);
});

test('unsupported manifest shapes and source symlinks cannot escape staging', async t => {
  const f = await fixture(t);
  const m = manifest({ agents: ['../escape'] });
  assert.throws(() => validateManifest(m), /invalid agents/);
  const catalog = await f.deps.catalog();
  catalog.tree[0].mode = '120000';
  assert.throws(() => expectedInventory(catalog, 'agents', 'development-tools/code-reviewer'), /unsupported source/);
  const dir = path.join(f.root, 'link'); await fs.mkdir(dir);
  await fs.symlink('/etc/passwd', path.join(dir, 'escape'));
  await assert.rejects(inventory(dir), /unsupported artifact entry/);
});

test('real Linux flock serializes and releases after errors', { skip: process.platform !== 'linux' }, async t => {
  const f = await fixture(t);
  const lock = path.join(f.root, 'locks/profile.lock');
  const entered = deferred(), leave = deferred();
  const order = [];
  const a = withProfileLock(lock, async () => { order.push('a'); entered.resolve(); await leave.promise; throw Error('fixture failure'); });
  const failed = assert.rejects(a, /fixture failure/);
  await entered.promise;
  const b = withProfileLock(lock, async () => { order.push('b'); });
  leave.resolve();
  await Promise.all([failed, b]);
  assert.deepEqual(order, ['a', 'b']);
  const m = manifest({ agents: ['development-tools/code-reviewer'] });
  await Promise.all([prepare(f.options(m), { ...f.deps, lock: withProfileLock }), prepare(f.options(m), { ...f.deps, lock: withProfileLock })]);
  assert.equal(f.calls.install, 1);
});

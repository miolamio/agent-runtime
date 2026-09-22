#!/usr/bin/env node
// Container-owned profile preparation. Only immutable payloads cross launches;
// the profile lock protects resolutions, not a shared Claude configuration.
import * as fs from 'node:fs/promises';
import path from 'node:path';
import { createHash, randomUUID } from 'node:crypto';
import { spawn } from 'node:child_process';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';

export const INSTALLER_VERSION = '1.29.6';
const TYPES = ['agents', 'skills', 'commands', 'mcps', 'mods', 'plugins'];
const FLAGS = { agents: '--agent', skills: '--skill', commands: '--command', mcps: '--mcp', mods: '--mod' };
const REPO = 'davila7/claude-code-templates';
const PROFILE = /^[a-zA-Z0-9][a-zA-Z0-9_.-]*$/;
const ID = /^[a-zA-Z0-9][a-zA-Z0-9_.-]*(?:\/[a-zA-Z0-9][a-zA-Z0-9_.-]*)*$/;
const ENV_NAME = /^[A-Za-z_][A-Za-z0-9_]*$/;
const ALIAS = /^AIRUN_COMPONENT_ENV_[0-9]+$/;
const NATIVE = /^[a-zA-Z0-9][a-zA-Z0-9_.-]*@[a-zA-Z0-9][a-zA-Z0-9_.-]*$/;
const plain = value => value !== null && typeof value === 'object' && !Array.isArray(value);
const hash = bytes => createHash('sha256').update(bytes).digest('hex');
const stable = value => JSON.stringify(sortObject(value));
function sortObject(value) {
  if (Array.isArray(value)) return value.map(sortObject);
  return plain(value) ? Object.fromEntries(Object.keys(value).sort().map(k => [k, sortObject(value[k])])) : value;
}
function fail(message) { throw new Error(message); }
function safeRelative(value) {
  if (typeof value !== 'string' || !value || value.includes('\\') || path.posix.isAbsolute(value) || value.split('/').some(p => !p || p === '.' || p === '..') || /[\x00-\x1f]/.test(value)) fail('unsafe artifact path');
  return value;
}
async function exists(file) { try { await fs.lstat(file); return true; } catch (e) { if (e.code === 'ENOENT') return false; throw e; } }
async function readJSON(file, fallback) {
  try { return JSON.parse(await fs.readFile(file, 'utf8')); }
  catch (e) { if (e.code === 'ENOENT' && fallback !== undefined) return fallback; fail(`cannot read valid JSON: ${path.basename(file)}`); }
}
async function writeJSON(file, value) { await fs.mkdir(path.dirname(file), { recursive: true }); await fs.writeFile(file, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o600 }); }
async function atomicJSON(file, value) {
  const tmp = `${file}.${randomUUID()}.tmp`;
  await writeJSON(tmp, value);
  const handle = await fs.open(tmp, 'r');
  try { await handle.sync(); } finally { await handle.close(); }
  await fs.rename(tmp, file);
}
function cleanEnvironment(env = process.env) {
  return Object.fromEntries(['PATH', 'HOME', 'TMPDIR', 'NODE_PATH', 'LANG', 'SSL_CERT_FILE', 'SSL_CERT_DIR'].filter(k => env[k] !== undefined).map(k => [k, env[k]]));
}
export async function runCommand(command, args, options = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, { cwd: options.cwd, env: options.env ?? cleanEnvironment(), stdio: ['ignore', 'pipe', 'pipe'] });
    const output = [];
    let size = 0;
    const timer = setTimeout(() => child.kill('SIGKILL'), options.timeout ?? 180000);
    child.stdout.on('data', b => { size += b.length; if (size < 4 * 1024 * 1024) output.push(b); });
    // Installer output can contain configuration and credentials. Never forward it.
    child.stderr.resume();
    child.once('error', () => { clearTimeout(timer); reject(new Error(`${path.basename(command)} could not start`)); });
    child.once('close', code => {
      clearTimeout(timer);
      if (code === 0) resolve(Buffer.concat(output).toString('utf8'));
      else reject(new Error(`${path.basename(command)} failed (${code ?? 'terminated'})`));
    });
  });
}

// flock is held by a child waiting for this process's pipe. Process death closes
// the pipe, so no stale PID files or lock-directory recovery is necessary.
export async function withProfileLock(file, action) {
  await fs.mkdir(path.dirname(file), { recursive: true });
  const child = spawn('flock', ['--exclusive', file, 'sh', '-c', 'printf "locked\\n"; cat >/dev/null'], { stdio: ['pipe', 'pipe', 'pipe'] });
  child.stderr.resume();
  child.stdin.on('error', () => {});
  const closed = new Promise(resolve => child.once('close', resolve));
  try {
    await new Promise((resolve, reject) => {
      let text = '';
      child.once('error', () => reject(new Error('flock is required for component preparation')));
      child.once('close', () => reject(new Error('profile lock could not be acquired')));
      child.stdout.on('data', b => { text += b; if (text.includes('locked\n')) resolve(); });
    });
    return await action();
  } finally { child.stdin.end(); await closed; }
}

export function validateManifest(manifest) {
  if (!plain(manifest) || manifest.version !== 1) fail('unsupported profile manifest version');
  if (!PROFILE.test(manifest.profile_key ?? '')) fail('invalid canonical profile key');
  if (!plain(manifest.settings) || !plain(manifest.components) || !Array.isArray(manifest.native_plugins)) fail('invalid profile manifest fields');
  const aliases = new Set();
  for (const type of TYPES) {
    const refs = manifest.components[type] ?? [];
    if (!Array.isArray(refs)) fail(`components.${type} must be an array`);
    const ids = new Set();
    for (const ref of refs) {
      if (!plain(ref) || !ID.test(ref.id ?? '') || ref.id.split('/').some(p => p === '.' || p === '..')) fail(`invalid ${type} component identity`);
      if (ids.has(ref.id)) fail(`${type}:${ref.id}: duplicate component`);
      ids.add(ref.id);
      if (Object.keys(ref).some(k => k !== 'id' && !(type === 'mcps' && k === 'env'))) fail(`${type}:${ref.id}: unsupported reference field`);
      if (ref.env !== undefined && !plain(ref.env)) fail(`mcps:${ref.id}: invalid environment mapping`);
      for (const [target, alias] of Object.entries(ref.env ?? {})) {
        if (!ENV_NAME.test(target) || !ALIAS.test(alias) || aliases.has(alias)) fail(`mcps:${ref.id}: invalid or reused transport alias`);
        aliases.add(alias);
      }
    }
  }
  if (Object.keys(manifest.components).some(k => !TYPES.includes(k))) fail('unsupported component category');
  for (const ref of manifest.native_plugins) if (typeof ref !== 'string' || !NATIVE.test(ref)) fail('invalid native plugin reference');
}

export async function inventory(directory, { links = false, ignore = () => false } = {}) {
  const files = [];
  async function walk(dir, prefix = '') {
    for (const entry of (await fs.readdir(dir, { withFileTypes: true })).sort((a, b) => a.name.localeCompare(b.name))) {
      const relative = safeRelative(prefix ? `${prefix}/${entry.name}` : entry.name);
      if (ignore(relative)) continue;
      const full = path.join(dir, entry.name);
      const stat = await fs.lstat(full);
      if (stat.isSymbolicLink()) {
        if (!links) fail(`unsupported artifact entry: ${relative}`);
        const resolved = await fs.realpath(full);
        if (!resolved.startsWith(`${path.resolve(directory)}/`)) fail(`artifact symlink escapes its root: ${relative}`);
        files.push({ path: relative, symlink: await fs.readlink(full) });
      } else if (!stat.isDirectory() && !stat.isFile()) fail(`unsupported artifact entry: ${relative}`);
      else if (stat.isDirectory()) await walk(full, relative);
      else {
        const bytes = await fs.readFile(full);
        files.push({ path: relative, size: bytes.length, sha256: hash(bytes), executable: Boolean(stat.mode & 0o111) });
      }
    }
  }
  await walk(directory);
  return files.sort((a, b) => a.path.localeCompare(b.path));
}
async function copyFiles(source, target, files) {
  files ??= await inventory(source);
  for (const file of files) {
    safeRelative(file.path);
    const dest = path.join(target, file.path);
    if (await exists(dest)) fail(`installed path collision: ${file.path}`);
    await fs.mkdir(path.dirname(dest), { recursive: true });
    if (file.symlink !== undefined) await fs.symlink(file.symlink, dest);
    else {
      await fs.copyFile(path.join(source, file.path), dest);
      await fs.chmod(dest, file.executable ? 0o755 : 0o644);
    }
  }
}

async function fetchJSON(url) {
  const response = await fetch(url, { headers: { Accept: 'application/vnd.github+json', 'User-Agent': 'agent-runtime' }, signal: AbortSignal.timeout(45000) });
  if (!response.ok) fail(`catalog request failed (HTTP ${response.status})`);
  return response.json();
}
export async function fetchCatalog() {
  const commit = await fetchJSON(`https://api.github.com/repos/${REPO}/commits/main`);
  if (!/^[0-9a-f]{40}$/.test(commit.sha ?? '')) fail('catalog returned an invalid commit');
  const tree = await fetchJSON(`https://api.github.com/repos/${REPO}/git/trees/${commit.sha}?recursive=1`);
  if (tree.truncated || !Array.isArray(tree.tree)) fail('catalog source inventory is incomplete');
  return { commit: commit.sha, tree: tree.tree };
}
function componentPaths(type, id) {
  const base = id.split('/').at(-1);
  if (['agents', 'commands'].includes(type) && id.split('/').length > 2) fail(`${type}:${id}: upstream installer cannot preserve this path`);
  if (type === 'agents' || type === 'commands') return { source: `cli-tool/components/${type}/${id}.md`, target: `.claude/${type}/${base}.md`, directory: false };
  if (type === 'mcps') return { source: `cli-tool/components/mcps/${id}.json`, target: '.mcp.json', directory: false };
  return { source: `cli-tool/components/${type}/${id}`, target: `.claude/skills/${base}`, directory: true };
}
export function expectedInventory(catalog, type, id) {
  if (type === 'plugins') fail(`plugins:${id}: no supported catalog plugin installer or resolvable entry`);
  const p = componentPaths(type, id);
  const entries = catalog.tree.filter(e => p.directory ? e.path.startsWith(`${p.source}/`) : e.path === p.source).filter(e => e.type !== 'tree');
  if (!entries.length) fail(`${type}:${id}: unknown catalog identity`);
  return entries.map(entry => {
    if (entry.type !== 'blob' || !['100644', '100755'].includes(entry.mode)) fail(`${type}:${id}: unsupported source entry`);
    const suffix = p.directory ? entry.path.slice(p.source.length + 1) : '';
    if (suffix) safeRelative(suffix);
    return { path: p.directory ? `${p.target}/${suffix}` : p.target, source: entry.path, gitSha: entry.sha, size: entry.size, mode: entry.mode };
  }).sort((a, b) => a.path.localeCompare(b.path));
}
function gitHash(bytes) { return createHash('sha1').update(`blob ${bytes.length}\0`).update(bytes).digest('hex'); }
function mcpProjection(value) {
  const result = structuredClone(value);
  if (!plain(result) || !plain(result.mcpServers) || !Object.keys(result.mcpServers).length) fail('MCP template must declare servers');
  for (const server of Object.values(result.mcpServers)) { if (!plain(server)) fail('invalid MCP server'); delete server.description; }
  return result;
}
export async function verifyCatalogOutput(directory, expected, type, sourceBytes) {
  const actual = await inventory(directory);
  if (stable(actual.map(f => f.path).sort()) !== stable(expected.map(f => f.path).sort())) fail('installed inventory differs from complete source inventory');
  for (const file of expected) {
    const bytes = await fs.readFile(path.join(directory, file.path));
    if (type === 'mcps') {
      if (!sourceBytes || gitHash(sourceBytes) !== file.gitSha) fail('MCP source content does not match inventory');
      let source, installed;
      try { source = JSON.parse(sourceBytes); installed = JSON.parse(bytes); } catch { fail('invalid MCP JSON'); }
      if (stable(installed) !== stable(mcpProjection(source))) fail('installed MCP differs from the expected source projection');
    } else if (gitHash(bytes) !== file.gitSha) fail(`installed resource differs from source: ${file.path}`);
  }
  // The pinned installer loses executable modes (and invents them for .py/.sh).
  // Only after every source byte matches may we restore the Git file modes.
  for (const file of expected) {
    if (!['100644', '100755'].includes(file.mode)) fail('source resource mode is missing');
    const mode = file.mode === '100755' ? 0o755 : 0o644;
    const target = path.join(directory, file.path);
    await fs.chmod(target, mode);
    if (((await fs.stat(target)).mode & 0o777) !== mode) fail(`source resource mode could not be restored: ${file.path}`);
  }
}
async function executable(name, env = process.env) {
  const candidates = name.includes('/') ? [name] : (env.PATH ?? '').split(path.delimiter).map(p => path.join(p, name));
  for (const candidate of candidates) { try { await fs.access(candidate, fs.constants.X_OK); return candidate; } catch {} }
  fail(`required executable unavailable: ${path.basename(name)}`);
}
async function defaultYAML(text) {
  const cli = await fs.realpath(await executable('claude-code-templates'));
  const require = createRequire(cli);
  return require('js-yaml').load(text);
}
async function frontmatter(file, parseYAML) {
  const text = await fs.readFile(file, 'utf8');
  const match = /^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/.exec(text);
  if (!match) fail(`missing YAML frontmatter: ${path.basename(file)}`);
  let data;
  try { data = await parseYAML(match[1]); } catch { fail(`invalid YAML frontmatter: ${path.basename(file)}`); }
  if (!plain(data)) fail(`invalid frontmatter: ${path.basename(file)}`);
  return data;
}
async function validateComponent(directory, type, id, deps) {
  const p = componentPaths(type, id);
  if (type === 'agents') {
    const data = await frontmatter(path.join(directory, p.target), deps.parseYAML);
    if (typeof data.name !== 'string' || !/^[a-zA-Z0-9][a-zA-Z0-9_-]*$/.test(data.name) || typeof data.description !== 'string' || !data.description.trim()) fail('agent requires a valid name and description');
  } else if (type === 'skills') {
    const data = await frontmatter(path.join(directory, p.target, 'SKILL.md'), deps.parseYAML);
    if (typeof data.description !== 'string' || !data.description.trim()) fail('skill requires a description');
  } else if (type === 'commands') {
    const text = await fs.readFile(path.join(directory, p.target), 'utf8');
    if (!text.trim()) fail('empty command');
    // Claude 2.1.278 loads command bodies whose optional argument-hint uses
    // unquoted bracket/pipe syntax rejected by js-yaml. Preserve source bytes
    // and let the native command loader interpret optional metadata.
  } else if (type === 'mods') {
    await validatePlugin(path.join(directory, p.target));
    const hooks = await readJSON(path.join(directory, p.target, 'hooks/hooks.json'));
    if (!Array.isArray(hooks.modules) || !hooks.modules.length) fail('mod requires hook modules');
    for (const module of hooks.modules) {
      if (typeof module !== 'string') fail('invalid mod hook module');
      const rel = safeRelative(module.replace(/^\.\//, ''));
      if (!(await exists(path.join(directory, p.target, 'hooks', rel)))) fail('mod hook module is missing');
    }
  } else if (type === 'mcps') mcpProjection(await readJSON(path.join(directory, p.target)));
}
async function validatePlugin(directory) {
  const manifest = await readJSON(path.join(directory, '.claude-plugin/plugin.json'));
  if (typeof manifest.name !== 'string' || !/^[a-zA-Z0-9][a-zA-Z0-9_-]*$/.test(manifest.name)) fail('plugin requires a valid manifest name');
  for (const field of ['commands', 'agents', 'skills', 'hooks', 'mcpServers', 'lspServers']) {
    const values = typeof manifest[field] === 'string' ? [manifest[field]] : Array.isArray(manifest[field]) ? manifest[field] : [];
    for (const value of values) {
      if (typeof value !== 'string') fail(`invalid plugin ${field} path`);
      const rel = safeRelative(value.replace(/^\.\//, ''));
      if (!(await exists(path.join(directory, rel)))) fail(`plugin ${field} resource is missing`);
    }
  }
  return manifest;
}
export async function installerPackageVersion(binary) {
  const real = await fs.realpath(binary ?? await executable('claude-code-templates'));
  let directory = path.dirname(real);
  let version;
  while (directory !== path.dirname(directory)) {
    const pkg = await readJSON(path.join(directory, 'package.json'), null);
    const bins = typeof pkg?.bin === 'string' ? [pkg.bin] : Object.values(pkg?.bin ?? {});
    if (pkg?.name === 'claude-code-templates' && bins.some(bin => typeof bin === 'string' && path.resolve(directory, bin) === real)) version = pkg.version;
    directory = path.dirname(directory);
  }
  if (typeof version !== 'string') fail('cannot verify installed claude-code-templates package metadata');
  return version;
}
async function installCatalog(type, id, directory, deps) {
  // Published 1.29.6 retains an inner cli-tool/package.json at 1.29.4;
  // its --version banner is not the installed npm distribution version.
  const version = await installerPackageVersion();
  if (version !== INSTALLER_VERSION) fail(`requires claude-code-templates ${INSTALLER_VERSION}`);
  await deps.run('claude-code-templates', [FLAGS[type], id, '--directory', directory, '--yes'], { cwd: directory });
}

export async function verifyNativeSource(root, entry, marketplaceDir, registration) {
  const actual = (await inventory(root, { links: true })).filter(file => file.path !== '.in_use');
  const source = entry.source;
  if (typeof source === 'string' && source.startsWith('./')) {
    const expectedRoot = path.join(marketplaceDir, safeRelative(source.slice(2)));
    const expected = (await inventory(expectedRoot, { links: true })).filter(file => file.path !== '.in_use');
    const content = files => files.map(({ path, sha256, size, symlink, executable }) => ({ path, sha256, size, symlink, executable }));
    if (stable(content(actual)) !== stable(content(expected))) fail('native plugin differs from complete marketplace source inventory');
    return { kind: 'marketplace-directory', path: source, commit: registration.gitCommitSha ?? null };
  }
  let repo, prefix = '';
  if (plain(source) && source.source === 'github') repo = source.repo;
  if (plain(source) && ['url', 'git-subdir'].includes(source.source)) {
    const match = /^https:\/\/github\.com\/([\w.-]+\/[\w.-]+?)(?:\.git)?\/?$/.exec(source.url ?? '');
    repo = match?.[1];
    if (source.source === 'git-subdir') prefix = `${safeRelative(source.path ?? '')}/`;
  }
  const commit = registration.gitCommitSha ?? source?.sha;
  if (!/^[\w.-]+\/[\w.-]+$/.test(repo ?? '') || !/^[a-f0-9]{40}$/.test(commit ?? '')) fail('native plugin source has no verifiable GitHub inventory');
  const tree = await fetchJSON(`https://api.github.com/repos/${repo}/git/trees/${commit}?recursive=1`);
  if (tree.truncated || !Array.isArray(tree.tree)) fail('native plugin source inventory is incomplete');
  const expected = tree.tree.filter(file => file.type !== 'tree' && file.path.startsWith(prefix)).map(file => ({ ...file, path: file.path.slice(prefix.length) }));
  if (stable(actual.map(f => f.path).sort()) !== stable(expected.map(f => f.path).sort())) fail('native plugin differs from complete GitHub source inventory');
  for (const file of expected) {
    safeRelative(file.path);
    if (file.type !== 'blob' || !['100644', '100755', '120000'].includes(file.mode)) fail('unsupported native plugin source entry');
    const bytes = file.mode === '120000' ? Buffer.from(await fs.readlink(path.join(root, file.path))) : await fs.readFile(path.join(root, file.path));
    if (gitHash(bytes) !== file.sha) fail(`native plugin resource differs from source: ${file.path}`);
    if (file.mode !== '120000' && Boolean((await fs.stat(path.join(root, file.path))).mode & 0o111) !== (file.mode === '100755')) fail(`native plugin resource mode differs from source: ${file.path}`);
  }
  return { kind: 'github', repo, commit, prefix };
}

// Native installation remains native. All installation state is private, and
// the complete native registry/payload closure becomes one retained artifact.
async function installNative(ref, directory, baseline, deps) {
  const [, marketplace] = ref.split('@');
  const known = await readJSON(path.join(baseline, 'plugins/known_marketplaces.json'), {});
  const source = known[marketplace]?.source;
  if (!plain(source)) fail(`native plugin ${ref}: marketplace is not in the image baseline`);
  let location;
  if (source.source === 'github' && /^[\w.-]+\/[\w.-]+$/.test(source.repo ?? '')) location = source.repo;
  else if (source.source === 'directory') location = path.join(baseline, 'plugins/marketplaces', marketplace);
  else if (source.source === 'git' && typeof source.url === 'string' && /^https:\/\//.test(source.url) && !new URL(source.url).username && !new URL(source.url).password) location = source.url;
  else fail(`native plugin ${ref}: unsupported baseline marketplace source`);
  const home = `${directory}-native`;
  await fs.mkdir(home, { recursive: true });
  const env = { ...cleanEnvironment(), CLAUDE_CONFIG_DIR: home, DISABLE_AUTOUPDATER: '1' };
  await writeJSON(path.join(home, '.claude.json'), { hasCompletedOnboarding: true, hasTrustDialogAccepted: true });
  await deps.run('claude', ['plugin', 'marketplace', 'add', location], { cwd: home, env });
  await deps.run('claude', ['plugin', 'install', ref, '--scope', 'user'], { cwd: home, env });
  const registry = await readJSON(path.join(home, 'plugins/installed_plugins.json'));
  const sources = await readJSON(path.join(home, 'plugins/known_marketplaces.json'));
  if (!Array.isArray(registry.plugins?.[ref]) || !registry.plugins[ref].length) fail(`native plugin ${ref}: native registration missing`);
  const plugins = [];
  for (const [identity, installs] of Object.entries(registry.plugins)) {
    if (!NATIVE.test(identity) || !Array.isArray(installs) || installs.length !== 1) fail('unsupported native plugin registration');
    const record = installs[0];
    const [name, market] = identity.split('@');
    const root = record.installPath;
    if (typeof root !== 'string' || !path.resolve(root).startsWith(`${path.resolve(home)}/`)) fail('native plugin escaped isolated installation');
    const manifest = await validatePlugin(root);
    await deps.run('claude', ['plugin', 'validate', root], { cwd: home, env });
    const mkt = sources[market];
    if (!plain(mkt?.source)) fail('native marketplace metadata missing');
    const catalog = await readJSON(path.join(mkt.installLocation, '.claude-plugin/marketplace.json'));
    const entry = catalog.plugins?.find(p => p.name === name);
    if (!entry) fail(`native plugin ${identity}: marketplace entry missing`);
    const provenance = await verifyNativeSource(root, entry, mkt.installLocation, record);
    const target = `payloads/${plugins.length}`;
    await copyFiles(root, path.join(directory, target), (await inventory(root, { links: true })).filter(file => file.path !== '.in_use'));
    plugins.push({ ref: identity, name: manifest.name, version: String(record.version ?? manifest.version ?? 'captured'), directory: target, source: mkt.source, provenance, marketplace: { name: market, owner: catalog.owner, entry } });
  }
  await writeJSON(path.join(directory, 'native.json'), { version: 1, plugins });
  await fs.rm(home, { recursive: true, force: true });
}

function expandTemplate(value, bindings, env, label) {
  if (Array.isArray(value)) return value.map(v => expandTemplate(v, bindings, env, label));
  if (plain(value)) return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, expandTemplate(v, bindings, env, label)]));
  if (typeof value !== 'string') return value;
  const replace = name => {
    const alias = bindings[name];
    if (!alias || !ALIAS.test(alias) || !env[alias]) fail(`${label}: unresolved required environment binding ${name}`);
    return '${' + alias + '}';
  };
  let result = value.replace(/\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}/g, (all, name, fallback) => {
    if (ALIAS.test(name) && Object.values(bindings).includes(name) && env[name]) return all;
    if (bindings[name]) return replace(name);
    if (fallback !== undefined) return fallback;
    fail(`${label}: unresolved required environment binding ${name}`);
  });
  result = result.replace(/<(?:YOUR_)?([A-Z][A-Z0-9_]*)>/g, (_, name) => replace(name));
  if (/<[^>]+>|\$\{|\bYOUR_[A-Z][A-Z0-9_]*\b/.test(result.replace(/\$\{AIRUN_COMPONENT_ENV_[0-9]+\}/g, ''))) fail(`${label}: unsupported or unresolved placeholder`);
  return result;
}
export async function renderMCP(template, ref, env, checkRuntime = async () => {}) {
  const label = `mcps:${ref.id}`;
  const bindings = ref.env ?? {};
  for (const [target, alias] of Object.entries(bindings)) if (!ENV_NAME.test(target) || !ALIAS.test(alias) || !env[alias]) fail(`${label}: missing environment binding ${target}`);
  const projected = mcpProjection(template);
  const servers = {};
  for (const [name, original] of Object.entries(projected.mcpServers)) {
    if (!/^[a-zA-Z0-9][a-zA-Z0-9_.-]*$/.test(name)) fail(`${label}: invalid server name`);
    const server = structuredClone(original);
    if (server.env !== undefined && !plain(server.env)) fail(`${label}: invalid server environment`);
    server.env = { ...server.env };
    // Bind by target key even when the catalog uses a generic <YOUR_TOKEN> label.
    for (const [target, alias] of Object.entries(bindings)) server.env[target] = '${' + alias + '}';
    const rendered = expandTemplate(server, bindings, env, label);
    if (Object.entries(rendered.env).some(([key, value]) => !ENV_NAME.test(key) || typeof value !== 'string')) fail(`${label}: invalid server environment`);
    if (rendered.headers !== undefined && (!plain(rendered.headers) || Object.values(rendered.headers).some(value => typeof value !== 'string'))) fail(`${label}: invalid server headers`);
    if (rendered.command !== undefined) {
      if (typeof rendered.command !== 'string' || !rendered.command || rendered.command.includes('${')) fail(`${label}: unsupported command`);
      if ((rendered.type !== undefined && rendered.type !== 'stdio') || rendered.url !== undefined || rendered.headers !== undefined) fail(`${label}: unsupported server transport`);
      if (rendered.args !== undefined && (!Array.isArray(rendered.args) || rendered.args.some(a => typeof a !== 'string'))) fail(`${label}: invalid command arguments`);
      await checkRuntime(rendered, label);
    } else {
      if (!['http', 'sse', 'ws'].includes(rendered.type) || typeof rendered.url !== 'string' || !rendered.url.trim() || rendered.args !== undefined) fail(`${label}: unsupported server transport`);
      let url;
      try { url = new URL(resolveAliases(rendered.url, env)); } catch { fail(`${label}: invalid server URL`); }
      if (!(rendered.type === 'ws' ? ['ws:', 'wss:'] : ['http:', 'https:']).includes(url.protocol)) fail(`${label}: invalid server URL`);
    }
    servers[name] = rendered;
  }
  return servers;
}

function resolveAliases(value, env) {
  return value.replace(/\$\{(AIRUN_COMPONENT_ENV_[0-9]+)\}/g, (_, alias) => env[alias] ?? '');
}
export async function checkRuntime(server, label, { workspace = '/workspace', action = 'prepare', env = process.env, run = runCommand } = {}) {
  const childEnv = { ...cleanEnvironment(), ...Object.fromEntries(Object.entries(server.env ?? {}).map(([key, value]) => [key, resolveAliases(value, env)])) };
  const command = await executable(server.command, childEnv);
  const name = path.basename(server.command);
  const python = /^python(?:[23](?:\.\d+)?)?$/.test(name);
  const node = name === 'node', shell = name === 'bash' || name === 'sh';
  if (!python && !node && !shell) return;
  const args = (server.args ?? []).map(value => resolveAliases(value, env));
  let script;
  for (let i = 0; i < args.length; i++) {
    const arg = args[i];
    if (arg === '--') { script = args[i + 1]; break; }
    if (!arg.startsWith('-') || arg === '-') { script = arg; break; }
    if ((python && arg === '-c') || (node && ['-e', '--eval', '-p', '--print'].includes(arg)) || (shell && /^-[a-z]*c[a-z]*$/.test(arg))) {
      if (args[i + 1] === undefined) fail(`${label}: runtime inline program is missing`);
      return;
    }
    if (python && arg === '-m') {
      const module = args[i + 1];
      if (!/^[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*$/.test(module ?? '')) fail(`${label}: unsupported Python module invocation`);
      // Probe availability without executing the requested module or logging
      // its arguments/environment. Updates still require installed modules.
      try {
        await run(command, [...args.slice(0, i), '-c', 'import importlib.util,sys; n=sys.argv[1]; s=importlib.util.find_spec(n); s=importlib.util.find_spec(n+".__main__") if s is not None and s.submodule_search_locations is not None else s; sys.exit(0 if s is not None and s.loader is not None else 1)', module], { cwd: action === 'prepare' && await exists(workspace) ? workspace : undefined, env: childEnv });
      } catch { fail(`${label}: Python module prerequisite is unavailable`); }
      return;
    }
    if ((python && /^-[bBdEiIOPqRsSuUvVx]+$/.test(arg)) || (node && ['--no-warnings', '--enable-source-maps', '--experimental-strip-types'].includes(arg)) || (shell && /^-[a-z]+$/.test(arg) && !arg.includes('o'))) continue;
    fail(`${label}: unsupported interpreter invocation`);
  }
  if (!script || script === '-') fail(`${label}: runtime script is unavailable`);
  const file = path.isAbsolute(script) ? path.normalize(script) : path.resolve(workspace, script);
  const inWorkspace = file === path.resolve(workspace) || file.startsWith(`${path.resolve(workspace)}/`);
  if (action === 'update' && (!path.isAbsolute(script) || inWorkspace)) return;
  try { if ((await fs.stat(file)).isFile()) return; } catch {}
  fail(`${label}: runtime script is unavailable`);
}

function npxPackage(server) {
  if (path.basename(server.command ?? '') !== 'npx') return null;
  const args = server.args ?? [];
  let index = 0;
  while (args[index] === '-y' || args[index] === '--yes') index++;
  const spec = args[index];
  if (typeof spec !== 'string' || !/^(?:@[a-zA-Z0-9][a-zA-Z0-9_.-]*\/)?[a-zA-Z0-9][a-zA-Z0-9_.-]*(?:@[a-zA-Z0-9^~*][a-zA-Z0-9_.^~*-]*)?$/.test(spec)) fail('unsupported npx package invocation');
  const name = spec.replace(/@[^/@]+$/, '');
  return { spec, name, skip: index + 1 };
}
async function provisionNpm(spec, name, directory, deps) {
  await deps.run('npm', ['install', '--prefix', directory, '--ignore-scripts', '--no-audit', '--no-fund', '--save-exact', spec], { cwd: directory });
  const packageDir = path.join(directory, 'node_modules', name);
  const pkg = await readJSON(path.join(packageDir, 'package.json'));
  const bins = typeof pkg.bin === 'string' ? [pkg.bin] : plain(pkg.bin) ? Object.values(pkg.bin) : [];
  if (bins.length !== 1 || typeof bins[0] !== 'string') fail('MCP npm package needs one unambiguous executable');
  const command = await fs.realpath(path.join(packageDir, safeRelative(bins[0].replace(/^\.\//, ''))));
  if (!command.startsWith(`${directory}/`)) fail('MCP executable escapes installed dependencies');
  await executable(command);
  return command;
}
async function prepareRuntimes(componentDir, cache, deps, allocations) {
  const template = mcpProjection(await readJSON(path.join(componentDir, '.mcp.json')));
  const runtimes = {};
  for (const [server, config] of Object.entries(template.mcpServers)) {
    const pkg = npxPackage(config);
    if (!pkg) continue;
    const parent = path.join(cache, 'runtimes');
    await fs.mkdir(parent, { recursive: true });
    const directory = await fs.mkdtemp(path.join(parent, 'npm-'));
    allocations.add(directory);
    try {
      const command = await deps.provisionNpm(pkg.spec, pkg.name, directory);
      const files = await inventory(directory, { links: true });
      runtimes[server] = { directory, command, skip: pkg.skip, digest: hash(stable(files)), inventory: files };
    } catch (e) { await fs.rm(directory, { recursive: true, force: true }); throw e; }
  }
  return runtimes;
}
async function verifyRuntimes(runtimes, cache) {
  for (const runtime of Object.values(runtimes ?? {})) {
    if (typeof runtime.directory !== 'string' || !runtime.directory.startsWith(`${path.join(cache, 'runtimes')}/`) || !runtime.command?.startsWith(`${runtime.directory}/`)) fail('invalid retained runtime path');
    const files = await inventory(runtime.directory, { links: true });
    if (stable(files) !== stable(runtime.inventory) || hash(stable(files)) !== runtime.digest) fail('retained MCP dependencies are corrupt');
  }
}

async function verifyBaseline(baseline) {
  const receipt = await readJSON(path.join(baseline, 'inventory.json'));
  if (receipt.version !== 1 || !Array.isArray(receipt.files)) fail('invalid image baseline inventory');
  const actual = await inventory(baseline, { links: true, ignore: name => name === 'inventory.json' || name.split('/').includes('.git') });
  if (stable(actual.map(file => file.path).sort()) !== stable(receipt.files.map(file => file.path).sort())) fail('image baseline inventory differs from captured files');
  for (const record of receipt.files) {
    const file = path.join(baseline, safeRelative(record.path));
    const stat = await fs.lstat(file);
    if (record.symlink !== undefined) {
      if (!stat.isSymbolicLink() || await fs.readlink(file) !== record.symlink || !(await fs.realpath(file)).startsWith(`${baseline}/`)) fail(`image baseline symlink changed: ${record.path}`);
    } else {
      if (!stat.isFile() || stat.size !== record.size || hash(await fs.readFile(file)) !== record.sha256 || (stat.mode & 0o777) !== record.mode) fail(`image baseline changed: ${record.path}`);
    }
  }
}

function merge(a, b) {
  const result = structuredClone(a);
  for (const [k, v] of Object.entries(b)) {
    if (['__proto__', 'constructor', 'prototype'].includes(k)) fail('unsupported configuration key');
    result[k] = plain(v) && plain(result[k]) ? merge(result[k], v) : structuredClone(v);
  }
  return result;
}
async function scanAgents(root, parseYAML, identities, prefix = '', seen = new Set()) {
  if (!(await exists(root))) return;
  const directory = (await fs.stat(root)).isDirectory();
  for (const file of directory ? await inventory(root) : [{ path: path.basename(root) }]) {
    if (!file.path.endsWith('.md')) continue;
    const full = directory ? path.join(root, file.path) : root;
    const real = await fs.realpath(full);
    if (seen.has(real)) continue;
    seen.add(real);
    const data = await frontmatter(full, parseYAML);
    if (typeof data.name !== 'string' || !data.name) fail('installed agent has no name');
    const folders = directory && path.dirname(file.path) !== '.' ? `${path.dirname(file.path).split(path.sep).join(':')}:` : '';
    const name = prefix ? `${prefix}:${folders}${data.name}` : data.name;
    if (identities.has(name)) fail(`installed agent identity collision: ${name}`);
    identities.add(name);
  }
}
async function plainComponents(root, origin, names, skillNames = names) {
  const components = [];
  const wanted = name => !names || names.has(name);
  const add = (kind, name, file, unsafe = false) => components.push({ kind, name, file, root, origin, unsafe, relative: path.relative(root, file) });
  // Never follow a repository root/category/directory link to discover a
  // collision. Only matching managed names make such a link our concern.
  async function directory(file, kinds, prefix = '') {
    if (!(await exists(file))) return false;
    const stat = await fs.lstat(file);
    if (!stat.isSymbolicLink()) return stat.isDirectory();
    // Baseline links have already passed the complete baseline inventory check.
    if (!names) return (await fs.stat(file)).isDirectory();
    for (const name of names) if (name.startsWith(prefix)) for (const kind of kinds) add(kind, name, file, true);
    return false;
  }
  if (!(await directory(root, ['skills', 'commands']))) return components;
  const skills = path.join(root, 'skills');
  if (await directory(skills, ['skills'])) for (const entry of await fs.readdir(skills, { withFileTypes: true })) {
    if (!wanted(entry.name)) continue;
    const file = path.join(skills, entry.name);
    if (entry.isSymbolicLink() && names) add('skills', entry.name, file, true);
    // Plugin wrappers use plugin namespaces, not plain skill invocations.
    else if (entry.isDirectory() || (entry.isSymbolicLink() && (await fs.stat(file)).isDirectory())) {
      const plugin = await exists(path.join(file, '.claude-plugin/plugin.json'));
      if (skillNames?.has(entry.name) || (await exists(path.join(file, 'SKILL.md')) && !plugin)) add(plugin ? 'mods' : 'skills', entry.name, file);
    } else if (skillNames?.has(entry.name)) add('skills', entry.name, file);
  }
  async function commands(dir, prefix = '', ancestors = new Set()) {
    if (!(await directory(dir, ['commands'], prefix))) return;
    const real = await fs.realpath(dir);
    if (ancestors.has(real)) return;
    const parents = new Set([...ancestors, real]);
    for (const entry of await fs.readdir(dir, { withFileTypes: true })) {
      const name = prefix + entry.name;
      const file = path.join(dir, entry.name);
      if (entry.name.endsWith('.md') && wanted(name.slice(0, -3)) && (names || !entry.isDirectory())) add('commands', name.slice(0, -3), file, Boolean(names) && entry.isSymbolicLink());
      if ((entry.isDirectory() || (entry.isSymbolicLink() && !entry.name.endsWith('.md'))) && (!names || [...names].some(value => value.startsWith(`${name}:`)))) await commands(file, `${name}:`, parents);
    }
  }
  await commands(path.join(root, 'commands'));
  return components;
}
function componentSource(component) {
  return `${component.kind} ${component.origin}: ${component.file}${component.source ? ` (source ${component.source})` : ''}`;
}
async function componentInventory(component) {
  if (component.unsafe) fail('unsafe component symlink');
  if (!component.files) {
    if (component.kind === 'skills') component.files = await inventory(await fs.realpath(component.file), { links: true });
    else {
      // Read only this command, and reject special entries before readFile can
      // block on a FIFO. Sibling names are outside this component's inventory.
      const stat = await fs.lstat(component.file);
      if (!stat.isFile()) fail('unsupported command entry');
      const bytes = await fs.readFile(component.file);
      component.files = [{ path: 'command.md', size: bytes.length, sha256: hash(bytes), executable: Boolean(stat.mode & 0o111) }];
    }
    // Receipts retain their existing Boolean executable field. Equivalence
    // additionally compares the permissions that the effective copy will have:
    // managed files are normalized by copyFiles; repository files stay in place.
    component.files = await Promise.all(component.files.map(async file => file.symlink !== undefined ? file : {
      ...file, executableMode: component.origin === 'repository'
        ? (await fs.stat(component.kind === 'skills' ? path.join(component.file, file.path) : component.file)).mode & 0o111
        : file.executable ? 0o111 : 0
    }));
  }
  return component.files;
}
async function selectPlainComponents(managed, repositoryRoot) {
  const selected = new Map();
  const repository = repositoryRoot ? await plainComponents(repositoryRoot, 'repository', new Set(managed.map(component => component.name)), new Set(managed.filter(component => component.kind === 'skills').map(component => component.name))) : [];
  const omitted = new Set();
  for (const component of [...managed, ...repository]) {
    const previous = selected.get(component.name);
    if (!previous) { selected.set(component.name, { effective: component, sources: [component] }); continue; }
    const conflict = reason => fail(`skill/command invocation collision: ${component.name}; ${[...previous.sources, component].map(componentSource).join('; ')}; ${reason}`);
    if (previous.effective.kind !== component.kind) conflict('different component kinds');
    let equal;
    try { equal = stable(await componentInventory(previous.effective)) === stable(await componentInventory(component)); }
    catch { conflict('cannot establish equivalence: unsafe or unreadable resource inventory'); }
    if (!equal) conflict('complete resource inventories differ');
    previous.sources.push(component);
    if (component.origin === 'repository') { omitted.add(previous.effective); previous.effective = component; }
    else omitted.add(component);
  }
  return omitted;
}
async function preflightSkillDirectories(managed, repositoryRoot) {
  const groups = new Map();
  for (const component of managed) {
    const group = groups.get(component.name) ?? [];
    group.push(component); groups.set(component.name, group);
  }
  for (const [name, group] of groups) {
    const wrapper = group.find(component => component.wrapper);
    if (!wrapper) continue;
    if (repositoryRoot) {
      // lstat each ancestor: wrapper checks must not read through workspace links.
      for (const file of [repositoryRoot, path.join(repositoryRoot, 'skills'), path.join(repositoryRoot, 'skills', name)]) {
        if (!(await exists(file))) break;
        const stat = await fs.lstat(file);
        if (stat.isSymbolicLink() || file === path.join(repositoryRoot, 'skills', name)) {
          const plugin = stat.isDirectory() && await exists(path.join(file, '.claude-plugin/plugin.json'));
          group.push({ kind: plugin ? 'mods' : 'skills', origin: 'repository', file });
          break;
        }
        if (!stat.isDirectory()) break;
      }
    }
    if (group.length > 1) fail(`${wrapper.id ? `${wrapper.kind}:${wrapper.id}: ` : ''}installed directory collision: ${name}; ${group.map(componentSource).join('; ')}`);
  }
}
async function copyBaselineComponents(baseline, target, components, omitted) {
  const all = await inventory(baseline, { links: true, ignore: name => name === 'inventory.json' || name.split('/').includes('.git') });
  const omissions = components.filter(component => omitted.has(component)).map(component => component.relative);
  const beneath = (file, root) => file === root || file.startsWith(`${root}/`);
  const discovered = file => ['agents', 'skills', 'commands'].some(root => beneath(file, root));
  const omittedPath = file => omissions.some(root => beneath(file, root));
  const privateTarget = file => !discovered(file) || omittedPath(file) || omissions.some(root => root.startsWith(`${file}/`));
  const destination = file => path.join(target, privateTarget(file) ? `airun-component-resources/${file}` : file);
  const queue = all.filter(file => discovered(file.path) && !omittedPath(file.path)).map(file => ({ file, relative: file.path, private: false }));
  const copied = new Set();
  const directories = new Set();
  for (let index = 0; index < queue.length; index++) {
    const entry = queue[index], file = entry.file;
    const dest = entry.private ? path.join(target, 'airun-component-resources', entry.relative) : path.join(target, entry.relative);
    if (copied.has(dest)) continue;
    copied.add(dest);
    let symlink = file.symlink;
    if (symlink !== undefined) {
      const resolved = path.relative(baseline, await fs.realpath(path.join(baseline, file.path)));
      const stat = await fs.stat(path.join(baseline, resolved));
      // A directory alias containing an omitted invocation needs a partial
      // materialized view; keeping the directory link would hide a duplicate.
      if (!entry.private && stat.isDirectory() && omissions.some(root => root.startsWith(`${entry.relative}/`))) {
        directories.add(dest);
        for (const child of all.filter(child => child.path.startsWith(`${resolved}/`))) {
          const relative = `${entry.relative}/${child.path.slice(resolved.length + 1)}`;
          if (!omittedPath(relative)) queue.push({ file: child, relative, private: false });
        }
        continue;
      }
      const resolvedDest = destination(resolved);
      symlink = path.relative(path.dirname(dest), resolvedDest) || '.';
      if (stat.isDirectory()) directories.add(resolvedDest);
      if (privateTarget(resolved)) {
        // Only incoming dependencies enter this non-discovered closure. All
        // links are rewritten to active paths; no input or receipt is changed.
        for (const child of all.filter(child => beneath(child.path, resolved))) queue.push({ file: child, relative: child.path, private: true });
      }
    }
    await copyFiles(path.dirname(path.join(baseline, file.path)), path.dirname(dest), [{ ...file, path: path.basename(dest), ...(symlink !== undefined ? { symlink } : {}) }]);
  }
  for (const directory of directories) await fs.mkdir(directory, { recursive: true });
}
async function baselineNative(baseline) {
  const meta = await readJSON(path.join(baseline, 'baseline.json'), { version: 1, native_plugins: [] });
  if (!Array.isArray(meta.native_plugins)) fail('invalid baseline plugin metadata');
  return new Set(meta.native_plugins);
}
async function repositoryDefinitions(workspace, parseYAML) {
  const agents = new Set(), mcps = new Set();
  const root = path.join(workspace, '.claude');
  async function visitAgents(directory) {
    if (!(await exists(directory))) return;
    for (const entry of await fs.readdir(directory, { withFileTypes: true })) {
      const file = path.join(directory, entry.name);
      if (entry.isDirectory()) await visitAgents(file);
      else if (entry.isFile() && entry.name.endsWith('.md')) {
        // Unrelated repository format problems remain the native loader's
        // responsibility. Only identities it can declare affect this contract.
        try {
          const data = await frontmatter(file, parseYAML);
          if (typeof data.name === 'string' && data.name) agents.add(data.name);
        } catch {}
      }
    }
  }
  await visitAgents(path.join(root, 'agents'));
  try {
    const config = JSON.parse(await fs.readFile(path.join(workspace, '.mcp.json'), 'utf8'));
    if (plain(config.mcpServers)) for (const name of Object.keys(config.mcpServers)) mcps.add(name);
  } catch {}
  return { agents, mcps, root };
}
async function addPluginToSeed(plugin, sourceDir, seed, config, state) {
  if (!NATIVE.test(plugin.ref)) fail('invalid captured native identity');
  const [name, market] = plugin.ref.split('@');
  if (state.refs.has(plugin.ref)) fail(`native plugin identity collision: ${plugin.ref}`);
  if (state.names.has(plugin.name)) fail(`plugin name collision: ${plugin.name}`);
  state.refs.add(plugin.ref); state.names.add(plugin.name);
  // Use a content-derived path, not an upstream mutable "latest" directory.
  const files = await inventory(sourceDir, { links: true });
  const version = hash(stable(files)).slice(0, 24);
  const relative = `cache/${market}/${name}/${version}`;
  await copyFiles(sourceDir, path.join(seed, relative), files);
  const known = state.known[market];
  if (known && stable(known.source) !== stable(plugin.source)) fail(`marketplace source collision: ${market}`);
  state.known[market] = { source: plugin.source, installLocation: path.join(seed, 'marketplaces', market), autoUpdate: false };
  state.catalogs[market] ??= { name: market, owner: plugin.marketplace.owner ?? { name: market }, plugins: [] };
  state.catalogs[market].plugins.push({ ...plugin.marketplace.entry, name, version });
  state.installed[plugin.ref] = [{ scope: 'user', installPath: path.join(seed, relative), version }];
  state.enabled[plugin.ref] = true;
  const manifest = await validatePlugin(sourceDir);
  const agentFiles = new Set();
  await scanAgents(path.join(sourceDir, 'agents'), state.parseYAML, state.agents, plugin.name, agentFiles);
  const agentPaths = typeof manifest.agents === 'string' ? [manifest.agents] : manifest.agents ?? [];
  for (const relative of agentPaths) await scanAgents(path.join(sourceDir, safeRelative(relative.replace(/^\.\//, ''))), state.parseYAML, state.agents, plugin.name, agentFiles);
  const mcp = await readJSON(path.join(sourceDir, '.mcp.json'), {});
  for (const server of Object.keys(mcp.mcpServers ?? mcp)) {
    if (state.mcpNames.has(server)) fail(`MCP server identity collision: ${server}`);
    state.mcpNames.add(server);
  }
}

async function activate(manifest, records, payloadDirectory, target, finalConfig, baseline, deps) {
  await fs.mkdir(target, { recursive: true });
  const defaults = await readJSON(path.join(baseline, 'settings.json'), {});
  for (const field of ['enabledPlugins', 'extraKnownMarketplaces']) if (Object.hasOwn(manifest.settings, field)) fail(`profile settings.${field} conflicts with adapter activation`);
  const settings = merge(defaults, manifest.settings);
  const launch = { args: [], env: {} };
  const agentNames = new Set();
  const repositoryRoot = deps.action === 'update' ? null : path.join(deps.workspace, '.claude');
  const baselineComponents = await plainComponents(baseline, 'image baseline');
  const catalogComponents = [];
  const skillDirectories = [];
  const baselineSkills = path.join(baseline, 'skills');
  if (await exists(baselineSkills)) for (const name of await fs.readdir(baselineSkills)) {
    const file = path.join(baselineSkills, name);
    const wrapper = (await fs.stat(file)).isDirectory() && await exists(path.join(file, '.claude-plugin/plugin.json'));
    skillDirectories.push({ kind: wrapper ? 'mods' : 'skills', name, file, origin: 'image baseline', wrapper });
  }
  for (const kind of ['skills', 'commands', 'mods']) for (const ref of manifest.components[kind] ?? []) {
    const root = payloadDirectory(records[`${kind}:${ref.id}`]);
    const location = componentPaths(kind, ref.id);
    const file = path.join(root, location.target);
    const wrapper = kind === 'mods' || (kind === 'skills' && await exists(path.join(file, '.claude-plugin/plugin.json')));
    const component = { kind, name: ref.id.split('/').at(-1), file, root: path.join(root, '.claude'), relative: location.target.slice('.claude/'.length), origin: `catalog ${kind}:${ref.id}`, source: location.source, id: ref.id, wrapper };
    if (kind !== 'commands') skillDirectories.push(component);
    if (!wrapper) catalogComponents.push(component);
  }
  // Wrappers never participate in plain equivalence, including when an equal
  // repository copy would otherwise omit a conflicting managed skill path.
  await preflightSkillDirectories(skillDirectories, repositoryRoot);
  const omitted = await selectPlainComponents([...baselineComponents, ...catalogComponents], repositoryRoot);
  const repository = repositoryRoot ? await repositoryDefinitions(deps.workspace, deps.parseYAML) : { agents: new Set(), mcps: new Set() };
  const mcpNames = new Set(repository.mcps);
  await copyBaselineComponents(baseline, target, baselineComponents, omitted);
  const hosts = deps.env.AIRUN_HOST_AGENTS;
  if (hosts && await exists(hosts)) await copyFiles(hosts, path.join(target, 'agents'));
  const seed = path.join(target, 'airun-plugin-seed');
  const finalSeed = path.join(finalConfig, 'airun-plugin-seed');
  const plugins = { refs: new Set(), names: new Set(), known: {}, catalogs: {}, installed: {}, enabled: {}, parseYAML: deps.parseYAML, agents: agentNames, mcpNames };
  const bases = await baselineNative(baseline);
  if (bases.size) {
    const registry = await readJSON(path.join(baseline, 'plugins/installed_plugins.json'));
    const known = await readJSON(path.join(baseline, 'plugins/known_marketplaces.json'));
    for (const ref of bases) {
      const [name, market] = ref.split('@');
      const installation = registry.plugins?.[ref]?.[0];
      if (!installation) fail(`baseline plugin is unavailable: ${ref}`);
      // Build-time absolute installPath is intentionally not trusted at runtime.
      const suffix = installation.installPath?.split('/plugins/cache/')[1];
      if (!suffix) fail(`baseline plugin cache path is invalid: ${ref}`);
      const root = path.join(baseline, 'plugins/cache', safeRelative(suffix));
      const metadata = await validatePlugin(root);
      const catalog = await readJSON(path.join(baseline, 'plugins/marketplaces', market, '.claude-plugin/marketplace.json'));
      const entry = catalog.plugins?.find(p => p.name === name);
      if (!entry || !known[market]?.source) fail(`baseline marketplace entry missing: ${ref}`);
      await addPluginToSeed({ ref, name: metadata.name, source: known[market].source, marketplace: { owner: catalog.owner, entry } }, root, seed, finalConfig, plugins);
    }
  }
  const mcps = {};
  for (const type of TYPES) for (const ref of manifest.components[type] ?? []) {
    const record = records[`${type}:${ref.id}`];
    const root = payloadDirectory(record);
    if (type === 'mcps') {
      const rendered = await renderMCP(await readJSON(path.join(root, '.mcp.json')), ref, deps.env, deps.checkRuntime);
      for (const [name, server] of Object.entries(rendered)) {
        const runtime = record.runtimes?.[name];
        if (runtime) { server.command = runtime.command; server.args = (server.args ?? []).slice(runtime.skip); }
        if (mcpNames.has(name)) fail(`mcps:${ref.id}: duplicate MCP server ${name}`);
        mcpNames.add(name); mcps[name] = server;
      }
    } else {
      const component = catalogComponents.find(component => component.kind === type && component.id === ref.id);
      if (component && omitted.has(component)) continue;
      if (['skills', 'mods'].includes(type) && await exists(path.join(target, 'skills', ref.id.split('/').at(-1)))) fail(`${type}:${ref.id}: installed directory collision`);
      if (type === 'mods') {
        const version = await deps.claudeVersion();
        if (!versionAtLeast(version, '2.1.259')) fail(`mods:${ref.id}: Claude Code >=2.1.259 is required`);
        launch.env.CLAUDE_CODE_ENABLE_FUNCTION_HOOKS = '1';
        const mod = await validatePlugin(path.join(root, componentPaths(type, ref.id).target));
        if (plugins.names.has(mod.name)) fail(`mods:${ref.id}: plugin name collision`);
        plugins.names.add(mod.name);
        settings.enabledPlugins ??= {};
        settings.enabledPlugins[`${mod.name}@skills-dir`] = true;
      }
      await copyFiles(path.join(root, '.claude'), target);
    }
  }
  for (const ref of new Set(manifest.native_plugins)) {
    if (bases.has(ref)) continue;
    const root = payloadDirectory(records[`native:${ref}`]);
    const native = await readJSON(path.join(root, 'native.json'));
    for (const plugin of native.plugins) await addPluginToSeed(plugin, path.join(root, safeRelative(plugin.directory)), seed, finalConfig, plugins);
  }
  await scanAgents(path.join(target, 'agents'), deps.parseYAML, agentNames);
  for (const name of repository.agents) {
    if (agentNames.has(name)) fail(`repository agent identity collision: ${name}`);
    agentNames.add(name);
  }
  if (Object.keys(plugins.enabled).length) {
    // Rewrite staging paths before anything becomes visible to Claude.
    for (const record of Object.values(plugins.known)) record.installLocation = record.installLocation.replace(seed, finalSeed);
    for (const records of Object.values(plugins.installed)) for (const record of records) record.installPath = record.installPath.replace(seed, finalSeed);
    await writeJSON(path.join(seed, 'known_marketplaces.json'), plugins.known);
    await writeJSON(path.join(seed, 'installed_plugins.json'), { version: 2, plugins: plugins.installed });
    for (const [name, catalog] of Object.entries(plugins.catalogs)) await writeJSON(path.join(seed, 'marketplaces', name, '.claude-plugin/marketplace.json'), catalog);
    await writeJSON(path.join(target, 'plugins/installed_plugins.json'), { version: 2, plugins: plugins.installed });
    settings.enabledPlugins = { ...settings.enabledPlugins, ...plugins.enabled };
    settings.extraKnownMarketplaces = Object.fromEntries(Object.entries(plugins.known).map(([name, entry]) => [name, { source: entry.source, autoUpdate: false }]));
    launch.env.CLAUDE_CODE_PLUGIN_SEED_DIR = finalSeed;
  }
  if (Object.keys(mcps).length) {
    await writeJSON(path.join(target, 'airun-mcp.json'), { mcpServers: mcps });
    launch.args.push('--mcp-config', path.join(finalConfig, 'airun-mcp.json'));
  }
  if (settings.agent !== undefined) {
    if (typeof settings.agent !== 'string' || !settings.agent || (deps.action !== 'update' && !agentNames.has(settings.agent))) fail(`selected main agent is unavailable: ${typeof settings.agent === 'string' ? settings.agent : '(invalid)'}`);
    if (deps.action !== 'update') launch.args.push('--agent', settings.agent);
  }
  await writeJSON(path.join(target, 'settings.json'), settings);
  await writeJSON(path.join(target, 'airun-launch.json'), launch);
  return launch;
}
function versionAtLeast(actual, required) {
  const a = String(actual).match(/\d+\.\d+\.\d+/)?.[0].split('.').map(Number);
  const b = required.split('.').map(Number);
  if (!a) return false;
  for (let i = 0; i < 3; i++) { if (a[i] !== b[i]) return a[i] > b[i]; }
  return true;
}

export async function prepare(options, injected = {}) {
  const manifest = structuredClone(typeof options.manifest === 'string' ? await readJSON(options.manifest) : options.manifest);
  if (Array.isArray(manifest?.native_plugins)) manifest.native_plugins = manifest.native_plugins.map(ref => ['context7', 'superpowers', 'skill-creator'].includes(ref) ? `${ref}@claude-plugins-official` : ref);
  validateManifest(manifest);
  const action = options.action ?? 'prepare';
  if (!['prepare', 'update'].includes(action)) fail('unsupported profile preparation action');
  for (const key of ['cache', 'config', 'baseline']) if (!path.isAbsolute(options[key] ?? '')) fail(`${key} must be an absolute path`);
  const deps = { run: runCommand, env: process.env, workspace: '/workspace', parseYAML: defaultYAML, catalog: fetchCatalog, lock: withProfileLock, ...injected, action };
  deps.checkRuntime ??= (server, label) => checkRuntime(server, label, deps);
  deps.claudeVersion ??= () => deps.run('claude', ['--version']);
  deps.installCatalog ??= (type, id, directory) => installCatalog(type, id, directory, deps);
  deps.installNative ??= (ref, directory) => installNative(ref, directory, options.baseline, deps);
  deps.provisionNpm ??= (spec, name, directory) => provisionNpm(spec, name, directory, deps);
  deps.sourceBytes ??= async (commit, file) => {
    const response = await fetch(`https://raw.githubusercontent.com/${REPO}/${commit}/${file}`, { signal: AbortSignal.timeout(45000) });
    if (!response.ok) fail(`catalog resource request failed (HTTP ${response.status})`);
    return Buffer.from(await response.arrayBuffer());
  };
  const profileDir = path.join(options.cache, 'profiles', manifest.profile_key);
  return deps.lock(path.join(profileDir, 'lock'), async () => {
    await fs.mkdir(path.join(options.cache, 'staging'), { recursive: true });
    await fs.mkdir(path.join(options.cache, 'payloads'), { recursive: true });
    await fs.mkdir(path.join(profileDir, 'generations'), { recursive: true });
    const staging = await fs.mkdtemp(path.join(options.cache, 'staging', `${manifest.profile_key}-`));
    const runtimeAllocations = new Set();
    let committed = false;
    try {
      await verifyBaseline(options.baseline);
      const pointer = await readJSON(path.join(profileDir, 'current.json'), null);
      let previous = { version: 1, profile_key: manifest.profile_key, records: {} };
      if (pointer !== null) {
        if (!/^[a-f0-9-]+$/.test(pointer.generation ?? '')) fail('invalid profile generation pointer');
        previous = await readJSON(path.join(profileDir, 'generations', `${pointer.generation}.json`));
        if (previous.version !== 1 || previous.profile_key !== manifest.profile_key || !plain(previous.records)) fail('invalid profile resolution generation');
      }
      const records = structuredClone(previous.records);
      const bases = await baselineNative(options.baseline);
      const selected = TYPES.flatMap(type => (manifest.components[type] ?? []).map(ref => ({ type, id: ref.id })));
      selected.push(...[...new Set(manifest.native_plugins)].filter(id => !bases.has(id)).map(id => ({ type: 'native', id })));
      const pending = new Map();
      let catalog;
      for (const { type, id } of selected) {
        const identity = `${type}:${id}`;
        try {
          if (records[identity] && action === 'prepare') {
            const record = records[identity];
            if (!/^[a-f0-9]{64}$/.test(record.digest ?? '') || record.type !== type || record.id !== id || !Array.isArray(record.inventory)) fail('invalid retained receipt');
            const actual = await inventory(path.join(options.cache, 'payloads', record.digest), { links: type === 'native' });
            if (stable(actual) !== stable(record.inventory) || hash(stable(actual)) !== record.digest) fail('retained artifact is corrupt');
            await verifyRuntimes(record.runtimes, options.cache);
            continue;
          }
          const directory = await fs.mkdtemp(path.join(staging, 'artifact-'));
          let source;
          if (type === 'native') {
            await deps.installNative(id, directory);
            source = { kind: 'native', installer: await deps.claudeVersion() };
          } else {
            catalog ??= await deps.catalog();
            const expected = expectedInventory(catalog, type, id);
            const bytes = type === 'mcps' ? await deps.sourceBytes(catalog.commit, expected[0].source) : undefined;
            await deps.installCatalog(type, id, directory);
            await verifyCatalogOutput(directory, expected, type, bytes);
            await validateComponent(directory, type, id, deps);
            source = { kind: 'claude-code-templates', installer: INSTALLER_VERSION, commit: catalog.commit, inventory: expected };
          }
          const files = await inventory(directory, { links: type === 'native' });
          if (!files.length) fail('empty installed artifact');
          const digest = hash(stable(files));
          records[identity] = { type, id, digest, inventory: files, source };
          if (type === 'mcps') records[identity].runtimes = await prepareRuntimes(directory, options.cache, deps, runtimeAllocations);
          pending.set(digest, directory);
        } catch (e) {
          const repair = records[identity] && action === 'prepare' ? `; run airun profile update ${manifest.profile_key} to repair` : '';
          throw new Error(`${identity}: ${e.message}${repair}`);
        }
      }
      const payload = record => pending.get(record.digest) ?? path.join(options.cache, 'payloads', record.digest);
      const active = path.join(staging, 'active');
      const launch = await activate(manifest, records, payload, active, options.config, options.baseline, deps);
      // Validate completely before publishing any resolution generation.
      await fs.mkdir(options.config, { recursive: true });
      // The active view and cache may be on different Docker filesystems.
      // Copy first; a failure cannot publish a new resolution generation.
      const activeFiles = await inventory(active, { links: true });
      await copyFiles(active, options.config, activeFiles);
      // File receipts intentionally omit directories. Preserve directory link
      // targets explicitly, including empty private resource dependencies.
      for (const file of activeFiles.filter(file => file.symlink !== undefined)) {
        const resolved = await fs.realpath(path.join(active, file.path));
        if ((await fs.stat(resolved)).isDirectory()) await fs.mkdir(path.join(options.config, path.relative(active, resolved)), { recursive: true });
      }
      // Recheck link reachability in the published view before changing any
      // generation; staging containment alone cannot detect lost empty targets.
      await inventory(options.config, { links: true });
      for (const [digest, directory] of pending) {
        const destination = path.join(options.cache, 'payloads', digest);
        if (await exists(destination)) {
          if (hash(stable(await inventory(destination, { links: true }))) !== digest) {
            // Explicit repair never mutates an existing reader's inode tree.
            if (action !== 'update') fail('shared artifact corrupt; explicit update required');
            await fs.rename(destination, `${destination}.corrupt-${randomUUID()}`);
            await fs.rename(directory, destination);
          }
        } else {
          try { await fs.rename(directory, destination); } catch (e) {
            if (!['EEXIST', 'ENOTEMPTY'].includes(e.code) || hash(stable(await inventory(destination, { links: true }))) !== digest) throw e;
          }
        }
      }
      if (stable(records) !== stable(previous.records)) {
        const generation = randomUUID();
        await atomicJSON(path.join(profileDir, 'generations', `${generation}.json`), { version: 1, profile_key: manifest.profile_key, records });
        await atomicJSON(path.join(profileDir, 'current.json'), { generation });
      }
      committed = true;
      return { launch, records };
    } finally {
      if (!committed) for (const directory of runtimeAllocations) await fs.rm(directory, { recursive: true, force: true });
      await fs.rm(staging, { recursive: true, force: true });
    }
  });
}

async function main() {
  const args = process.argv.slice(2);
  const options = {};
  for (let i = 0; i < args.length; i += 2) {
    const flag = args[i];
    if (!['--manifest', '--cache', '--config', '--baseline', '--action'].includes(flag) || !args[i + 1]) fail('invalid component adapter arguments');
    options[flag.slice(2)] = args[i + 1];
  }
  await prepare(options);
}
if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch(error => { console.error(`[airun] profile preparation failed: ${error.message}`); process.exitCode = 1; });
}

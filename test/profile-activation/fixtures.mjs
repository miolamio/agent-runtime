import * as fs from 'node:fs/promises';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { inventory, runCommand } from '/airun/component-adapter.mjs';

export const root = '/tmp/airun-profile-activation';
const markdown = (name, description, body) => `---\nname: ${name}\ndescription: ${description}\n---\n${body}\n`;
export const sources = {
  'cli-tool/components/agents/testing/probe-reviewer.md': markdown('probe-reviewer', 'Acceptance specialist reviewer', 'AIRUN_MAIN_ROLE_ACTIVE. You are the specialist reviewer for this acceptance test.'),
  'cli-tool/components/skills/testing/probe-skill/SKILL.md': markdown('probe-skill', 'An acceptance skill available for explicit use', 'AIRUN_SKILL_BODY_ACTIVE. The skill was expanded by Claude.'),
  'cli-tool/components/skills/testing/probe-skill/references/deep/checklist.txt': 'Nested acceptance resource: preserve every byte.\n',
  'cli-tool/components/commands/testing/probe-command.md': '---\ndescription: An acceptance slash command\n---\nAIRUN_COMMAND_BODY_ACTIVE. The command was expanded by Claude.\n',
  'cli-tool/components/mods/testing/probe-mod/.claude-plugin/plugin.json': JSON.stringify({ name: 'probe-mod', version: '1.0.0', description: 'Acceptance function hook' }),
  'cli-tool/components/mods/testing/probe-mod/hooks/hooks.json': JSON.stringify({ modules: ['./probe.ts'] }),
  'cli-tool/components/mods/testing/probe-mod/hooks/probe.ts': `export function register(on) {
    on('session.start', async ($, e, next) => {
      await $.fs.write('${root}/mod-executed', 'AIRUN_FUNCTION_HOOK_EXECUTED');
      const run = await $.env.get('AIRUN_ACCEPTANCE_RUN');
      const config = await $.env.get('CLAUDE_CONFIG_DIR');
      const previous = await $.env.get('AIRUN_ACCEPTANCE_PREVIOUS_TRANSCRIPT');
      await $.fs.write('${root}/run-proofs/' + run + '.json', JSON.stringify({
        config, historyImported: previous ? await $.fs.exists(config + '/' + previous) : false,
        privateSkill: await $.fs.exists(config + '/skills/probe-skill/SKILL.md'),
        privateCommand: await $.fs.exists(config + '/commands/probe-command.md')
      }));
      return next(e);
    });
  }`,
  'cli-tool/components/mcps/testing/probe-mcp.json': JSON.stringify({ mcpServers: {
    probe: { command: 'node', args: ['/acceptance/mcp.mjs', 'first'], env: { TOKEN: '<YOUR_TOKEN>' } }
  } }),
  'cli-tool/components/mcps/testing/second-mcp.json': JSON.stringify({ mcpServers: {
    second: { command: 'node', args: ['/acceptance/mcp.mjs', 'second'], env: { TOKEN: '<YOUR_TOKEN>' } }
  } }),
  'cli-tool/components/mcps/testing/npm-mcp.json': JSON.stringify({ mcpServers: {
    npmprobe: { command: 'npx', args: ['-y', 'airun-fixture-mcp@1.0.0', 'third'], env: { TOKEN: '<YOUR_TOKEN>' } }
  } })
};
export const manifest = selected => ({
  version: 1, profile_key: 'activation',
  settings: selected ? { agent: 'probe-reviewer' } : {},
  native_plugins: selected ? ['probe-native@fixture-market'] : [],
  components: {
    agents: selected ? [{ id: 'testing/probe-reviewer' }] : [],
    skills: selected ? [{ id: 'testing/probe-skill' }] : [],
    commands: selected ? [{ id: 'testing/probe-command' }] : [],
    mcps: selected ? [
      { id: 'testing/probe-mcp', env: { TOKEN: 'AIRUN_COMPONENT_ENV_0001' } },
      { id: 'testing/second-mcp', env: { TOKEN: 'AIRUN_COMPONENT_ENV_0002' } },
      { id: 'testing/npm-mcp', env: { TOKEN: 'AIRUN_COMPONENT_ENV_0003' } }
    ] : [],
    mods: selected ? [{ id: 'testing/probe-mod' }] : [],
    plugins: []
  }
});
export async function put(base, relative, data) {
  const target = path.join(base, relative);
  await fs.mkdir(path.dirname(target), { recursive: true });
  await fs.writeFile(target, typeof data === 'string' ? data : JSON.stringify(data));
}
export async function sourceSnapshot(directory) {
  const entries = [];
  async function visit(relative = '') {
    const file = path.join(directory, relative), stat = await fs.lstat(file);
    const entry = { path: relative || '.', mode: stat.mode & 0o7777 };
    if (stat.isSymbolicLink()) entries.push({ ...entry, type: 'link', target: await fs.readlink(file) });
    else if (stat.isDirectory()) {
      entries.push({ ...entry, type: 'directory' });
      for (const name of (await fs.readdir(file)).sort()) await visit(path.join(relative, name));
    } else if (stat.isFile()) entries.push({ ...entry, type: 'file', size: stat.size, sha256: createHash('sha256').update(await fs.readFile(file)).digest('hex') });
    else entries.push({ ...entry, type: 'special' });
  }
  await visit();
  return entries;
}
async function baselineReceipt(baseline) {
  const files = (await inventory(baseline, { links: true })).filter(file => file.path !== 'inventory.json');
  await put(baseline, 'inventory.json', { version: 1, files: await Promise.all(files.map(async file => file.symlink !== undefined ? {
    path: file.path, symlink: file.symlink
  } : {
    path: file.path, size: file.size, sha256: file.sha256, mode: (await fs.stat(path.join(baseline, file.path))).mode & 0o777
  })) });
}
export async function createFixtures() {
  await fs.mkdir(root, { recursive: true });
  const baseline = path.join(root, 'baseline');
  await put(baseline, 'settings.json', { permissions: { defaultMode: 'bypassPermissions' } });
  await put(baseline, 'baseline.json', { version: 1, native_plugins: [] });
  const marketplace = path.join(baseline, 'plugins/marketplaces/fixture-market');
  await put(baseline, 'plugins/known_marketplaces.json', {
    'fixture-market': { source: { source: 'directory', path: marketplace }, installLocation: marketplace, autoUpdate: false }
  });
  await put(marketplace, '.claude-plugin/marketplace.json', {
    name: 'fixture-market', owner: { name: 'Airun acceptance' },
    plugins: [{ name: 'probe-native', version: '1.0.0', source: './probe-native' }]
  });
  await put(marketplace, 'probe-native/.claude-plugin/plugin.json', { name: 'probe-native', version: '1.0.0', description: 'Local native plugin fixture' });
  await put(marketplace, 'probe-native/skills/native-probe/SKILL.md', markdown('native-probe', 'Native plugin acceptance skill', 'AIRUN_NATIVE_SKILL_ACTIVE. This came from a native marketplace plugin.'));
  await put(marketplace, 'probe-native/hooks/hooks.json', { hooks: { SessionStart: [{ hooks: [{
    type: 'command', command: `printf '%s' '{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"AIRUN_NATIVE_HOOK_EXECUTED"}}'`
  }] }] } });
  await baselineReceipt(baseline);
  await fs.mkdir(path.join(root, 'workspace'), { recursive: true });
  await fs.mkdir(path.join(root, 'run-proofs'), { recursive: true });
  await put(root, 'selected.json', manifest(true));
  await put(root, 'removed.json', manifest(false));
}
export async function createDedupFixtures() {
  const repository = path.join(root, 'workspace/.claude');
  const baseline = path.join(root, 'baseline');
  const originalReceipt = await fs.readFile(path.join(baseline, 'inventory.json'));
  const prefix = 'cli-tool/components/skills/testing/probe-skill/';
  for (const directory of [baseline, repository]) {
    for (const [file, bytes] of Object.entries(sources)) if (file.startsWith(prefix)) await put(directory, `skills/probe-skill/${file.slice(prefix.length)}`, bytes);
    await put(directory, 'commands/probe-command.md', sources['cli-tool/components/commands/testing/probe-command.md']);
    await fs.mkdir(path.join(directory, 'skills/probe-skill/empty'), { mode: 0o751 });
  }
  await fs.symlink('probe-skill', path.join(baseline, 'skills/probe-alias'));
  await put(baseline, 'skills/probe-resource-alias/SKILL.md', markdown('probe-resource-alias', 'Acceptance baseline resource alias', 'AIRUN_BASELINE_ALIAS_ACTIVE. Read checklist.txt to use the linked baseline resource.'));
  await fs.symlink('../probe-skill/references/deep/checklist.txt', path.join(baseline, 'skills/probe-resource-alias/checklist.txt'));
  await fs.symlink('../probe-skill/empty', path.join(baseline, 'skills/probe-resource-alias/empty'));
  await baselineReceipt(baseline);
  return { repository, restoreBaseline: async () => {
    // These directories exist only for this phase of the isolated fixture.
    await fs.rm(path.join(baseline, 'skills'), { recursive: true });
    await fs.rm(path.join(baseline, 'commands'), { recursive: true });
    await fs.writeFile(path.join(baseline, 'inventory.json'), originalReceipt);
  } };
}
export function fixtureDependencies() {
  return {
    workspace: path.join(root, 'workspace'),
    run: async (command, args, options) => {
      if (command !== 'npm') return runCommand(command, args, options);
      await fs.appendFile(path.join(root, 'npm-invocations'), 'install\n');
      // Retain production provisionNpm and actual npm; only select the loopback
      // registry and a fresh test-owned package cache. Credentials are excluded.
      return runCommand(command, args, { ...options, env: {
        PATH: process.env.PATH, HOME: process.env.HOME,
        npm_config_registry: process.env.AIRUN_ACCEPTANCE_REGISTRY,
        npm_config_cache: path.join(root, 'npm-cache'),
        npm_config_fetch_retries: '0', npm_config_update_notifier: 'false'
      } });
    },
    catalog: async () => {
      await fs.appendFile(path.join(root, 'catalog-requests'), 'catalog\n');
      return { commit: 'a'.repeat(40), tree: Object.entries(sources).map(([file, bytes]) => ({
        path: file, type: 'blob', mode: '100644', size: Buffer.byteLength(bytes),
        sha: createHash('sha1').update(`blob ${Buffer.byteLength(bytes)}\0`).update(bytes).digest('hex')
      })) };
    },
    sourceBytes: async (_commit, file) => Buffer.from(sources[file]),
    installCatalog: async (type, id, directory) => {
      const source = `cli-tool/components/${type}/${id}`;
      const base = id.split('/').at(-1);
      if (type === 'agents' || type === 'commands') await put(directory, `.claude/${type}/${base}.md`, sources[`${source}.md`]);
      else if (type === 'mcps') await put(directory, '.mcp.json', sources[`${source}.json`]);
      else for (const [file, bytes] of Object.entries(sources)) if (file.startsWith(`${source}/`)) await put(directory, `.claude/skills/${base}/${file.slice(source.length + 1)}`, bytes);
    }
  };
}

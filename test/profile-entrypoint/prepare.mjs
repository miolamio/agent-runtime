// Fixture acquisition only: publish a verified catalog agent into a temporary
// test cache. The later session uses the image's unmodified production adapter.
import * as fs from 'node:fs/promises';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { prepare } from '/usr/local/lib/airun/component-adapter.mjs';

const source = '---\nname: code-reviewer\ndescription: Production entrypoint acceptance reviewer\n---\nAIRUN_ENTRYPOINT_SPECIALIST_ACTIVE. You are the selected specialist reviewer.\n';
const sourcePath = 'cli-tool/components/agents/development-tools/code-reviewer.md';
const manifest = JSON.parse(await fs.readFile('/entrypoint-test/profile.json', 'utf8'));
const config = await fs.mkdtemp('/tmp/airun-entrypoint-fixture-');
try {
  await prepare({ manifest, cache: '/var/lib/airun/components', config, baseline: '/opt/airun/profile-baseline', action: 'prepare' }, {
    catalog: async () => ({ commit: 'a'.repeat(40), tree: [{ path: sourcePath, type: 'blob', mode: '100644', size: Buffer.byteLength(source), sha: createHash('sha1').update(`blob ${Buffer.byteLength(source)}\0`).update(source).digest('hex') }] }),
    sourceBytes: async () => Buffer.from(source),
    installCatalog: async (type, id, directory) => {
      if (type !== 'agents' || id !== 'development-tools/code-reviewer') throw new Error('unexpected fixture request');
      const output = path.join(directory, '.claude/agents/code-reviewer.md');
      await fs.mkdir(path.dirname(output), { recursive: true });
      await fs.writeFile(output, source);
    },
  });
} finally { await fs.rm(config, { recursive: true, force: true }); }

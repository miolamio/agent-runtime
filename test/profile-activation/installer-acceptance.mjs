import assert from 'node:assert/strict';
import * as fs from 'node:fs/promises';
import path from 'node:path';
import { prepare, runCommand, installerPackageVersion, INSTALLER_VERSION } from '/airun/component-adapter.mjs';
import { fixtureDependencies, manifest, put, root, sources } from './fixtures.mjs';

export async function verifyPinnedInstaller() {
  assert.equal(await installerPackageVersion(), INSTALLER_VERSION);
  const selected = manifest(false);
  selected.profile_key = 'pinned-installer';
  selected.components.agents = [{ id: 'testing/probe-reviewer' }];
  selected.components.skills = [{ id: 'testing/probe-skill' }];
  const sourceFile = path.join(root, 'installer-sources.json');
  const requestLog = path.join(root, 'installer-requests');
  await put(root, 'installer-sources.json', sources);
  const { catalog, sourceBytes } = fixtureDependencies();
  const invoked = [];
  const options = {
    manifest: selected, cache: path.join(root, 'installer-cache'),
    baseline: path.join(root, 'baseline'), config: path.join(root, 'installer-active')
  };
  // installCatalog is deliberately not injected: wrong flags, the wrong CCT
  // binary or incomplete recursive output must fail production verification.
  const result = await prepare(options, { catalog, sourceBytes, run: async (command, args, runOptions) => {
    if (command !== 'claude-code-templates') return runCommand(command, args, runOptions);
    invoked.push(args);
    return runCommand(command, args, { ...runOptions, timeout: 15000, env: {
      PATH: process.env.PATH, HOME: process.env.HOME,
      NODE_OPTIONS: '--import=/acceptance/installer-fetch.mjs',
      AIRUN_TEST_CATALOG_SOURCE: sourceFile, AIRUN_TEST_CATALOG_REQUESTS: requestLog
    } });
  } });
  assert.equal(invoked.length, 2, 'production preparation did not run both actual installer operations');
  for (const [identity, relative, source] of [
    ['agents:testing/probe-reviewer', 'agents/probe-reviewer.md', 'agents/testing/probe-reviewer.md'],
    ['skills:testing/probe-skill', 'skills/probe-skill/SKILL.md', 'skills/testing/probe-skill/SKILL.md'],
    ['skills:testing/probe-skill', 'skills/probe-skill/references/deep/checklist.txt', 'skills/testing/probe-skill/references/deep/checklist.txt']
  ]) {
    const expected = sources['cli-tool/components/' + source];
    assert.equal(await fs.readFile(path.join(options.config, relative), 'utf8'), expected);
    const record = result.records[identity];
    assert.equal(record.source.installer, INSTALLER_VERSION);
    assert.equal(await fs.readFile(path.join(options.cache, 'payloads', record.digest, '.claude', relative), 'utf8'), expected);
  }
  const requests = await fs.readFile(requestLog, 'utf8');
  assert(requests.includes('/contents/cli-tool/components/skills/testing/probe-skill/references/deep'));
  assert(requests.includes('/components/agents/testing/probe-reviewer.md'));
  const pointer = JSON.parse(await fs.readFile(path.join(options.cache, 'profiles/pinned-installer/current.json'), 'utf8'));
  const published = JSON.parse(await fs.readFile(path.join(options.cache, 'profiles/pinned-installer/generations', pointer.generation + '.json'), 'utf8'));
  assert.deepEqual(published.records, result.records, 'verified pinned-installer artifacts were not published');
  console.log(`PASS actual claude-code-templates ${INSTALLER_VERSION} publishes agent and recursive nested skill resources through production installCatalog`);
}

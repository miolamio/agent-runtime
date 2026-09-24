#!/usr/bin/env node
// ART-23 diagnostics against the already-built image. No provider call or image build.
// Usage: node benchmarks/art23-profile-startup.mjs [--large-mib 1024] [--reps 5]
import { spawn } from 'node:child_process';
import { createHash, randomUUID } from 'node:crypto';
import * as fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { performance } from 'node:perf_hooks';
import { fileURLToPath } from 'node:url';

const image = 'agent-runtime:latest';
const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const resultsPath = path.join(scriptDir, 'art23-results.json');
const args = process.argv.slice(2);
function option(name, fallback) {
  const index = args.indexOf(name);
  return index < 0 ? fallback : Number(args[index + 1]);
}
const largeMiB = option('--large-mib', 1024);
const reps = option('--reps', 5);
if (!Number.isInteger(largeMiB) || largeMiB < 1 || largeMiB > 1024 || !Number.isInteger(reps) || reps < 1 || reps > 20) {
  throw new Error('use --large-mib 1..1024 and --reps 1..20; each JSONL file is capped at 256 MiB');
}

function command(program, argv, { ready = false } = {}) {
  return new Promise((resolve, reject) => {
    const started = performance.now();
    const child = spawn(program, argv, { stdio: ['ignore', 'pipe', 'pipe'] });
    let stdout = '', stderr = '', readyMs = null;
    child.stdout.on('data', chunk => { stdout += chunk; });
    child.stderr.on('data', chunk => {
      stderr += chunk;
      if (ready && readyMs === null && stderr.includes('[airun] ready ts=')) readyMs = performance.now() - started;
    });
    child.once('error', reject);
    child.once('close', code => code === 0 && (!ready || readyMs !== null)
      ? resolve({ stdout, stderr, wallMs: performance.now() - started, readyMs })
      : reject(new Error(`${program} exited ${code}; ${stderr.slice(-1200)}`)));
  });
}

const docker = (argv, options) => command('docker', argv, options);
const extract = text => {
  const line = text.split('\n').find(line => line.startsWith('ART23_JSON='));
  if (!line) throw new Error(`container did not report ART23_JSON: ${text.slice(-500)}`);
  return JSON.parse(line.slice('ART23_JSON='.length));
};
const hash = bytes => createHash('sha256').update(bytes).digest('hex');
const round = number => Math.round(number * 10) / 10;
const stats = values => {
  const sorted = [...values].sort((a, b) => a - b);
  return { samplesMs: values.map(round), medianMs: round(sorted[Math.floor(sorted.length / 2)]), minMs: round(sorted[0]), maxMs: round(sorted.at(-1)) };
};

const historyCode = String.raw`
import * as fs from 'node:fs/promises';
import path from 'node:path';
import { performance } from 'node:perf_hooks';
import { spawnSync } from 'node:child_process';
import { importHistory, mergeHistory } from 'file:///usr/local/lib/airun/profile-start.mjs';
import { prepare } from 'file:///usr/local/lib/airun/component-adapter.mjs';
const root = '/bench';
const mode = process.argv[1];
const mib = Number(process.env.ART23_MIB);
const now = () => performance.now();
const rssMiB = () => Math.round(process.resourceUsage().maxRSS / 1024);
if (mode === 'generate') {
  const dir = path.join(root, 'projects/-workspace');
  await fs.mkdir(dir, { recursive: true });
  const parts = Math.ceil(mib / 256);
  for (let part = 0; part < parts; part++) {
    const handle = await fs.open(path.join(dir, 'session-' + part + '.jsonl'), 'w', 0o600);
    try {
      const count = Math.min(256, mib - part * 256) * 1024 * 1024 / 256;
      for (let base = 0; base < count; base += 4096) {
        let chunk = '';
        for (let i = base; i < Math.min(base + 4096, count); i++) {
          const prefix = '{"id":"' + String(i + part * 1048576).padStart(12, '0') + '","payload":"';
          chunk += prefix + 'x'.repeat(256 - prefix.length - 3) + '"}\n';
        }
        const bytes = Buffer.from(chunk);
        for (let offset = 0; offset < bytes.length;) {
          const { bytesWritten } = await handle.write(bytes, offset, bytes.length - offset);
          offset += bytesWritten;
        }
      }
    } finally { await handle.close(); }
  }
  let actual = 0;
  for (let part = 0; part < parts; part++) actual += (await fs.stat(path.join(dir, 'session-' + part + '.jsonl'))).size;
  if (actual !== mib * 1024 * 1024) throw new Error('generated history size differs from requested size');
  await fs.writeFile(path.join(root, 'history.jsonl'), '{"prompt":"fixture"}\n');
  await fs.mkdir(path.join(root, 'plans'), { recursive: true });
  await fs.writeFile(path.join(root, 'plans/current.md'), 'Synthetic plan\n');
  console.log('ART23_JSON=' + JSON.stringify({ sizeMiB: mib }));
} else if (mode === 'stages') {
  const config = await fs.mkdtemp(path.join(root, 'stage-config-'));
  const state = process.env.ART23_STATE === '1' ? root : undefined;
  try {
    let t = now();
    const version = spawnSync('claude', ['--version'], { encoding: 'utf8' });
    if (version.status !== 0) throw new Error('claude --version failed');
    const versionMs = now() - t;
    t = now();
    await importHistory(state, config);
    const importMs = now() - t;
    t = now();
    const manifest = JSON.parse(await fs.readFile('/run/airun/profile.json', 'utf8'));
    await prepare({ manifest, cache: '/run/airun/components', config, baseline: '/opt/airun/profile-baseline', action: 'prepare' });
    const adapterMs = now() - t;
    console.log('ART23_JSON=' + JSON.stringify({ versionMs, importMs, adapterMs, maxRssMiB: rssMiB() }));
  } finally { await fs.rm(config, { recursive: true, force: true }); }
} else if (mode === 'merge') {
  const active = await fs.mkdtemp(path.join(root, 'merge-active-'));
  try {
    let t = now();
    const original = await importHistory(root, active);
    const importMs = now() - t;
    t = now();
    await mergeHistory(root, active, original);
    const unchangedMs = now() - t;
    const unchangedRssMiB = rssMiB();
    for (let part = 0; part < Math.ceil(mib / 256); part++) {
      await fs.appendFile(path.join(active, 'projects/-workspace/session-' + part + '.jsonl'), '{"id":"new-' + part + '","payload":"changed"}\n');
    }
    t = now();
    await mergeHistory(root, active, original);
    const changedMs = now() - t;
    const changedRssMiB = rssMiB();
    console.log('ART23_JSON=' + JSON.stringify({ sizeMiB: mib, importMs, unchangedMs, changedMs, unchangedRssMiB, changedRssMiB }));
  } finally { await fs.rm(active, { recursive: true, force: true }); }
} else throw new Error('unknown diagnostic mode');
`;

const runID = randomUUID().slice(0, 8);
const volumes = {
  cache: `airun-art23-${runID}-cache`,
  typical: `airun-art23-${runID}-typical`,
  large: `airun-art23-${runID}-large`,
};
const created = [];
const temporary = await fs.mkdtemp(path.join(os.tmpdir(), 'airun-art23-'));
const manifest = path.join(temporary, 'manifest.json');
await fs.writeFile(manifest, JSON.stringify({ version: 1, profile_key: 'art23-bench', settings: {}, native_plugins: [], components: { agents: [], skills: [], commands: [], mcps: [], mods: [], plugins: [] } }));
const manifestMount = `${manifest}:/run/airun/profile.json:ro`;
const cacheMount = `${volumes.cache}:/run/airun/components`;

async function createVolume(name) {
  await docker(['volume', 'create', '--label', 'org.miolamio.airun-benchmark=ART-23', name]);
  created.push(name);
}
async function initializeVolume(name) {
  await docker(['run', '--rm', '-v', `${name}:/bench`, '--entrypoint', 'chown', image, '1001:1001', '/bench']);
}
async function diagnostic(mode, name, mib, state = true) {
  const result = await docker(['run', '--rm', '--network', 'none', '--user', '1001:1001',
    '-v', `${name}:/bench`, '-v', manifestMount, '-v', cacheMount,
    '-e', `ART23_MIB=${mib}`, '-e', `ART23_STATE=${state ? 1 : 0}`,
    '--entrypoint', 'node', image, '--input-type=module', '-e', historyCode, mode]);
  return extract(result.stdout);
}
async function startup(stateVolume) {
  const argv = ['run', '--rm', '--network', 'none', '--label', 'org.miolamio.airun-benchmark=ART-23',
    '-v', manifestMount, '-v', cacheMount,
    '-e', 'AIRUN_PROFILE_MANIFEST=/run/airun/profile.json',
    '-e', 'AIRUN_COMPONENT_CACHE=/run/airun/components',
    '-e', 'AIRUN_WORKSPACE_MODE=bind'];
  if (stateVolume) argv.push('-v', `${stateVolume}:/run/airun/state`, '-e', 'AIRUN_PROFILE_STATE=/run/airun/state');
  argv.push(image, '/bin/true');
  return (await docker(argv, { ready: true })).readyMs;
}

try {
  const disk = (await command('df', ['-Pk', temporary])).stdout.trim().split('\n').at(-1).trim().split(/\s+/);
  const availableGiB = Number(disk[3]) / 1024 / 1024;
  if (availableGiB < 5) throw new Error(`only ${availableGiB.toFixed(1)} GiB free; need at least 5 GiB for temporary history copies`);
  for (const name of Object.values(volumes)) { await createVolume(name); await initializeVolume(name); }
  const imageInfo = JSON.parse((await docker(['image', 'inspect', image])).stdout)[0];
  const source = await fs.readFile(path.join(scriptDir, '../docker/profile-start.mjs'));
  const sourceAdapter = await fs.readFile(path.join(scriptDir, '../docker/component-adapter.mjs'));
  const imageHashes = (await docker(['run', '--rm', '--entrypoint', 'sha256sum', image,
    '/usr/local/lib/airun/profile-start.mjs', '/usr/local/lib/airun/component-adapter.mjs'])).stdout.trim().split('\n').map(line => line.split(' ')[0]);

  const coldNoState = await startup();
  const warmNoState = [];
  for (let i = 0; i < reps; i++) warmNoState.push(await startup());
  await diagnostic('generate', volumes.typical, 1);
  const typicalStartup = [];
  for (let i = 0; i < reps; i++) typicalStartup.push(await startup(volumes.typical));
  const typicalStages = await diagnostic('stages', volumes.typical, 1);
  const typicalMerge = await diagnostic('merge', volumes.typical, 1);

  await diagnostic('generate', volumes.large, largeMiB);
  const largeStartup = [];
  for (let i = 0; i < Math.min(reps, 2); i++) largeStartup.push(await startup(volumes.large));
  const largeStages = await diagnostic('stages', volumes.large, largeMiB);
  const largeMerge = await diagnostic('merge', volumes.large, largeMiB);
  const report = {
    date: new Date().toISOString(), image, imageId: imageInfo.Id, imageSizeBytes: imageInfo.Size,
    imageProfileStartSha256: imageHashes[0], imageAdapterSha256: imageHashes[1],
    sourceProfileStartSha256: hash(source), sourceAdapterSha256: hash(sourceAdapter),
    host: { platform: process.platform, arch: process.arch },
    method: { provider: 'none', network: 'none', profile: 'empty components, baked baseline only', command: '/bin/true', reps, largeMiB, jsonlPartMiB: 256 },
    startup: { coldNoState: round(coldNoState), warmNoState: stats(warmNoState), typicalState: stats(typicalStartup), largeState: stats(largeStartup) },
    stages: { typical: Object.fromEntries(Object.entries(typicalStages).map(([key, value]) => [key, round(value)])), large: Object.fromEntries(Object.entries(largeStages).map(([key, value]) => [key, round(value)])) },
    merge: { typical: Object.fromEntries(Object.entries(typicalMerge).map(([key, value]) => [key, round(value)])), large: Object.fromEntries(Object.entries(largeMerge).map(([key, value]) => [key, round(value)])) },
  };
  await fs.writeFile(resultsPath, `${JSON.stringify(report, null, 2)}\n`);
  console.log(`ART23 results: ${resultsPath}`);
  console.log(JSON.stringify({ startup: report.startup, stages: report.stages, merge: report.merge }, null, 2));
} finally {
  for (const name of created.reverse()) {
    try { await docker(['volume', 'rm', name]); } catch (error) { console.error(`temporary volume cleanup failed: ${name}: ${error.message}`); }
  }
  await fs.rm(temporary, { recursive: true, force: true });
}

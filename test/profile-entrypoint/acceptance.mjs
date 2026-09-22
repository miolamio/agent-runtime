import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { fileURLToPath } from 'node:url';

const execute = promisify(execFile);
const image = process.env.AIRUN_TEST_IMAGE || 'agent-runtime:profiles-dev';
const fixture = fileURLToPath(new URL('.', import.meta.url));
const prefix = `airun-entrypoint-${randomUUID().slice(0, 8)}`;
const volume = Object.fromEntries(['cache', 'state', 'proof', 'legacy'].map(name => [name, `${prefix}-${name}`]));
const containers = [];
const createdVolumes = [];
const fixtureMount = ['--mount', `type=bind,src=${fixture},dst=/entrypoint-test,readonly`];
const mount = (name, target) => ['--mount', `type=volume,src=${volume[name]},dst=${target}`];
async function docker(args, timeout = 180000) {
  try { return await execute('docker', args, { timeout, maxBuffer: 8 * 1024 * 1024 }); }
  catch (error) { throw new Error(`docker ${args[0]} failed: ${error.stderr || error.message}\n${error.stdout || ''}`, { cause: error }); }
}
async function inspectFiles(script, extra = []) {
  return (await docker(['run', '--rm', '--network', 'none', ...fixtureMount, ...mount('proof', '/proof'), ...extra, '--entrypoint', 'node', image, '-e', script])).stdout.trim();
}

try {
  const metadata = JSON.parse((await docker(['image', 'inspect', image])).stdout)[0];
  assert.deepEqual(metadata.Config.Entrypoint, ['entrypoint.sh'], 'acceptance requires the production image entrypoint');
  assert.ok(!metadata.Config.User || metadata.Config.User === 'root' || metadata.Config.User === '0', 'production startup must initially run as root');
  for (const name of Object.values(volume)) { await docker(['volume', 'create', name]); createdVolumes.push(name); }
  // Only setup uses an entrypoint override. Both sessions below enter through
  // the production ENTRYPOINT, without --user or replacement launcher scripts.
  await docker(['run', '--rm', '--network', 'none', ...fixtureMount,
    ...mount('cache', '/var/lib/airun/components'), ...mount('state', '/var/lib/airun/state'), ...mount('proof', '/proof'),
    '--entrypoint', '/bin/bash', image, '-c',
    'chown 1001:1001 /var/lib/airun/components /proof && gosu claude node /entrypoint-test/prepare.mjs && chown 0:0 /var/lib/airun/components /var/lib/airun/state && chmod 755 /var/lib/airun/components /var/lib/airun/state']);
  const initial = JSON.parse(await inspectFiles('const f=require("node:fs"); console.log(JSON.stringify([f.statSync("/cache").uid,f.statSync("/state").uid]));', [...mount('cache', '/cache'), ...mount('state', '/state')]));
  assert.deepEqual(initial, [0, 0], 'test cache and state must start root-owned');

  const server = `${prefix}-model`;
  containers.push(server);
  await docker(['run', '-d', '--name', server, '--network', 'none', '--user', '1001:1001', ...fixtureMount, ...mount('proof', '/proof'), '--entrypoint', 'node', image, '/entrypoint-test/model.mjs']);
  let ready = false;
  for (let attempt = 0; attempt < 50; attempt++) {
    try { await docker(['exec', server, 'test', '-f', '/proof/server-ready'], 10000); ready = true; break; } catch {}
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  assert.ok(ready, 'synthetic loopback model did not start');
  const runtime = `${prefix}-profile`;
  containers.push(runtime);
  const env = {
    AIRUN_PROFILE_MANIFEST: '/run/airun/profile.json', AIRUN_COMPONENT_CACHE: '/var/lib/airun/components', AIRUN_PROFILE_STATE: '/var/lib/airun/state',
    ANTHROPIC_BASE_URL: 'http://127.0.0.1:8383', ANTHROPIC_AUTH_TOKEN: 'synthetic-only', ANTHROPIC_API_KEY: '',
    ANTHROPIC_MODEL: 'claude-sonnet-4-6', ANTHROPIC_DEFAULT_SONNET_MODEL: 'claude-sonnet-4-6', ANTHROPIC_DEFAULT_OPUS_MODEL: 'claude-sonnet-4-6', ANTHROPIC_DEFAULT_HAIKU_MODEL: 'claude-sonnet-4-6',
    CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: '1', DISABLE_AUTOUPDATER: '1', ENABLE_TOOL_SEARCH: 'false', API_TIMEOUT_MS: '10000', MCP_TIMEOUT: '2000',
  };
  const result = await docker(['run', '--name', runtime, '--network', `container:${server}`, ...fixtureMount,
    '--mount', `type=bind,src=${fixture}profile.json,dst=/run/airun/profile.json,readonly`,
    ...mount('cache', '/var/lib/airun/components'), ...mount('state', '/var/lib/airun/state'), ...mount('proof', '/proof'),
    ...Object.entries(env).flatMap(([key, value]) => ['-e', `${key}=${value}`]), image,
    'claude', '-p', 'AIRUN_ENTRYPOINT_PERSISTED_PROMPT: verify the reviewer role.', '--output-format', 'stream-json', '--verbose', '--max-turns', '1']);
  assert.match(result.stderr, /\[airun\] ready ts=/, 'production profile dispatch never emitted readiness');
  const events = result.stdout.split('\n').filter(line => line.startsWith('{')).map(JSON.parse);
  const session = events.find(event => event.type === 'system' && event.subtype === 'init')?.session_id;
  assert.ok(session, 'real Claude did not report a session');
  assert.ok(events.some(event => event.type === 'result' && event.result?.includes('AIRUN_ENTRYPOINT_MODEL_COMPLETE')), 'real Claude did not consume the local synthetic model response');
  const proof = JSON.parse(await inspectFiles('console.log(require("node:fs").readFileSync("/proof/session.json","utf8"))'));
  assert.equal(proof.uid, 1001, 'actual Claude hook did not run as the non-root user');
  assert.deepEqual([proof.cacheUID, proof.stateUID], [1001, 1001], 'production entrypoint did not initialize root-owned mount ownership');
  assert.equal(proof.agent, 'code-reviewer');
  assert.equal(proof.permissions.defaultMode, 'bypassPermissions');
  assert.match(proof.config, /^\/var\/lib\/airun\/state\/\.airun-runs\/config-/);
  const requests = (await inspectFiles('console.log(require("node:fs").readFileSync("/proof/requests.jsonl","utf8"))')).split('\n').filter(Boolean).map(JSON.parse);
  assert.ok(requests.some(request => JSON.stringify(request.system).includes('AIRUN_ENTRYPOINT_SPECIALIST_ACTIVE')), 'requested specialist was not active in the real model system prompt');
  const history = await inspectFiles(`const f=require('node:fs'),p=require('node:path'); const files=f.readdirSync('/state/projects').map(name=>p.join('/state/projects',name,${JSON.stringify(session)}+'.jsonl')).filter(file=>f.existsSync(file)); if(files.length!==1) throw Error('native session transcript missing'); const data=f.readFileSync(files[0],'utf8'); if(!data.includes('AIRUN_ENTRYPOINT_PERSISTED_PROMPT')) throw Error('native prompt not saved'); if(f.statSync(files[0]).uid!==1001) throw Error('history not owned by claude'); if(f.readdirSync('/state/.airun-runs').length) throw Error('successful private config not cleaned'); console.log('saved');`, mount('state', '/state'));
  assert.equal(history, 'saved');
  console.log('PASS production root entrypoint prepares root-owned cache/state, runs actual Claude as non-root, activates the reviewer role, and saves native session history');

  const legacy = `${prefix}-no-profile`;
  containers.push(legacy);
  const defaults = await docker(['run', '--name', legacy, '--network', 'none', ...fixtureMount,
    ...mount('legacy', '/home/claude/.claude'), '-e', 'CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1', image,
    'node', '/entrypoint-test/no-profile.mjs']);
  assert.match(defaults.stdout, /PASS fresh no-profile/);
  console.log(defaults.stdout.trim());
  console.log('REAL: production entrypoint, ownership setup, adapter, image baseline, Claude CLI and transcript storage. FIXTURES: one cached catalog agent and loopback model responses.');
} finally {
  for (const name of containers.reverse()) await docker(['rm', '-f', name]).catch(() => {});
  for (const name of createdVolumes.reverse()) await docker(['volume', 'rm', name]).catch(() => {});
}

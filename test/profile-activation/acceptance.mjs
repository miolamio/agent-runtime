import assert from 'node:assert/strict';
import * as fs from 'node:fs/promises';
import path from 'node:path';
import http from 'node:http';
import { spawn, spawnSync } from 'node:child_process';
import { startProfile } from '/airun/profile-start.mjs';
import { inventory } from '/airun/component-adapter.mjs';
import { createFixtures, createDedupFixtures, sourceSnapshot, sources, root } from './fixtures.mjs';
import { verifyPinnedInstaller } from './installer-acceptance.mjs';
import { startRegistry } from './npm-registry.mjs';

if (process.argv[2] === '--session') {
  process.exitCode = await startProfile(process.argv.slice(3), {
    adapterPath: '/acceptance/fixture-adapter.mjs', baselinePath: path.join(root, 'baseline')
  });
} else {
  await main();
}

async function main() {
  const version = spawnSync('claude', ['--version'], { encoding: 'utf8' });
  assert.equal(version.status, 0, 'actual Claude CLI is unavailable');
  console.log(`REAL CLI: ${version.stdout.trim()}`);
  await createFixtures();
  await verifyPinnedInstaller();
  const registry = await startRegistry();
  const requests = [];
  let plan = [];
  let turn = 0;
  let concurrentBarrier;
  let concurrentArrivals = 0;
  const server = http.createServer(async (request, response) => {
    let raw = '';
    for await (const chunk of request) raw += chunk;
    if (request.url.includes('count_tokens')) {
      response.setHeader('content-type', 'application/json');
      response.end(JSON.stringify({ input_tokens: 100 }));
      return;
    }
    if (!request.url.startsWith('/v1/messages')) {
      response.setHeader('content-type', 'application/json'); response.end('{}'); return;
    }
    const body = JSON.parse(raw);
    requests.push(body);
    await fs.writeFile(path.join(root, 'last-request.json'), JSON.stringify(body, null, 2));
    if (concurrentBarrier && JSON.stringify(body.messages).includes('AIRUN_CONCURRENT_')) {
      concurrentArrivals++;
      if (concurrentArrivals === 2) concurrentBarrier.release();
      await concurrentBarrier.promise;
    }
    const step = plan[turn++];
    const tool = typeof step === 'function' ? await step() : step;
    const content = tool ? [{ type: 'tool_use', id: `toolu_probe_${turn}`, name: tool.name, input: tool.input }] : [{ type: 'text', text: 'AIRUN_ACCEPTANCE_COMPLETE' }];
    const message = {
      id: `msg_probe_${requests.length}`, type: 'message', role: 'assistant', model: body.model,
      content, stop_reason: tool ? 'tool_use' : 'end_turn', stop_sequence: null,
      usage: { input_tokens: 100, output_tokens: 10 }
    };
    if (!body.stream) {
      response.setHeader('content-type', 'application/json'); response.end(JSON.stringify(message)); return;
    }
    response.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' });
    const event = (name, data) => response.write(`event: ${name}\ndata: ${JSON.stringify({ type: name, ...data })}\n\n`);
    event('message_start', { message: { ...message, content: [], stop_reason: null, usage: { input_tokens: 100, output_tokens: 0 } } });
    content.forEach((block, index) => {
      event('content_block_start', { index, content_block: block.type === 'text' ? { type: 'text', text: '' } : { ...block, input: {} } });
      event('content_block_delta', { index, delta: block.type === 'text' ? { type: 'text_delta', text: block.text } : { type: 'input_json_delta', partial_json: JSON.stringify(block.input) } });
      event('content_block_stop', { index });
    });
    event('message_delta', { delta: { stop_reason: message.stop_reason, stop_sequence: null }, usage: { output_tokens: 10 } });
    event('message_stop', {});
    response.end();
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  try {
    const env = {
      ...process.env, HOME: '/home/claude',
      ANTHROPIC_BASE_URL: `http://127.0.0.1:${server.address().port}`,
      ANTHROPIC_AUTH_TOKEN: 'synthetic-provider-only', ANTHROPIC_API_KEY: '',
      ANTHROPIC_MODEL: 'claude-sonnet-4-6', ANTHROPIC_DEFAULT_SONNET_MODEL: 'claude-sonnet-4-6',
      ANTHROPIC_DEFAULT_OPUS_MODEL: 'claude-sonnet-4-6', ANTHROPIC_DEFAULT_HAIKU_MODEL: 'claude-sonnet-4-6',
      CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: '1', DISABLE_AUTOUPDATER: '1', ENABLE_TOOL_SEARCH: 'false',
      AIRUN_PROFILE_MANIFEST: path.join(root, 'selected.json'), AIRUN_COMPONENT_CACHE: path.join(root, 'cache'),
      AIRUN_PROFILE_STATE: path.join(root, 'state'), AIRUN_ACCEPTANCE_RUN: 'first',
      AIRUN_COMPONENT_ENV_0001: 'synthetic-mcp-first', AIRUN_COMPONENT_ENV_0002: 'synthetic-mcp-second',
      AIRUN_COMPONENT_ENV_0003: 'synthetic-mcp-third', AIRUN_ACCEPTANCE_REGISTRY: registry.url, API_TIMEOUT_MS: '10000'
    };
    plan = [
      { name: 'mcp__probe__credential_probe', input: {} },
      { name: 'mcp__second__credential_probe', input: {} },
      { name: 'mcp__npmprobe__credential_probe', input: {} },
      { name: 'Skill', input: { skill: 'probe-skill' } },
      { name: 'Skill', input: { skill: 'probe-command' } },
      { name: 'Skill', input: { skill: 'probe-native:native-probe' } }
    ];
    const initialPrompt = 'AIRUN_FIRST_PERSISTED_PROMPT: run the local acceptance probe.';
    const selected = await launch(env, initialPrompt);
    const selectedRequests = requests.splice(0);
    assert.equal(selected.code, 0, `real Claude selected session failed: ${selected.stderr}\n${selected.stdout.slice(-1000)}`);
    assert(selectedRequests.length > 0, 'no actual model request reached the local endpoint');
    assert(JSON.stringify(selectedRequests[0].system).includes('AIRUN_MAIN_ROLE_ACTIVE'), 'specialist main-role text missing from actual system prompt');
    for (const child of ['first', 'second', 'third']) assert(JSON.stringify(selectedRequests).includes(`AIRUN_MCP_${child}_CREDENTIAL_OK`), `real ${child} MCP tool did not execute with its intended credential`);
    console.log('PASS actual specialist main-role system prompt and three MCP children with distinct TOKEN bindings');
    assert(registry.requests.includes('/airun-fixture-mcp'), 'production npm did not resolve the local package');
    assert(registry.requests.some(url => url.endsWith('.tgz')), 'production npm did not download the package tarball');
    const retainedRuntime = await runtimeSnapshot();
    assert(retainedRuntime.record.command.startsWith(path.join(root, 'cache/runtimes/')));
    assert.equal(await fs.readFile(path.join(root, 'npm-invocations'), 'utf8'), 'install\n');
    console.log('PASS production provisionNpm installs a registry package and actual Claude executes its retained MCP binary');
    await registry.close();
    await assert.rejects(fetch(registry.url), 'fixture registry should be inaccessible');
    // An erroneous warm install cannot succeed using npm's own download cache.
    await fs.rename(path.join(root, 'npm-cache'), path.join(root, 'npm-cache-cold'));
    assert(JSON.stringify(selectedRequests).includes('AIRUN_NATIVE_HOOK_EXECUTED'), 'native plugin session hook did not run');
    console.log('PASS native marketplace plugin hook activation');
    assert.equal(await fs.readFile(path.join(root, 'mod-executed'), 'utf8'), 'AIRUN_FUNCTION_HOOK_EXECUTED');
    console.log('PASS mod function-hook callback execution');
    for (const [marker, label] of [
      ['AIRUN_SKILL_BODY_ACTIVE', 'catalog skill'],
      ['AIRUN_COMMAND_BODY_ACTIVE', 'catalog command'],
      ['AIRUN_NATIVE_SKILL_ACTIVE', 'native plugin skill']
    ]) {
      assert(JSON.stringify(selectedRequests).includes(marker), `${label} body was not expanded into an actual model request`);
      console.log(`PASS ${label} expansion through the actual Skill tool`);
    }
    const selectedSession = sessionID(selected.stdout);
    const firstTranscript = await transcriptFor(selectedSession);
    assert((await fs.readFile(firstTranscript, 'utf8')).includes(initialPrompt), 'real CLI transcript was not persisted');

    const pointer = await fs.readFile(path.join(root, 'cache/profiles/activation/current.json'), 'utf8');
    const catalogBefore = await fs.readFile(path.join(root, 'catalog-requests'), 'utf8');
    const baseline = path.join(root, 'baseline');
    const originalBaseline = await sourceSnapshot(baseline);
    const { repository, restoreBaseline } = await createDedupFixtures();
    const repositoryBefore = await sourceSnapshot(repository), baselineBefore = await sourceSnapshot(baseline);
    const dedupCacheBefore = await retainedCacheSnapshot();
    const readAlias = relative => async () => {
      const config = (await proof('deduplicated')).config;
      const file = path.join(config, 'skills', relative);
      assert((await fs.realpath(file)).startsWith(path.join(config, 'airun-component-resources') + '/'), 'alias did not reach its private baseline dependency');
      assert.deepEqual(await fs.readdir(path.join(config, 'skills/probe-resource-alias/empty')), [], 'empty directory link target was not published');
      return { name: 'Read', input: { file_path: file } };
    };
    plan = [
      { name: 'Skill', input: { skill: 'probe-skill' } },
      { name: 'Skill', input: { skill: 'probe-command' } },
      { name: 'Skill', input: { skill: 'probe-alias' } },
      { name: 'Skill', input: { skill: 'probe-resource-alias' } },
      readAlias('probe-alias/references/deep/checklist.txt'),
      readAlias('probe-resource-alias/checklist.txt')
    ]; turn = 0;
    const deduplicated = await launch({ ...env, AIRUN_ACCEPTANCE_RUN: 'deduplicated' }, 'Verify the identical project skill and command.');
    const deduplicatedRequests = requests.splice(0);
    assert.equal(deduplicated.code, 0, `deduplicated selection failed: ${deduplicated.stderr}`);
    for (const marker of ['AIRUN_SKILL_BODY_ACTIVE', 'AIRUN_COMMAND_BODY_ACTIVE', 'AIRUN_BASELINE_ALIAS_ACTIVE']) assert(JSON.stringify(deduplicatedRequests).includes(marker), 'deduplicated component did not expand through native Skill');
    const nativeResults = deduplicatedRequests.flatMap(request => request.messages.flatMap(message => Array.isArray(message.content) ? message.content : [])).filter(block => block.type === 'tool_result');
    for (const id of [3, 4, 5, 6]) {
      const result = nativeResults.find(block => block.tool_use_id === `toolu_probe_${id}`);
      assert(result && !result.is_error, `native alias tool ${id} failed`);
      if (id >= 5) assert(JSON.stringify(result.content).includes(sources['cli-tool/components/skills/testing/probe-skill/references/deep/checklist.txt'].trim()), 'native Read did not access the private resource');
    }
    const deduplicatedProof = await proof('deduplicated');
    assert.equal(deduplicatedProof.privateSkill, false, 'private duplicate skill remains');
    assert.equal(deduplicatedProof.privateCommand, false, 'private duplicate command remains');
    assert.deepEqual(await sourceSnapshot(repository), repositoryBefore, 'preparation modified repository resources or modes');
    assert.deepEqual(await sourceSnapshot(baseline), baselineBefore, 'preparation modified baseline resources or modes');
    assert.deepEqual(await retainedCacheSnapshot(), dedupCacheBefore, 'deduplication changed retained generation or payload content');
    assert.equal(await fs.readFile(path.join(root, 'cache/profiles/activation/current.json'), 'utf8'), pointer);
    assert.equal(await fs.readFile(path.join(root, 'catalog-requests'), 'utf8'), catalogBefore);
    console.log('PASS identical baseline/catalog/repository components and baseline aliases expand through native Skill; Read reaches private resources without discoverable duplicates or cache changes');

    const resource = path.join(repository, 'skills/probe-skill/references/deep/checklist.txt');
    await fs.writeFile(resource, 'AIRUN_DIFFERING_RESOURCE_SECRET');
    const conflictBefore = await sourceSnapshot(repository), conflictCacheBefore = await retainedCacheSnapshot();
    plan = []; turn = 0;
    const conflicting = await launch({ ...env, AIRUN_ACCEPTANCE_RUN: 'conflicting' }, 'This model task must not start.');
    assert.notEqual(conflicting.code, 0, 'different nested resource unexpectedly activated');
    assert.equal(requests.length, 0, 'component conflict started a model request');
    assert.match(conflicting.stderr, /invocation collision: probe-skill/);
    assert(conflicting.stderr.includes('catalog skills:testing/probe-skill'));
    assert(conflicting.stderr.includes(path.join(baseline, 'skills/probe-skill')));
    assert(conflicting.stderr.includes(path.join(repository, 'skills/probe-skill')));
    assert(!conflicting.stderr.includes('AIRUN_DIFFERING_RESOURCE_SECRET'));
    await assert.rejects(fs.stat(path.join(root, 'run-proofs/conflicting.json')), { code: 'ENOENT' });
    assert.deepEqual(await sourceSnapshot(repository), conflictBefore);
    assert.deepEqual(await sourceSnapshot(baseline), baselineBefore);
    assert.deepEqual(await retainedCacheSnapshot(), conflictCacheBefore, 'conflict changed retained generation or payload content');
    assert.equal(await fs.readFile(path.join(root, 'cache/profiles/activation/current.json'), 'utf8'), pointer);
    console.log('PASS same-body nested-resource conflict reports both sources before native hooks or model execution and preserves generation');
    // Keep the existing removal/resume/concurrency phases scoped to their
    // original repository-independent fixture.
    await fs.rm(repository, { recursive: true });
    await restoreBaseline();
    assert.deepEqual(await sourceSnapshot(baseline), originalBaseline, 'dedup phase did not restore the original baseline');
    await fs.rm(path.join(root, 'mod-executed'));
    plan = []; turn = 0;
    const removed = await launch({ ...env, AIRUN_PROFILE_MANIFEST: path.join(root, 'removed.json'), AIRUN_ACCEPTANCE_RUN: 'removed' }, 'Verify the reduced profile.');
    const removedRequests = requests.splice(0);
    assert.equal(removed.code, 0, `removed selection failed: ${removed.stderr}`);
    assert(removedRequests.length > 0, 'removed profile produced no real model request');
    const actual = JSON.stringify(removedRequests);
    for (const absent of ['AIRUN_MAIN_ROLE_ACTIVE', 'probe-skill', 'probe-command', 'probe-native', 'mcp__probe__credential_probe', 'mcp__second__credential_probe', 'mcp__npmprobe__credential_probe', 'AIRUN_NATIVE_HOOK_EXECUTED']) {
      assert(!actual.includes(absent), `removed capability remains active: ${absent}`);
    }
    await assert.rejects(fs.stat(path.join(root, 'mod-executed')), { code: 'ENOENT' });
    assert.equal(await fs.readFile(path.join(root, 'cache/profiles/activation/current.json'), 'utf8'), pointer);
    console.log('PASS removed role, skill, command, native plugin, MCP and mod stay absent with retained artifacts');
    const removedSession = sessionID(removed.stdout);
    assert.notEqual(removedSession, selectedSession);
    await transcriptFor(removedSession);
    assert((await fs.readFile(firstTranscript, 'utf8')).includes(initialPrompt), 'later run lost previous native transcript');
    console.log('PASS two sequential real Claude sessions preserve separate native transcripts across profile edits');

    turn = 0;
    plan = [{ name: 'mcp__npmprobe__credential_probe', input: {} }];
    const restoredEnv = { ...env, AIRUN_ACCEPTANCE_RUN: 'restored', AIRUN_ACCEPTANCE_PREVIOUS_TRANSCRIPT: path.relative(path.join(root, 'state'), firstTranscript) };
    const restored = await launch(restoredEnv, 'Verify the restored specialist.');
    const restoredRequests = requests.splice(0);
    assert.equal(restored.code, 0, `restored selection failed: ${restored.stderr}`);
    assert(JSON.stringify(restoredRequests[0].system).includes('AIRUN_MAIN_ROLE_ACTIVE'));
    assert(JSON.stringify(restoredRequests).includes('AIRUN_NATIVE_HOOK_EXECUTED'));
    assert(JSON.stringify(restoredRequests).includes('AIRUN_MCP_third_CREDENTIAL_OK'), 'warm actual Claude did not execute the retained npm MCP binary');
    assert.deepEqual(await runtimeSnapshot(), retainedRuntime, 'warm preparation changed retained npm runtime or inventory');
    assert.equal(await fs.readFile(path.join(root, 'npm-invocations'), 'utf8'), 'install\n', 'warm preparation invoked npm again');
    console.log('PASS npm MCP executes with registry inaccessible, empty npm cache, unchanged retained runtime and no reinstall');
    assert.equal(await fs.readFile(path.join(root, 'mod-executed'), 'utf8'), 'AIRUN_FUNCTION_HOOK_EXECUTED');
    assert((await proof('restored')).historyImported, 'native hook could not see imported prior transcript in private config');
    assert.equal(await fs.readFile(path.join(root, 'catalog-requests'), 'utf8'), catalogBefore, 'warm preparation fetched the catalog again');
    assert.equal(await fs.readFile(path.join(root, 'cache/profiles/activation/current.json'), 'utf8'), pointer);
    console.log('PASS re-adding selection restores actual role/plugin/mod activation without refreshing resolutions');

    plan = []; turn = 0;
    const resumed = await launch({ ...restoredEnv, AIRUN_ACCEPTANCE_RUN: 'resumed' }, 'AIRUN_RESUMED_PROMPT: continue the retained session.', ['--resume', selectedSession]);
    assert.equal(resumed.code, 0, `native resume failed: ${resumed.stderr}`);
    assert.equal(sessionID(resumed.stdout), selectedSession, 'native resume did not retain session identity');
    const resumedRequests = requests.splice(0);
    assert(JSON.stringify(resumedRequests).includes(initialPrompt), 'native resumed model request lost prior conversation');
    assert(JSON.stringify(resumedRequests).includes('AIRUN_RESUMED_PROMPT'), 'native resumed model request lost new message');
    console.log('PASS actual Claude --resume loads persisted conversation in a fresh private configuration');

    let release;
    concurrentBarrier = { promise: new Promise(resolve => { release = resolve; }), release: () => release() };
    const concurrent = await Promise.all(['one', 'two'].map(id => launch({
      ...restoredEnv, AIRUN_ACCEPTANCE_RUN: `concurrent-${id}`
    }, `AIRUN_CONCURRENT_${id}: verify an independent session.`)));
    assert.equal(concurrentArrivals, 2, 'sessions did not reach the model endpoint concurrently');
    for (const result of concurrent) {
      assert.equal(result.code, 0, `concurrent native session failed: ${result.stderr}`);
      await transcriptFor(sessionID(result.stdout));
    }
    const proofs = await Promise.all(['concurrent-one', 'concurrent-two'].map(proof));
    assert.notEqual(proofs[0].config, proofs[1].config, 'concurrent sessions shared active configuration');
    assert(proofs.every(item => item.historyImported), 'concurrent sessions did not import prior history');
    assert((await fs.readFile(firstTranscript, 'utf8')).includes(initialPrompt));
    console.log('PASS concurrent actual sessions use different private configuration paths and retain both native transcripts');
    const requestText = JSON.stringify([...selectedRequests, ...deduplicatedRequests, ...removedRequests, ...restoredRequests, ...resumedRequests, ...requests]);
    for (const value of ['synthetic-provider-only', 'synthetic-mcp-first', 'synthetic-mcp-second', 'synthetic-mcp-third']) {
      assert(!requestText.includes(value), 'credential value entered model request content');
    }
    await checkRetainedSecrets(path.join(root, 'cache'));
    assert.deepEqual(await runtimeSnapshot(), retainedRuntime);
    assert.equal(await fs.readFile(path.join(root, 'npm-invocations'), 'utf8'), 'install\n');
    console.log('PASS synthetic credentials remain absent from retained artifacts, receipts and model request content');
    console.log('REAL: pinned installer agent/skill downloads, npm provisioning and retained binary, Claude CLI, supervisor/cache, native installation, MCP, hooks and history.');
    console.log('FIXTURES: catalog responses (remaining catalog installs injected), npm registry/package, minimal baseline, MCP child and loopback model responses. External networking is disabled.');
  } finally {
    await registry.close();
    server.closeAllConnections();
    await new Promise(resolve => server.close(resolve));
  }
}

async function launch(env, prompt, extra = []) {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, ['/acceptance/acceptance.mjs', '--session', 'claude', '-p', prompt,
      '--dangerously-skip-permissions', '--output-format', 'stream-json', '--verbose', '--max-turns', '8', ...extra], {
      cwd: path.join(root, 'workspace'), env, stdio: ['ignore', 'pipe', 'pipe']
    });
    let stdout = '', stderr = '';
    child.stdout.on('data', chunk => { stdout += chunk; });
    child.stderr.on('data', chunk => { stderr += chunk; });
    const timeout = setTimeout(() => { child.kill('SIGTERM'); setTimeout(() => child.kill('SIGKILL'), 3000).unref(); }, 45000);
    child.once('error', error => { clearTimeout(timeout); reject(error); });
    child.once('close', async code => {
      clearTimeout(timeout);
      await fs.writeFile(path.join(root, 'session-output.jsonl'), stdout);
      await fs.writeFile(path.join(root, 'session-stderr.log'), stderr);
      resolve({ code, stdout, stderr });
    });
  });
}

function sessionID(stdout) {
  const init = stdout.split('\n').filter(Boolean).map(line => JSON.parse(line)).find(event => event.type === 'system' && event.subtype === 'init');
  assert(init?.session_id, 'actual CLI emitted no session identity');
  return init.session_id;
}

async function transcriptFor(session) {
  const projects = path.join(root, 'state/projects');
  for (const entry of await fs.readdir(projects)) {
    const file = path.join(projects, entry, `${session}.jsonl`);
    try { await fs.access(file); return file; } catch {}
  }
  assert.fail('actual CLI session transcript was not retained');
}

async function proof(run) {
  return JSON.parse(await fs.readFile(path.join(root, 'run-proofs', `${run}.json`), 'utf8'));
}

async function checkRetainedSecrets(directory) {
  for (const entry of await fs.readdir(directory, { withFileTypes: true })) {
    const file = path.join(directory, entry.name);
    if (entry.isDirectory()) await checkRetainedSecrets(file);
    else if (entry.isFile()) {
      const text = await fs.readFile(file, 'utf8');
      for (const value of ['synthetic-provider-only', 'synthetic-mcp-first', 'synthetic-mcp-second', 'synthetic-mcp-third']) {
        assert(!text.includes(value), 'credential value entered a retained artifact or receipt');
      }
    }
  }
}

async function runtimeSnapshot() {
  const profiles = path.join(root, 'cache/profiles/activation');
  const pointer = JSON.parse(await fs.readFile(path.join(profiles, 'current.json'), 'utf8'));
  const generation = JSON.parse(await fs.readFile(path.join(profiles, 'generations', pointer.generation + '.json'), 'utf8'));
  const record = generation.records['mcps:testing/npm-mcp'].runtimes.npmprobe;
  return { record, files: await inventory(record.directory, { links: true }) };
}

async function retainedCacheSnapshot() {
  const cache = path.join(root, 'cache');
  return {
    pointer: await fs.readFile(path.join(cache, 'profiles/activation/current.json'), 'utf8'),
    generations: await sourceSnapshot(path.join(cache, 'profiles/activation/generations')),
    payloads: await sourceSnapshot(path.join(cache, 'payloads')),
    runtimes: await sourceSnapshot(path.join(cache, 'runtimes'))
  };
}

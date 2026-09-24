import assert from 'node:assert/strict';
import * as fs from 'node:fs/promises';
import path from 'node:path';
import http from 'node:http';
import { createHash } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import { root, put } from './fixtures.mjs';

export async function startRegistry() {
  const packageRoot = path.join(root, 'registry-package');
  const pkg = { name: 'airun-fixture-mcp', version: '1.0.0', type: 'module', bin: { 'airun-fixture-mcp': 'bin.mjs' } };
  await put(packageRoot, 'package/package.json', pkg);
  await put(packageRoot, 'package/bin.mjs', '#!/usr/bin/env node\n' + await fs.readFile('/acceptance/mcp.mjs', 'utf8'));
  await fs.chmod(path.join(packageRoot, 'package/bin.mjs'), 0o755);
  const tarball = path.join(root, 'airun-fixture-mcp-1.0.0.tgz');
  assert.equal(spawnSync('tar', ['-czf', tarball, '-C', packageRoot, 'package']).status, 0);
  const bytes = await fs.readFile(tarball);
  const requests = [];
  let url;
  const server = http.createServer((request, response) => {
    requests.push(request.url);
    if (request.url === '/airun-fixture-mcp/-/airun-fixture-mcp-1.0.0.tgz') {
      response.setHeader('content-type', 'application/octet-stream'); response.end(bytes); return;
    }
    if (request.url === '/airun-fixture-mcp') {
      response.setHeader('content-type', 'application/json');
      response.end(JSON.stringify({ name: pkg.name, 'dist-tags': { latest: pkg.version }, versions: {
        [pkg.version]: { ...pkg, dist: {
          tarball: `${url}/airun-fixture-mcp/-/airun-fixture-mcp-1.0.0.tgz`,
          shasum: createHash('sha1').update(bytes).digest('hex'),
          integrity: 'sha512-' + createHash('sha512').update(bytes).digest('base64')
        } }
      } }));
      return;
    }
    response.writeHead(404); response.end('unknown fixture package');
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  url = `http://127.0.0.1:${server.address().port}`;
  let closed = false;
  return { url, requests, close: async () => {
    if (closed) return;
    closed = true;
    server.closeAllConnections();
    await new Promise(resolve => server.close(resolve));
  } };
}

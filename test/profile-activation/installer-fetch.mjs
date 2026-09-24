// Loaded only into the real pinned installer child via NODE_OPTIONS. The CLI
// itself, argument parser and recursive downloads remain unmodified.
import * as fs from 'node:fs/promises';
const source = JSON.parse(await fs.readFile(process.env.AIRUN_TEST_CATALOG_SOURCE, 'utf8'));
const rawPrefix = 'https://raw.githubusercontent.com/davila7/claude-code-templates/main/';
const apiPrefix = 'https://api.github.com/repos/davila7/claude-code-templates/contents/';
globalThis.fetch = async input => {
  const url = typeof input === 'string' ? input : input.url ?? String(input);
  await fs.appendFile(process.env.AIRUN_TEST_CATALOG_REQUESTS, url + '\n');
  if (url.startsWith(rawPrefix)) {
    const bytes = source[url.slice(rawPrefix.length)];
    return new Response(bytes ?? 'unknown fixture file', { status: bytes === undefined ? 404 : 200 });
  }
  if (url.startsWith(apiPrefix)) {
    const directory = url.slice(apiPrefix.length).split('?')[0];
    const children = new Map();
    for (const file of Object.keys(source)) if (file.startsWith(directory + '/')) {
      const relative = file.slice(directory.length + 1);
      const name = relative.split('/')[0];
      const child = directory + '/' + name;
      children.set(name, {
        name, path: child, type: relative.includes('/') ? 'dir' : 'file',
        url: apiPrefix + child, download_url: rawPrefix + child
      });
    }
    return Response.json([...children.values()], { status: children.size ? 200 : 404 });
  }
  throw new Error('unexpected installer fixture request');
};

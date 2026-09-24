// Executed by the real Claude SessionStart hook, after production preparation.
import * as fs from 'node:fs/promises';
import path from 'node:path';
const config = process.env.CLAUDE_CONFIG_DIR;
const settings = JSON.parse(await fs.readFile(path.join(config, 'settings.json'), 'utf8'));
await fs.writeFile('/proof/session.json', JSON.stringify({
  uid: process.getuid(), config, agent: settings.agent, permissions: settings.permissions,
  cacheUID: (await fs.stat('/var/lib/airun/components')).uid,
  stateUID: (await fs.stat('/var/lib/airun/state')).uid,
}));

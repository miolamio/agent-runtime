import assert from 'node:assert/strict';
import * as fs from 'node:fs/promises';
import path from 'node:path';
const settings = JSON.parse(await fs.readFile(path.join(process.env.CLAUDE_CONFIG_DIR, 'settings.json'), 'utf8'));
assert.equal(process.getuid(), 1001, 'no-profile entrypoint must drop root');
assert.equal(settings.permissions?.defaultMode, 'bypassPermissions', 'native plugin installation erased image permission defaults');
for (const name of ['context7', 'skill-creator', 'superpowers']) assert.equal(settings.enabledPlugins?.[`${name}@claude-plugins-official`], true, `baseline ${name} is not enabled`);
console.log('PASS fresh no-profile production startup retains image permission defaults and native plugins');

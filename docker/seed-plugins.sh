#!/bin/bash
set -euo pipefail

# Capture native installer output once, explicitly preserving the image's
# original three native plugins and its independently supplied direct skills.
USERNAME="${1:-claude}"
HOME_DIR="/home/${USERNAME}"
CONFIG_DIR="${HOME_DIR}/.claude"
PLUGINS_DIR="${CONFIG_DIR}/plugins"
BASELINE=/opt/airun/profile-baseline
BASE_PLUGINS=(context7@claude-plugins-official skill-creator@claude-plugins-official superpowers@claude-plugins-official)

mkdir -p "$CONFIG_DIR" "$BASELINE"
chown "${USERNAME}:${USERNAME}" "$CONFIG_DIR"
# Native installation writes settings.json. Seed defaults first so subsequent
# unprofiled startup does not mistake plugin-only settings for a complete file.
cp "$BASELINE/settings.json" "$CONFIG_DIR/settings.json"
chown "${USERNAME}:${USERNAME}" "$CONFIG_DIR/settings.json"
CLAUDE_VERSION=$(gosu "$USERNAME" claude --version | awk '{print $1}')
jq -n --arg version "$CLAUDE_VERSION" '{hasCompletedOnboarding:true,hasTrustDialogAccepted:true,lastOnboardingVersion:$version,autoUpdaterStatus:"disabled",projects:{}}' > "$CONFIG_DIR/.claude.json"
chown "${USERNAME}:${USERNAME}" "$CONFIG_DIR/.claude.json"

native() {
    gosu "$USERNAME" env HOME="$HOME_DIR" CLAUDE_CONFIG_DIR="$CONFIG_DIR" DISABLE_AUTOUPDATER=1 claude plugin "$@"
}

native marketplace add anthropics/claude-plugins-official
native marketplace add miolamio/agent-skills
native marketplace add anthropics/skills
for reference in "${BASE_PLUGINS[@]}"; do
    native install "$reference" --scope user
    installed_path=$(jq -er --arg ref "$reference" '.plugins[$ref][] | select(.scope == "user") | .installPath' "$PLUGINS_DIR/installed_plugins.json")
    test -d "$installed_path"
    test -f "$installed_path/.claude-plugin/plugin.json"
    jq -e --arg name "${reference%@*}" '.name == $name' "$installed_path/.claude-plugin/plugin.json" >/dev/null
    native validate "$installed_path"
done
test -f "$PLUGINS_DIR/known_marketplaces.json"
jq -e '.permissions.defaultMode == "bypassPermissions"' "$CONFIG_DIR/settings.json" >/dev/null

SKILLS_SOURCE="$PLUGINS_DIR/marketplaces/miolamio-agent-skills/skills"
test -d "$SKILLS_SOURCE"
mkdir -p "$CONFIG_DIR/skills"
cp -a "$SKILLS_SOURCE/." "$CONFIG_DIR/skills/"
for skill in ascii-art-beautifier en-ru-translator-adv krrkt ru-editor ru-textovod telegram-cli; do
    test -f "$CONFIG_DIR/skills/$skill/SKILL.md"
done
chown -R "${USERNAME}:${USERNAME}" "$CONFIG_DIR/skills"
cp -a "$PLUGINS_DIR" "$BASELINE/plugins"
cp -a "$CONFIG_DIR/skills" "$BASELINE/skills"
jq -n '{version:1,native_plugins:["context7@claude-plugins-official","skill-creator@claude-plugins-official","superpowers@claude-plugins-official"]}' > "$BASELINE/baseline.json"
# Record all captured files, not just entrypoint manifests. The adapter verifies
# this inventory before it uses the image baseline for a profile launch.
node --input-type=module - "$BASELINE" <<'NODE'
import * as fs from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
const root = process.argv[2];
const files = [];
function walk(relative = '') {
  for (const entry of fs.readdirSync(path.join(root, relative), { withFileTypes: true }).sort((a,b) => a.name.localeCompare(b.name))) {
    if (entry.name === '.git') continue;
    const name = path.join(relative, entry.name);
    const absolute = path.join(root, name);
    if (entry.isDirectory()) walk(name);
    else if (entry.isFile()) {
      const bytes = fs.readFileSync(absolute);
      files.push({path:name,sha256:createHash('sha256').update(bytes).digest('hex'),size:bytes.length,mode:fs.statSync(absolute).mode & 0o777});
    } else if (entry.isSymbolicLink()) {
      const resolved = fs.realpathSync(absolute);
      if (!resolved.startsWith(root + path.sep)) throw Error(`baseline symlink leaves capture: ${name}`);
      files.push({path:name,symlink:fs.readlinkSync(absolute)});
    } else throw Error(`unsupported baseline file: ${name}`);
  }
}
walk();
fs.writeFileSync(path.join(root, 'inventory.json'), JSON.stringify({version:1,files}, null, 2) + '\n');
NODE
echo "[seed-plugins] verified native plugins and direct skills captured in $BASELINE"

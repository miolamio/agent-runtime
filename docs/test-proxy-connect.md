# Test Cases: Proxy Connect / Disconnect

## Prerequisites

- Proxy server running: `airun proxy serve` (or any machine with proxy)
- Known proxy URL and valid token (e.g. `sk-ai-...`)
- Claude Code CLI installed on the test machine
- `jq` installed (for bash tests)

> **Warning:** Run these tests on a dedicated test machine, NOT on your dev machine.

---

## Test 1: Go — Fresh Connect

**Setup:** Ensure no proxy settings exist.
```bash
# Backup existing configs if any
cp ~/.claude/settings.json ~/.claude/settings.json.bak 2>/dev/null
cp ~/.claude.json ~/.claude.json.bak 2>/dev/null
```

**Run:**
```bash
airun proxy connect http://PROXY:8080 sk-ai-TOKEN
```

**Verify:**
```bash
echo "=== settings.json env ==="
jq '.env | {ANTHROPIC_AUTH_TOKEN, ANTHROPIC_BASE_URL, ANTHROPIC_DEFAULT_SONNET_MODEL, API_TIMEOUT_MS}' ~/.claude/settings.json

echo "=== claude.json bypass ==="
jq '{hasCompletedOnboarding, hasTrustDialogAccepted, lastOnboardingVersion, _airunManaged, customApiKeyResponses}' ~/.claude.json
```

**Expected:**
- `settings.json` has all 6 env keys
- `claude.json` has `hasCompletedOnboarding: true`, `hasTrustDialogAccepted: true`
- `claude.json` has `_airunManaged: true`
- `customApiKeyResponses.approved` contains last 20 chars of the token

**Smoke test:**
```bash
claude -p "Say hello" --no-input
```
Should connect to proxy, no login/onboarding dialogs.

---

## Test 2: Go — Disconnect

**Run:**
```bash
airun proxy disconnect
```

**Verify:**
```bash
echo "=== settings.json env (should be clean) ==="
jq '.env | {ANTHROPIC_AUTH_TOKEN, ANTHROPIC_BASE_URL, ANTHROPIC_DEFAULT_SONNET_MODEL}' ~/.claude/settings.json

echo "=== claude.json exists? ==="
ls -la ~/.claude.json 2>/dev/null || echo "REMOVED (expected if we created it)"
```

**Expected:**
- `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL`, model keys are `null`/missing
- `~/.claude.json` is deleted (if `_airunManaged` was true)
- Other keys in `settings.json` (permissions, plugins, etc.) are preserved

---

## Test 3: Go — Connect with Existing settings.json

**Setup:** Create a settings.json with existing user settings.
```bash
mkdir -p ~/.claude
cat > ~/.claude/settings.json << 'EOF'
{
  "permissions": { "defaultMode": "plan" },
  "env": { "MY_CUSTOM_VAR": "keep-me" }
}
EOF
```

**Run:**
```bash
airun proxy connect http://PROXY:8080 sk-ai-TOKEN
```

**Verify:**
```bash
jq '.env.MY_CUSTOM_VAR' ~/.claude/settings.json
jq '.permissions.defaultMode' ~/.claude/settings.json
```

**Expected:**
- `MY_CUSTOM_VAR` = `"keep-me"` (preserved)
- `permissions.defaultMode` = `"plan"` (preserved)
- Proxy env keys added alongside existing ones

---

## Test 4: Go — Disconnect Preserves User Settings

**Run (after Test 3):**
```bash
airun proxy disconnect
```

**Verify:**
```bash
jq '.env.MY_CUSTOM_VAR' ~/.claude/settings.json
jq '.permissions' ~/.claude/settings.json
```

**Expected:**
- `MY_CUSTOM_VAR` = `"keep-me"` (still there)
- `permissions` block untouched
- Only proxy-related keys removed

---

## Test 5: Go — Disconnect with Pre-existing claude.json

**Setup:** Create a claude.json that was NOT created by us.
```bash
cat > ~/.claude.json << 'EOF'
{
  "hasCompletedOnboarding": true,
  "hasTrustDialogAccepted": true,
  "userID": "user-original-id",
  "theme": "dark"
}
EOF
```

**Run:**
```bash
airun proxy connect http://PROXY:8080 sk-ai-TOKEN
airun proxy disconnect
```

**Verify:**
```bash
jq '{userID, theme, hasCompletedOnboarding}' ~/.claude.json
jq 'has("customApiKeyResponses")' ~/.claude.json
jq 'has("_airunManaged")' ~/.claude.json
```

**Expected:**
- File still exists (NOT deleted, because it pre-existed)
- `userID` = `"user-original-id"` (preserved)
- `theme` = `"dark"` (preserved)
- `customApiKeyResponses` removed
- `_airunManaged` removed

---

## Test 6: Bash Script — Connect

**Setup:** Clean state.
```bash
rm -f ~/.claude.json
rm -f ~/.claude/settings.json
```

**Run:**
```bash
bash scripts/connect-proxy.sh http://PROXY:8080 sk-ai-TOKEN
```

**Verify:** Same checks as Test 1.

---

## Test 7: Bash Script — Disconnect

**Run:**
```bash
bash scripts/connect-proxy.sh --disconnect
```

**Verify:** Same checks as Test 2.

---

## Test 8: Bash Script — One-liner (curl pipe)

**Run:**
```bash
curl -fsSL https://raw.githubusercontent.com/miolamio/agent-runtime/main/scripts/connect-proxy.sh | bash -s -- http://PROXY:8080 sk-ai-TOKEN
```

**Verify:** Same as Test 1.

**Disconnect via curl:**
```bash
curl -fsSL https://raw.githubusercontent.com/miolamio/agent-runtime/main/scripts/connect-proxy.sh | bash -s -- --disconnect
```

---

## Test 9: PowerShell — Connect (Windows)

**Run:**
```powershell
.\scripts\connect-proxy.ps1 http://PROXY:8080 sk-ai-TOKEN
```

**Verify:**
```powershell
(Get-Content ~/.claude/settings.json | ConvertFrom-Json).env | Select-Object ANTHROPIC_AUTH_TOKEN, ANTHROPIC_BASE_URL, ANTHROPIC_DEFAULT_SONNET_MODEL
(Get-Content ~/.claude.json | ConvertFrom-Json) | Select-Object hasCompletedOnboarding, hasTrustDialogAccepted, _airunManaged
```

---

## Test 10: PowerShell — Disconnect (Windows)

**Run:**
```powershell
.\scripts\connect-proxy.ps1 --disconnect
```

**Verify:** Proxy keys removed, user settings preserved.

---

## Test 11: Model Selection

**Run:**
```bash
airun proxy connect http://PROXY:8080 sk-ai-TOKEN
# When prompted for default model, choose a non-default one (e.g. kimi-k2.5)
```

**Verify:**
```bash
jq '.env.ANTHROPIC_DEFAULT_SONNET_MODEL' ~/.claude/settings.json
```

**Expected:** The model you chose, not the first one in the list.

---

## Test 12: Invalid Token

**Run:**
```bash
airun proxy connect http://PROXY:8080 invalid-token-123
```

**Expected:**
- Error: `invalid API key (HTTP 401)`
- No files modified

---

## Test 13: Unreachable Proxy

**Run:**
```bash
airun proxy connect http://localhost:99999 sk-ai-TOKEN
```

**Expected:**
- Error: `cannot connect to proxy`
- No files modified

---

## Test 14: End-to-End — Claude Code Works Through Proxy

**Run:**
```bash
airun proxy connect http://PROXY:8080 sk-ai-TOKEN
claude -p "What is 2+2? Reply with just the number." --no-input
```

**Expected:**
- No login/onboarding dialogs
- Model responds (e.g. "4")
- Check proxy server logs show the request

**Cleanup:**
```bash
airun proxy disconnect
```

---

## Cleanup After All Tests

```bash
# Restore original configs
mv ~/.claude/settings.json.bak ~/.claude/settings.json 2>/dev/null
mv ~/.claude.json.bak ~/.claude.json 2>/dev/null
```

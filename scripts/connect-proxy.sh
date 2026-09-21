#!/usr/bin/env bash
set -euo pipefail

# Connect/disconnect Claude Code CLI to an airun proxy.
#
# Connect:
#   bash connect-proxy.sh <proxy-url> <api-key>
#   curl -fsSL https://raw.githubusercontent.com/miolamio/agent-runtime/main/scripts/connect-proxy.sh | bash -s -- <proxy-url> <api-key>
#
# Disconnect (clean all proxy settings):
#   bash connect-proxy.sh --disconnect

# Shared journal schema: _airunBackup={version:1,created,before,after}.
# Never infer ownership from the legacy _airunManaged boolean alone.
umask 077
command -v jq >/dev/null || { echo "Error: jq is required." >&2; exit 1; }
JOURNAL_JQ=$(cat <<'JQ'
def undo($before; $after):
  reduce ((($before | keys) + ($after | keys) | unique)[]) as $k (. ;
    if (($before|has($k)) == ($after|has($k)) and $before[$k] == $after[$k]) then .
    elif (has($k) == ($after|has($k)) and .[$k] == $after[$k]) then
      if $before|has($k) then .[$k] = $before[$k] else del(.[$k]) end
    elif (.[$k]|type) == "object" and ($after[$k]|type) == "object" then
      .[$k] |= undo(($before[$k] // {}); $after[$k]) |
      if (($before|has($k)|not) and .[$k] == {}) then del(.[$k]) else . end
    elif (.[$k]|type) == "array" and ($after[$k]|type) == "array" then
      .[$k] |= map(. as $item | select(($after[$k]|index($item)) == null or (($before[$k] // [])|index($item)) != null)) |
      if (($before|has($k)|not) and .[$k] == []) then del(.[$k]) else . end
    else . end);
def checked_backup:
  if has("_airunBackup") and (._airunBackup.version != 1 or (._airunBackup.before|type) != "object" or (._airunBackup.after|type) != "object")
  then error("unsupported or damaged airun settings backup") else . end;
def journal($current; $created):
  . as $desired |
  ($current | checked_backup) as $c |
  (if $c|has("_airunBackup") then
     $c | del(._airunBackup, ._airunManaged) | undo($c._airunBackup.before; $c._airunBackup.after)
   else $c end) as $before |
  ($desired | del(._airunBackup, ._airunManaged)) as $after |
  (reduce ($after|keys[]) as $k ({before:{},after:{}};
    if ($before|has($k)|not) or $before[$k] != $after[$k] then
      .after[$k] = $after[$k] |
      if $before|has($k) then .before[$k] = $before[$k] else . end
    else . end)) as $delta |
  $after + {_airunManaged:true, _airunBackup:($delta + {version:1,created:(if $c|has("_airunBackup") then $c._airunBackup.created else $created end)})};
JQ
)
read_document() {
    if [ -e "$1" ]; then jq -e 'if type == "object" then . else error("expected JSON object") end' "$1"
    else printf '{}\n'; fi
}
write_document() {
    local target="$1" data="$2" temporary
    mkdir -p "$(dirname "$target")"
    temporary=$(mktemp "${target}.airun-XXXXXX")
    if ! printf '%s\n' "$data" > "$temporary" || ! mv -f "$temporary" "$target"; then
        rm -f "$temporary"
        return 1
    fi
}
if [ "${1:-}" = "--disconnect" ] || [ "${1:-}" = "disconnect" ]; then
    for target in "$HOME/.claude/settings.json" "$HOME/.claude.json"; do
        current=$(read_document "$target")
        printf '%s\n' "$current" | jq -e "$JOURNAL_JQ checked_backup" >/dev/null
        if [ "$(printf '%s\n' "$current" | jq 'has("_airunBackup")')" = true ]; then
            updated=$(printf '%s\n' "$current" | jq "$JOURNAL_JQ ._airunBackup as \$b | del(._airunBackup, ._airunManaged) | undo(\$b.before; \$b.after)")
            if [ "$(printf '%s\n' "$current" | jq '._airunBackup.created')" = true ] && [ "$updated" = '{}' ]; then
                rm -f "$target"
            else write_document "$target" "$updated"; fi
        fi
    done
    echo "  Previous settings restored; subsequent user edits preserved."
    exit 0
fi

# ── Connect mode ──
PROXY_URL="${1:-}"
API_KEY="${2:-}"

if [ -z "$PROXY_URL" ]; then
    printf "  Proxy URL (e.g. http://server:8080): "
    read -r PROXY_URL
fi
if [ -z "$API_KEY" ]; then
    printf "  API key (sk-ai-...): "
    read -r API_KEY
fi

if [ -z "$PROXY_URL" ] || [ -z "$API_KEY" ]; then
    echo "Usage: $0 <proxy-url> <api-key>" >&2
    echo "       $0 --disconnect" >&2
    exit 1
fi

PROXY_URL="${PROXY_URL%/}"

for cmd in curl jq; do
    if ! command -v "$cmd" &>/dev/null; then
        echo "Error: $cmd is required but not installed." >&2
        exit 1
    fi
done

# ── Validate connection ──
printf "\n  Connecting to %s... " "$PROXY_URL"
MODELS_JSON=$(curl -sf -H "x-api-key: $API_KEY" "$PROXY_URL/v1/models" 2>/dev/null) || {
    echo "FAILED"
    echo "  Could not connect. Check URL and API key." >&2
    exit 1
}

MODELS=$(echo "$MODELS_JSON" | jq -r '.data[].id' 2>/dev/null)
MODEL_COUNT=$(echo "$MODELS" | wc -l | tr -d ' ')

if [ -z "$MODELS" ]; then
    echo "FAILED"
    echo "  No models available." >&2
    exit 1
fi

echo "OK ($MODEL_COUNT models)"
echo ""
echo "$MODELS" | while IFS= read -r m; do
    echo "  [x] $m"
done

# Prefer glm-5.3 if available, otherwise first model
DEFAULT_MODEL=$(echo "$MODELS" | head -1)
PREFERRED=$(echo "$MODELS" | grep -x "glm-5.3" || true)
if [ -n "$PREFERRED" ]; then
    DEFAULT_MODEL="$PREFERRED"
fi
if [ "$MODEL_COUNT" -gt 1 ]; then
    printf "\n  Default model [%s]: " "$DEFAULT_MODEL"
    read -r CHOSEN
    if [ -n "$CHOSEN" ]; then
        DEFAULT_MODEL="$CHOSEN"
    fi
fi

# Validate both documents before changing either one.
SETTINGS_FILE="$HOME/.claude/settings.json"
CLAUDE_JSON="$HOME/.claude.json"
SETTINGS=$(read_document "$SETTINGS_FILE")
CLAUDE_SETTINGS=$(read_document "$CLAUDE_JSON")
settings_created=true; [ ! -e "$SETTINGS_FILE" ] || settings_created=false
claude_created=true; [ ! -e "$CLAUDE_JSON" ] || claude_created=false
UPDATED_SETTINGS=$(printf '%s\n' "$SETTINGS" | jq \
    --arg url "$PROXY_URL" --arg key "$API_KEY" --arg model "$DEFAULT_MODEL" --argjson created "$settings_created" \
    "$JOURNAL_JQ"'
    . as $current |
    if has("env") and (.env|type) != "object" then error("env must be an object") else . end |
    .env = (.env // {}) + {
      ANTHROPIC_AUTH_TOKEN:$key, ANTHROPIC_BASE_URL:$url,
      ANTHROPIC_DEFAULT_SONNET_MODEL:$model, ANTHROPIC_DEFAULT_OPUS_MODEL:$model,
      ANTHROPIC_DEFAULT_HAIKU_MODEL:$model, API_TIMEOUT_MS:"3000000"
    } | journal($current; $created)')
CLAUDE_VER=$(claude --version 2>/dev/null | head -1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' || echo "99.0.0")
KEY_TAIL="$API_KEY"
if [ "${#KEY_TAIL}" -gt 20 ]; then KEY_TAIL="${KEY_TAIL: -20}"; fi
USER_ID=$(od -An -tx1 -N32 /dev/urandom | tr -d ' \n')
UPDATED_CLAUDE=$(printf '%s\n' "$CLAUDE_SETTINGS" | jq \
    --arg ver "$CLAUDE_VER" --arg tail "$KEY_TAIL" --arg uid "$USER_ID" --argjson created "$claude_created" \
    "$JOURNAL_JQ"'
    . as $current |
    if has("customApiKeyResponses") and (.customApiKeyResponses|type) != "object" then error("customApiKeyResponses must be an object") else . end |
    .hasCompletedOnboarding = true | .hasTrustDialogAccepted = true |
    .lastOnboardingVersion = $ver | .autoUpdaterStatus = "disabled" |
    if has("numStartups") then . else .numStartups = 184 end |
    if has("userID") then . else .userID = $uid end |
    if has("projects") then . else .projects = {} end |
    .customApiKeyResponses = (.customApiKeyResponses // {}) |
    .customApiKeyResponses.approved = ((.customApiKeyResponses.approved // []) as $a | if $a|index($tail) then $a else $a + [$tail] end) |
    if .customApiKeyResponses|has("rejected") then . else .customApiKeyResponses.rejected = [] end |
    journal($current; $created)')
write_document "$SETTINGS_FILE" "$UPDATED_SETTINGS"
write_document "$CLAUDE_JSON" "$UPDATED_CLAUDE"
echo ""
echo "  Claude Code configured:"
echo "    URL:      $PROXY_URL"
echo "    Model:    $DEFAULT_MODEL"
echo "    Settings: $SETTINGS_FILE"
echo "    Auth:     $CLAUDE_JSON (onboarding bypassed)"
echo ""
echo "  Run 'claude' to start using the proxy."
echo "  To disconnect: $0 --disconnect"

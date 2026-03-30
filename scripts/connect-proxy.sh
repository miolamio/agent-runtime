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

# ── Disconnect mode ──
if [ "${1:-}" = "--disconnect" ] || [ "${1:-}" = "disconnect" ]; then
    SETTINGS_FILE="$HOME/.claude/settings.json"
    CLAUDE_JSON="$HOME/.claude.json"
    removed=0

    # Clean settings.json
    if [ -f "$SETTINGS_FILE" ] && command -v jq &>/dev/null; then
        KEYS='["ANTHROPIC_AUTH_TOKEN","ANTHROPIC_BASE_URL","ANTHROPIC_DEFAULT_SONNET_MODEL","ANTHROPIC_DEFAULT_OPUS_MODEL","ANTHROPIC_DEFAULT_HAIKU_MODEL","API_TIMEOUT_MS"]'
        has_any=$(jq --argjson keys "$KEYS" '[.env // {} | keys[] | select(. as $k | $keys | index($k))] | length' "$SETTINGS_FILE" 2>/dev/null || echo 0)
        if [ "$has_any" -gt 0 ]; then
            UPDATED=$(jq --argjson keys "$KEYS" 'if .env then .env |= with_entries(select(.key as $k | $keys | index($k) | not)) else . end' "$SETTINGS_FILE")
            echo "$UPDATED" > "$SETTINGS_FILE"
            removed=$((removed + 1))
        fi
    fi

    # Clean claude.json
    if [ -f "$CLAUDE_JSON" ] && command -v jq &>/dev/null; then
        is_managed=$(jq -r '._airunManaged // false' "$CLAUDE_JSON" 2>/dev/null)
        if [ "$is_managed" = "true" ]; then
            rm -f "$CLAUDE_JSON"
            removed=$((removed + 1))
        else
            has_car=$(jq 'has("customApiKeyResponses")' "$CLAUDE_JSON" 2>/dev/null || echo false)
            if [ "$has_car" = "true" ]; then
                UPDATED=$(jq 'del(._airunManaged, .customApiKeyResponses)' "$CLAUDE_JSON")
                echo "$UPDATED" > "$CLAUDE_JSON"
                removed=$((removed + 1))
            fi
        fi
    fi

    if [ "$removed" -eq 0 ]; then
        echo "  No proxy settings found."
    else
        echo "  Proxy settings removed."
        echo "  Claude Code will use its default Anthropic API."
    fi
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

# Prefer glm-5.1 if available, otherwise first model
DEFAULT_MODEL=$(echo "$MODELS" | head -1)
PREFERRED=$(echo "$MODELS" | grep -x "glm-5.1" || true)
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

# ── 1. Write ~/.claude/settings.json ──
SETTINGS_DIR="$HOME/.claude"
SETTINGS_FILE="$SETTINGS_DIR/settings.json"

mkdir -p "$SETTINGS_DIR"

if [ -f "$SETTINGS_FILE" ]; then
    UPDATED=$(jq \
        --arg url "$PROXY_URL" \
        --arg key "$API_KEY" \
        --arg model "$DEFAULT_MODEL" \
        '.env = (.env // {}) + {
            ANTHROPIC_AUTH_TOKEN: $key,
            ANTHROPIC_BASE_URL: $url,
            ANTHROPIC_DEFAULT_SONNET_MODEL: $model,
            ANTHROPIC_DEFAULT_OPUS_MODEL: $model,
            ANTHROPIC_DEFAULT_HAIKU_MODEL: $model,
            API_TIMEOUT_MS: "3000000"
        }' "$SETTINGS_FILE")
    echo "$UPDATED" > "$SETTINGS_FILE"
else
    jq -n \
        --arg url "$PROXY_URL" \
        --arg key "$API_KEY" \
        --arg model "$DEFAULT_MODEL" \
        '{
            env: {
                ANTHROPIC_AUTH_TOKEN: $key,
                ANTHROPIC_BASE_URL: $url,
                ANTHROPIC_DEFAULT_SONNET_MODEL: $model,
                ANTHROPIC_DEFAULT_OPUS_MODEL: $model,
                ANTHROPIC_DEFAULT_HAIKU_MODEL: $model,
                API_TIMEOUT_MS: "3000000"
            }
        }' > "$SETTINGS_FILE"
fi
chmod 600 "$SETTINGS_FILE"

# ── 2. Write ~/.claude.json (onboarding/auth bypass) ──
CLAUDE_JSON="$HOME/.claude.json"

# Detect Claude Code version
CLAUDE_VER=$(claude --version 2>/dev/null | head -1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' || echo "99.0.0")

# Last 20 chars of API key for trust
KEY_TAIL="${API_KEY: -20}"

if [ -f "$CLAUDE_JSON" ]; then
    UPDATED=$(jq \
        --arg ver "$CLAUDE_VER" \
        --arg tail "$KEY_TAIL" \
        '
        .hasCompletedOnboarding = true |
        .hasTrustDialogAccepted = true |
        .lastOnboardingVersion = $ver |
        .autoUpdaterStatus = "disabled" |
        .numStartups = (.numStartups // 184) |
        .userID = (.userID // (now | tostring | gsub("\\."; ""))) |
        .projects = (.projects // {}) |
        ._airunManaged = true |
        .customApiKeyResponses = ((.customApiKeyResponses // {}) + {
            approved: (((.customApiKeyResponses // {}).approved // []) + [$tail] | unique),
            rejected: ((.customApiKeyResponses // {}).rejected // [])
        })
        ' "$CLAUDE_JSON")
    echo "$UPDATED" > "$CLAUDE_JSON"
else
    USER_ID=$(od -An -tx1 -N32 /dev/urandom 2>/dev/null | tr -d ' \n' || date +%s%N)
    jq -n \
        --arg ver "$CLAUDE_VER" \
        --arg uid "$USER_ID" \
        --arg tail "$KEY_TAIL" \
        '{
            hasCompletedOnboarding: true,
            hasTrustDialogAccepted: true,
            lastOnboardingVersion: $ver,
            autoUpdaterStatus: "disabled",
            numStartups: 184,
            userID: $uid,
            projects: {},
            _airunManaged: true,
            customApiKeyResponses: {
                approved: [$tail],
                rejected: []
            }
        }' > "$CLAUDE_JSON"
fi
chmod 600 "$CLAUDE_JSON"

echo ""
echo "  Claude Code configured:"
echo "    URL:      $PROXY_URL"
echo "    Model:    $DEFAULT_MODEL"
echo "    Settings: $SETTINGS_FILE"
echo "    Auth:     $CLAUDE_JSON (onboarding bypassed)"
echo ""
echo "  Run 'claude' to start using the proxy."
echo "  To disconnect: $0 --disconnect"

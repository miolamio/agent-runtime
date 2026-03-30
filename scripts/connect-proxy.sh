#!/usr/bin/env bash
set -euo pipefail

# Connect Claude Code CLI to an airun proxy.
# Usage:
#   bash connect-proxy.sh <proxy-url> <api-key>
#   curl -fsSL https://raw.githubusercontent.com/miolamio/agent-runtime/main/scripts/connect-proxy.sh | bash -s -- <proxy-url> <api-key>

PROXY_URL="${1:-}"
API_KEY="${2:-}"

# ── Interactive input if args missing ──
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
    exit 1
fi

PROXY_URL="${PROXY_URL%/}"  # trim trailing slash

# ── Check dependencies ──
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

DEFAULT_MODEL=$(echo "$MODELS" | head -1)
if [ "$MODEL_COUNT" -gt 1 ]; then
    printf "\n  Default model [%s]: " "$DEFAULT_MODEL"
    read -r CHOSEN
    if [ -n "$CHOSEN" ]; then
        DEFAULT_MODEL="$CHOSEN"
    fi
fi

# ── Write to ~/.claude/settings.json ──
SETTINGS_DIR="$HOME/.claude"
SETTINGS_FILE="$SETTINGS_DIR/settings.json"

mkdir -p "$SETTINGS_DIR"

if [ -f "$SETTINGS_FILE" ]; then
    # Merge into existing settings
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
    # Create new settings
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

echo ""
echo "  Claude Code configured:"
echo "    URL:   $PROXY_URL"
echo "    Model: $DEFAULT_MODEL"
echo "    File:  $SETTINGS_FILE"
echo ""
echo "  Run 'claude' to start using the proxy."

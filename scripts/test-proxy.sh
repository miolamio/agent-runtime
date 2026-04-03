#!/bin/bash
# airun proxy connectivity test
# Usage: curl -fsSL https://raw.githubusercontent.com/miolamio/agent-runtime/main/scripts/test-proxy.sh | bash

set -euo pipefail

# Read config from Claude Code settings
SETTINGS="$HOME/.claude/settings.json"
if [ ! -f "$SETTINGS" ]; then
  echo "ERROR: $SETTINGS not found"
  echo "Run connect-proxy.sh first"
  exit 1
fi

TOKEN=$(grep -o '"ANTHROPIC_AUTH_TOKEN":"[^"]*"' "$SETTINGS" 2>/dev/null | cut -d'"' -f4)
URL=$(grep -o '"ANTHROPIC_BASE_URL":"[^"]*"' "$SETTINGS" 2>/dev/null | cut -d'"' -f4)
MODEL=$(grep -o '"ANTHROPIC_DEFAULT_SONNET_MODEL":"[^"]*"' "$SETTINGS" 2>/dev/null | cut -d'"' -f4)

if [ -z "$TOKEN" ]; then
  echo "ERROR: ANTHROPIC_AUTH_TOKEN not found in $SETTINGS"
  exit 1
fi
if [ -z "$URL" ]; then
  echo "ERROR: ANTHROPIC_BASE_URL not found in $SETTINGS"
  exit 1
fi

MASKED="${TOKEN:0:10}...${TOKEN: -4}"
echo "Token:  $MASKED"
echo "URL:    $URL"
echo "Model:  ${MODEL:-<not set>}"
echo ""

FAIL=0

# 1. Models endpoint
echo -n "[1/3] GET /v1/models ... "
RESP=$(curl -s -w "\n%{http_code}" --max-time 10 "$URL/v1/models" -H "x-api-key: $TOKEN" 2>&1)
CODE=$(echo "$RESP" | tail -1)
BODY=$(echo "$RESP" | sed '$d')
if [ "$CODE" = "200" ]; then
  MODELS=$(echo "$BODY" | grep -o '"id":"[^"]*"' | cut -d'"' -f4 | tr '\n' ', ' | sed 's/,$//')
  echo "OK ($MODELS)"
else
  echo "FAIL (HTTP $CODE)"
  echo "  $BODY"
  FAIL=1
fi

# 2. Messages endpoint (tiny request)
echo -n "[2/3] POST /v1/messages ... "
TEST_MODEL="${MODEL:-glm-5.1}"
RESP=$(curl -s -w "\n%{http_code}" --max-time 30 "$URL/v1/messages" \
  -H "x-api-key: $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"model\":\"$TEST_MODEL\",\"max_tokens\":32,\"messages\":[{\"role\":\"user\",\"content\":\"Say OK\"}]}" 2>&1)
CODE=$(echo "$RESP" | tail -1)
BODY=$(echo "$RESP" | sed '$d')
if [ "$CODE" = "200" ]; then
  echo "OK (model: $TEST_MODEL)"
else
  echo "FAIL (HTTP $CODE, model: $TEST_MODEL)"
  echo "  $BODY"
  FAIL=1
fi

# 3. Claude CLI
echo -n "[3/3] Claude CLI ... "
if command -v claude &>/dev/null; then
  VER=$(claude --version 2>&1 | head -1)
  echo "$VER"
else
  echo "NOT FOUND"
  echo "  Install: npm i -g @anthropic-ai/claude-code"
  FAIL=1
fi

echo ""
if [ "$FAIL" -eq 0 ]; then
  echo "All checks passed."
else
  echo "Some checks failed. See above."
  exit 1
fi

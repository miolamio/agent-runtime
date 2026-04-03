#!/bin/bash
# Proxy connectivity test for Claude Code
set -euo pipefail

echo "=== Proxy Connectivity Test ==="
echo ""

# --- Detect OS ---
echo "OS: $(uname -s) $(uname -r)"
if grep -qi microsoft /proc/version 2>/dev/null; then
  echo "    WSL detected"
fi

# --- Check curl ---
if ! command -v curl &>/dev/null; then
  echo "ERROR: curl is not installed"
  exit 1
fi
echo "curl: $(curl --version 2>&1 | head -1)"

# --- Check Node.js ---
echo -n "node: "
if command -v node &>/dev/null; then
  node --version 2>&1
else
  echo "not found"
fi

# --- Check Claude CLI ---
echo -n "claude: "
if command -v claude &>/dev/null; then
  claude --version 2>&1 | head -1
else
  echo "not found (install: npm i -g @anthropic-ai/claude-code)"
fi

echo ""

# --- Read settings ---
SETTINGS="$HOME/.claude/settings.json"
echo "Settings: $SETTINGS"

if [ ! -f "$SETTINGS" ]; then
  echo "ERROR: file not found"
  echo ""
  echo "Claude Code settings file is missing."
  echo "Run the connect script provided by your admin first."
  exit 1
fi

echo "  File exists: yes ($(wc -c < "$SETTINGS") bytes)"

# Extract env block values
TOKEN=$(grep -o '"ANTHROPIC_AUTH_TOKEN":"[^"]*"' "$SETTINGS" 2>/dev/null | cut -d'"' -f4 || true)
URL=$(grep -o '"ANTHROPIC_BASE_URL":"[^"]*"' "$SETTINGS" 2>/dev/null | cut -d'"' -f4 || true)
MODEL=$(grep -o '"ANTHROPIC_DEFAULT_SONNET_MODEL":"[^"]*"' "$SETTINGS" 2>/dev/null | cut -d'"' -f4 || true)
HAIKU=$(grep -o '"ANTHROPIC_DEFAULT_HAIKU_MODEL":"[^"]*"' "$SETTINGS" 2>/dev/null | cut -d'"' -f4 || true)
OPUS=$(grep -o '"ANTHROPIC_DEFAULT_OPUS_MODEL":"[^"]*"' "$SETTINGS" 2>/dev/null | cut -d'"' -f4 || true)
TIMEOUT=$(grep -o '"API_TIMEOUT_MS":"[^"]*"' "$SETTINGS" 2>/dev/null | cut -d'"' -f4 || true)

echo "  ANTHROPIC_BASE_URL: ${URL:-<not set>}"
if [ -n "$TOKEN" ]; then
  echo "  ANTHROPIC_AUTH_TOKEN: ${TOKEN:0:10}...${TOKEN: -4}"
else
  echo "  ANTHROPIC_AUTH_TOKEN: <not set>"
fi
echo "  ANTHROPIC_DEFAULT_SONNET_MODEL: ${MODEL:-<not set>}"
echo "  ANTHROPIC_DEFAULT_HAIKU_MODEL: ${HAIKU:-<not set>}"
echo "  ANTHROPIC_DEFAULT_OPUS_MODEL: ${OPUS:-<not set>}"
echo "  API_TIMEOUT_MS: ${TIMEOUT:-<not set>}"

# Check .claude.json
AUTH_JSON="$HOME/.claude.json"
echo ""
echo "Auth: $AUTH_JSON"
if [ -f "$AUTH_JSON" ]; then
  ONBOARDING=$(grep -o '"hasCompletedOnboarding":[a-z]*' "$AUTH_JSON" 2>/dev/null | cut -d: -f2 || true)
  echo "  File exists: yes"
  echo "  hasCompletedOnboarding: ${ONBOARDING:-<not found>}"
else
  echo "  File exists: no"
fi

echo ""

# --- Validate config ---
if [ -z "$URL" ]; then
  echo "ERROR: ANTHROPIC_BASE_URL is not configured in settings.json"
  exit 1
fi
if [ -z "$TOKEN" ]; then
  echo "ERROR: ANTHROPIC_AUTH_TOKEN is not configured in settings.json"
  exit 1
fi

# --- Test 1: DNS resolution ---
HOST=$(echo "$URL" | sed 's|https\?://||' | cut -d/ -f1 | cut -d: -f1)
echo -n "[1/4] DNS resolve $HOST ... "
if IP=$(getent hosts "$HOST" 2>/dev/null | awk '{print $1}' | head -1) && [ -n "$IP" ]; then
  echo "OK ($IP)"
elif IP=$(dig +short "$HOST" 2>/dev/null | head -1) && [ -n "$IP" ]; then
  echo "OK ($IP)"
elif IP=$(nslookup "$HOST" 2>/dev/null | grep -A1 "Name:" | grep "Address:" | awk '{print $2}' | head -1) && [ -n "$IP" ]; then
  echo "OK ($IP)"
else
  echo "FAIL"
  echo "  Cannot resolve hostname $HOST"
  exit 1
fi

# --- Test 2: TCP connection ---
PORT=443
if echo "$URL" | grep -q "http://"; then
  PORT=80
fi
echo -n "[2/4] TCP connect $HOST:$PORT ... "
if curl -s --max-time 5 -o /dev/null -w "%{http_code}" "$URL" -H "x-api-key: test" >/dev/null 2>&1; then
  echo "OK"
else
  echo "FAIL"
  echo "  Cannot connect to $HOST:$PORT"
  echo "  Check your network/firewall"
  exit 1
fi

# --- Test 3: Models endpoint ---
echo -n "[3/4] GET /v1/models ... "
RESP=$(curl -s -w "\n%{http_code}" --max-time 10 "$URL/v1/models" -H "x-api-key: $TOKEN" 2>&1)
CODE=$(echo "$RESP" | tail -1)
BODY=$(echo "$RESP" | sed '$d')
if [ "$CODE" = "200" ]; then
  MODELS=$(echo "$BODY" | grep -o '"id":"[^"]*"' | cut -d'"' -f4 | tr '\n' ', ' | sed 's/,$//')
  echo "OK"
  echo "  Models: $MODELS"
else
  echo "FAIL (HTTP $CODE)"
  echo "  Response: $BODY"
  if [ "$CODE" = "401" ]; then
    echo "  Your token is invalid or revoked. Contact your admin."
  fi
  exit 1
fi

# --- Test 4: Messages endpoint ---
TEST_MODEL="${MODEL:-glm-5.1}"
echo -n "[4/4] POST /v1/messages (model: $TEST_MODEL) ... "
RESP=$(curl -s -w "\n%{http_code}" --max-time 60 "$URL/v1/messages" \
  -H "x-api-key: $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"model\":\"$TEST_MODEL\",\"max_tokens\":32,\"messages\":[{\"role\":\"user\",\"content\":\"Say OK\"}]}" 2>&1)
CODE=$(echo "$RESP" | tail -1)
BODY=$(echo "$RESP" | sed '$d')
if [ "$CODE" = "200" ]; then
  echo "OK"
else
  echo "FAIL (HTTP $CODE)"
  echo "  Response: $BODY"
  if [ "$CODE" = "400" ]; then
    echo "  Model '$TEST_MODEL' may not be available. Check ANTHROPIC_DEFAULT_SONNET_MODEL."
  fi
  exit 1
fi

echo ""
echo "All checks passed. Run 'claude' to start."

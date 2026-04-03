#!/usr/bin/env bash
# Proxy connectivity test for Claude Code

echo "=== Proxy Connectivity Test ==="
echo ""

# --- Detect OS ---
echo "OS: $(uname -s) $(uname -m)"
if [ -f /proc/version ] && grep -qi microsoft /proc/version 2>/dev/null; then
  echo "    WSL detected"
fi
echo "bash: $BASH_VERSION"

# --- Check curl ---
if ! command -v curl >/dev/null 2>&1; then
  echo "ERROR: curl is not installed"
  exit 1
fi
echo "curl: $(curl --version 2>&1 | head -1)"

# --- Check Node.js ---
if command -v node >/dev/null 2>&1; then
  echo "node: $(node --version 2>&1)"
else
  echo "node: not found"
fi

# --- Check Claude CLI ---
if command -v claude >/dev/null 2>&1; then
  echo "claude: $(claude --version 2>&1 | head -1)"
else
  echo "claude: not found (install: npm i -g @anthropic-ai/claude-code)"
fi

echo ""

# --- Read settings ---
SETTINGS="$HOME/.claude/settings.json"
echo "Settings: $SETTINGS"

if [ ! -f "$SETTINGS" ]; then
  echo "  File exists: no"
  echo ""
  echo "ERROR: Claude Code settings file is missing."
  echo "Run the connect script provided by your admin first."
  exit 1
fi

echo "  File exists: yes ($(wc -c < "$SETTINGS" | tr -d ' ') bytes)"

# Extract env block values (portable grep, no bash-4 features)
extract() { grep -o "\"$1\":\"[^\"]*\"" "$SETTINGS" 2>/dev/null | head -1 | cut -d'"' -f4; }

TOKEN=$(extract ANTHROPIC_AUTH_TOKEN)
URL=$(extract ANTHROPIC_BASE_URL)
MODEL=$(extract ANTHROPIC_DEFAULT_SONNET_MODEL)
HAIKU=$(extract ANTHROPIC_DEFAULT_HAIKU_MODEL)
OPUS=$(extract ANTHROPIC_DEFAULT_OPUS_MODEL)
TIMEOUT=$(extract API_TIMEOUT_MS)

if [ -n "$URL" ]; then
  echo "  ANTHROPIC_BASE_URL: $URL"
else
  echo "  ANTHROPIC_BASE_URL: <not set>"
fi

if [ -n "$TOKEN" ]; then
  # Portable token masking (works on bash 3.x)
  T_LEN=${#TOKEN}
  if [ "$T_LEN" -gt 14 ]; then
    T_HEAD=$(echo "$TOKEN" | cut -c1-10)
    T_TAIL=$(echo "$TOKEN" | cut -c$((T_LEN - 3))-)
    echo "  ANTHROPIC_AUTH_TOKEN: ${T_HEAD}...${T_TAIL}"
  else
    echo "  ANTHROPIC_AUTH_TOKEN: ***"
  fi
else
  echo "  ANTHROPIC_AUTH_TOKEN: <not set>"
fi

echo "  SONNET_MODEL: ${MODEL:-<not set>}"
echo "  HAIKU_MODEL:  ${HAIKU:-<not set>}"
echo "  OPUS_MODEL:   ${OPUS:-<not set>}"
echo "  API_TIMEOUT:  ${TIMEOUT:-<not set>}"

# Check .claude.json
AUTH_JSON="$HOME/.claude.json"
echo ""
echo "Auth: $AUTH_JSON"
if [ -f "$AUTH_JSON" ]; then
  ONBOARDING=$(grep -o '"hasCompletedOnboarding":[a-z]*' "$AUTH_JSON" 2>/dev/null | head -1 | cut -d: -f2)
  echo "  File exists: yes"
  echo "  hasCompletedOnboarding: ${ONBOARDING:-<not found>}"
else
  echo "  File exists: no"
fi

echo ""

# --- Validate config ---
if [ -z "$URL" ]; then
  echo "STOP: ANTHROPIC_BASE_URL is not set in settings.json"
  echo "Run the connect script provided by your admin."
  exit 1
fi
if [ -z "$TOKEN" ]; then
  echo "STOP: ANTHROPIC_AUTH_TOKEN is not set in settings.json"
  echo "Run the connect script provided by your admin."
  exit 1
fi

echo "--- Running tests ---"
echo ""

# --- Test 1: DNS resolution ---
HOST=$(echo "$URL" | sed 's|https*://||' | cut -d/ -f1 | cut -d: -f1)
echo -n "[1/4] DNS resolve $HOST ... "
RESOLVED=""
if command -v dig >/dev/null 2>&1; then
  RESOLVED=$(dig +short "$HOST" 2>/dev/null | head -1)
elif command -v getent >/dev/null 2>&1; then
  RESOLVED=$(getent hosts "$HOST" 2>/dev/null | awk '{print $1}' | head -1)
elif command -v nslookup >/dev/null 2>&1; then
  RESOLVED=$(nslookup "$HOST" 2>/dev/null | awk '/^Address: / {print $2}' | head -1)
fi
if [ -n "$RESOLVED" ]; then
  echo "OK ($RESOLVED)"
else
  # Try ping as last resort
  if ping -c1 -W2 "$HOST" >/dev/null 2>&1; then
    echo "OK (resolved via ping)"
  else
    echo "FAIL — cannot resolve $HOST"
    exit 1
  fi
fi

# --- Test 2: TCP + TLS ---
PORT=443
if echo "$URL" | grep -q "^http://"; then
  PORT=80
fi
echo -n "[2/4] TCP connect $HOST:$PORT ... "
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" --max-time 5 "$URL/" -H "x-api-key: test" 2>&1) || true
if [ -n "$HTTP_CODE" ] && [ "$HTTP_CODE" != "000" ]; then
  echo "OK (HTTP $HTTP_CODE)"
else
  echo "FAIL — cannot connect to $HOST:$PORT"
  echo "       Check your network, VPN, or firewall."
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
  echo "       Available models: $MODELS"
else
  echo "FAIL (HTTP $CODE)"
  echo "       $BODY"
  if [ "$CODE" = "401" ]; then
    echo "       Token is invalid or revoked. Contact your admin."
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
  echo "       $BODY"
  if [ "$CODE" = "400" ]; then
    echo "       Model '$TEST_MODEL' is not available on this proxy."
  fi
  exit 1
fi

echo ""
echo "All checks passed. Run 'claude' to start."

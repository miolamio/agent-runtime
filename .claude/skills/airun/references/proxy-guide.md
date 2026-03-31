# Proxy Setup Guide

## Overview

The airun proxy enables multi-user access to LLM providers without sharing API keys. Admin runs the proxy server with real provider keys; users get personal tokens.

```
User (sk-ai-token) → Proxy (auth + rate limit) → Z.AI / Kimi / MiniMax
```

## Admin Setup

### 1. Initialize

```bash
airun proxy init
```

Creates `~/proxy.yaml` and `~/students.json`.

### 2. Configure providers

Edit `~/proxy.yaml`:

```yaml
listen: ":8080"
rpm: 0                  # 0 = unlimited, N = requests/min per user
user_agent: "claude-cli/2.1.80 (external, cli)"

providers:
  zai:
    base_url: "https://api.z.ai/api/anthropic"
    api_key: "YOUR_ZAI_KEY"
    models:
      - glm-5.1
      - glm-4.7
      - GLM-4.5-Air
  kimi:
    base_url: "https://api.kimi.com/coding/"
    api_key: "YOUR_KIMI_KEY"
    models:
      - kimi-k2.5
  minimax:
    base_url: "https://api.minimax.io/anthropic"
    api_key: "YOUR_MINIMAX_KEY"
    models:
      - MiniMax-M2.7
```

### 3. Add users

```bash
airun proxy user add "Alice"    # prints: sk-ai-a3f5c8d2...
airun proxy user add "Bob"
airun proxy user list           # shows all users with masked tokens
airun proxy user export         # full tokens for distribution
```

### 4. Start server

```bash
airun proxy serve
airun proxy serve --port 9090   # custom port
```

### 5. User management

```bash
airun proxy user revoke "Alice"     # deactivate (keeps record)
airun proxy user restore "Alice"    # reactivate
airun proxy user import list.txt    # bulk import (one name per line)
```

Reload users without restart:
```bash
kill -HUP $(pgrep -f "airun proxy serve")
```

## User Connection

### With airun installed

```bash
airun proxy connect <url> <token>
# Validates, shows models, writes ~/.claude/settings.json + ~/.claude.json
```

### Without airun (macOS/Linux)

```bash
curl -fsSL https://raw.githubusercontent.com/miolamio/agent-runtime/main/scripts/connect-proxy.sh | bash -s -- <url> <token>
```

### Without airun (Windows)

```powershell
$env:PROXY_URL='<url>'; $env:PROXY_KEY='<token>'
irm https://raw.githubusercontent.com/miolamio/agent-runtime/main/scripts/connect-proxy.ps1 | iex
```

### Manual configuration

Edit `~/.claude/settings.json`, add to the `env` section:

```json
{
  "env": {
    "ANTHROPIC_AUTH_TOKEN": "sk-ai-your-token",
    "ANTHROPIC_BASE_URL": "http://proxy-server:8080",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "glm-5.1",
    "API_TIMEOUT_MS": "3000000"
  }
}
```

### Disconnect

```bash
airun proxy disconnect
# or
curl -fsSL .../connect-proxy.sh | bash -s -- --disconnect
```

## Proxy Features

- **Authentication**: per-user `sk-ai-...` tokens validated on every request
- **Rate limiting**: sliding window per user (configurable RPM in proxy.yaml)
- **Model routing**: request model field determines which provider gets the call
- **Streaming**: full SSE streaming support for real-time responses
- **Header security**: user tokens never forwarded to providers; real keys stay on server
- **Endpoints**: `GET /v1/models` (list), `POST /v1/messages` (forward)

## Token Format

Tokens are 32 random hex chars prefixed with `sk-ai-`:
```
sk-ai-a3f5c8d2e1b9f4a6c7e8d9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8
```

Stored in `~/students.json` with name, active status, and creation timestamp.

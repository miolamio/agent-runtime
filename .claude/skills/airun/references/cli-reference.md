# airun CLI Reference

## Run Modes

### One-shot task
```bash
airun "prompt"
airun -p dev "prompt"                   # with profile
airun --provider mm "prompt"            # override provider
airun --model kimi-k2.5 "prompt"        # override model
airun --output ./results "prompt"       # export workspace after run
```

### Interactive shell
```bash
airun shell
airun shell -p dev                      # with profile
airun shell --provider kimi             # override provider
airun shell --mount /path/to/project    # custom workspace mount
airun shell --no-state                  # ephemeral (no persistent volume)
```

### Parallel agents
```bash
airun --parallel --agent "name1:prompt1" --agent "name2:prompt2"
```
Each agent runs in a separate container. State volume is disabled for parallel runs to avoid concurrent writes.

### Autonomous loop
```bash
airun --loop --max-loops 5 "prompt"
```

## Key Management

```bash
airun keys list                         # show configured keys (masked)
airun keys add zai                      # guided setup + live validation
airun keys add remote                   # connect to proxy server
airun keys remove minimax               # remove provider
airun keys test                         # validate all keys
airun keys test kimi                    # validate specific key
airun keys default kimi                 # change default provider
airun keys model glm-5.1               # change default model
```

## Proxy (Admin)

```bash
airun proxy init                        # create proxy.yaml + students.json
airun proxy serve                       # start on :8080
airun proxy serve --port 9090           # custom port
airun proxy user add "Name"             # create user, prints token
airun proxy user list                   # show all users
airun proxy user revoke "Name"          # deactivate user
airun proxy user restore "Name"         # reactivate user
airun proxy user import list.txt        # bulk import from file
airun proxy user export                 # export name + token pairs
```

Reload users without restart: `kill -HUP $(pgrep -f "airun proxy serve")`

## Proxy (User)

```bash
airun proxy connect <url> <token>       # configure Claude Code for proxy
airun proxy disconnect                  # remove proxy settings
```

## State Management

```bash
airun state info                        # show Docker volume details
airun state reset                       # remove volume (re-seeds on next run)
```

## Docker Image

```bash
airun rebuild                           # build agent-runtime:latest
airun rebuild --no-cache                # clean rebuild
airun rebuild --fresh                   # reinstall Claude Code CLI (latest)
```

## System

```bash
airun --check                           # show config + prerequisites
airun --status                          # show running containers
airun history                           # last 20 runs with timing
airun --version                         # show version
```

## Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--provider` | | Provider: z/zai, m/mm/minimax, k/kimi, r/remote |
| `--model` | `-m` | Model override (e.g. glm-5.1, kimi-k2.5) |
| `--profile` | `-p` | Profile name (dev, text, default) |
| `--output` | | Export workspace to directory after run |
| `--no-state` | | Disable persistent state volume |
| `--loop` | | Enable autonomous loop mode |
| `--max-loops` | | Maximum loop iterations (default: 5) |
| `--parallel` | | Run agents in parallel |
| `--agent` | | Agent spec "name:prompt" (repeatable) |
| `--mount` | | Directory to mount (shell mode only) |

## Configuration File

`~/.airun.env` (chmod 600):

```bash
ARUN_PROVIDER=zai                       # default provider
ARUN_WORKSPACE=~/src                    # default mount directory

ZAI_API_KEY=...                         # Z.AI
ZAI_BASE_URL=https://api.z.ai/api/anthropic
ZAI_MODEL=glm-5.1

MINIMAX_API_KEY=...                     # MiniMax
MINIMAX_BASE_URL=https://api.minimax.io/anthropic
MINIMAX_MODEL=MiniMax-M2.7

KIMI_API_KEY=...                        # Kimi
KIMI_BASE_URL=https://api.kimi.com/coding/
KIMI_MODEL=kimi-k2.5

REMOTE_BASE_URL=https://proxy.example.com  # Remote proxy
REMOTE_API_KEY=sk-ai-...
REMOTE_MODELS=glm-5.1,kimi-k2.5
REMOTE_DEFAULT_MODEL=glm-5.1

API_TIMEOUT_MS=3000000                  # 50 minutes
```

## Directory Structure (after init)

```
~/.airun.env                            # config
~/.airun/runs/                          # run history
~/airun-profiles/                       # profile YAMLs
~/airun-skills/                         # skills mounted into containers
```

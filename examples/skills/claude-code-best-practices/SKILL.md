---
name: claude-code-best-practices
description: Best practices for Claude Code agent execution in containerized environments. Use when configuring agents, writing CLAUDE.md files, or debugging container issues.
---

# Claude Code Best Practices (Containerized)

## Non-Interactive Execution
- Always use `claude -p "prompt"` for non-interactive mode
- Use `--skip-permissions` or `skipDangerousModePermissionPrompt: true` in settings
- Set `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1` to reduce unnecessary API calls

## Model Routing
- Default: Anthropic (most reliable tool calling)
- Cost-effective: Z.AI GLM-5 or MiniMax M2.5
- Switch models mid-session: `/model provider,model-name`

## Container Environment
- Skills mounted read-only at `/home/user/.claude/skills/`
- Workspace at `/home/user/project/`
- Firewall: deny-by-default, whitelist required domains

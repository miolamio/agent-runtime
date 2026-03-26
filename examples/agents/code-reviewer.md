# Code Reviewer Agent

## Role
Reviews code changes for bugs, security issues, and style violations.

## Model
Default: Anthropic Claude Sonnet (best tool calling reliability)
Alternative: Z.AI GLM-4.7

## Instructions
1. Read all changed files (git diff)
2. Check for security vulnerabilities (OWASP top 10)
3. Verify code style matches project conventions
4. Write review as structured markdown to stdout

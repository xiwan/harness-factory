# harness-factory

Lightweight ACP agent harness — single Go binary, profile-driven tool activation.

## What Is This

harness-factory is a standalone ACP-compatible agent binary. It communicates via stdin/stdout JSON-RPC, receives a profile at session creation, and runs an agent loop (LLM ↔ tool-use) with only the tools and permissions the profile allows.

```
acp-bridge (HTTP)
    │
    │  fork + stdin/stdout (ACP JSON-RPC)
    │  pass profile → activate tools + permissions
    ▼
harness-factory (single binary, ~8MB)
    │
    ├── fs    — read, write, list, search
    ├── git   — status, diff, log, show, commit, push
    ├── shell — exec (allowlist/blocklist)
    └── web   — fetch
```

Same binary, different profiles → different agents (code reviewer, devops bot, research assistant).

## Quick Start

```bash
# Build
go build -o harness-factory ./cmd/harness-factory

# Version
./harness-factory --version

# Test (protocol only, no LiteLLM needed)
bash test/test_agent_compliance.sh ./harness-factory

# Test (full e2e, needs LiteLLM + .env)
cp .env.example .env
# edit .env with your tokens
bash test/test_agent_compliance.sh ./harness-factory
```

## How It Works

1. Bridge forks harness-factory
2. `initialize` → returns agent info
3. `session/new { cwd, profile }` → activates tools per profile
4. `session/prompt { prompt }` → runs agent loop:
   - Calls LLM via LiteLLM (OpenAI-compatible API)
   - LLM requests tool calls → permission check → execute → return result
   - Sends `session/update` notifications (tool.start, tool.done, text)
   - Loops until LLM says done or max_turns reached
5. Returns `{ stopReason }` 

## Profile Example

> Full profile reference with all fields, types, and valid values → [AGENT.md#profile-reference](AGENT.md#profile-reference)

```json
{
  "tools": {
    "fs": { "permissions": ["read"] },
    "git": { "permissions": ["diff", "log"] },
    "shell": { "allowlist": ["pytest", "mypy"] }
  },
  "orchestration": "free",
  "resources": { "timeout": "300s", "max_turns": 20 },
  "agent": {
    "model": "bedrock/anthropic.claude-sonnet-4-6",
    "system_prompt": "You are a code reviewer...",
    "temperature": 0.3
  },
  "litellm_url": "http://localhost:4000",
  "litellm_api_key": "sk-..."
}
```

Two layers of protection:
- **Exposure**: LLM only sees activated tools (doesn't know others exist)
- **Enforcement**: Permission checker blocks any unauthorized tool call at runtime

## Project Structure

```
cmd/harness-factory/main.go    — entry point, ACP JSON-RPC loop
internal/
  acp/          — stdin/stdout JSON-RPC transport
  profile/      — profile struct + permission queries
  permission/   — runtime tool call permission checker
  tools/        — registry + fs/git/shell/web implementations
  llm/          — LiteLLM HTTP client (OpenAI-compatible)
  agent/        — agent loop (LLM ↔ tool-use cycle)
  constraint/   — orchestration constraints (P1)
test/
  main_test.go                — Go unit tests
  test_agent_compliance.sh    — ACP compliance + e2e test
```

## Requirements

- Go 1.21+
- LiteLLM running (for agent loop / e2e tests)

## Related

- [acp-bridge](https://github.com/xiwan/acp-bridge) — HTTP gateway that forks and manages harness-factory instances

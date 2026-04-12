```
╔══════════════════════════════════════════════════════════════╗
║                                                              ║
║   _  _                                                       ║
║  | || |__ _ _ _ _ _  ___ ______                              ║
║  | __ / _` | '_| ' \/ -_|_-<_-<                              ║
║  |_||_\__,_|_| |_||_\___/__/__/                              ║
║     ___        _                                             ║
║    | __|_ _ __| |_ ___ _ _ _  _                              ║
║    | _/ _` / _|  _/ _ \ '_| || |                             ║
║    |_|\__,_\__|\__\___/_|  \_, |                             ║
║                            |__/                              ║
║                                                              ║
║  🏭 Profile-driven ACP agent — one binary, many roles        ║
║                                                              ║
║  https://github.com/xiwan/harness-factory                    ║
╚══════════════════════════════════════════════════════════════╝
```

# harness-factory

[![Agent Guide](https://img.shields.io/badge/Agent_Guide-for_AI_Agents-blue?logo=robot)](AGENT.md)
[![License: MIT-0](https://img.shields.io/badge/License-MIT--0-green.svg)](LICENSE)

Lightweight ACP agent harness — single Go binary (~6MB), profile-driven tool activation, zero external dependencies.

## Architecture

```
acp-bridge (HTTP)
    │
    │  fork + stdin/stdout (ACP JSON-RPC)
    │  pass profile → activate tools + permissions
    ▼
harness-factory (single binary, ~6MB)
    │
    ├── fs    — read, write, list, search
    ├── git   — status, diff, log, show, commit, push
    ├── shell — exec (allowlist/blocklist)
    └── web   — fetch
```

Same binary, different profiles → different agents (code reviewer, devops bot, research assistant).

## Quick Start

```bash
# Build (stripped binary, ~6MB)
make build

# Version
./harness-factory --version

# List built-in profiles
./harness-factory --profiles

# Run with a specific profile
./harness-factory --profile pr-reviewer

# Test (protocol only, no LiteLLM needed)
make test

# Test (full e2e, needs LiteLLM + .env)
cp .env.example .env
# edit .env with your tokens
make test-e2e

# One-shot run
bash scripts/run.sh "list files in /tmp"

# Interactive mode
bash scripts/run.sh
```

## How It Works

1. Bridge forks harness-factory
2. `initialize` → returns agent info + capabilities
3. `session/new { cwd, profile }` → activates tools per profile
4. `session/prompt { prompt }` → runs agent loop:
   - Calls LLM via LiteLLM (OpenAI-compatible API)
   - LLM requests tool calls → permission check → execute → return result
   - Sends `session/update` notifications (`tool_call`, `tool_call_update`, `agent_message_chunk`)
   - Loops until LLM says done or max_turns reached
5. Returns `{ sessionId, stopReason: "end_turn" }`

## Integration with acp-bridge

Add to acp-bridge `config.yaml`:

```yaml
agents:
  pr-reviewer:
    enabled: true
    mode: "acp"
    command: "harness-factory"
    acp_args: []
    working_dir: "/tmp"
    description: "Code review agent (harness-factory)"
    profile:
      tools:
        fs: { permissions: [read, list] }
        git: { permissions: [diff, log, show] }
        shell: { allowlist: [pytest, mypy, grep] }
      orchestration: free
      resources:
        timeout: 300s
        max_turns: 20
      agent:
        model: "bedrock/anthropic.claude-sonnet-4-6"
        system_prompt: "You are a code reviewer."
        temperature: 0.3
```

Bridge injects `litellm_url` and `litellm_api_key` automatically.

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
  "resources": { "timeout": "300s", "max_turns": 20, "log_level": "info" },
  "agent": {
    "model": "bedrock/anthropic.claude-sonnet-4-6",
    "system_prompt": "You are a code reviewer...",
    "temperature": 0.3
  }
}
```

Two layers of protection:
- **Exposure**: LLM only sees activated tools (doesn't know others exist)
- **Enforcement**: Permission checker blocks any unauthorized tool call at runtime

## Makefile

| Command | Description |
|---------|-------------|
| `make build` | Build stripped binary (~6MB) |
| `make test` | Go unit tests |
| `make test-e2e` | Build + compliance + e2e tests |
| `make run` | Build + start (stdin JSON-RPC) |
| `make clean` | Remove binary |
| `make version` | Print version |

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
  logger/       — structured stderr logging
  constraint/   — orchestration constraints (P1)
scripts/
  run.sh        — convenience launcher (one-shot + interactive)
test/
  main_test.go                — Go unit tests
  test_agent_compliance.sh    — ACP compliance + e2e test
```

## Supported Models

harness-factory calls LLMs through [LiteLLM](https://docs.litellm.ai/) proxy — any provider LiteLLM supports works out of the box:

| Provider | Model example | LiteLLM config |
|----------|--------------|----------------|
| AWS Bedrock | `bedrock/anthropic.claude-sonnet-4-6` | `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION_NAME` |
| OpenAI | `gpt-4o` | `OPENAI_API_KEY` |
| Anthropic | `anthropic/claude-sonnet-4-6` | `ANTHROPIC_API_KEY` |
| Azure OpenAI | `azure/gpt-4o` | `AZURE_API_KEY`, `AZURE_API_BASE` |
| Google Gemini | `gemini/gemini-2.5-pro` | `GEMINI_API_KEY` |
| Ollama (local) | `ollama/llama3` | Ollama running locally |

Set the `model` field in your profile — harness-factory passes it directly to LiteLLM. Provider API keys are configured on the LiteLLM side, not in harness-factory.

## Requirements

- Go 1.21+
- LiteLLM running (for agent loop / e2e tests)

## Changelog

See [versions/](versions/) for version history.

## Related

- [acp-bridge](https://github.com/xiwan/acp-bridge) — HTTP gateway that forks and manages harness-factory instances
- [ACP Protocol](https://agentclientprotocol.com/) — Agent Client Protocol specification

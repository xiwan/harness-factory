```
╔══════════════════════════════════════════════════════════════════╗
║  _  _                             ___        _                  ║
║ | || |__ _ _ _ _ _  ___ ______   | __|_ _ __| |_ ___ _ _ _  _  ║
║ | __ / _` | '_| ' \/ -_|_-<_-<  | _/ _` / _|  _/ _ \ '_| || | ║
║ |_||_\__,_|_| |_||_\___/__/__/  |_|\__,_\__|\__\___/_|  \_, | ║
║                                                          |__/  ║
╠══════════════════════════════════════════════════════════════════╣
║ 🏭 Profile-driven ACP agent — one binary, many roles           ║
║ https://github.com/xiwan/harness-factory                       ║
╚══════════════════════════════════════════════════════════════════╝
```

# harness-factory

[![Agent Guide](https://img.shields.io/badge/Agent_Guide-for_AI_Agents-blue?logo=robot)](AGENT.md)
[![License: AGPL-3.0](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](LICENSE)

Lightweight ACP agent harness — single Go binary (~6MB), profile-driven tool activation, zero external dependencies.

## Why Harness Factory?

|      | OpenClaw 子进程  | Bridge 常驻 Agent | Harness Factory |
| ---- | ------------- | --------------- | --------------- |
| 类比   | 让同事帮忙干个活      | 招全职员工           | 请外包顾问           |
| 权限   | 继承父权限，没法限制    | 完整独立权限          | **preset 精确控制**     |
| 协作   | 手动编排          | pipeline 原生     | pipeline 原生     |
| 资源   | 共享进程          | 常驻占资源           | **用完释放**            |
| 角色定制 | system prompt | 固定角色            | **prompt + preset** |

## Architecture

```
acp-bridge (HTTP)
    │
    │  fork + stdin/stdout (ACP JSON-RPC)
    │  pass profile → activate tools + permissions
    ▼
harness-factory (single binary, ~6MB)
    │
    ├── fs       — read, write, list, search
    ├── git      — status, diff, log, show, commit, push
    ├── shell    — exec (allowlist/blocklist)
    ├── web      — fetch
    └── artifact — write, read, list (sandboxed `outputs/` for inter-agent exchange)
```

Same binary, different profiles → different agents (code reviewer, devops bot, research assistant).

## Built-in Profiles

10 profiles designed from tool combination permutations (fs/git/shell/web):

| Profile | fs | git | shell | web | Use case |
|---------|-----|-----|-------|-----|----------|
| `reader` | R/L/S | — | — | — | File reading and search |
| `executor` | — | — | allowlist | — | Command execution only |
| `scout` | — | — | — | F/S | Web information gathering |
| `reviewer` | R/L | diff/log/show | — | — | Code review |
| `analyst` | R/L/S | — | analysis tools | — | Log and data analysis |
| `researcher` | R/L/S | — | — | F/S | Research and investigation |
| `developer` | R/W | ALL | test+build | — | Software development |
| `writer` | R/W | diff/log/show | — | F | Technical writing |
| `operator` | R/W | — | infra tools | F | Operations and infrastructure |
| `admin` | ALL | ALL | ALL | ALL | Full unrestricted access |

**`artifact` tool** is additionally activated on the five read-only-leaning profiles (`reader`, `scout`, `reviewer`, `analyst`, `researcher`) so they can hand data to downstream agents without gaining full `fs.write`. See [Artifact tool](#artifact-tool-inter-agent-exchange) below.

Each scenario is just a different profile on the same ~6MB binary. No new code, no new deployment — add a profile in `config.yaml` and go.

### Artifact tool (inter-agent exchange)

Profiles without `fs.write` can still produce output for the next agent in a pipeline via the `artifact` tool. It writes into a sandboxed `<cwd>/outputs/` directory with a tighter safety envelope than free-form `fs.write`:

| Guard | Limit |
|-------|-------|
| API shape | LLM supplies `name` only — never `path`; the `outputs/` prefix is fixed in code |
| Filename | `^[a-zA-Z0-9._-]+$`, no `..`, no slash/backslash, no absolute path |
| Extension whitelist | `.md .txt .json .yaml .yml .csv .log .html` (data, never scripts) |
| Per file | ≤ 1 MiB |
| Per process | ≤ 100 files |
| On disk | `O_NOFOLLOW` (blocks symlink escape), mode `0600` |

Lifecycle of `outputs/` is the **caller's** responsibility — harness-factory never cleans it up. Bridges running a pipeline should either point each session at a shared `cwd` to stitch stages together, or tear down per-session scratch directories themselves.

## Quick Start

```bash
# Build (stripped binary, ~6MB)
make build

# Version
./harness-factory --version

# List built-in profiles
./harness-factory --profiles

# List built-in models
./harness-factory --models

# Run with a specific profile
./harness-factory --profile pr-reviewer

# Run with a specific model
./harness-factory --profile reviewer --model claude-haiku

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

### Observing the resolved model

`session/new` responds with the actual model ID selected for this session, so the client never has to guess what `auto` or an alias expanded to:

```json
{
  "sessionId": "sess_1234",
  "activated": {
    "tools": ["fs_read", "git_diff"],
    "toolCount": 2,
    "orchestration": "free",
    "resolvedModel": "bedrock/anthropic.claude-sonnet-4-6"
  }
}
```

- `auto` / empty → one of the registry models (randomly picked)
- alias (e.g. `claude-sonnet`) → expanded to its full ID
- full ID → passed through unchanged

If a model fails mid-session and falls back to the next one, harness-factory emits a `session/update` notification so the client can track the change:

```json
{
  "jsonrpc": "2.0",
  "method": "session/update",
  "params": {
    "sessionId": "sess_1234",
    "update": {
      "sessionUpdate": "model_resolved",
      "model": "bedrock/deepseek.v3.2",
      "reason": "fallback"
    }
  }
}
```

`reason` is one of: `fallback` (primary failed), `user_switch` (natural-language switch mid-prompt). Clients that don't recognise `model_resolved` simply ignore the update — the extension is backward-compatible.

A mirrored stderr log is also emitted for scrapers: `[MODEL_RESOLVED] model=<id> reason=<...>`.

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

### Built-in Model Registry (Bedrock)

7 representative Bedrock models built-in with alias support:

| Alias | Model ID | Provider | Use case |
|-------|----------|----------|----------|
| `claude-sonnet` | `bedrock/anthropic.claude-sonnet-4-6` | Anthropic | Flagship, best tool-use |
| `claude-opus` | `bedrock/anthropic.claude-opus-4-6-v1` | Anthropic | Most capable |
| `deepseek-v3` | `bedrock/deepseek.v3.2` | DeepSeek | General purpose |
| `kimi-k2` | `bedrock/converse/moonshotai.kimi-k2.5` | Moonshot | Multimodal reasoning |
| `glm-5` | `bedrock/converse/zai.glm-5` | Zhipu | Agentic engineering |
| `qwen3` | `bedrock/converse/qwen.qwen3-235b-a22b-2507-v1:0` | Qwen | Alibaba MoE flagship |
| `minimax-m2` | `bedrock/converse/minimax.minimax-m2.5` | MiniMax | Agent-native frontier |
| `gemma-3` | `bedrock/converse/google.gemma-3-12b-it` | Google | Lightweight open model |

Features:
- **Auto-selection**: Default `auto` randomly picks a model from the registry
- **Auto-fallback**: If a model fails, automatically tries the next one until all exhausted
- **Natural language switch**: Say "use claude" or "换个模型" in your prompt to switch models mid-session
- **Alias or full ID**: Use `claude-haiku` or pass any full LiteLLM model ID directly

```bash
# List built-in models
./harness-factory --models

# Use a specific model alias
./harness-factory --profile reviewer --model claude-haiku

# Auto-select (default)
./harness-factory --profile reviewer
```

## Requirements

- Go 1.21+
- LiteLLM running (for agent loop / e2e tests)

## Changelog

See [versions/](versions/) for version history.

## Related

- [acp-bridge](https://github.com/xiwan/acp-bridge) — HTTP gateway that forks and manages harness-factory instances
- [ACP Protocol](https://agentclientprotocol.com/) — Agent Client Protocol specification

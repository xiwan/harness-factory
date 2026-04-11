# harness-factory — Agent Execution Guide

> You are the executing agent. Human docs → [README.md](README.md)

harness-factory is a lightweight ACP agent binary. Bridge forks it, passes a profile, it runs an agent loop.

```
acp-bridge (HTTP)
    ↕ fork + stdin/stdout (ACP JSON-RPC)
harness-factory (Go binary, ~6MB)
    ↕ LiteLLM (OpenAI-compatible)
LLM provider (Bedrock/OpenAI/...)
```

---

## Phase 1: Diagnose (🤖 auto)

```bash
echo "=== Runtime ==="
go version 2>&1 || echo "GO_NOT_FOUND"

echo "=== Binary ==="
[ -f ./harness-factory ] && echo "BINARY_EXISTS ($(./harness-factory --version))" || echo "BINARY_NOT_FOUND"

echo "=== Source ==="
[ -f cmd/harness-factory/main.go ] && echo "SOURCE_EXISTS" || echo "SOURCE_NOT_FOUND"
[ -f go.mod ] && echo "GOMOD_EXISTS" || echo "GOMOD_NOT_FOUND"

echo "=== Config ==="
[ -f .env ] && echo "ENV_EXISTS" || echo "ENV_NOT_FOUND"

echo "=== Env Vars ==="
[ -n "$LITELLM_API_KEY" ] && echo "LITELLM_API_KEY=set" || echo "LITELLM_API_KEY=NOT_SET"

echo "=== LiteLLM ==="
curl -s --max-time 3 http://localhost:4000/health 2>/dev/null && echo "LITELLM_OK" || echo "LITELLM_NOT_RUNNING"

echo "=== Tests ==="
[ -f test/test_agent_compliance.sh ] && echo "TEST_SCRIPT_EXISTS" || echo "TEST_SCRIPT_NOT_FOUND"
```

### Decision Tree

```
Binary exists + tests pass?
└─ YES → Skip to Phase 5 (verify only)
└─ NO → Source exists?
    └─ YES → Build (Phase 3, step 3.2 only)
    └─ NO → Full setup (Phase 3 all steps)
```

---

## Phase 2: Collect Human Input (👤 ask once)

| What | Ask If | Example |
|---|---|---|
| `LITELLM_API_KEY` | `NOT_SET` | `sk-litellm-bedrock` |
| LiteLLM URL | Not default | `http://localhost:4000` |
| Model name | Custom model | `bedrock/anthropic.claude-sonnet-4-6` |

---

## Phase 3: Build & Configure (🤖 auto)

| Step | Action | Skip If |
|---|---|---|
| 3.1 | Install Go ≥1.21 | `go version` found |
| 3.2 | Build binary | `BINARY_EXISTS` |
| 3.3 | Create `.env` from `.env.example` | `ENV_EXISTS` |

### 3.2 Build

```bash
make build
./harness-factory --version
```

### 3.3 Environment

```bash
cp .env.example .env
# Edit .env with actual tokens
```

---

## Phase 4: Integration with acp-bridge (🤖 auto)

Add harness agent to acp-bridge `config.yaml`:

```yaml
agents:
  pr-reviewer:
    enabled: true
    mode: "acp"
    command: "/path/to/harness-factory"
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
        system_prompt: |
          You are a code reviewer. Analyze the diff, read relevant files,
          run linters if needed, and produce a structured review report.
        temperature: 0.3
```

Bridge injects `litellm_url` and `litellm_api_key` automatically.

---

## Phase 5: Verify (🤖 auto)

### 5.1 Protocol Compliance

```bash
make test
```

Expected: 3/3 Go unit tests pass.

### 5.2 Full Compliance + E2E

```bash
make test-e2e
```

Expected: 10/10 all tests pass (needs LiteLLM + .env).

### 5.3 Manual stdin Test

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | ./harness-factory
```

Expected:
```json
{"jsonrpc":"2.0","id":1,"result":{"agentInfo":{"name":"harness-factory","version":"0.3.0"},"capabilities":{}}}
```

---

## ACP Protocol Reference

### Methods

| Method | Direction | Purpose |
|---|---|---|
| `initialize` | → harness | Returns agentInfo + capabilities |
| `ping` | → harness | Health check, returns `{}` |
| `session/new` | → harness | Create session with profile, returns sessionId |
| `session/prompt` | → harness | Run agent loop for a prompt |
| `session/cancel` | → harness | Cancel current execution (notification, no response) |
| `session/update` | ← harness | Notifications: `tool_call`, `tool_call_update`, `agent_message_chunk` |

### session/new Params

```json
{
  "cwd": "/workspace/project",
  "mcpServers": [],
  "profile": {
    "tools": {
      "fs": { "permissions": ["read", "write", "list", "search"] },
      "git": { "permissions": ["status", "diff", "log", "show", "commit", "push"] },
      "shell": { "allowlist": ["pytest", "grep"] },
      "web": { "permissions": ["fetch"] }
    },
    "orchestration": "free",
    "resources": { "timeout": "300s", "max_turns": 20, "log_level": "info" },
    "agent": {
      "model": "bedrock/anthropic.claude-sonnet-4-6",
      "system_prompt": "You are a ...",
      "temperature": 0.3
    },
    "litellm_url": "http://localhost:4000",
    "litellm_api_key": "sk-..."
  }
}
```

### session/prompt Params

```json
{
  "sessionId": "<session_id>",
  "prompt": [{"type": "text", "text": "user input"}]
}
```

### session/prompt Response

```json
{
  "sessionId": "<session_id>",
  "stopReason": "end_turn"
}
```

### session/update Notifications

```json
{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"...","update":{"sessionUpdate":"tool_call","toolCallId":"...","title":"fs_read","status":"running"}}}
{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"...","update":{"sessionUpdate":"tool_call_update","toolCallId":"...","title":"fs_read","status":"completed","content":{"text":"file contents..."}}}}
{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"...","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"Final response text"}}}}
```

### Tool Names (as exposed to LLM)

| Tool | Operations | Permission Granularity |
|---|---|---|
| `fs_read` `fs_write` `fs_list` `fs_search` | File system | Per-operation: `[read]` / `[read, write]` / `[all]` |
| `git_status` `git_diff` `git_log` `git_show` `git_commit` `git_push` | Git | Per-operation: `[diff, log]` / `[all]` |
| `shell_exec` | Shell command | By command: `allowlist: [pytest]` or `blocklist: [rm]` |
| `web_fetch` | HTTP fetch | Per-operation: `[fetch]` / `[all]` |

### Permission Model

Two layers:
1. **Exposure** — Only activated tools appear in LLM tool definitions
2. **Enforcement** — Permission checker blocks unauthorized calls at runtime

---

### Profile Reference

Profile is passed via `session/new` params. It defines what tools are activated, what permissions they have, and how the agent behaves.

```yaml
tools:                                    # which tools to activate
  fs:
    permissions: [read]                   # read | write | list | search | all
  git:
    permissions: [diff, log, show]        # status | diff | log | show | commit | push | all
  shell:
    allowlist: [pytest, mypy, grep]       # whitelist mode (recommended)
    # blocklist: [rm, sudo, chmod]        # blacklist mode (pick one)
  web:
    permissions: [fetch]                  # fetch | all

orchestration: free                       # free | constrained | pipeline (P1)

resources:
  timeout: 300s                           # per-prompt timeout
  max_turns: 20                           # max LLM ↔ tool rounds per prompt
  log_level: info                         # debug | info | error

agent:
  model: "bedrock/anthropic.claude-sonnet-4-6"  # LiteLLM model name
  system_prompt: "You are a ..."          # system prompt for LLM
  temperature: 0.3                        # LLM temperature (0.0 - 1.0)

litellm_url: "http://localhost:4000"      # injected by Bridge
litellm_api_key: "sk-..."                # injected by Bridge
```

#### tools

| Tool | Valid permissions | Description |
|------|-----------------|-------------|
| `fs` | `read`, `write`, `list`, `search`, `all` | File system operations |
| `git` | `status`, `diff`, `log`, `show`, `commit`, `push`, `all` | Git operations |
| `shell` | N/A — uses `allowlist` or `blocklist` | Shell command execution |
| `web` | `fetch`, `all` | HTTP fetch |

- `all` grants every operation for that tool
- `shell` uses `allowlist` (recommended) or `blocklist`, not `permissions`
- Omitted tools are not activated — LLM cannot see or call them

#### orchestration

| Value | Behavior |
|-------|----------|
| `free` | No constraints, agent decides freely |
| `constrained` | Runtime checks: prerequisites + mutual exclusion (P1) |
| `pipeline` | Fixed step flow (P2) |

#### resources

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `timeout` | string | `"300s"` | Per-prompt timeout |
| `max_turns` | int | `20` | Max LLM ↔ tool rounds |
| `log_level` | string | `"info"` | `debug` \| `info` \| `error` — overrides `HARNESS_LOG_LEVEL` env |

#### agent

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `model` | string | yes | LiteLLM model name (e.g. `bedrock/anthropic.claude-sonnet-4-6`) |
| `system_prompt` | string | yes | System prompt injected into every LLM call |
| `temperature` | float | no | LLM temperature, default `0` |

#### Bridge-injected fields

These are set by acp-bridge automatically, not by the user:

| Field | Description |
|-------|-------------|
| `litellm_url` | LiteLLM proxy URL |
| `litellm_api_key` | LiteLLM API key |

---

### Key Files

| File | Purpose |
|---|---|
| `cmd/harness-factory/main.go` | Entry point, ACP JSON-RPC loop |
| `internal/acp/acp.go` | stdin/stdout JSON-RPC transport |
| `internal/profile/profile.go` | Profile struct + permission queries |
| `internal/permission/permission.go` | Runtime permission checker |
| `internal/tools/registry.go` | Tool registry + activation filter |
| `internal/tools/fs.go` | File system operations |
| `internal/tools/git.go` | Git operations |
| `internal/tools/shell.go` | Shell exec with allowlist |
| `internal/tools/web.go` | HTTP fetch |
| `internal/llm/llm.go` | LiteLLM HTTP client |
| `internal/agent/agent.go` | Agent loop (LLM ↔ tool-use) |
| `internal/logger/logger.go` | Structured stderr logging |
| `test/test_agent_compliance.sh` | Compliance + e2e test suite |
| `.env.example` | Environment template |
| `Makefile` | Build, test, run commands |

### Troubleshooting

| Symptom | Fix |
|---|---|
| `llm HTTP 400: Invalid model name` | Check model name matches LiteLLM config (`curl /v1/models`) |
| `llm request failed: connection refused` | LiteLLM not running at `litellm_url` |
| `no active session` | Must call `session/new` before `session/prompt` |
| `tool "X" not activated in profile` | Add tool to profile's `tools` section |
| `fs.write not permitted` | Add `write` to `fs.permissions` in profile |
| `shell command "X" not in allowlist` | Add command to `shell.allowlist` in profile |
| Binary too large | `make build` uses `-ldflags="-s -w"` for stripped binary (~6MB) |
| No logs | Set `HARNESS_LOG_LEVEL=debug` or `log_level: debug` in profile resources |
| Logs polluting stdout | Logs go to stderr only, stdout is reserved for JSON-RPC |

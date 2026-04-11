#!/bin/bash
# ACP Agent Compliance Test for harness-factory
# Usage: bash test/test_agent_compliance.sh [binary_path]
set -euo pipefail

BINARY="${1:-./harness-factory}"
PASS=0
FAIL=0

# Load .env from project root
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
source "$PROJECT_ROOT/.env" 2>/dev/null || true

run_test() {
  local name="$1" input="$2" check="$3"
  local output
  output=$(echo "$input" | "$BINARY" 2>/dev/null) || true
  if echo "$output" | grep -q "$check"; then
    echo "✅ $name"
    PASS=$((PASS + 1))
  else
    echo "❌ $name"
    echo "   expected: $check"
    echo "   got: $output"
    FAIL=$((FAIL + 1))
  fi
}

echo "=== ACP Agent Compliance Test ==="
echo "Binary: $BINARY"
echo ""

# 1. --version
VERSION_OUT=$("$BINARY" --version 2>/dev/null)
if echo "$VERSION_OUT" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+'; then
  echo "✅ --version ($VERSION_OUT)"
  PASS=$((PASS + 1))
else
  echo "❌ --version"; FAIL=$((FAIL + 1))
fi

# 2. initialize
run_test "initialize → agentInfo" \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
  '"agentInfo"'

# 3. ping
run_test "ping → pong" \
  '{"jsonrpc":"2.0","id":1,"method":"ping","params":{}}' \
  '"pong"'

# 4. session/new
run_test "session/new → sessionId" \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}
{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"cwd":"/tmp","profile":{"tools":{"fs":{"permissions":["read"]}},"orchestration":"free","resources":{"max_turns":5},"agent":{"model":"test","system_prompt":"test"},"litellm_url":"http://localhost:4000"}}}' \
  '"sessionId"'

# 5. prompt without session → error
run_test "prompt without session → error" \
  '{"jsonrpc":"2.0","id":1,"method":"session/prompt","params":{"prompt":"hello"}}' \
  '"error"'

# 6. unknown method → error
run_test "unknown method → error" \
  '{"jsonrpc":"2.0","id":1,"method":"bogus","params":{}}' \
  '"error"'

# 7. E2E with LiteLLM (optional)
LITELLM_URL="${LITELLM_URL:-http://localhost:4000}"
LITELLM_KEY="${LITELLM_API_KEY:-}"
MODEL="${HARNESS_MODEL:-bedrock/anthropic.claude-sonnet-4-6}"

if [ -n "$LITELLM_KEY" ]; then
  echo ""
  echo "--- E2E tests (LiteLLM: $LITELLM_URL, model: $MODEL) ---"

  E2E_OUT=$(cat <<JSONRPC | "$BINARY" 2>/dev/null
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}
{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"cwd":"/tmp","profile":{"tools":{"fs":{"permissions":["read","list"]},"shell":{"allowlist":["echo"]}},"orchestration":"free","resources":{"timeout":"60s","max_turns":5},"agent":{"model":"$MODEL","system_prompt":"You are a test agent. Always use tools. Be concise.","temperature":0.3},"litellm_url":"$LITELLM_URL","litellm_api_key":"$LITELLM_KEY"}}}
{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"prompt":"Run: echo hello"}}
JSONRPC
) || true

  if echo "$E2E_OUT" | grep -q '"tool.start"'; then
    echo "✅ e2e: tool.start notification"; PASS=$((PASS + 1))
  else
    echo "❌ e2e: tool.start notification"; FAIL=$((FAIL + 1))
  fi

  if echo "$E2E_OUT" | grep -q '"tool.done"'; then
    echo "✅ e2e: tool.done notification"; PASS=$((PASS + 1))
  else
    echo "❌ e2e: tool.done notification"; FAIL=$((FAIL + 1))
  fi

  if echo "$E2E_OUT" | grep -q '"stopReason"'; then
    echo "✅ e2e: stopReason returned"; PASS=$((PASS + 1))
  else
    echo "❌ e2e: stopReason missing"; FAIL=$((FAIL + 1))
  fi

  # Permission: fs_write should not be exposed
  PERM_OUT=$(cat <<JSONRPC | "$BINARY" 2>/dev/null
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}
{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"cwd":"/tmp","profile":{"tools":{"fs":{"permissions":["read"]}},"orchestration":"free","resources":{"timeout":"60s","max_turns":3},"agent":{"model":"$MODEL","system_prompt":"Always try to write files.","temperature":0.3},"litellm_url":"$LITELLM_URL","litellm_api_key":"$LITELLM_KEY"}}}
{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"prompt":"Write 'test' to /tmp/perm_test.txt"}}
JSONRPC
) || true

  if echo "$PERM_OUT" | grep -q '"fs_write"'; then
    echo "❌ e2e permission: fs_write was called"; FAIL=$((FAIL + 1))
  else
    echo "✅ e2e permission: fs_write not exposed"; PASS=$((PASS + 1))
  fi
else
  echo ""
  echo "--- Skipping e2e (no LITELLM_API_KEY) ---"
fi

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
[ "$FAIL" -eq 0 ] && exit 0 || exit 1

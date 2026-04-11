#!/bin/bash
# Quick launcher for harness-factory with a sample profile
# Usage:
#   bash scripts/run.sh                          # interactive stdin
#   bash scripts/run.sh "list files in /tmp"     # one-shot prompt
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BINARY="$PROJECT_ROOT/harness-factory"

# Build if needed
if [ ! -f "$BINARY" ]; then
  echo "Building..." >&2
  make -C "$PROJECT_ROOT" build >&2
fi

# Load env
source "$PROJECT_ROOT/.env" 2>/dev/null || true

LITELLM_URL="${LITELLM_URL:-http://localhost:4000}"
LITELLM_KEY="${LITELLM_API_KEY:-}"
MODEL="${HARNESS_MODEL:-bedrock/anthropic.claude-sonnet-4-6}"
CWD="${HARNESS_CWD:-$(pwd)}"

PROMPT="${1:-}"

if [ -z "$LITELLM_KEY" ]; then
  echo "Error: LITELLM_API_KEY not set. Create .env or export it." >&2
  exit 1
fi

# Build JSON-RPC messages
INIT='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}'
SESSION=$(cat <<EOF
{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"cwd":"$CWD","profile":{"tools":{"fs":{"permissions":["read","write","list","search"]},"git":{"permissions":["all"]},"shell":{"allowlist":["ls","cat","grep","find","wc","head","tail","echo","date","pwd","env"]},"web":{"permissions":["fetch"]}},"orchestration":"free","resources":{"timeout":"300s","max_turns":20},"agent":{"model":"$MODEL","system_prompt":"You are a helpful assistant with access to filesystem, git, shell, and web tools. Be concise.","temperature":0.3},"litellm_url":"$LITELLM_URL","litellm_api_key":"$LITELLM_KEY"}}}
EOF
)

if [ -n "$PROMPT" ]; then
  # One-shot mode
  PROMPT_MSG=$(printf '{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"prompt":"%s"}}' "$(echo "$PROMPT" | sed 's/"/\\"/g')")
  printf '%s\n%s\n%s\n' "$INIT" "$SESSION" "$PROMPT_MSG" | "$BINARY" 2>/dev/null
else
  # Interactive mode
  echo "harness-factory interactive mode (Ctrl+D to exit)" >&2
  echo "Initializing..." >&2
  {
    echo "$INIT"
    echo "$SESSION"
    ID=3
    while IFS= read -r -p "> " line; do
      PROMPT_MSG=$(printf '{"jsonrpc":"2.0","id":%d,"method":"session/prompt","params":{"prompt":"%s"}}' "$ID" "$(echo "$line" | sed 's/"/\\"/g')")
      echo "$PROMPT_MSG"
      ID=$((ID + 1))
    done
  } | "$BINARY" 2>/dev/null
fi

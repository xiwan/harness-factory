#!/usr/bin/env bash
# check-env.sh — detect game-qa runtime dependencies. Read-only, never installs.
# Output: single JSON object on stdout. Exit 0 = ready, exit 1 = missing deps.
set -u

NODE_MIN_MAJOR=18
# Resolve playwright from the skill dir (where setup.sh installs it), not the
# caller's cwd — qa-driver.mjs resolves the same way via createRequire.
SKILL_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ready=true
node_ok=false
node_version=""
playwright_ok=false
playwright_version=""
chromium_ok=false
missing=()

if command -v node >/dev/null 2>&1; then
  node_version=$(node --version 2>/dev/null | tr -d 'v')
  major=${node_version%%.*}
  if [ "${major:-0}" -ge "$NODE_MIN_MAJOR" ] 2>/dev/null; then
    node_ok=true
  else
    missing+=("node>=${NODE_MIN_MAJOR} (found ${node_version})")
    ready=false
  fi
else
  missing+=("node>=${NODE_MIN_MAJOR}")
  ready=false
fi

if [ "$node_ok" = true ]; then
  pw_out=$(cd "$SKILL_DIR" && node -e "console.log(require('playwright/package.json').version)" 2>/dev/null)
  if [ -n "$pw_out" ]; then
    playwright_ok=true
    playwright_version="$pw_out"
    if (cd "$SKILL_DIR" && node -e "const{chromium}=require('playwright');if(!chromium.executablePath())process.exit(1);require('fs').accessSync(chromium.executablePath())") >/dev/null 2>&1; then
      chromium_ok=true
    else
      missing+=("chromium browser (npx playwright install chromium)")
      ready=false
    fi
  else
    missing+=("playwright npm package")
    missing+=("chromium browser")
    ready=false
  fi
fi

missing_json=""
for m in ${missing[@]+"${missing[@]}"}; do
  [ -n "$missing_json" ] && missing_json+=","
  missing_json+="\"$m\""
done

cat <<EOF
{
  "ready": $ready,
  "node": {"ok": $node_ok, "version": "$node_version"},
  "playwright": {"ok": $playwright_ok, "version": "$playwright_version"},
  "chromium": {"ok": $chromium_ok},
  "missing": [$missing_json],
  "next_step": $([ "$ready" = true ] && echo '"environment ready — proceed with qa-driver.mjs"' || echo '"MISSING DEPS: inform the user what setup.sh will install and get confirmation BEFORE running it"')
}
EOF

[ "$ready" = true ] && exit 0 || exit 1

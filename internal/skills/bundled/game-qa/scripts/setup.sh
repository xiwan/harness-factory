#!/usr/bin/env bash
# setup.sh — install game-qa dependencies (playwright + chromium) into the skill dir.
# PRECONDITION (enforced by SKILL.md): the user has been told what this installs
# and has confirmed. This script must never be run silently.
#
# Installs:
#   - npm package `playwright` into skills/game-qa/ (local node_modules, not global)
#   - chromium browser via `npx playwright install chromium` (~130MB, under ~/.cache/ms-playwright)
# Does NOT install node itself — if node is missing, tell the user to install it
# via their system package manager (e.g. dnf/apt/nvm); that is out of scope here.
set -eu

SKILL_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$SKILL_DIR"

if ! command -v node >/dev/null 2>&1; then
  echo "ERROR: node not found. Ask the user to install Node.js >= 18 first (dnf/apt/nvm)." >&2
  exit 1
fi

echo "[game-qa setup] installing playwright into $SKILL_DIR ..."
[ -f package.json ] || npm init -y >/dev/null
npm install --no-fund --no-audit playwright

echo "[game-qa setup] installing chromium browser (~130MB) ..."
npx playwright install chromium

echo "[game-qa setup] done. verifying:"
bash "$SKILL_DIR/scripts/check-env.sh"

#!/usr/bin/env node
// qa-driver.mjs — rule-driven Playwright harness for web game QA.
// The LLM never operates the browser frame-by-frame; it invokes this driver
// and reads back structured JSON (trace, score, evidence paths).
//
// Subcommands:
//   probe  --url <u>                          detect GameFlowAgentAPI, capability level
//   play   --url <u> --type <t> [--duration s] [--max-steps n] [--out dir]
//                                              one attempt: observe→act loop, trace + screenshots
//   score  --trace <file.json>                 five-dimension rule scoring with caps
//
// All output: single JSON object on stdout. Screenshots land in --out (default ./qa-evidence).
// Requires: node>=18, playwright, chromium (run check-env.sh first).

import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);

const STANDARD_API_METHODS = [
  "getGameInfo", "observe", "availableActions", "step", "evaluate",
  "listTestScenarios", "checkScenarioPreconditions", "repairScenario",
  "jumpToScenario", "evaluateScenario",
];

// Black-box keyboard fallback (from wai-play's adapter mapping)
const KEY_MAP = {
  UP: "ArrowUp", DOWN: "ArrowDown", LEFT: "ArrowLeft", RIGHT: "ArrowRight",
  ATTACK: "KeyJ", JUMP: "Space", CONFIRM: "Enter", RESTART: "KeyR",
  CHOOSE_1: "Digit1", CHOOSE_2: "Digit2", CHOOSE_3: "Digit3",
};

// Per-type default action policies (distilled from wai-play game_profiles)
const TYPE_POLICIES = {
  survivor_like:  { actions: ["UP", "DOWN", "LEFT", "RIGHT", "ATTACK", "CHOOSE_1"], goalHint: "survive until target_duration" },
  arcade_shooter: { actions: ["LEFT", "RIGHT", "ATTACK", "UP", "DOWN"], goalHint: "score / clear waves" },
  platformer:     { actions: ["RIGHT", "JUMP", "DASH", "LEFT", "UP"], goalHint: "reach the goal flag" },
  puzzle_card:    { actions: ["CHOOSE_1", "CHOOSE_2", "CHOOSE_3", "CONFIRM"], goalHint: "solve level objective" },
  visual_novel:   { actions: ["CONFIRM", "CHOOSE_1", "CHOOSE_2"], goalHint: "reach an ending" },
};

function parseArgs(argv) {
  const args = { _: [] };
  for (let i = 0; i < argv.length; i++) {
    if (argv[i].startsWith("--")) args[argv[i].slice(2)] = argv[i + 1] && !argv[i + 1].startsWith("--") ? argv[++i] : true;
    else args._.push(argv[i]);
  }
  return args;
}

function out(obj) { process.stdout.write(JSON.stringify(obj, null, 2) + "\n"); }
function fail(msg) { out({ error: msg }); process.exit(1); }

async function launch(url, videoDir) {
  const { chromium } = require("playwright");
  const browser = await chromium.launch({ headless: true });
  const viewport = { width: 960, height: 540 };
  const opts = videoDir
    ? { viewport, recordVideo: { dir: videoDir, size: viewport } }
    : { viewport };
  const context = await browser.newContext(opts);
  const page = await context.newPage();
  const consoleErrors = [];
  page.on("pageerror", (e) => consoleErrors.push(String(e).slice(0, 300)));
  page.on("console", (m) => { if (m.type() === "error") consoleErrors.push(m.text().slice(0, 300)); });
  await page.goto(url, { waitUntil: "domcontentloaded", timeout: 30_000 });
  await page.waitForTimeout(1500);
  return { browser, context, page, consoleErrors };
}

// Video is only flushed to disk when the context closes; call before browser.close().
async function closeAndCollectVideo(context, page) {
  const video = page.video();
  await context.close();
  if (!video) return null;
  try { return await video.path(); } catch { return null; }
}

async function probeAPI(page) {
  const status = await page.evaluate((methods) => {
    const api = window.GameFlowAgentAPI;
    if (!api) return { present: false, methods: {} };
    const m = {};
    for (const name of methods) m[name] = typeof api[name] === "function";
    return { present: true, methods: m };
  }, STANDARD_API_METHODS);
  if (!status.present) return { capability: "black_box", ...status };

  // Functional check: do observe/step actually work, not just exist?
  const functional = await page.evaluate(async () => {
    const api = window.GameFlowAgentAPI;
    const r = { observe: false, actions: false, step: false };
    try { const o = api.observe(); r.observe = o && typeof o === "object"; } catch {}
    try { const a = api.availableActions ? api.availableActions() : null; r.actions = Array.isArray(a) && a.length > 0; } catch {}
    try { if (r.actions) { api.step(api.availableActions()[0]); r.step = true; } } catch {}
    return r;
  });
  const missing = STANDARD_API_METHODS.filter((m) => !status.methods[m]);
  const capability = functional.observe && functional.step
    ? (missing.length === 0 ? "full_test" : "limited")
    : "black_box";
  return { capability, present: true, methods: status.methods, missing, functional };
}

async function tryStartGame(page) {
  for (const sel of ["#overlay button", "button:has-text('开始')", "button:has-text('Start')", "button:has-text('START')", "canvas"]) {
    try { await page.click(sel, { timeout: 1000 }); return sel; } catch {}
  }
  return null;
}

async function cmdProbe(args) {
  if (!args.url) fail("probe requires --url");
  const { browser, page, consoleErrors } = await launch(args.url);
  try {
    const probe = await probeAPI(page);
    let info = null, scenarios = [];
    if (probe.capability !== "black_box") {
      info = await page.evaluate(() => { try { return window.GameFlowAgentAPI.getGameInfo(); } catch { return null; } });
      scenarios = await page.evaluate(() => { try { return window.GameFlowAgentAPI.listTestScenarios() || []; } catch { return []; } });
    }
    out({ url: args.url, ...probe, game_info: info, scenarios: scenarios.map((s) => ({ id: s.id ?? s.scenario_id, priority: s.priority, goal: s.goal ?? s.description ?? s.name })), console_errors: consoleErrors });
  } finally { await browser.close(); }
}

async function cmdPlay(args) {
  if (!args.url) fail("play requires --url");
  const type = args.type || "survivor_like";
  const policy = TYPE_POLICIES[type] || TYPE_POLICIES.survivor_like;
  const maxSteps = parseInt(args["max-steps"] || "120", 10);
  const durationS = parseFloat(args.duration || "60");
  const outDir = args.out || "qa-evidence";
  mkdirSync(outDir, { recursive: true });

  const { browser, context, page, consoleErrors } = await launch(args.url, outDir);
  const trace = [];
  const screenshots = [];
  let verdict = { done: false, success: false, failed: false, reason: "max_steps_or_timeout" };
  try {
    const probe = await probeAPI(page);
    const apiMode = probe.capability !== "black_box";
    await tryStartGame(page);
    await page.screenshot({ path: join(outDir, "step-000-start.png") });
    screenshots.push("step-000-start.png");

    const t0 = Date.now();
    let lastStateJSON = "";
    let stagnant = 0;
    let ai = 0; // action index for round-robin fallback

    for (let step = 1; step <= maxSteps && (Date.now() - t0) / 1000 < durationS; step++) {
      let stateBefore = null, actions = policy.actions, chosen;
      if (apiMode) {
        stateBefore = await page.evaluate(() => { try { return window.GameFlowAgentAPI.observe(); } catch { return null; } });
        const avail = await page.evaluate(() => { try { return window.GameFlowAgentAPI.availableActions(); } catch { return null; } });
        if (Array.isArray(avail) && avail.length) actions = avail;
      }
      // Rule policy: round-robin through the type's action set so movement varies
      // (a fixed preference order walks into walls). Steer away from danger when
      // the game exposes it; pick pending choices (e.g. upgrade options) first.
      const rotation = policy.actions.filter((a) => actions.includes(a));
      chosen = rotation.length ? rotation[ai++ % rotation.length] : actions[ai++ % actions.length];
      const choice = actions.find((a) => String(a).startsWith("CHOOSE"));
      if (stateBefore?.upgrade?.is_selecting_upgrade && choice) chosen = choice;
      const danger = stateBefore?.world?.danger_direction;
      if (danger && KEY_MAP[danger] && chosen === danger) {
        const opposite = { UP: "DOWN", DOWN: "UP", LEFT: "RIGHT", RIGHT: "LEFT" }[danger];
        if (opposite && actions.includes(opposite)) chosen = opposite;
      }

      if (apiMode) {
        await page.evaluate((a) => { try { window.GameFlowAgentAPI.step(a); } catch {} }, chosen);
      } else {
        const key = KEY_MAP[chosen] || "Space";
        await page.keyboard.press(key);
      }
      await page.waitForTimeout(250);

      let stateAfter = null;
      if (apiMode) {
        stateAfter = await page.evaluate(() => { try { return window.GameFlowAgentAPI.observe(); } catch { return null; } });
        const j = JSON.stringify(stateAfter);
        if (j === lastStateJSON) stagnant++; else { stagnant = 0; lastStateJSON = j; }
        const st = stateAfter && stateAfter.status;
        if (st && (st.done || st.success || st.failed)) {
          verdict = { done: !!st.done, success: !!st.success, failed: !!st.failed, reason: "status_signal" };
        }
      }
      trace.push({ step, action: chosen, mode: apiMode ? "api" : "black_box", state_before: stateBefore, state_after: stateAfter, stagnant });

      if (step % 20 === 0 || verdict.done) {
        const name = `step-${String(step).padStart(3, "0")}.png`;
        await page.screenshot({ path: join(outDir, name) });
        screenshots.push(name);
      }
      if (verdict.done || verdict.success || verdict.failed) break;
      if (stagnant >= 10) { verdict.reason = "stagnation"; break; }
    }

    const name = "final.png";
    await page.screenshot({ path: join(outDir, name) });
    screenshots.push(name);

    // Close context first: Playwright only flushes the video file on context close.
    const videoPath = await closeAndCollectVideo(context, page);

    const result = {
      url: args.url, game_type: type, capability: apiMode ? probe.capability : "black_box",
      steps: trace.length, elapsed_s: Math.round((Date.now() - t0) / 100) / 10,
      verdict, stagnant_streak_max: Math.max(0, ...trace.map((t) => t.stagnant)),
      console_errors: consoleErrors, screenshots: screenshots.map((s) => join(outDir, s)),
      video: videoPath, trace_tail: trace.slice(-15),
    };
    const traceFile = join(outDir, "trace.json");
    writeFileSync(traceFile, JSON.stringify({ ...result, trace }, null, 2));
    out({ ...result, trace_file: traceFile, note: "full trace in trace_file; trace_tail is last 15 steps" });
  } finally { await browser.close(); }
}

// Five-dimension rule scoring with caps (distilled from wai-play rater_v6 / scoring_standards)
const WEIGHTS = { task_flow: 0.24, gameplay: 0.26, ui_quality: 0.20, feedback: 0.15, technical_quality: 0.15 };

function cmdScore(args) {
  if (!args.trace) fail("score requires --trace <trace.json>");
  const t = JSON.parse(readFileSync(args.trace, "utf8"));
  const s = {};
  const apiMode = t.capability !== "black_box";
  const errors = (t.console_errors || []).length;
  const v = t.verdict || {};

  s.task_flow = v.success ? 90 : v.done ? 70 : t.steps > 10 ? 50 : 30;
  const changed = (t.trace || []).filter((x) => x.stagnant === 0).length;
  s.gameplay = apiMode ? Math.min(90, 40 + Math.round((changed / Math.max(1, t.steps)) * 50)) : null;
  s.ui_quality = null; // needs human/LLM review of screenshots — driver cannot judge visuals
  s.feedback = apiMode ? (t.stagnant_streak_max >= 10 ? 35 : 70) : null;
  s.technical_quality = errors === 0 ? 90 : errors <= 3 ? 60 : 35;

  // caps (wai-play build_caps, simplified)
  const caps = [];
  if (v.reason === "stagnation") caps.push({ cap: 55, why: "flow blocked / stagnation" });
  if (errors > 3) caps.push({ cap: 50, why: "serious technical errors" });
  if (!v.success && v.failed) caps.push({ cap: 58, why: "run failed" });

  let ratedWeight = 0, total = 0;
  for (const [k, w] of Object.entries(WEIGHTS)) if (s[k] != null) { ratedWeight += w; total += s[k] * w; }
  let overall = ratedWeight >= 0.6 ? Math.round(total / ratedWeight) : null;
  if (overall != null) for (const c of caps) overall = Math.min(overall, c.cap);
  if (overall != null && overall > 89 && !(v.success && errors === 0)) overall = 89; // release-quality gate

  out({
    dimensions: s, caps, rated_weight: Math.round(ratedWeight * 100) / 100,
    overall_score_100: overall,
    confidence: apiMode && v.success ? "high" : apiMode ? "medium" : "low",
    note: overall == null ? "insufficient evidence coverage (<60% weight rated) — score withheld" :
      "ui_quality unset: LLM must review screenshots and merge its judgment into the final report",
  });
}

const args = parseArgs(process.argv.slice(2));
const cmd = args._[0];
try {
  if (cmd === "probe") await cmdProbe(args);
  else if (cmd === "play") await cmdPlay(args);
  else if (cmd === "score") cmdScore(args);
  else fail("usage: qa-driver.mjs <probe|play|score> [--url u] [--type t] [--trace f]");
} catch (e) {
  fail(String(e && e.message || e));
}

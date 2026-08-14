#!/usr/bin/env node
// conform.test.mjs — NEGATIVE tests for the conformance checker (the acceptance gate; §0a "verify the
// tool before trusting it"). Each fixture PLANTS one invariant violation and asserts conform.mjs FLAGS
// exactly that rule; a clean fixture asserts an all-pass. A checker that can't catch a planted
// violation is worthless — so this file is the reason to trust conform.mjs at all.
//
// Isolation: each fixture is its own tiny "repo" rooted by a `.zenith-protocol.json` marker (so
// findRepoRoot stops there, never walking up into the real repo). The append-only test needs real git
// history, so it inits a throwaway git repo. Run: node conform.test.mjs
import { mkdtempSync, writeFileSync, mkdirSync, rmSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const CONFORM = join(dirname(fileURLToPath(import.meta.url)), "conform.mjs");
let passed = 0, failed = 0;
const fails = [];

// ---- clean, conformant ledger content (the baseline every negative fixture mutates) --------------
const CLEAN = {
  ".zenith-protocol.json": JSON.stringify({
    ledgerDir: "led", capabilityMap: "led/CAPABILITY-MAP.md", backlog: "led/HYPOTHESIS-BACKLOG.md",
    assumptions: "led/ASSUMPTIONS-LEDGER.md", pdrs: "led/PROCESS-IMPROVEMENTS.md",
    metricsDoc: "led/METRICS.md", metricsLedger: "led/metrics-ledger.jsonl",
    operatingModelDoc: "led/OM.md", operatingModelStartMarker: "OPERATING MODEL",
    contextDocs: ["led/PROJECT.md"],
    emittersDir: "no-such-emitters", generatedTrees: [],
  }, null, 2),
  "led/CAPABILITY-MAP.md": [
    "# Capability map", "",
    "| Hyp | claim / method | status + evidence |",
    "|---|---|---|",
    "| H1 | probe the route | **BLOCKED** — 403 IAM on `asset-server`, see `probe.mjs` |",
    "| H2 | read the bean | **OPEN** — `javap -p` on the `-objects` jar pending |",
  ].join("\n"),
  "led/HYPOTHESIS-BACKLOG.md": [
    "# Backlog", "",
    "| id | proposed hypothesis | origin | gatekeeper verdict |",
    "|---|---|---|---|",
    "| PH-1 | make the differential a fleet gate | orchestrator | **ACCEPT → B8** |",
    "| PH-2 | seed fake objects | agent-x | **REJECT — violates no-fake-data** |",
  ].join("\n"),
  "led/ASSUMPTIONS-LEDGER.md": "# Assumptions\n\n| id | claim | status |\n|---|---|---|\n| A1 | x | CONFIRMED |\n",
  "led/PROCESS-IMPROVEMENTS.md": [
    "# PDRs", "",
    "| PDR | change | status | trial exit criterion | measure | holistic check | votes |",
    "|---|---|---|---|---|---|---|",
    "| P1 | fan out subagents | **TRIAL** | beats serial on 2 threads | `[observed]` 2.4x on that batch | +tokens/agent | good×3 |",
    "| P2 | single-writer per file-set | **ADOPTED** | 0 races | `[observed]` 0 races over 35 commits | ~0 cost | — |",
  ].join("\n"),
  "led/METRICS.md": "# Metrics\n",
  "led/metrics-ledger.jsonl":
    JSON.stringify({ id: "m0001", date: "2026-07-31T00:00:00Z", commit: "abc1234", category: "gate", metric: "coverage", value: 41, unit: "services", tier: "observed", source: "verify.mjs", note: "ok" }) + "\n" +
    JSON.stringify({ id: "m0002", date: "2026-07-31T00:00:01Z", commit: "abc1234", category: "process", metric: "speedup", value: 2.4, unit: "x", tier: "observed", source: "retro", note: "ok" }) + "\n",
  "led/OM.md": "# OM\n\nOPERATING MODEL\n\nsome method text\n\n## next\n",
  "led/PROJECT.md": "# Project-specific authority and gates\n",
};

function makeFixture(overrides = {}) {
  const dir = mkdtempSync(join(tmpdir(), "zp-conform-"));
  const files = { ...CLEAN, ...overrides };
  for (const [rel, content] of Object.entries(files)) {
    const full = join(dir, rel);
    mkdirSync(dirname(full), { recursive: true });
    writeFileSync(full, content);
  }
  return dir;
}

function runConform(dir) {
  try {
    const out = execFileSync("node", [CONFORM, "--json"], { cwd: dir, stdio: ["ignore", "pipe", "pipe"] }).toString();
    return { code: 0, out: JSON.parse(out) };
  } catch (e) {
    const stdout = (e.stdout || "").toString();
    return { code: e.status ?? 1, out: stdout ? JSON.parse(stdout) : { results: [] } };
  }
}

const statusOf = (out, rule) => (out.results.find((r) => r.rule === rule) || {}).status;

function assert(name, cond, msg) {
  if (cond) { passed++; console.log(`  ok   ${name}`); }
  else { failed++; fails.push(`${name}: ${msg}`); console.log(`  FAIL ${name} — ${msg}`); }
}

// ---- 1. clean fixture PASSES ---------------------------------------------------------------------
{
  const dir = makeFixture();
  const { code, out } = runConform(dir);
  assert("clean fixture: exit 0", code === 0, `exit ${code}, fails=${JSON.stringify(out.results.filter((r) => r.status === "FAIL"))}`);
  assert("clean fixture: no FAIL rules", out.results.every((r) => r.status !== "FAIL"), JSON.stringify(out.results.filter((r) => r.status === "FAIL").map((r) => r.rule)));
  rmSync(dir, { recursive: true, force: true });
}

// ---- 2. scattered double good×N → pdr-single-vote-tally ------------------------------------------
{
  const dir = makeFixture();
  rmSync(join(dir, "led/PROJECT.md"), { force: true });
  const { code, out } = runConform(dir);
  assert("missing context doc: flagged", statusOf(out, "context-docs-exist") === "FAIL", `got ${statusOf(out, "context-docs-exist")}`);
  assert("missing context doc: exit non-zero", code !== 0, `exit ${code}`);
  rmSync(dir, { recursive: true, force: true });
}

// ---- 2. scattered double good×N → pdr-single-vote-tally ------------------------------------------
{
  const dir = makeFixture({
    "led/PROCESS-IMPROVEMENTS.md": CLEAN["led/PROCESS-IMPROVEMENTS.md"].replace(
      "| net-positive **only for non-trivial independent threads**; +tokens/agent | good×3 |",
      "").replace("| good×3 |", "| good×3 and later good×2 |"),
  });
  const { code, out } = runConform(dir);
  assert("scattered tally: flagged", statusOf(out, "pdr-single-vote-tally") === "FAIL", `got ${statusOf(out, "pdr-single-vote-tally")}`);
  assert("scattered tally: exit non-zero", code !== 0, `exit ${code}`);
  rmSync(dir, { recursive: true, force: true });
}

// ---- 3. ADOPTED with empty measure → pdr-adopted-has-measure -------------------------------------
{
  const dir = makeFixture({
    "led/PROCESS-IMPROVEMENTS.md": CLEAN["led/PROCESS-IMPROVEMENTS.md"].replace(
      "| P2 | single-writer per file-set | **ADOPTED** | 0 races | `[observed]` 0 races over 35 commits | ~0 cost | — |",
      "| P2 | single-writer per file-set | **ADOPTED** | 0 races | — | ~0 cost | — |"),
  });
  const { out } = runConform(dir);
  assert("adopted-empty-measure: flagged", statusOf(out, "pdr-adopted-has-measure") === "FAIL", `got ${statusOf(out, "pdr-adopted-has-measure")}`);
  rmSync(dir, { recursive: true, force: true });
}

// ---- 4. un-gatekept hypothesis row → hypothesis-intake-gatekept ----------------------------------
{
  const dir = makeFixture({
    "led/HYPOTHESIS-BACKLOG.md": CLEAN["led/HYPOTHESIS-BACKLOG.md"].replace(
      "| PH-2 | seed fake objects | agent-x | **REJECT — violates no-fake-data** |",
      "| PH-2 | a lead nobody gatekept | agent-x |  |"),
  });
  const { out } = runConform(dir);
  assert("ungatekept-hypothesis: flagged", statusOf(out, "hypothesis-intake-gatekept") === "FAIL", `got ${statusOf(out, "hypothesis-intake-gatekept")}`);
  rmSync(dir, { recursive: true, force: true });
}

// ---- 5. malformed metrics line → metrics-jsonl-valid ---------------------------------------------
{
  const dir = makeFixture({
    "led/metrics-ledger.jsonl": CLEAN["led/metrics-ledger.jsonl"] +
      JSON.stringify({ id: "m0003", date: "2026-07-31T00:00:02Z", commit: "abc1234", category: "gate" /* missing metric,value,tier */ }) + "\n" +
      "{not valid json at all}\n",
  });
  const { out } = runConform(dir);
  assert("malformed-metric: flagged", statusOf(out, "metrics-jsonl-valid") === "FAIL", `got ${statusOf(out, "metrics-jsonl-valid")}`);
  rmSync(dir, { recursive: true, force: true });
}

// ---- 6. invalid PDR status → pdr-status-valid ----------------------------------------------------
{
  const dir = makeFixture({
    "led/PROCESS-IMPROVEMENTS.md": CLEAN["led/PROCESS-IMPROVEMENTS.md"].replace("**TRIAL**", "**MAYBE**"),
  });
  const { out } = runConform(dir);
  assert("invalid-pdr-status: flagged", statusOf(out, "pdr-status-valid") === "FAIL", `got ${statusOf(out, "pdr-status-valid")}`);
  rmSync(dir, { recursive: true, force: true });
}

// ---- 7. append-only violation (real git repo) → metrics-append-only ------------------------------
{
  const dir = makeFixture();
  const g = (...a) => execFileSync("git", a, { cwd: dir, stdio: ["ignore", "pipe", "pipe"] });
  g("init", "-q");
  g("config", "user.email", "t@t"); g("config", "user.name", "t"); g("config", "commit.gpgsign", "false");
  g("add", "-A"); g("commit", "-qm", "init");
  // EDIT an existing committed line in place (append-only violation), then commit.
  writeFileSync(join(dir, "led/metrics-ledger.jsonl"),
    JSON.stringify({ id: "m0001", date: "2026-07-31T00:00:00Z", commit: "abc1234", category: "gate", metric: "coverage", value: 99, unit: "services", tier: "observed", source: "verify.mjs", note: "EDITED" }) + "\n" +
    JSON.stringify({ id: "m0002", date: "2026-07-31T00:00:01Z", commit: "abc1234", category: "process", metric: "speedup", value: 2.4, unit: "x", tier: "observed", source: "retro", note: "ok" }) + "\n");
  g("add", "-A"); g("commit", "-qm", "edit m0001 in place");
  const { out } = runConform(dir);
  assert("append-only-edit: flagged", statusOf(out, "metrics-append-only") === "FAIL", `got ${statusOf(out, "metrics-append-only")}`);
  // sanity: a pure APPEND to the same repo must NOT be flagged.
  const dir2 = makeFixture();
  const g2 = (...a) => execFileSync("git", a, { cwd: dir2, stdio: ["ignore", "pipe", "pipe"] });
  g2("init", "-q"); g2("config", "user.email", "t@t"); g2("config", "user.name", "t"); g2("config", "commit.gpgsign", "false");
  g2("add", "-A"); g2("commit", "-qm", "init");
  writeFileSync(join(dir2, "led/metrics-ledger.jsonl"), CLEAN["led/metrics-ledger.jsonl"] +
    JSON.stringify({ id: "m0003", date: "2026-07-31T00:00:02Z", commit: "abc1234", category: "gate", metric: "x", value: 1, tier: "observed" }) + "\n");
  g2("add", "-A"); g2("commit", "-qm", "append m0003");
  const { out: out2 } = runConform(dir2);
  assert("append-only-append: NOT flagged", statusOf(out2, "metrics-append-only") === "PASS", `got ${statusOf(out2, "metrics-append-only")}`);
  rmSync(dir, { recursive: true, force: true });
  rmSync(dir2, { recursive: true, force: true });
}

console.log(`\n${passed} passed · ${failed} failed`);
if (failed) { console.error("NEGATIVE TESTS FAILED:\n- " + fails.join("\n- ")); process.exit(1); }
console.log("all negative fixtures were flagged; clean fixture passed — conform.mjs is trustworthy.");

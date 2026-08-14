#!/usr/bin/env node
// Meta-retrospective — a process improvement ABOUT the process-improvement mechanism.
//
// Per-thread self-improvement improves the METHOD; this closes the loop one level up. On a cadence
// (every N delegated threads) it asks "is the improvement mechanism itself still earning its cost, or
// is it churning / bloating / rubber-stamping?" — answered with MEASURED signals, not vibes, so the
// meta-review obeys the same discipline it audits. READ-ONLY over the repo; APPENDS its signals to the
// metrics ledger (with --record) so the meta-review is itself a durable time-series.
//
// Repo-agnostic via .zenith-protocol.json. Usage: node meta-retro.mjs [--record]
import { readFileSync, appendFileSync, existsSync, mkdirSync } from "node:fs";
import { execSync } from "node:child_process";
import { dirname } from "node:path";
import { loadConfig } from "../lib/config.mjs";

const cfg = loadConfig();
const doRecord = process.argv.includes("--record");

// --- Signal 1: PDR state distribution + vote volume (churn / rubber-stamp detectors) ---
let pdrRows = [];
if (existsSync(cfg.pdrs)) pdrRows = readFileSync(cfg.pdrs, "utf8").split("\n").filter((l) => /^\| P\d+ /.test(l));
const state = { PROPOSED: 0, TRIAL: 0, ADOPTED: 0, REJECTED: 0 };
let votesGood = 0, votesNonGood = 0, votesNone = 0;
for (const row of pdrRows) {
  for (const s of Object.keys(state)) if (row.includes(`**${s}**`)) state[s]++;
  const cells = row.split("|").slice(1, -1).map((c) => c.trim());
  const votes = cells[cells.length - 1] || "";
  // Count actual vote VOLUME, not rows-carrying-a-vote: a cell like "good×4" is 4 good votes, not 1.
  const countOf = (kw) => {
    const m = votes.match(new RegExp(`\\b${kw}\\s*[×x]\\s*(\\d+)`, "i"));
    if (m) return Number(m[1]);
    return new RegExp(`\\b${kw}\\b`, "i").test(votes) ? 1 : 0;
  };
  const g = countOf("good");
  const ng = countOf("bad") + countOf("insignificant") + countOf("out-of-scope") + countOf("missing-the-big-picture");
  votesGood += g; votesNonGood += ng;
  if (g + ng === 0) votesNone++;
}
const pdrTotal = pdrRows.length;

// --- Signal 2: method SIZE (bloat proxy) — lines in the operating-model span, if configured ---
let methodSizeLines = -1;
if (existsSync(cfg.operatingModelDoc) && cfg.operatingModelStartMarker) {
  const lines = readFileSync(cfg.operatingModelDoc, "utf8").split("\n");
  const start = lines.findIndex((l) => l.includes(cfg.operatingModelStartMarker));
  if (start >= 0) {
    let end;
    if (cfg.operatingModelEndMarker) end = lines.findIndex((l, i) => i > start && l.includes(cfg.operatingModelEndMarker));
    else end = lines.findIndex((l, i) => i > start && /^## /.test(l)); // next section header
    methodSizeLines = end > start ? end - start : lines.length - start;
  }
}

// --- Signal 3: durable-measurement usage — points in the metrics ledger ---
let ledgerPoints = 0, ledgerMetrics = 0;
if (existsSync(cfg.metricsLedger)) {
  const rows = readFileSync(cfg.metricsLedger, "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l));
  ledgerPoints = rows.length;
  ledgerMetrics = new Set(rows.map((r) => `${r.category}/${r.metric}`)).size;
}

const flags = [];
if (state.TRIAL > 0 && state.ADOPTED + state.REJECTED === 0)
  flags.push("CHURN?: TRIALs exist but nothing has graduated to ADOPTED/REJECTED yet — watch the exit criteria.");
if (votesGood >= 4 && votesNonGood === 0)
  flags.push(`RUBBER-STAMP? (weak proxy): ${votesGood} 'good' votes, 0 dissent. But vote-uniformity ≠ rubber-stamp — agents often vote 'good' on the METHOD while putting genuine ARTIFACT critique in the return BODY. Before concluding rubber-stamp, check recent return bodies for honest critical engagement. If votes AND bodies are both uncritical, THEN downgrade the vote PDR to spot-check.`);
if (methodSizeLines > 120)
  flags.push(`BLOAT?: operating-model span is ${methodSizeLines} lines — check measures improved commensurately, else prune (measurably sharper, not merely bigger).`);
if (pdrTotal > 0 && ledgerPoints < pdrTotal)
  flags.push(`UNDER-MEASURED?: ${ledgerPoints} ledger points for ${pdrTotal} PDRs — some decisions may still be prose-only.`);

let commit = "unknown";
try { commit = execSync("git rev-parse --short HEAD", { cwd: cfg.root }).toString().trim(); } catch {}

console.log("=== META-RETRO (process improvement about process improvement) ===");
console.log(`PDRs: ${pdrTotal} total → ${state.PROPOSED} proposed / ${state.TRIAL} trial / ${state.ADOPTED} adopted / ${state.REJECTED} rejected`);
console.log(`Votes: ${votesGood} good / ${votesNonGood} non-good / ${votesNone} none-yet`);
console.log(`Method size: ${methodSizeLines < 0 ? "n/a (operating-model marker not found/configured)" : methodSizeLines + " lines"}`);
console.log(`Metrics ledger: ${ledgerPoints} points across ${ledgerMetrics} series`);
console.log(flags.length ? "\nFLAGS:\n- " + flags.join("\n- ") : "\nFLAGS: none — mechanism looks healthy this pass.");

if (doRecord) {
  const date = new Date().toISOString().replace(/\.\d+Z$/, "Z");
  let n = existsSync(cfg.metricsLedger) ? readFileSync(cfg.metricsLedger, "utf8").split("\n").filter(Boolean).length + 1 : 1;
  const rows = [
    ["process", "method-size-lines", methodSizeLines, "lines", "operating-model size (bloat proxy)"],
    ["process", "pdr-adopted", state.ADOPTED, "pdrs", "PDRs graduated to ADOPTED"],
    ["process", "pdr-trial", state.TRIAL, "pdrs", "PDRs in TRIAL"],
    ["process", "agent-votes-collected", votesGood + votesNonGood, "votes", "non-empty process_feedback votes tallied"],
  ];
  mkdirSync(dirname(cfg.metricsLedger), { recursive: true });
  for (const [category, metric, val, unit, note] of rows) {
    if (val < 0) continue; // skip unmeasured size
    const id = `m${String(n++).padStart(4, "0")}`;
    appendFileSync(cfg.metricsLedger, JSON.stringify({ id, date, commit, category, metric, value: val, unit, tier: "observed", source: "meta-retro.mjs", note }) + "\n");
    console.log(`recorded ${id}: ${category}/${metric} = ${val} ${unit}`);
  }
}

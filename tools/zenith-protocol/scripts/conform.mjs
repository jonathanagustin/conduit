#!/usr/bin/env node
// conform.mjs — the zenith-protocol CONFORMANCE checker.
//
// The protocol is a set of testable invariants: things that, if violated, mean a repo/agent is NOT
// actually following the operating model (as opposed to merely claiming to). Today conformance is by
// discipline only — this script makes the MECHANICALLY-CHECKABLE half enforceable.
//
// It is modeled on policy-as-code (OPA/Conftest): a fixed set of named rules evaluated over the
// structured ledgers, each emitting deny-style findings. Unlike Conftest it is a plain Node script
// (zero deps, already the plugin's idiom) so it runs at any gate — a Claude Code hook, the worker's
// self-check, the orchestrator's re-verify, or a pre-commit hook. See the Conformance docs for the
// posture recommendation.
//
// Each RULE is a function returning { ok, detail } (or a list). Output is grouped PASS/FAIL/SKIP;
// exit 0 iff every mechanically-checkable rule passes, non-zero on any FAIL. Rules that can't be
// evaluated in this repo (unconfigured branches, untracked ledger, no emitters) SKIP — a skip is not
// a failure, but it is reported so the gap is visible.
//
// What it CANNOT check (needs trust/attestation, not a script): whether the orchestrator truly re-ran
// an agent's gate, whether a vote's objective justification is genuine, whether a change was holistic.
// Those are review/attestation concerns — see the Conformance section of the skill.
//
// Repo-agnostic via .zenith-protocol.json. Usage:
//   node conform.mjs [--config <path-to-config.json>] [--json] [--quiet]
import { existsSync, readFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { relative } from "node:path";
import { fileURLToPath } from "node:url";
import { loadConfig } from "../lib/config.mjs";

const argv = process.argv.slice(2);
const arg = (name) => { const i = argv.indexOf(`--${name}`); return i >= 0 && i + 1 < argv.length ? argv[i + 1] : undefined; };
const asJson = argv.includes("--json");
const quiet = argv.includes("--quiet");
const cfg = loadConfig(process.cwd(), { configPath: arg("config") });

// ---- helpers -------------------------------------------------------------------------------------
const results = []; // { rule, status: PASS|FAIL|SKIP, detail }
const add = (rule, status, detail) => results.push({ rule, status, detail });
const readLines = (p) => readFileSync(p, "utf8").split("\n");
// A markdown table DATA row: starts with `|`, is not the header separator (`|---|`), and (for our
// tables) not the header itself. Cells are the pipe-split interior, trimmed.
const cellsOf = (line) => line.split("|").slice(1, -1).map((c) => c.trim());
const isSeparator = (line) => /^\|[\s:|-]+\|?\s*$/.test(line);
const git = (args, cwd = cfg.root) => execFileSync("git", args, { cwd, stdio: ["ignore", "pipe", "pipe"] }).toString();
const rel = (p) => { try { return relative(cfg.root, p) || p; } catch { return p; } };

// ---- RULE: ledgers-exist -------------------------------------------------------------------------
function ruleLedgersExist() {
  const paths = {
    "capability-map": cfg.capabilityMap, backlog: cfg.backlog, assumptions: cfg.assumptions,
    pdrs: cfg.pdrs, "metrics-doc": cfg.metricsDoc, "metrics-ledger": cfg.metricsLedger,
  };
  const missing = Object.entries(paths).filter(([, p]) => !existsSync(p)).map(([k, p]) => `${k} (${rel(p)})`);
  if (missing.length) add("ledgers-exist", "FAIL", `missing ledger(s): ${missing.join(", ")}`);
  else add("ledgers-exist", "PASS", `all 6 ledger paths exist`);
}

function ruleContextDocsExist() {
  if (!cfg.contextDocs.length) { add("context-docs-exist", "SKIP", "no `contextDocs` configured"); return; }
  const missing = cfg.contextDocs.filter((p) => !existsSync(p)).map(rel);
  if (missing.length) add("context-docs-exist", "FAIL", `missing configured context document(s): ${missing.join(", ")}`);
  else add("context-docs-exist", "PASS", `${cfg.contextDocs.length} configured project/domain context document(s) exist`);
}

// ---- RULE: metrics-jsonl-valid -------------------------------------------------------------------
// Modeled on JSON Schema / ajv row validation: every line is a JSON object carrying the required
// fields from the ledger row schema (id,date,commit,category,metric,value,tier).
const METRIC_REQUIRED = ["id", "date", "commit", "category", "metric", "value", "tier"];
function ruleMetricsJsonlValid() {
  if (!existsSync(cfg.metricsLedger)) { add("metrics-jsonl-valid", "SKIP", "no metrics-ledger.jsonl"); return; }
  const lines = readLines(cfg.metricsLedger);
  const bad = [];
  lines.forEach((l, i) => {
    if (!l.trim()) return; // blank lines ignored
    let o; try { o = JSON.parse(l); } catch (e) { bad.push(`line ${i + 1}: invalid JSON (${e.message})`); return; }
    if (o === null || typeof o !== "object" || Array.isArray(o)) { bad.push(`line ${i + 1}: not a JSON object`); return; }
    const miss = METRIC_REQUIRED.filter((k) => !(k in o));
    if (miss.length) bad.push(`line ${i + 1} (${o.id ?? "?"}): missing field(s) ${miss.join(",")}`);
  });
  if (bad.length) add("metrics-jsonl-valid", "FAIL", `${rel(cfg.metricsLedger)}\n    ` + bad.join("\n    "));
  else add("metrics-jsonl-valid", "PASS", `${lines.filter((l) => l.trim()).length} rows valid, all required fields present`);
}

// ---- RULE: metrics-append-only -------------------------------------------------------------------
// The ledger is an append-only event log (METRICS.md): committed lines are NEVER edited/removed in
// place — a correction is a NEW `supersedes` row. Modeled on the git-history invariant that
// pre-commit/CI append-only guards enforce: inspect `git log -p` and flag any removed committed data
// line that isn't re-added verbatim (the identical-readd case covers a trailing-newline rewrite).
function ruleMetricsAppendOnly() {
  if (!existsSync(cfg.metricsLedger)) { add("metrics-append-only", "SKIP", "no metrics-ledger.jsonl"); return; }
  let tracked = true;
  try { git(["ls-files", "--error-unmatch", cfg.metricsLedger]); } catch { tracked = false; }
  if (!tracked) { add("metrics-append-only", "SKIP", "ledger not tracked in git — no history to check"); return; }
  let patch;
  try { patch = git(["log", "-p", "--format=COMMIT %h", "--", cfg.metricsLedger]); }
  catch (e) { add("metrics-append-only", "SKIP", `git log failed (${e.message})`); return; }
  const violations = [];
  let commit = "", removed = [], added = [];
  const flush = () => {
    const pool = added.slice();
    for (const r of removed) {
      const idx = pool.indexOf(r);
      if (idx >= 0) pool.splice(idx, 1); // identical re-add (e.g. no-newline rewrite) — allowed
      else violations.push(`@ ${commit}: committed line changed/removed → ${r.slice(0, 80)}`);
    }
    removed = []; added = [];
  };
  for (const l of patch.split("\n")) {
    if (l.startsWith("COMMIT ")) { flush(); commit = l.slice(7); continue; }
    if (/^(diff |index |@@|\+\+\+|---)/.test(l)) continue;
    if (l.startsWith("-")) removed.push(l.slice(1));
    else if (l.startsWith("+")) added.push(l.slice(1));
  }
  flush();
  if (violations.length) add("metrics-append-only", "FAIL", `${violations.length} in-place edit(s) of committed metric lines:\n    ` + violations.join("\n    "));
  else add("metrics-append-only", "PASS", "no committed metric line was ever edited in place");
}

// ---- PDR table parsing (shared by three rules) ---------------------------------------------------
const PDR_STATUSES = ["PROPOSED", "TRIAL", "ADOPTED", "REJECTED", "SUPERSEDED"];
function parsePdrRows() {
  if (!existsSync(cfg.pdrs)) return null;
  return readLines(cfg.pdrs)
    .map((line, i) => ({ line, i }))
    .filter((r) => /^\|\s*P\d+\b/.test(r.line))
    .map((r) => ({ n: (r.line.match(/^\|\s*(P\d+)\b/) || [])[1], cells: cellsOf(r.line), lineNo: r.i + 1, raw: r.line }));
}

// ---- RULE: pdr-status-valid (parse + status vocabulary) ------------------------------------------
function rulePdrStatusValid(rows) {
  if (rows === null) { add("pdr-status-valid", "SKIP", "no PROCESS-IMPROVEMENTS.md"); return; }
  if (!rows.length) { add("pdr-status-valid", "SKIP", "no PDR rows found"); return; }
  const bad = [];
  for (const r of rows) {
    if (r.cells.length < 7) { bad.push(`${r.n} (line ${r.lineNo}): row has ${r.cells.length} cells, expected ≥7 (PDR|change|status|exit|measure|holistic|votes)`); continue; }
    const statusCell = r.cells[2];
    const found = PDR_STATUSES.filter((s) => new RegExp(`\\b${s}\\b`).test(statusCell));
    if (!found.length) bad.push(`${r.n} (line ${r.lineNo}): status cell "${statusCell.slice(0, 40)}" has no valid status (${PDR_STATUSES.join("/")})`);
  }
  if (bad.length) add("pdr-status-valid", "FAIL", `${rel(cfg.pdrs)}\n    ` + bad.join("\n    "));
  else add("pdr-status-valid", "PASS", `${rows.length} PDR rows parse; every status ∈ {${PDR_STATUSES.join(",")}}`);
}

// ---- RULE: pdr-adopted-has-measure ---------------------------------------------------------------
// ADR-lifecycle discipline: an ADOPTED decision must carry an objective measure (PROCESS-IMPROVEMENTS
// rule). Flag any ADOPTED PDR whose measure cell (col 4) is empty/placeholder.
const EMPTY_CELL = (c) => { const s = c.replace(/[`*_]/g, "").trim(); return s === "" || /^[—–-]+$/.test(s) || /^(n\/?a|tbd|todo)$/i.test(s); };
function rulePdrAdoptedHasMeasure(rows) {
  if (rows === null) { add("pdr-adopted-has-measure", "SKIP", "no PROCESS-IMPROVEMENTS.md"); return; }
  const bad = [];
  for (const r of rows) {
    if (r.cells.length < 7) continue; // structural problem already flagged by pdr-status-valid
    if (!/\bADOPTED\b/.test(r.cells[2])) continue;
    if (EMPTY_CELL(r.cells[4])) bad.push(`${r.n} (line ${r.lineNo}): ADOPTED but measure cell is empty/placeholder`);
  }
  if (bad.length) add("pdr-adopted-has-measure", "FAIL", `${rel(cfg.pdrs)}\n    ` + bad.join("\n    "));
  else add("pdr-adopted-has-measure", "PASS", `every ADOPTED PDR carries a non-empty measure`);
}

// ---- RULE: pdr-single-vote-tally -----------------------------------------------------------------
// The meta-retro's vote count is only honest if each PDR keeps ONE `good×N` tally in a single cell
// (PROCESS-IMPROVEMENTS rule; scattered tallies repeatedly broke the count). Flag a votes cell (col 6)
// carrying more than one `good×<digit>` (or more than one `bad×<digit>`) token.
function rulePdrSingleVoteTally(rows) {
  if (rows === null) { add("pdr-single-vote-tally", "SKIP", "no PROCESS-IMPROVEMENTS.md"); return; }
  const bad = [];
  const countTok = (cell, kw) => (cell.match(new RegExp(`\\b${kw}\\s*[×x]\\s*\\d+`, "gi")) || []).length;
  for (const r of rows) {
    if (r.cells.length < 7) continue;
    const votes = r.cells[6];
    const g = countTok(votes, "good"), b = countTok(votes, "bad");
    if (g > 1) bad.push(`${r.n} (line ${r.lineNo}): ${g} scattered 'good×N' tallies — consolidate to one`);
    if (b > 1) bad.push(`${r.n} (line ${r.lineNo}): ${b} scattered 'bad×N' tallies — consolidate to one`);
  }
  if (bad.length) add("pdr-single-vote-tally", "FAIL", `${rel(cfg.pdrs)}\n    ` + bad.join("\n    "));
  else add("pdr-single-vote-tally", "PASS", `no PDR row scatters its vote tally`);
}

// ---- RULE: hypothesis-intake-gatekept ------------------------------------------------------------
// Every proposed hypothesis (PH-# row in the backlog intake table) must carry a gatekeeper verdict —
// nothing silently dropped or adopted (operating-model intake rule). Verdict cell (last col) must
// name a decision: ACCEPT / REJECT / NOTE / QUEUED.
const VERDICT_RE = /\b(ACCEPT|REJECT|NOTE|QUEUED)\b/i;
function ruleHypothesisIntakeGatekept() {
  if (!existsSync(cfg.backlog)) { add("hypothesis-intake-gatekept", "SKIP", "no HYPOTHESIS-BACKLOG.md"); return; }
  const rows = readLines(cfg.backlog)
    .map((line, i) => ({ line, i }))
    .filter((r) => /^\|\s*PH-\d+\b/.test(r.line));
  if (!rows.length) { add("hypothesis-intake-gatekept", "SKIP", "no PH-# intake rows"); return; }
  const bad = [];
  for (const r of rows) {
    const cells = cellsOf(r.line);
    const id = (r.line.match(/^\|\s*(PH-\d+)\b/) || [])[1];
    const verdict = cells[cells.length - 1] || "";
    if (!VERDICT_RE.test(verdict)) bad.push(`${id} (line ${r.i + 1}): no gatekeeper verdict (want ACCEPT/REJECT/NOTE/QUEUED) — got "${verdict.slice(0, 40)}"`);
  }
  if (bad.length) add("hypothesis-intake-gatekept", "FAIL", `${rel(cfg.backlog)}\n    ` + bad.join("\n    "));
  else add("hypothesis-intake-gatekept", "PASS", `${rows.length} proposed hypotheses all gatekept`);
}

// ---- RULE: capmap-blocked-names-instrument -------------------------------------------------------
// HEURISTIC. Capability-map rule: a BLOCKED/OPEN negative must name the exact instrument that failed
// (tool/error/path) so the next attempt is cheaper. Flag a table row whose status text says BLOCKED
// or OPEN but references no instrument-like token. Deliberately lenient (false-negatives beat
// false-positives here); reported as a heuristic, not a hard gate.
const INSTRUMENT_RE = /`[^`]+`|\b[\w./-]+\.(mjs|py|js|ts|json|java|yml|yaml|sh)\b|\b[45]\d\d\b|\b(error|probe|gate|permission|jar|endpoint|route|compile|IAM|403|404|405|422|500|timeout|ERR_[A-Z_]+)\b/i;
function ruleCapmapBlockedNamesInstrument() {
  if (!existsSync(cfg.capabilityMap)) { add("capmap-blocked-names-instrument", "SKIP", "no CAPABILITY-MAP.md"); return; }
  const lines = readLines(cfg.capabilityMap);
  const bad = [];
  let checked = 0;
  lines.forEach((line, i) => {
    if (!line.startsWith("|") || isSeparator(line)) return;
    if (!/\b(BLOCKED|OPEN)\b/.test(line)) return;
    if (/claim \/ method|status \+ evidence/i.test(line)) return; // header row
    checked++;
    if (!INSTRUMENT_RE.test(line)) bad.push(`line ${i + 1}: BLOCKED/OPEN row names no instrument (tool/error/path) → ${cellsOf(line)[0]?.slice(0, 50)}`);
  });
  if (bad.length) add("capmap-blocked-names-instrument", "FAIL", `${rel(cfg.capabilityMap)} (heuristic)\n    ` + bad.join("\n    "));
  else add("capmap-blocked-names-instrument", "PASS", `${checked} BLOCKED/OPEN rows each name an instrument (heuristic)`);
}

// ---- RULE: drift-gate ----------------------------------------------------------------------------
// Reuse verify-no-drift.mjs (don't reimplement): generated modules must match a fresh emit.
function ruleDriftGate() {
  const script = fileURLToPath(new URL("./verify-no-drift.mjs", import.meta.url));
  try {
    const out = execFileSync("node", [script], { cwd: cfg.root, stdio: ["ignore", "pipe", "pipe"] }).toString();
    const last = out.trim().split("\n").pop();
    if (/nothing to check|no emitters matched/.test(out)) add("drift-gate", "SKIP", "no emitters configured/found");
    else add("drift-gate", "PASS", last || "no drift");
  } catch (e) {
    const out = `${e.stdout || ""}${e.stderr || ""}`.trim().split("\n").filter(Boolean).slice(-4).join("\n    ");
    add("drift-gate", "FAIL", `verify-no-drift.mjs reported drift/broken emitter:\n    ${out}`);
  }
}

// ---- RULE: push-merge-one-sha (optional) ---------------------------------------------------------
// Push-merge discipline: the configured shared branches sit at one SHA (P7). Optional — SKIP when no
// `branches` are configured.
function rulePushMergeOneSha() {
  const branches = cfg.branches || [];
  if (!branches.length) { add("push-merge-one-sha", "SKIP", "no `branches` configured"); return; }
  const shas = {};
  const unresolved = [];
  for (const b of branches) {
    try { shas[b] = git(["rev-parse", "--short", b]).trim(); }
    catch { unresolved.push(b); }
  }
  const resolved = Object.keys(shas);
  // An unresolvable ref is a config/environment mismatch (e.g. no local `main`), NOT proven
  // divergence — so it SKIPs rather than crying wolf (§0). We only FAIL on ≥2 refs that actually
  // resolve AND diverge.
  if (resolved.length < 2) {
    add("push-merge-one-sha", "SKIP", `only ${resolved.length}/${branches.length} branch(es) resolve locally${unresolved.length ? ` (missing: ${unresolved.join(", ")})` : ""} — need ≥2 to compare`);
    return;
  }
  const uniq = new Set(Object.values(shas));
  if (uniq.size > 1) add("push-merge-one-sha", "FAIL", `resolvable branches diverge: ${JSON.stringify(shas)}${unresolved.length ? ` (unresolved: ${unresolved.join(", ")})` : ""}`);
  else add("push-merge-one-sha", "PASS", `${resolved.join(", ")} all at ${[...uniq][0]}${unresolved.length ? ` (${unresolved.join(", ")} not local — skipped)` : ""}`);
}

// ---- run -----------------------------------------------------------------------------------------
ruleLedgersExist();
ruleContextDocsExist();
ruleMetricsJsonlValid();
ruleMetricsAppendOnly();
const pdrRows = parsePdrRows();
rulePdrStatusValid(pdrRows);
rulePdrAdoptedHasMeasure(pdrRows);
rulePdrSingleVoteTally(pdrRows);
ruleHypothesisIntakeGatekept();
ruleCapmapBlockedNamesInstrument();
ruleDriftGate();
rulePushMergeOneSha();

const fails = results.filter((r) => r.status === "FAIL");
const skips = results.filter((r) => r.status === "SKIP");
const passes = results.filter((r) => r.status === "PASS");

if (asJson) {
  console.log(JSON.stringify({ root: cfg.root, pass: passes.length, fail: fails.length, skip: skips.length, results }, null, 2));
} else if (!quiet) {
  console.log(`=== zenith-protocol conformance @ ${cfg.root} ===`);
  for (const r of results) {
    const tag = r.status === "PASS" ? "PASS" : r.status === "FAIL" ? "FAIL" : "SKIP";
    console.log(`${tag} ${r.rule} — ${r.detail}`);
  }
  console.log(`\n${passes.length} pass · ${fails.length} fail · ${skips.length} skip`);
  if (fails.length) console.log(`\nNON-CONFORMANT: ${fails.map((f) => f.rule).join(", ")}`);
  else console.log(`\nCONFORMANT (mechanically-checkable invariants). Attestation-only invariants are NOT covered — see the skill's Conformance section.`);
}

process.exit(fails.length ? 1 : 0);

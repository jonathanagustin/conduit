#!/usr/bin/env node
// Read the append-only metrics ledger and print a decision-oriented summary: per metric, its latest
// value, the number of recorded points, and the Δ vs the first observation — the "make an informed
// decision from data kept over time" view. Read-only. Repo-agnostic via .zenith-protocol.json.
//
// Usage: node metrics-summary.mjs [--metric <name>] [--pdr P1] [--hyp H1] [--json]
import { readFileSync, existsSync } from "node:fs";
import { loadConfig } from "../lib/config.mjs";

const cfg = loadConfig();
const LEDGER = cfg.metricsLedger;
const arg = (n) => { const i = process.argv.indexOf(`--${n}`); return i >= 0 ? process.argv[i + 1] : undefined; };
const wantJson = process.argv.includes("--json");

if (!existsSync(LEDGER)) { console.log(`metrics-summary: no ledger at ${LEDGER} yet.`); process.exit(0); }
let rows = readFileSync(LEDGER, "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l));

const superseded = new Set(rows.map((r) => r.supersedes).filter(Boolean));
if (arg("metric")) rows = rows.filter((r) => r.metric === arg("metric"));
if (arg("pdr")) rows = rows.filter((r) => r.pdr === arg("pdr"));
if (arg("hyp")) rows = rows.filter((r) => r.hyp === arg("hyp"));

const byMetric = new Map();
for (const r of rows) {
  const k = `${r.category}/${r.metric}`;
  if (!byMetric.has(k)) byMetric.set(k, []);
  byMetric.get(k).push(r);
}

const out = [];
for (const [k, pts] of [...byMetric.entries()].sort()) {
  pts.sort((a, b) => a.date.localeCompare(b.date));
  const live = pts.filter((p) => !superseded.has(p.id));
  const latest = live[live.length - 1] ?? pts[pts.length - 1];
  const first = pts[0];
  const numeric = typeof latest.value === "number" && typeof first.value === "number";
  const delta = numeric && pts.length > 1 ? latest.value - first.value : null;
  out.push({ metric: k, latest: latest.value, unit: latest.unit ?? "", points: pts.length, delta,
    tier: latest.tier, at: latest.commit, date: latest.date, tags: [latest.pdr, latest.hyp].filter(Boolean).join(",") });
}

if (wantJson) { console.log(JSON.stringify(out, null, 2)); process.exit(0); }
if (!out.length) { console.log("metrics-summary: no matching rows."); process.exit(0); }
console.log(`metric${" ".repeat(38)} latest        points  Δ        tier        @commit   tags`);
console.log("-".repeat(112));
for (const o of out) {
  const d = o.delta === null ? "—" : (o.delta > 0 ? `+${o.delta}` : `${o.delta}`);
  console.log(`${o.metric.padEnd(44)} ${String(o.latest + (o.unit ? " " + o.unit : "")).padEnd(13)} ${String(o.points).padEnd(7)} ${d.padEnd(8)} ${String(o.tier).padEnd(11)} ${o.at.padEnd(9)} ${o.tags}`);
}
console.log(`\n${out.length} metric series over ${rows.length} recorded points.`);

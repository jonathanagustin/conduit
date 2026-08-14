#!/usr/bin/env node
// Append a single, traceable measurement to the durable metrics ledger (append-only JSONL time-series).
//
// Every method change and recovery fact in the operating model must be MEASURED and kept DURABLY, not
// left as prose — so a later cycle can ask "is this still earning its cost?" from data. Rows are
// immutable: a correction is a NEW row that supersedes an old one (`--supersedes`), never an in-place
// edit. Each row is traceable to the exact code (git commit) and evidence (`--source`) that produced it.
//
// Repo-agnostic: the ledger path comes from `.zenith-protocol.json` (see lib/config.mjs), so this
// works in any repo. Usage:
//   node record-metric.mjs --category <c> --metric <name> --value <n|string> [--unit u] \
//        [--tier observed|benchmarked|expected] [--pdr P1] [--hyp H1] [--source "..."] \
//        [--supersedes <id>] [--note "..."]
import { appendFileSync, readFileSync, existsSync, mkdirSync } from "node:fs";
import { execSync } from "node:child_process";
import { dirname } from "node:path";
import { loadConfig } from "../lib/config.mjs";

const cfg = loadConfig();
const LEDGER = cfg.metricsLedger;

function arg(name, def) {
  const i = process.argv.indexOf(`--${name}`);
  return i >= 0 && i + 1 < process.argv.length ? process.argv[i + 1] : def;
}

const category = arg("category"), metric = arg("metric"), rawValue = arg("value");
if (!category || !metric || rawValue === undefined) {
  console.error("record-metric: --category, --metric and --value are required.");
  process.exit(2);
}
const value = /^-?\d+(\.\d+)?$/.test(rawValue) ? Number(rawValue) : rawValue;

let commit = "unknown";
try { commit = execSync("git rev-parse --short HEAD", { cwd: cfg.root }).toString().trim(); } catch {}
const date = new Date().toISOString().replace(/\.\d+Z$/, "Z");

let n = 1;
if (existsSync(LEDGER)) n = readFileSync(LEDGER, "utf8").split("\n").filter(Boolean).length + 1;
const id = `m${String(n).padStart(4, "0")}`;

const row = {
  id, date, commit, category, metric, value,
  ...(arg("unit") ? { unit: arg("unit") } : {}),
  tier: arg("tier", "observed"),
  ...(arg("pdr") ? { pdr: arg("pdr") } : {}),
  ...(arg("hyp") ? { hyp: arg("hyp") } : {}),
  ...(arg("source") ? { source: arg("source") } : {}),
  ...(arg("supersedes") ? { supersedes: arg("supersedes") } : {}),
  ...(arg("note") ? { note: arg("note") } : {}),
};

mkdirSync(dirname(LEDGER), { recursive: true });
appendFileSync(LEDGER, JSON.stringify(row) + "\n");
console.log(`recorded ${id}: ${category}/${metric} = ${value}${row.unit ? " " + row.unit : ""} @ ${commit} (${row.tier})`);

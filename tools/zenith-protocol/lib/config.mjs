// Shared config resolution for the zenith-protocol plugin's scripts.
//
// The plugin is repo-agnostic: its scripts run from ANY target repo (cwd = that repo or a subdir),
// so they must NOT hard-code paths. Resolution order:
//   1. `.zenith-protocol.json` at the repo root (walk up from cwd to find it or a `.git` dir).
//   2. Built-in defaults (a `docs/ledgers/` convention) merged under any keys the config omits.
// All returned paths are absolute (resolved against the repo root), so callers never touch cwd again.
import { existsSync, readFileSync } from "node:fs";
import { resolve, dirname } from "node:path";

export function findRepoRoot(start = process.cwd()) {
  let dir = resolve(start);
  for (;;) {
    if (existsSync(resolve(dir, ".zenith-protocol.json")) || existsSync(resolve(dir, ".git"))) return dir;
    const parent = dirname(dir);
    if (parent === dir) return resolve(start); // no marker found — fall back to the start dir
    dir = parent;
  }
}

const DEFAULTS = {
  ledgerDir: "docs/ledgers",
  capabilityMap: "docs/ledgers/CAPABILITY-MAP.md",
  backlog: "docs/ledgers/HYPOTHESIS-BACKLOG.md",
  assumptions: "docs/ledgers/ASSUMPTIONS-LEDGER.md",
  pdrs: "docs/ledgers/PROCESS-IMPROVEMENTS.md",
  metricsDoc: "docs/ledgers/METRICS.md",
  metricsLedger: "docs/ledgers/metrics-ledger.jsonl",
  // The doc + marker pair the meta-retro measures as a "method size / bloat" proxy. Point this at
  // wherever the operating model lives (AGENTS.md §0, a SKILL.md, etc.). If startMarker is absent the
  // bloat proxy is skipped rather than measuring the wrong span.
  operatingModelDoc: "AGENTS.md",
  operatingModelStartMarker: "THE DEFAULT OPERATING MODEL",
  operatingModelEndMarker: "",
  // verify-no-drift inputs: where idempotent emitters live + which trees hold committed generated files.
  emittersDir: "scripts",
  emitterPattern: "^emit-.*\\.mjs$",
  generatedTrees: [],
  // Project/domain/situation overlays that specialize the portable method.
  contextDocs: [],
  // Optional push-merge conformance: branch refs that must sit at one SHA. Empty = check skipped.
  branches: [],
};

// `opts.configPath` overrides where the config JSON is read from (used by conform.mjs to point at a
// target repo's ledgers without writing a `.zenith-protocol.json` into that repo's root). Relative
// ledger paths in that config still resolve against the discovered repo root.
export function loadConfig(start = process.cwd(), opts = {}) {
  const root = findRepoRoot(start);
  const cfgPath = opts.configPath ? resolve(opts.configPath) : resolve(root, ".zenith-protocol.json");
  let cfg = { ...DEFAULTS };
  if (existsSync(cfgPath)) {
    try { cfg = { ...cfg, ...JSON.parse(readFileSync(cfgPath, "utf8")) }; }
    catch (e) { console.error(`zenith-protocol: bad .zenith-protocol.json — using defaults (${e.message})`); }
  }
  const abs = (p) => resolve(root, p);
  return {
    root,
    ledgerDir: abs(cfg.ledgerDir),
    capabilityMap: abs(cfg.capabilityMap),
    backlog: abs(cfg.backlog),
    assumptions: abs(cfg.assumptions),
    pdrs: abs(cfg.pdrs),
    metricsDoc: abs(cfg.metricsDoc),
    metricsLedger: abs(cfg.metricsLedger),
    operatingModelDoc: abs(cfg.operatingModelDoc),
    operatingModelStartMarker: cfg.operatingModelStartMarker,
    operatingModelEndMarker: cfg.operatingModelEndMarker,
    emittersDir: abs(cfg.emittersDir),
    emitterPattern: cfg.emitterPattern,
    generatedTrees: (cfg.generatedTrees || []).map(abs),
    contextDocs: (cfg.contextDocs || []).map(abs),
    branches: cfg.branches || [],
    raw: cfg,
  };
}

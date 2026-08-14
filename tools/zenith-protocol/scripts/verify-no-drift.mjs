#!/usr/bin/env node
// Drift gate for every generated module: re-run each idempotent emitter and fail if any COMMITTED
// generated file changed. A generated module is only trustworthy if a fresh emit reproduces the
// committed bytes; otherwise "generated from evidence X" silently drifts from X. Parameterized
// emitters (that need args) are skipped, not failed. Repo-agnostic via .zenith-protocol.json:
// set `emittersDir`, `emitterPattern`, and `generatedTrees`.
//
// Run: node verify-no-drift.mjs   (exit 0 = no drift, 1 = drift or a genuinely broken emitter)
import { readdirSync, existsSync } from "node:fs";
import { execSync } from "node:child_process";
import { resolve } from "node:path";
import { loadConfig } from "../lib/config.mjs";

const cfg = loadConfig();
if (!existsSync(cfg.emittersDir)) { console.log(`verify-no-drift: emitters dir ${cfg.emittersDir} not found — nothing to check.`); process.exit(0); }
const re = new RegExp(cfg.emitterPattern);
const emitters = readdirSync(cfg.emittersDir).filter((f) => re.test(f)).sort();
if (!emitters.length) { console.log("verify-no-drift: no emitters matched — nothing to check."); process.exit(0); }

console.log(`Re-running ${emitters.length} emitter(s) in ${cfg.emittersDir}:`);
let ran = 0, skipped = 0, broken = 0;
for (const e of emitters) {
  try {
    execSync(`node ${JSON.stringify(resolve(cfg.emittersDir, e))}`, { cwd: cfg.root, stdio: "pipe" });
    console.log(`  ok   ${e}`); ran++;
  } catch (err) {
    const out = `${err.stdout || ""}${err.stderr || ""}`;
    if (/usage:|are required|required\b.*--|\b--\w+ (is|are) required/i.test(out)) {
      console.log(`  skip ${e} — parameterized emitter (needs args)`); skipped++;
    } else { console.log(`  FAIL ${e} — emitter errored (exit ${err.status ?? "?"})`); broken++; }
  }
}

const trees = (cfg.generatedTrees || []).filter((t) => existsSync(t));
let dirty = "";
try { dirty = execSync(`git status --porcelain -- ${trees.map((t) => JSON.stringify(t)).join(" ")}`, { cwd: cfg.root }).toString(); } catch {}
const changed = dirty.split("\n").filter((l) => l && !l.startsWith("??")).map((l) => l.slice(3));

if (changed.length) {
  console.log(`\nDRIFT: ${changed.length} committed generated file(s) changed after a fresh emit:`);
  for (const c of changed) console.log(`  ~ ${c}`);
  console.log(`\nEither the evidence changed (update provenance) or an emitter is non-idempotent.`);
  process.exit(1);
}
console.log(`\nno drift: ${ran} emitters ran clean, all committed generated files match a fresh emit` +
  `${skipped ? ` (${skipped} parameterized, skipped)` : ""}${broken ? ` — but ${broken} emitter(s) BROKE, see FAIL above` : ""}.`);
process.exit(broken ? 1 : 0);

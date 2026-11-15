#!/usr/bin/env node
// The offline half of the mutation-testing gate.
//
// A real mutation sweep needs to compile and run the suite once per mutant, so
// it is slow and belongs in a scheduled job (scripts/mutation-sweep.sh, run by
// .github/workflows/mutation-sweep.yml). This runs on every candidate, reaches
// nothing, and fails when a committed baseline is malformed, internally
// inconsistent, points at code that no longer exists, or has gone stale enough
// that "no regression" has stopped meaning anything.
//
// It cannot re-measure efficacy. What it can do is stop the recorded number from
// rotting silently -- a baseline hand-edited upward, or left behind while the
// code moved, reads as coverage it no longer has.
//
// Two engines feed one baseline. `packages` holds the Go bounded-context
// domains, measured by gremlins; each key is a package directory. `web` holds
// the console files measured by Stryker; each key is a source file. Both record
// efficacy = killed / (killed + lived) -- for the web that is the Stryker score
// over covered code, with a survived mutant playing the LIVED role.
import { equivalentReview, quality } from "./mutation-report-score.mjs";
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(HERE, "..");
// The baseline path is overridable so the planted-violation harness
// (verify-mutation-scores.test.sh) can point the gate at a malformed copy
// without touching the committed baseline. Entry paths still resolve against
// ROOT -- a baseline names real repository code wherever it is read from.
const SCORES = process.env.CONDUIT_MUTATION_SCORES || resolve(ROOT, "docs/operations/mutation-scores.json");
const MAX_AGE_DAYS = 90;
const DAY = 24 * 60 * 60 * 1000;

const failures = [];

if (!existsSync(SCORES)) {
  console.error(`FAIL mutation scores: ${SCORES} is missing`);
  process.exit(1);
}

let record;
try {
  record = JSON.parse(readFileSync(SCORES, "utf8"));
} catch (error) {
  console.error(`FAIL mutation scores: not valid JSON -- ${error.message}`);
  process.exit(1);
}

// A Go entry must still be a package directory holding Go source, or the
// baseline is measuring a package that has moved or been deleted.
const isGoPackage = (path) =>
  existsSync(path) && statSync(path).isDirectory() && readdirSync(path).some((file) => file.endsWith(".go"));
// A web entry must still be the source file Stryker mutated.
const isSourceFile = (path) => existsSync(path) && statSync(path).isFile();

// Validate the numeric shape and internal consistency of one entry. `exists`
// decides what "still on disk" means for the section it came from.
const checkEntry = (name, score, exists) => {
  const directory = resolve(ROOT, name);
  if (!exists(directory)) {
    failures.push(`${name} is recorded but is not on disk where the baseline says`);
    return;
  }
  for (const field of ["efficacy", "killed", "lived", "total", "coverage"]) {
    if (typeof score[field] !== "number" || !Number.isFinite(score[field])) {
      failures.push(`${name}.${field} is not a number`);
    }
  }
  if (failures.some((message) => message.startsWith(name))) return;

  // Efficacy must be exactly what killed and lived say it is. A hand-edited
  // efficacy that does not follow from the counts is the tampering this catches.
  const denominator = score.killed + score.lived;
  const derived = denominator === 0 ? 100 : (100 * score.killed) / denominator;
  if (Math.abs(derived - score.efficacy) > 0.01) {
    failures.push(
      `${name} efficacy ${score.efficacy} does not follow from ${score.killed} killed and ${score.lived} lived (should be ${derived.toFixed(2)})`,
    );
  }
  if (score.total < score.killed + score.lived) {
    failures.push(`${name} total ${score.total} is less than killed+lived ${score.killed + score.lived}`);
  }
  if (score.lived < 0 || score.killed < 0) {
    failures.push(`${name} has a negative mutant count`);
  }
  try { quality(score, equivalentReview(ROOT, name)); }
  catch (error) { failures.push(`${name}: ${error.message}`); }

};

const packages = record.packages ?? {};
const web = record.web ?? {};
const goNames = Object.keys(packages);
const webNames = Object.keys(web);
if (goNames.length === 0) {
  failures.push("no Go packages are recorded; a baseline of nothing ratchets nothing");
}
for (const [name, score] of Object.entries(packages)) checkEntry(name, score, isGoPackage);
for (const [name, score] of Object.entries(web)) checkEntry(name, score, isSourceFile);

const today = process.env.CONDUIT_MUTATION_TODAY || new Date().toISOString().slice(0, 10);
const capturedMillis = Date.parse(record.capturedAt);
if (!/^\d{4}-\d{2}-\d{2}$/.test(record.capturedAt ?? "") || !Number.isFinite(capturedMillis)) {
  failures.push(`capturedAt ${JSON.stringify(record.capturedAt)} is not a real YYYY-MM-DD date`);
} else {
  const age = Math.floor((Date.parse(today) - capturedMillis) / DAY);
  if (age < 0) {
    failures.push(`capturedAt ${record.capturedAt} is in the future`);
  } else if (age > MAX_AGE_DAYS) {
    failures.push(
      `the baseline was captured ${age} days ago, over the ${MAX_AGE_DAYS}-day maximum; ` +
        `re-sweep with scripts/mutation-sweep.sh --write`,
    );
  }
}

if (failures.length) {
  for (const failure of failures) console.error(`FAIL mutation scores: ${failure}`);
  process.exit(1);
}

const entries = [...Object.entries(packages), ...Object.entries(web)];
const worst = entries.reduce((low, [name, score]) =>
  score.efficacy < low.efficacy ? { name, efficacy: score.efficacy } : low, { name: "", efficacy: 100 });
console.log(
  `PASS mutation scores: ${goNames.length} Go package(s) and ${webNames.length} web file(s) ` +
    `baselined ${record.capturedAt}, weakest ${worst.name} at ${worst.efficacy.toFixed(1)}% raw efficacy`,
);

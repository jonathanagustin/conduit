#!/usr/bin/env node
// Fails when the committed public-documentation corpus has gone stale, or when
// the manifest no longer matches the capture that produced it.
//
// This is the offline half of the drift problem, and it exists because the
// online half cannot be scheduled. `verify-documentation-manifest.mjs` re-crawls
// palantir.com and is deliberately not wired into the ordinary build -- it is
// flaky and rude to run on every commit -- so the plan was to give it a
// scheduled job. A4 forbids that: repository-authored workflows run only after a
// person dispatches them, and `docs/operations/manual-ci.md` names `schedule`
// explicitly. So nothing ran it, which is how the corpus aged two weeks while
// Apollo added a fifth Environment connection setting to a contract this
// repository implements with four.
//
// The fix that does not need the decision overturned: stop trying to detect
// drift on a timer, and make the AGE itself fail a gate that already runs. This
// reaches no network, so it runs everywhere `verify-local.sh` does. It cannot
// tell you the corpus has drifted -- only that nobody has checked recently
// enough for "it has not drifted" to still mean anything.
//
//   node scripts/verify-documentation-freshness.mjs
//   CONDUIT_DOCUMENTATION_TODAY=2026-09-30 node scripts/verify-documentation-freshness.mjs
//
// The environment variable exists so this file's own test can reach the failing
// branch without waiting 45 days for it.
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(HERE, "..");
const CAPTURE = resolve(ROOT, "docs/apollo/public-documentation-capture.json");
const MANIFEST = resolve(ROOT, "docs/apollo/public-documentation-manifest.tsv");

const DAY = 24 * 60 * 60 * 1000;
const failures = [];

const capture = JSON.parse(readFileSync(CAPTURE, "utf8"));
for (const field of ["capturedAt", "pageCount", "maximumAgeDays"]) {
  if (capture[field] === undefined) failures.push(`capture record has no ${field}`);
}

// The manifest and the capture record must agree about size. They are separate
// files, so a hand-edited manifest -- adding a page to make a citation resolve,
// which is exactly the temptation this whole area guards against -- would
// otherwise leave no trace. Editing both is still possible, but it is no longer
// something you can do without noticing that you did it.
const rows = readFileSync(MANIFEST, "utf8").trimEnd().split("\n").length - 1;
if (rows !== capture.pageCount) {
  failures.push(
    `the manifest holds ${rows} page(s) but the capture record says ${capture.pageCount}; ` +
      `one of the two was edited without the other`,
  );
}

const today = process.env.CONDUIT_DOCUMENTATION_TODAY || new Date().toISOString().slice(0, 10);
if (!/^\d{4}-\d{2}-\d{2}$/.test(capture.capturedAt)) {
  failures.push(`capturedAt ${JSON.stringify(capture.capturedAt)} is not a YYYY-MM-DD date`);
} else {
  const age = Math.floor((Date.parse(today) - Date.parse(capture.capturedAt)) / DAY);
  if (age < 0) {
    failures.push(`capturedAt ${capture.capturedAt} is in the future`);
  } else if (age > capture.maximumAgeDays) {
    failures.push(
      `the corpus was captured ${age} days ago, over the ${capture.maximumAgeDays}-day maximum. ` +
        `Every documentedBy citation is being checked against an index nobody has verified since then. ` +
        `Re-crawl with: ${capture.refreshWith}`,
    );
  }
}

if (failures.length) {
  for (const failure of failures) console.error(`FAIL documentation freshness: ${failure}`);
  process.exit(1);
}
console.log(
  `PASS documentation freshness: ${capture.pageCount} page(s) captured ${capture.capturedAt}, ` +
    `within the ${capture.maximumAgeDays}-day maximum`,
);

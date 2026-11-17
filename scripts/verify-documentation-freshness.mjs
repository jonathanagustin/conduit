#!/usr/bin/env node
// Fails when the committed public-documentation corpus has gone stale, or when
// the manifest no longer matches the capture that produced it.
//
// This is the offline half of the drift problem. ADR 0006 permits the bounded
// weekly report-only crawl; it is deliberately not run on every commit. This
// independent age check reaches no network and cannot substitute for that crawl.
// It makes the AGE itself fail a gate that already runs. This
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
const CAPTURE = resolve(ROOT, "docs/reference-platform/public-documentation-capture.json");
const MANIFEST = resolve(ROOT, "docs/reference-platform/public-documentation-manifest.tsv");

const DAY = 24 * 60 * 60 * 1000;
const failures = [];

const capture = JSON.parse(readFileSync(CAPTURE, "utf8"));
for (const field of ["capturedAt", "pageCount", "maximumAgeDays"]) {
  if (capture[field] === undefined) failures.push(`capture record has no ${field}`);
}
// Types are checked, not assumed. A string where a number belongs makes every
// later comparison NaN, and NaN loses every comparison silently -- so
// `maximumAgeDays: "forever"` would not error, it would simply never be
// exceeded. That is the specific way a freshness gate becomes decorative.
for (const field of ["pageCount", "maximumAgeDays"]) {
  if (capture[field] !== undefined && !Number.isFinite(capture[field])) {
    failures.push(`${field} must be a number, not ${JSON.stringify(capture[field])}`);
  }
}

// The manifest and the capture record must agree about size. They are separate
// files, so a hand-edited manifest -- adding a page to make a citation resolve,
// which is exactly the temptation this whole area guards against -- would
// otherwise leave no trace. Editing both is still possible, but it is no longer
// something you can do without noticing that you did it.
// Counted as rows, not as lines. Counting lines meant a blank line, or a
// truncated one, still counted as a page -- so a manifest could be edited to
// insert an invented URL, or to blank a real one, without the count moving.
// That matters because verify-apollo-interface.mjs resolves every documentedBy
// citation by asking whether the URL is in this file: a hand-added row makes a
// citation "resolve" against a page nobody crawled.
const rows = readFileSync(MANIFEST, "utf8")
  .trimEnd()
  .split("\n")
  .slice(1)
  .filter((line) => {
    const fields = line.split("\t");
    return fields.length >= 6 && /^https:\/\/\S+$/.test(fields[0]) && fields[5].trim() !== "";
  }).length;
if (rows !== capture.pageCount) {
  failures.push(
    `the manifest holds ${rows} page(s) but the capture record says ${capture.pageCount}; ` +
      `one of the two was edited without the other`,
  );
}

const today = process.env.CONDUIT_DOCUMENTATION_TODAY || new Date().toISOString().slice(0, 10);
// Shape and validity are different questions and both have to be asked. The
// regex accepts 2026-13-40, which Date.parse then reads as NaN -- and every
// comparison against NaN is false, so an impossible date would be neither
// stale nor in the future. It would pass.
const capturedMillis = Date.parse(capture.capturedAt);
const todayMillis = Date.parse(today);
if (!/^\d{4}-\d{2}-\d{2}$/.test(capture.capturedAt) || !Number.isFinite(capturedMillis)) {
  failures.push(`capturedAt ${JSON.stringify(capture.capturedAt)} is not a real YYYY-MM-DD date`);
} else if (!Number.isFinite(todayMillis)) {
  failures.push(`CONDUIT_DOCUMENTATION_TODAY ${JSON.stringify(today)} is not a real YYYY-MM-DD date`);
} else if (!Number.isFinite(capture.maximumAgeDays)) {
  // Already reported above; skip the comparison rather than run it against NaN.
} else {
  const age = Math.floor((todayMillis - capturedMillis) / DAY);
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

#!/usr/bin/env node
// Refuses a committed tenant origin.
//
// The rule this enforces is already written down in several places: commit
// scrubbed metadata, source hashes and counts; never commit tenant origins,
// tokens, cookies, authenticated documents or response bodies. It was still
// broken. `docs/evidence/apollo-authorization-graph.json` carried
// `source.host: "https://<tenant>.usw-23.palantirfoundry.com"` -- the live
// tenant, verbatim -- from the commit that extracted this repository onto its
// trunk until 2026-08-15.
//
// It survived because nothing looked. Every other rule in this repository that
// matters is enforced by a gate; this one was enforced by everyone remembering,
// and one capture script that wrote its source metadata the obvious way was
// enough to break it. The repository already had the right idiom to hand -- a
// salted digest of the origin, as `hubFingerprint` -- so the fix was to use it,
// and this exists so the next capture script cannot skip it.
//
// What is allowed: an origin whose host contains `example`, because the
// documentation and the planted-violation harnesses need to show the shape of
// the thing being refused. A gate that could not spell out what it rejects
// would be untestable.
import { readFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");

// Hosts under the tenant domains this repository actually touches. Deliberately
// not a general secret scanner: a narrow rule that always runs is worth more
// than a broad one that gets muted.
const ORIGIN = /\b([a-z0-9][a-z0-9-]*(?:\.[a-z0-9][a-z0-9-]*)*)\.(palantirfoundry\.com|palantircloud\.com)\b/gi;

// Only what git tracks. An untracked scratch file is not a disclosure, and
// scanning them would make the gate fail on a working tree rather than on the
// repository.
const tracked = execFileSync("git", ["ls-files", "-z"], { cwd: ROOT, encoding: "utf8" })
  .split("\0")
  .filter(Boolean);

const findings = [];
for (const file of tracked) {
  let contents;
  try {
    contents = readFileSync(resolve(ROOT, file), "utf8");
  } catch {
    continue; // binary, or removed between listing and reading
  }
  if (!contents.includes("palantirfoundry") && !contents.includes("palantircloud")) continue;
  for (const match of contents.matchAll(ORIGIN)) {
    const host = match[0];
    if (/example/i.test(host)) continue;
    const line = contents.slice(0, match.index).split("\n").length;
    findings.push({ file, line, host });
  }
}

if (findings.length) {
  for (const { file, line, host } of findings) {
    // The host is NOT echoed. Printing it to make the failure convenient would
    // put the origin in a CI log, which is one of the places the rule names.
    const redacted = host.replace(/^[^.]+/, "<host>");
    console.error(
      `FAIL ${file}:${line} contains a tenant origin (${redacted}). Record a salted digest instead -- ` +
        "see `hubFingerprint` in scripts/apollo-graphql/probe-apollo-schema.mjs -- or use an example host.",
    );
  }
  console.error(
    `\n${findings.length} committed tenant origin(s). Scrubbed metadata, source hashes and counts are ` +
      "committable; origins are not.",
  );
  process.exit(1);
}
console.log(`PASS no tenant origins: ${tracked.length} tracked file(s) carry none`);

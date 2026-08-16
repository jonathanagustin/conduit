#!/usr/bin/env node
// Guards the committed Rosetta release-note registry.
//
// The registry exists because the tenant's Rosetta APOLLO corpus is a larger
// record than the public What's New page, so it is worth keeping. It is
// committed as facts only -- identity, dates, versions, digests -- because the
// notes themselves are Palantir's prose and the tenant origin is not ours to
// publish.
//
// "Facts only" is easy to state and easy to erode: the next person to extend
// the capture reaches for `title` because it makes the artifact readable, and
// nothing stops them. So the invariant is enforced here rather than remembered.
// This runs offline against the committed file and needs no credentials.
import { existsSync, readFileSync } from "node:fs";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(HERE, "..");
const REGISTRY = resolve(ROOT, "docs/apollo/observed/apollo-rosetta-release-notes.json");

const failures = [];
const fail = (message) => failures.push(message);

if (!existsSync(REGISTRY)) {
  console.error(`FAIL missing ${relative(ROOT, REGISTRY)}`);
  process.exit(1);
}
const registry = JSON.parse(readFileSync(REGISTRY, "utf8"));
const rows = registry.rows ?? [];

// 1. No prose, ever. These are the three fields Rosetta returns that carry
// Palantir's wording, and the whole disclosure rule is that none of them lands
// here.
const prose = ["title", "description", "longDescription", "body", "text"];
for (const row of rows) {
  for (const field of prose) {
    if (field in row) {
      fail(`row ${row.id} carries "${field}"; release-note prose is not copied into this repository`);
      break;
    }
  }
}

// 2. No tenant origin. The artifact records WHICH tenant answered as a salted
// digest and must not record the tenant. A hostname is the one thing most
// likely to be added back by someone making the file "easier to trace".
const serialized = JSON.stringify(registry);
for (const pattern of [/palantirfoundry\.com/i, /https?:\/\//]) {
  if (pattern.test(serialized)) fail(`the registry contains a tenant origin (${pattern}); only a salted digest belongs here`);
}
if (!/^[0-9a-f]{64}$/.test(registry.originFingerprint ?? "")) {
  fail("originFingerprint is not a SHA-256 digest, so the artifact does not record which tenant answered");
}

// 3. The summary must describe the rows, not an earlier capture. A count that
// drifts from its own rows is how a stale artifact passes as a current one.
if (registry.noteCount !== rows.length) {
  fail(`noteCount is ${registry.noteCount}; the file carries ${rows.length} row(s)`);
}
const byType = {};
for (const row of rows) if (row.type) byType[row.type] = (byType[row.type] ?? 0) + 1;
for (const [type, total] of Object.entries(registry.byType ?? {})) {
  if (byType[type] !== total) fail(`byType.${type} is ${total}; the rows carry ${byType[type] ?? 0}`);
}
const versions = new Set(rows.map((row) => row.releaseVersion).filter(Boolean));
if (registry.distinctReleaseVersions !== versions.size) {
  fail(`distinctReleaseVersions is ${registry.distinctReleaseVersions}; the rows carry ${versions.size}`);
}

// 4. Stable order, so a re-capture diffs as what changed rather than as a
// reshuffle. Sorted by date then id, which is what the capture writes.
for (let index = 1; index < rows.length; index += 1) {
  const [previous, current] = [rows[index - 1], rows[index]];
  const ordered = previous.date === current.date ? previous.id <= current.id : previous.date <= current.date;
  if (!ordered) {
    fail(`rows are not sorted by (date, id) at index ${index}; a re-capture would diff as a reshuffle`);
    break;
  }
}

// 5. Identity is what makes a row a row. A note with no id or no digest cannot
// be compared against a later capture, which is the only thing this file is for.
for (const row of rows) {
  if (!/^[0-9a-f]{64}$/.test(row.sha256 ?? "")) fail(`row ${row.id ?? "(no id)"} has no record digest`);
  if (!row.id) fail("a row has no id");
  if (!row.date) fail(`row ${row.id} has no date`);
}

if (failures.length) {
  for (const failure of failures) console.error(`FAIL ${failure}`);
  process.exit(1);
}
console.log(
  `PASS Rosetta release notes: ${rows.length} APOLLO note(s), ${versions.size} distinct version(s), ` +
    `no prose and no tenant origin`,
);

#!/usr/bin/env node
// Fails when docs/apollo/reference/review-ledger.tsv stops being re-checkable.
//
// The ledger is the source of identity for the Apollo reference notes: ten of
// them open with some form of "Sources are indexed in `review-ledger.tsv`", and
// each row records the exact URL, the review date, and the SHA-256 of the
// content that was read. That is a strong record -- a spot check of
// `tenablesc/agentscan.go` at its pinned commit reproduced the recorded hash
// byte for byte -- and nothing verified any of it. A file that records
// verifiable facts and is never verified is the "validation surface that never
// executes" pattern this repository has now hit three times.
//
// What this gate can and cannot do is decided by the network, exactly as it was
// for documentation freshness. The recorded hashes are of THIRD-PARTY content,
// so confirming them means fetching the sources, which needs a network and a
// person. That check stays manual. What is checkable offline is whether each
// row is still capable of being re-checked at all, and that is what runs here:
//
//   - the header is the five columns the readers assume;
//   - every row has five fields, an ISO review date, and a 64-hex lowercase
//     digest, so a malformed row cannot masquerade as evidence;
//   - every `topic` resolves to a file that exists, so renaming or deleting a
//     note cannot silently orphan the evidence that backs it;
//   - every URL is absolute https;
//   - no (url, topic) pair appears twice, since the same source reviewed twice
//     for one note is a duplicate rather than two findings;
//   - a GitHub blob/tree URL carries a FULL 40-character commit sha.
//
// The last rule is the one with a story. A short sha is not a re-checkable
// coordinate: `git fetch origin 35e868a` fails outright, and only the full
// forty characters resolve. A ledger row exists precisely so someone can go
// back to the exact bytes later, so a row that cannot be fetched is a row that
// has already lost the thing it was written to preserve. Every committed row
// already satisfies this; the gate keeps it that way.
//
// Note that the ledger being intact says nothing about whether a note's PROSE
// stays within what its row covers. That failure is real -- `security-scanning.md`
// characterised a whole client from a row scoped to one file -- but it is
// semantic, and no mechanical check catches it. Read the disposition before
// trusting a summary.
import { existsSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const LEDGER = process.argv[2] ?? "docs/apollo/reference/review-ledger.tsv";
const LEDGER_PATH = resolve(ROOT, LEDGER);
const COLUMNS = ["url", "reviewed_at_utc", "snapshot_sha256", "topic", "disposition"];

const failures = [];
const fail = (line, message) => failures.push(`${LEDGER}:${line}: ${message}`);

if (!existsSync(LEDGER_PATH)) {
  console.error(`FAIL review ledger: ${LEDGER} does not exist; the reference notes cite it as their source of identity`);
  process.exit(1);
}

// A trailing newline is normal; anything else empty in the middle is not.
const lines = readFileSync(LEDGER_PATH, "utf8").split("\n");
while (lines.length > 0 && lines[lines.length - 1] === "") lines.pop();

if (lines.length === 0) {
  console.error(`FAIL review ledger: ${LEDGER} is empty`);
  process.exit(1);
}

const header = lines[0].split("\t");
if (header.length !== COLUMNS.length || header.some((name, index) => name !== COLUMNS[index])) {
  console.error(`FAIL review ledger: header is [${header.join(", ")}], expected [${COLUMNS.join(", ")}]`);
  process.exit(1);
}

// The topic is written relative to the ledger's own directory, so `../` reaches
// out of reference/ the way row 64 does for IMAGE-DERIVED-GAPS.md.
const ledgerDir = dirname(LEDGER_PATH);
const seen = new Map();

for (let index = 1; index < lines.length; index += 1) {
  const line = lines[index];
  const number = index + 1;
  if (line.trim() === "") {
    fail(number, "blank row");
    continue;
  }
  const fields = line.split("\t");
  if (fields.length !== COLUMNS.length) {
    fail(number, `has ${fields.length} fields, expected ${COLUMNS.length}`);
    continue;
  }
  const [url, reviewedAt, digest, topic, disposition] = fields;

  if (!/^https:\/\/\S+$/.test(url)) {
    fail(number, `url is not an absolute https URL: ${JSON.stringify(url)}`);
  }
  if (!/^\d{4}-\d{2}-\d{2}$/.test(reviewedAt)) {
    fail(number, `reviewed_at_utc is not YYYY-MM-DD: ${JSON.stringify(reviewedAt)}`);
  }
  if (!/^[0-9a-f]{64}$/.test(digest)) {
    fail(number, `snapshot_sha256 is not 64 lowercase hex characters: ${JSON.stringify(digest)}`);
  }
  if (topic === "") {
    fail(number, "topic is empty; the row names no note it supports");
  } else if (!existsSync(resolve(ledgerDir, topic))) {
    fail(number, `topic ${JSON.stringify(topic)} does not resolve to a file; the evidence is orphaned`);
  }
  if (disposition === "") {
    fail(number, "disposition is empty; a row must say what the review concluded");
  }

  // A blob or tree reference is only re-fetchable at a full sha.
  const gitHubRef = /github\.com\/[^/]+\/[^/]+\/(?:blob|tree)\/([^/]+)/.exec(url);
  if (gitHubRef !== null && !/^[0-9a-f]{40}$/.test(gitHubRef[1])) {
    fail(
      number,
      `GitHub reference is pinned to ${JSON.stringify(gitHubRef[1])} rather than a full 40-character commit sha; ` +
        "a short sha cannot be fetched, so the row cannot be re-checked",
    );
  }

  const key = `${url}\t${topic}`;
  if (seen.has(key)) {
    fail(number, `duplicates the (url, topic) pair first recorded on line ${seen.get(key)}`);
  } else {
    seen.set(key, number);
  }
}

if (failures.length > 0) {
  for (const failure of failures) console.error(`FAIL review ledger: ${failure}`);
  process.exit(1);
}
console.log(`PASS review ledger: ${lines.length - 1} rows are well formed and re-checkable`);

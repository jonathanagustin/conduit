#!/usr/bin/env node
// Keeps docs/reference-platform/reference/review-ledger.tsv well formed and its GitHub rows
// pinned.
//
// The ledger is the source of identity for the Apollo reference notes -- 19 of
// them open with some form of "Sources are indexed in `review-ledger.tsv`" --
// and it records, per row, a URL, a review date, the SHA-256 of the content
// that was read, the note it supports, and what the review concluded. Nothing
// checked any of it, and AGENTS.md instructs agents to append to it, so format
// rot in a machine-appended TSV is the base case rather than a hypothetical.
//
// BE PRECISE ABOUT WHAT THIS DOES, because an earlier revision of this comment
// was not. It does NOT establish that a row can be re-checked. Of the 81
// committed rows, 5 are `github.com` blob/tree URLs carrying a full commit sha
// -- genuinely re-fetchable coordinates -- and the other 76 are unversioned
// `palantir.com` and `helm.sh` pages that serve current content. A hash
// recorded against an unversioned URL stopped being reproducible the moment
// upstream edited the page, and no offline gate can change that. Confirming any
// recorded hash needs a network and a person, and stays manual.
//
// What this actually enforces:
//
//   - the header is the five columns the readers assume, and the file has at
//     least one row, so the guarded evidence cannot be emptied silently;
//   - every row has five fields, a real calendar date, and a 64-hex lowercase
//     digest, so a malformed row cannot masquerade as evidence;
//   - every `topic` resolves to a file that exists, so renaming or deleting a
//     note cannot silently orphan the rows that back it -- the rule with the
//     most real consumers, given how many notes name this file;
//   - every URL is absolute https and no (url, topic) pair repeats;
//   - a github.com or raw.githubusercontent.com reference is pinned to a full
//     40-character commit sha. A short sha is not a coordinate: `git fetch
//     origin 35e868a` fails outright and only the full forty resolve, so a row
//     pinned that way has already lost what it was written to preserve.
//
// What it cannot do: notice a fabricated digest, a backdated review, or a note
// whose prose generalises past what its row covers. That last one is a real
// failure -- security-scanning.md once characterised a whole client from a row
// scoped to one file -- and it is a reading problem, not a checkable one.
import { existsSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const LEDGER = process.argv[2] ?? "docs/reference-platform/reference/review-ledger.tsv";
const LEDGER_PATH = resolve(ROOT, LEDGER);
const COLUMNS = ["url", "reviewed_at_utc", "snapshot_sha256", "topic", "disposition"];

const failures = [];
const fail = (line, message) => failures.push(`${LEDGER}:${line}: ${message}`);

if (!existsSync(LEDGER_PATH)) {
  console.error(`FAIL review ledger: ${LEDGER} does not exist; the reference notes cite it as their source of identity`);
  process.exit(1);
}

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

// A header with no rows is well formed and worthless. Without this the gate
// reports "0 rows" over a ledger whose evidence has been deleted wholesale.
if (lines.length === 1) {
  console.error(`FAIL review ledger: ${LEDGER} has a header and no rows; the evidence it indexes is gone`);
  process.exit(1);
}

/** True when the string is a calendar date that actually exists. The regex
 * alone accepts 2026-99-99, which is not a day anything was reviewed on. */
function isRealDate(value) {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  const parsed = new Date(`${value}T00:00:00Z`);
  return !Number.isNaN(parsed.getTime()) && parsed.toISOString().slice(0, 10) === value;
}

/** The ref a GitHub-family URL is pinned to, or null when it names none.
 * Parsed rather than pattern-matched so the host is anchored -- `evilgithub.com`
 * and `github.com.example.org` are not GitHub -- and so the query and fragment
 * are stripped before the ref is read, which a substring match got wrong: a
 * legitimate `.../tree/<full-sha>#readme` had its fragment swallowed into the
 * captured ref and was rejected as unpinned. */
function gitHubRef(url) {
  let parsed;
  try {
    parsed = new URL(url);
  } catch {
    return null;
  }
  if (parsed.hostname === "github.com") {
    const blob = /^\/[^/]+\/[^/]+\/(?:blob|tree)\/([^/]+)/.exec(parsed.pathname);
    return blob === null ? null : blob[1];
  }
  // raw.githubusercontent.com/<owner>/<repo>/<ref>/<path> is the most likely
  // way a future row cites GitHub content, and it was silently unchecked.
  if (parsed.hostname === "raw.githubusercontent.com") {
    const raw = /^\/[^/]+\/[^/]+\/([^/]+)/.exec(parsed.pathname);
    return raw === null ? null : raw[1];
  }
  return null;
}

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
  if (!isRealDate(reviewedAt)) {
    fail(number, `reviewed_at_utc is not a real YYYY-MM-DD date: ${JSON.stringify(reviewedAt)}`);
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

  const ref = gitHubRef(url);
  if (ref !== null && !/^[0-9a-f]{40}$/.test(ref)) {
    fail(
      number,
      `GitHub reference is pinned to ${JSON.stringify(ref)} rather than a full 40-character commit sha; ` +
        "a short sha or a branch name cannot be fetched, so the row cannot be re-checked",
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
const pinned = lines.slice(1).filter((line) => gitHubRef(line.split("\t")[0]) !== null).length;
console.log(
  `PASS review ledger: ${lines.length - 1} rows well formed, ${pinned} pinned to a full commit sha`,
);

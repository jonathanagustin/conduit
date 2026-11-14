#!/usr/bin/env node
// Checks the one class of documentation claim this repository can falsify for
// free: "no public page was captured for this".
//
// A capability note that opens by saying the public documentation has nothing
// on its subject sets the ceiling for everything downstream. It decides that
// the evidence tier is tenant observation rather than public contract, and it
// licenses inventing a shape instead of reading one. It is also the single
// easiest claim in the repository to check, because the corpus it is a claim
// about is a TSV sitting in the same directory.
//
// Nothing was checking it. docs/reference-platform/reference/notifications.md carried that
// sentence while three managing-notifications pages sat in the manifest with
// their content digests, and five screenshots on those pages carried the
// capability's entire enum vocabulary. A notification domain was invented on
// top of it -- severity levels Apollo does not have, a closed type vocabulary
// where Apollo's is explicitly open -- and every part of it was displaced the
// following day by evidence that had never moved.
//
// So: if a document claims the public corpus is silent about its subject, and
// the manifest has pages for that subject, this fails and names them.
//
// Runs offline against committed files and needs no credentials.
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { basename, dirname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(HERE, "..");
const MANIFEST = resolve(ROOT, "docs/reference-platform/public-documentation-manifest.tsv");

const failures = [];

if (!existsSync(MANIFEST)) {
  console.error(`FAIL missing ${relative(ROOT, MANIFEST)}`);
  process.exit(1);
}

// section -> [url, ...]
const sections = new Map();
for (const row of readFileSync(MANIFEST, "utf8").trim().split("\n").slice(1)) {
  const [url, section] = row.split("\t");
  if (!section) continue;
  if (!sections.has(section)) sections.set(section, []);
  sections.get(section).push(url);
}

// The claim, in the forms it actually gets written in. Deliberately narrow: it
// matches an assertion that the public corpus holds nothing, not a bounded
// statement that some particular private surface is unrecovered, which is a
// legitimate and common thing for these notes to say.
const CLAIM =
  /\bno\s+(?:public\s+)?(?:palantir\s+)?(?:docs?|documentation)\s+(?:page|pages)\b[^.\n]*\bcaptur\w*/i;

// A document's subject, taken from its filename. "reference/notifications.md"
// is about notifications; the manifest calls that section
// "managing-notifications". Matching on containment in either direction covers
// the naming Palantir actually uses without a hand-maintained mapping that
// would itself go stale.
const subjectOf = (path) => basename(path).replace(/\.md$/, "").toLowerCase();

const sectionsFor = (subject) => {
  const hits = [];
  for (const [section, urls] of sections) {
    const normalized = section.toLowerCase();
    if (normalized === subject || normalized.includes(subject) || subject.includes(normalized)) {
      hits.push({ section, urls });
    }
  }
  return hits;
};

const tracked = execFileSync("git", ["ls-files", "-z", "docs"], { cwd: ROOT, encoding: "utf8" })
  .split("\0")
  .filter((path) => path.endsWith(".md"));

let claims = 0;
for (const path of tracked) {
  const lines = readFileSync(resolve(ROOT, path), "utf8").split("\n");
  for (let index = 0; index < lines.length; index += 1) {
    if (!CLAIM.test(lines[index])) continue;
    claims += 1;
    const subject = subjectOf(path);
    // A one-word subject is too weak to match on: "overview.md" would collide
    // with half the corpus. Those are reported as unmatchable rather than
    // silently passing, so the gate never looks like it checked something it
    // could not.
    if (subject.length < 5) {
      failures.push(
        `${path}:${index + 1} claims the public corpus is silent, but its filename (${subject}) is too generic ` +
          `to identify a manifest section. Name the section the claim is about, or move the claim to a document that can be checked`,
      );
      continue;
    }
    const hits = sectionsFor(subject);
    if (hits.length === 0) continue;
    for (const hit of hits) {
      failures.push(
        `${path}:${index + 1} claims no public documentation page was captured for its subject, but ` +
          `${relative(ROOT, MANIFEST)} lists ${hit.urls.length} page(s) in section "${hit.section}":\n` +
          hit.urls.map((url) => `    ${url}`).join("\n") +
          `\n  Read them before treating this capability as ungrounded. Screenshots on those pages count: ` +
          `docs/reference-platform/IMAGE-DERIVED-GAPS.md records that a UI-first product states its enum values in images, not prose`,
      );
    }
  }
}

if (failures.length) {
  for (const failure of failures) console.error(`FAIL ${failure}`);
  process.exit(1);
}
console.log(
  `PASS documentation claims: ${claims} silence claim(s) checked against ${sections.size} manifest sections, none contradicted`,
);

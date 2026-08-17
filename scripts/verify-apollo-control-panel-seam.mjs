#!/usr/bin/env node
// Guards the Foundry-to-Apollo control-panel seam evidence.
//
// This file exists because an audit found that three of the four Apollo
// artifacts under docs/evidence/ are read by nothing at all, while
// docs/evidence/README.md says files there "are inputs to tests,
// candidate-admission checks, or bounded compatibility claims". An artifact no
// gate reads can drift arbitrarily and nobody notices, which is how a count in
// one of them came to overstate a divergence by a factor of two. A new artifact
// arriving without a gate would repeat that, so this one arrives with its gate.
//
// What is checked is the disclosure boundary rather than the shapes: the shapes
// are what the artifact is for, and re-asserting them here would only restate
// the file against itself. The boundary is the part that erodes -- the next
// person extending this reaches for a docstring because it makes the artifact
// readable, exactly as the Rosetta registry's header warns.
//
// Runs offline against the committed file and needs no credentials.
import { existsSync, readFileSync } from "node:fs";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(HERE, "..");
const EVIDENCE = resolve(ROOT, "docs/evidence/apollo-control-panel-seam.json");

const failures = [];
const fail = (message) => failures.push(message);

if (!existsSync(EVIDENCE)) {
  console.error(`FAIL missing ${relative(ROOT, EVIDENCE)}`);
  process.exit(1);
}
const evidence = JSON.parse(readFileSync(EVIDENCE, "utf8"));
const serialized = JSON.stringify(evidence);

// 1. No tenant identity. The upstream Maven coordinate is product identity and
// is named on purpose; a hostname, URL or enrollment name is tenant identity and
// is not ours to publish. The internal SCM host is the one most likely to be
// added back by someone making the provenance "easier to trace".
for (const pattern of [/https?:\/\//, /palantir\.build/i, /palantirfoundry\.com/i]) {
  if (pattern.test(serialized)) {
    fail(`the artifact carries a tenant or internal origin (${pattern}); name the upstream artifact instead`);
  }
}

// 2. No prose. The source projection carries zero docs: entries, so there is
// none to copy -- which means any that appears here was written from somewhere
// else and the boundary statement stops being true.
for (const field of ["docs", "documentation", "description", "javadoc"]) {
  if (serialized.includes(`"${field}"`)) {
    fail(`the artifact carries a "${field}" field; contract prose is not copied into this repository`);
  }
}

// 3. The provenance has to be pinned to something re-checkable. A projection
// digest with no artifact coordinate cannot be re-derived, and a coordinate with
// no digest cannot be shown to be the thing that was read.
const source = evidence.source ?? {};
if (!/^[0-9a-f]{64}$/.test(source.projectionSha256 ?? "")) {
  fail("source.projectionSha256 is not a SHA-256 digest, so what was read cannot be identified");
}
if (!/^[\w.-]+:[\w.-]+:[\w.-]+$/.test(source.upstreamArtifact ?? "")) {
  fail("source.upstreamArtifact is not a Maven coordinate, so the authority behind the projection is unnamed");
}

// 4. Every declared operation must actually carry its shape. A half-filled entry
// reads as a recovered contract while establishing nothing.
const types = evidence.types ?? {};
for (const operation of evidence.operations ?? []) {
  for (const field of ["name", "method", "path", "responseType"]) {
    if (!operation[field]) fail(`operation ${operation.name ?? "(unnamed)"} has no ${field}`);
  }
  for (const referenced of [operation.requestType, operation.responseType]) {
    if (referenced && !(referenced in types)) {
      fail(`operation ${operation.name} references ${referenced}, which this artifact does not describe`);
    }
  }
}

// 5. The vocabulary collisions are load-bearing and must not be quietly dropped.
// "Apollo labels" here means PalSec accreditation markings and has nothing to do
// with Apollo's own labels; joining the two on the spelling is precisely the
// defect class this repository has been burned by, and the warning is the most
// useful thing in the file.
const collisions = new Set((evidence.vocabularyCollisions ?? []).map((entry) => entry.term));
for (const term of ["Apollo labels", "spaceRid"]) {
  if (!collisions.has(term)) {
    fail(`the recorded vocabulary collision for "${term}" is gone; it is the reason this artifact is safe to read`);
  }
}

// 6. The negative claims are the half a reader is most likely to skip, and the
// Space one bears directly on ADR 0005. Losing it would let a future reader take
// this seam for a met reopen condition, which it is not.
const negatives = (evidence.whatThisDoesNotEstablish ?? []).join("\n");
for (const [claim, why] of [
  ["ri.apollo", "the absence of any RID grammar is what keeps issue #57 undecided by this artifact"],
  ["ADR 0005", "the Space reopen condition is unmet and this file is the record of that"],
]) {
  if (!negatives.includes(claim)) fail(`whatThisDoesNotEstablish no longer mentions ${claim}: ${why}`);
}

if (failures.length) {
  for (const failure of failures) console.error(`FAIL ${failure}`);
  process.exit(1);
}
console.log(
  `PASS Apollo control-panel seam: ${(evidence.operations ?? []).length} operation(s), ` +
    `${Object.keys(types).length} type(s), ${collisions.size} recorded vocabulary collision(s), no prose and no tenant origin`,
);

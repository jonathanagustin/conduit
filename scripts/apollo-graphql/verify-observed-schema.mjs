#!/usr/bin/env node
// The offline commit gate for the observed Apollo schema.
//
// It never contacts a Hub. It re-derives the SDL from the committed recovery
// record, requires a byte-clean match against the committed SDL, and refuses
// anything that looks like tenant material.
//
// The scrub check is the part that matters. The recovery script is careful, but
// "careful" is not a gate: a future edit, a hand-written fixture, or a pasted
// diagnostic could put a Hub origin or a real RID into the record, and nothing
// else in this repository would notice. So this asserts the shape of what is
// allowed rather than trusting how it was produced.
import { existsSync, readFileSync } from "node:fs";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { buildObservedSchema } from "./build-observed-schema.mjs";

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(HERE, "../..");

const OBSERVED = resolve(ROOT, "docs/reference-platform/observed/reference-graphql-observed.json");
const SCHEMA = resolve(ROOT, "api/graphql/apollo.observed.graphql");
const REPORT = resolve(ROOT, "docs/reference-platform/observed/reference-graphql-completeness.json");

// Shapes that must never appear in committed evidence.
const FORBIDDEN = [
  { name: "bearer token", pattern: /\bBearer\s+[A-Za-z0-9._~+/-]{16,}/i },
  { name: "JSON web token", pattern: /\beyJ[A-Za-z0-9._-]{20,}/ },
  { name: "tenant RID", pattern: /\bri\.[a-z0-9-]+\.[a-z0-9-]*\.[a-z0-9-]+\.[A-Za-z0-9_-]{8,}/ },
  { name: "https origin", pattern: /https:\/\/(?!www\.palantir\.com|github\.com|spec\.graphql\.org)[a-z0-9.-]+\.[a-z]{2,}/i },
  { name: "email address", pattern: /[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}/ },
  { name: "cookie header", pattern: /\bset-cookie\b/i },
];

const failures = [];
const fail = (message) => failures.push(message);

for (const path of [OBSERVED, SCHEMA, REPORT]) {
  if (!existsSync(path)) fail(`missing committed evidence: ${relative(ROOT, path)}`);
}
if (failures.length) {
  for (const failure of failures) console.error(`FAIL ${failure}`);
  process.exit(1);
}

const observedRaw = readFileSync(OBSERVED, "utf8");
const schemaRaw = readFileSync(SCHEMA, "utf8");
const reportRaw = readFileSync(REPORT, "utf8");

// 1. No tenant material anywhere in the committed evidence.
for (const [label, contents] of [
  ["recovery record", observedRaw],
  ["observed SDL", schemaRaw],
  ["completeness report", reportRaw],
]) {
  for (const { name, pattern } of FORBIDDEN) {
    const hit = contents.match(pattern);
    if (hit) fail(`${label} contains what looks like a ${name} (${hit[0].slice(0, 24)}…)`);
  }
}

// 2. The SDL is exactly what the record derives, so neither can drift.
const observed = JSON.parse(observedRaw);
const { sdl, dangling, omitted } = buildObservedSchema(observed);
if (dangling.length) fail(`observed SDL references undeclared type(s): ${dangling.join(", ")}`);
if (sdl !== schemaRaw) {
  fail(`${relative(ROOT, SCHEMA)} is not what ${relative(ROOT, OBSERVED)} derives; re-run build-observed-schema.mjs`);
}

// 3. The report describes this record and not an earlier one.
const report = JSON.parse(reportRaw);
if (report.candidatesSha256 !== observed.candidatesSha256) {
  fail("completeness report was produced from a different candidate set");
}
if (report.hubFingerprint !== observed.hubFingerprint) {
  fail("completeness report describes a different Hub than the recovery record");
}
if (report.fieldsOmitted !== omitted.length) {
  fail(`report claims ${report.fieldsOmitted} omitted field(s); the record derives ${omitted.length}`);
}

// 4. The record must not claim more than the method can support.
if (observed.method !== "validation-probe") fail(`unrecognized recovery method '${observed.method}'`);
if (!Array.isArray(observed.diagnostics)) fail("recovery record carries no diagnostics list");
for (const type of observed.types) {
  for (const field of type.fields) {
    if (field.shape !== "scalar" && field.returns && !observed.types.some((t) => t.name === field.returns)) {
      // A composite naming a type this record never described is not fatal --
      // the walk is depth-bounded -- but it must be visible in the diagnostics.
      const noted = observed.diagnostics.some((line) => line.includes(field.returns));
      if (!noted) fail(`${type.name}.${field.name} returns undescribed type ${field.returns} with no diagnostic`);
    }
  }
}

if (failures.length) {
  for (const failure of failures) console.error(`FAIL ${failure}`);
  process.exit(1);
}
console.log(
  `PASS observed Apollo GraphQL: ${report.fieldsEmitted} field(s) across ${report.typesRecovered} type(s) ` +
    `re-derive byte-identically; ${report.fieldsOmitted} omitted; no tenant material`,
);

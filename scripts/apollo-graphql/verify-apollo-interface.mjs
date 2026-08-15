#!/usr/bin/env node
// The displacement gate.
//
// Conduit serves an Apollo-shaped interface that is part recovered and part
// invented. That is a deliberate, temporary state, and the danger in it is not
// the invention -- it is the invention outliving the evidence that would replace
// it. Nobody re-reads a schema comment when a capture lands.
//
// So this makes displacement mechanical rather than remembered:
//
//   1. Every OBSERVED field must still match the recovery record. If a capture
//      changes what the Hub says, the served interface changes with it or this
//      fails.
//   2. If a capture recovers a coordinate still declared in conduit-native.json,
//      this FAILS and names it. The invented definition must be deleted so the
//      recovered one takes its place. Ground truth displaces invention; it does
//      not sit beside it.
//   3. The interface is re-derived and byte-compared, so neither the schema nor
//      the native declarations can drift from each other.
//
// Rule 2 is the one worth stating plainly: finding ground truth breaks the build
// until the invention is removed. That is the intended cost. The alternative is
// a repository that quietly serves guesses it could have replaced.
import { existsSync, readFileSync } from "node:fs";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { buildInterfaceSchema } from "./build-interface-schema.mjs";

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(HERE, "../..");

const OBSERVED = resolve(ROOT, "docs/apollo/observed/apollo-graphql-observed.json");
const NATIVE = resolve(HERE, "conduit-native.json");
const SCHEMA = resolve(ROOT, "api/graphql/apollo.interface.graphql");
const REPORT = resolve(ROOT, "docs/apollo/observed/apollo-interface-provenance.json");

const failures = [];
const fail = (message) => failures.push(message);

for (const path of [OBSERVED, NATIVE, SCHEMA, REPORT]) {
  if (!existsSync(path)) fail(`missing: ${relative(ROOT, path)}`);
}
if (failures.length) {
  for (const failure of failures) console.error(`FAIL ${failure}`);
  process.exit(1);
}

const observed = JSON.parse(readFileSync(OBSERVED, "utf8"));
const native = JSON.parse(readFileSync(NATIVE, "utf8"));
const committedSchema = readFileSync(SCHEMA, "utf8");
const report = JSON.parse(readFileSync(REPORT, "utf8"));

const { sdl, observedCount, nativeCount, displaceable, unresolved } = buildInterfaceSchema(observed, native);

// 1 & 3. The served interface is exactly what the record and the declarations
// derive, so an OBSERVED field cannot drift from the Hub that established it.
if (sdl !== committedSchema) {
  fail(
    `${relative(ROOT, SCHEMA)} is not what the recovery record and conduit-native.json derive; ` +
      "re-run build-interface-schema.mjs",
  );
}

// 2. Displacement. This is the reason the gate exists.
for (const { type, field } of displaceable) {
  fail(
    `ground truth now exists for ${type}.${field}: the Hub answers for it, but it is still declared in ` +
      "conduit-native.json. Delete that declaration so the recovered definition displaces the invented one.",
  );
}

// An invented field naming a type nothing declares is a mistake in
// conduit-native.json: it would serve a shape no client can select against.
//
// An OBSERVED field in the same position is not a mistake. It means the Hub
// named a type the walk could not describe, which the recovery record already
// reports as a completeness gap. Omitting it here is the same honest choice the
// observed SDL makes, so it is not failed on twice.
for (const { type, field, returns, tier } of unresolved) {
  if (tier !== "CONDUIT_NATIVE") continue;
  fail(`${type}.${field} returns ${returns}, which the interface does not declare`);
}

// Every OBSERVED field in the served schema must be traceable to the record.
const recorded = new Set();
for (const type of observed.types) {
  for (const field of type.fields) recorded.add(`${type.name}.${field.name}`);
}
// The root field is recorded separately from the walked types, because it is
// what the walk started from rather than something the walk found.
recorded.add(`Query.${observed.rootField}`);
for (const match of committedSchema.matchAll(/^type ([A-Za-z_][A-Za-z0-9_]*) \{$([\s\S]*?)^\}$/gm)) {
  const [, typeName, body] = match;
  for (const line of body.split("\n")) {
    const field = line.match(/^ {2}([a-zA-Z_][a-zA-Z0-9_]*)/)?.[1];
    if (!field || !line.includes("tier: OBSERVED")) continue;
    if (!recorded.has(`${typeName}.${field}`)) {
      fail(`${typeName}.${field} is marked OBSERVED but the recovery record does not contain it`);
    }
  }
}

// The report must describe this interface, not an earlier one.
if (report.observedFields !== observedCount || report.conduitNativeFields !== nativeCount) {
  fail(
    `provenance report claims ${report.observedFields} observed / ${report.conduitNativeFields} native; ` +
      `the interface derives ${observedCount} / ${nativeCount}`,
  );
}
if (report.hubFingerprint !== observed.hubFingerprint) {
  fail("provenance report describes a different Hub than the recovery record");
}

if (failures.length) {
  for (const failure of failures) console.error(`FAIL ${failure}`);
  process.exit(1);
}

const share = Math.round((report.conduitNativeShare ?? 0) * 100);
console.log(
  `PASS Apollo interface provenance: ${observedCount} observed + ${nativeCount} Conduit-native field(s); ` +
    `${share}% of the served interface is invented and awaiting ground truth`,
);

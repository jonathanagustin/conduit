#!/usr/bin/env node
// Emits the interface Conduit actually serves: the observed Apollo surface plus
// the Conduit-native additions, with every element carrying its provenance.
//
// The annotation is the whole design. An interface that mixes recovered truth
// with invention and does not say which is which cannot be audited, and the
// invention silently becomes the claim. Here each field carries
// @provenance(tier: OBSERVED) or @provenance(tier: CONDUIT_NATIVE), so the SDL
// itself answers "who established this?" for every line of it.
//
// OBSERVED elements are copied from the recovery record and never edited here.
// CONDUIT_NATIVE elements come from conduit-native.json and are invented on
// purpose. `verify-apollo-interface.mjs` fails the moment a capture recovers a
// coordinate that is still declared native, so an invention cannot outlive the
// evidence that would replace it.
import { readFileSync, writeFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const BUILT_IN = new Set(["Int", "Float", "String", "Boolean", "ID"]);

function parseArgs(argv) {
  const options = {
    observed: resolve(HERE, "../../docs/apollo/observed/apollo-graphql-observed.json"),
    native: resolve(HERE, "conduit-native.json"),
  };
  for (let index = 0; index < argv.length; index += 1) {
    const flag = argv[index];
    if (flag === "--help") return { help: true };
    if (!["--observed", "--native", "--out", "--report"].includes(flag)) throw new Error(`Unknown argument: ${flag}`);
    const value = argv[index + 1];
    index += 1;
    if (!value) throw new Error(`${flag} requires a value`);
    options[flag.slice(2)] = resolve(value);
  }
  for (const required of ["out", "report"]) if (!options[required]) throw new Error(`--${required} is required`);
  return options;
}

// A field is "observed" only if the recovery record both names it and could
// type it. A recovered-but-untyped field is not evidence for a served contract.
function observedFields(observed) {
  const byType = new Map();
  for (const type of observed.types) {
    const usable = type.fields.filter((field) => field.shape === "scalar" || Boolean(field.returns));
    if (usable.length) byType.set(type.name, usable);
  }
  return byType;
}

export function buildInterfaceSchema(observed, native) {
  const fromHub = observedFields(observed);
  const types = new Map();
  const scalars = new Set();
  const displaceable = [];

  const addField = (typeName, field, tier) => {
    if (!types.has(typeName)) types.set(typeName, new Map());
    types.get(typeName).set(field.name, { ...field, tier });
  };

  for (const [typeName, fields] of fromHub) {
    for (const field of fields) {
      const returns = field.shape === "scalar" ? "String" : field.returns;
      for (const argument of field.arguments) if (argument.type && !BUILT_IN.has(argument.type)) scalars.add(argument.type);
      addField(typeName, { name: field.name, arguments: field.arguments, returns }, "OBSERVED");
    }
  }

  for (const [typeName, declaration] of Object.entries(native.types ?? {})) {
    for (const field of declaration.fields ?? []) {
      // The displacement check: the Hub now answers for a coordinate still
      // declared native. Recorded here and enforced by the gate.
      if (fromHub.get(typeName)?.some((observedField) => observedField.name === field.name)) {
        displaceable.push({ type: typeName, field: field.name });
        continue;
      }
      for (const argument of field.arguments ?? []) if (!BUILT_IN.has(argument.type)) scalars.add(argument.type);
      addField(
        typeName,
        { name: field.name, arguments: field.arguments ?? [], returns: field.returns, list: field.list === true },
        "CONDUIT_NATIVE",
      );
    }
  }

  // Only emit a field whose type this document declares, so the SDL loads.
  const declaredTypes = new Set(types.keys());
  const rendered = [];
  // Starts at one for the annotated root field, which is emitted outside the
  // type loop below. Counting it keeps this report and the Go reading of the
  // same SDL in agreement; a count that skipped it would make one of them wrong.
  let observedCount = 1;
  let nativeCount = 0;
  const unresolved = [];
  for (const typeName of [...types.keys()].sort()) {
    const body = [];
    for (const [, field] of [...types.get(typeName)].sort(([left], [right]) => left.localeCompare(right))) {
      if (!BUILT_IN.has(field.returns) && !declaredTypes.has(field.returns)) {
        unresolved.push({ type: typeName, field: field.name, returns: field.returns, tier: field.tier });
        continue;
      }
      const args = field.arguments.length
        ? `(${field.arguments.map((argument) => `${argument.name}: ${argument.type}!`).join(", ")})`
        : "";
      // Cardinality is only ever claimed by whoever established it. Conduit
      // knows its own collections are lists and says so; validation probing
      // cannot see through a list type, so an OBSERVED field never claims one
      // and the resolver refuses the collection-shaped ones rather than
      // guessing. See the limitations in the completeness report.
      const returns = field.list ? `[${field.returns}]` : field.returns;
      body.push(`  ${field.name}${args}: ${returns} @provenance(tier: ${field.tier})`);
      if (field.tier === "OBSERVED") observedCount += 1;
      else nativeCount += 1;
    }
    if (body.length) rendered.push({ name: typeName, body });
  }

  const lines = [
    "# The Apollo-shaped GraphQL interface Conduit serves.",
    "#",
    "# Every field says who established it. OBSERVED was recovered from an",
    "# authorized Hub and is reproduced from the recovery record. CONDUIT_NATIVE",
    "# was invented here because no Hub established it, and it is NOT an Apollo",
    "# compatibility claim. When a capture recovers a CONDUIT_NATIVE coordinate,",
    "# scripts/apollo-graphql/verify-apollo-interface.mjs fails until the invented",
    "# definition is displaced by the recovered one.",
    "#",
    `# observedFrom: ${observed.hubFingerprint}`,
    "",
    "enum ProvenanceTier {",
    "  OBSERVED",
    "  CONDUIT_NATIVE",
    "}",
    "",
    "directive @provenance(tier: ProvenanceTier!) on FIELD_DEFINITION",
    "",
    ...[...scalars]
      .filter((scalar) => !declaredTypes.has(scalar))
      .sort()
      .map((scalar) => `scalar ${scalar}`),
    "",
    "type Query {",
    // The root field is annotated like every other. An unannotated field would
    // be the one coordinate whose provenance nobody states, and the entry point
    // is the last place that should be true of.
    `  ${observed.rootField}: ${observed.rootType} @provenance(tier: OBSERVED)`,
    "}",
    "",
    ...rendered.flatMap((type) => [`type ${type.name} {`, ...type.body, "}", ""]),
  ];
  const sdl = `${lines.join("\n").replace(/\n{3,}/g, "\n\n").trimEnd()}\n`;
  return { sdl, observedCount, nativeCount, displaceable, unresolved };
}

function main() {
  const options = parseArgs(process.argv.slice(2));
  if (options.help) {
    console.log("Usage: node scripts/apollo-graphql/build-interface-schema.mjs --out <schema> --report <report>");
    return;
  }
  const observed = JSON.parse(readFileSync(options.observed, "utf8"));
  const native = JSON.parse(readFileSync(options.native, "utf8"));
  const { sdl, observedCount, nativeCount, displaceable, unresolved } = buildInterfaceSchema(observed, native);

  const total = observedCount + nativeCount;
  const report = {
    schemaVersion: 1,
    hubFingerprint: observed.hubFingerprint,
    interfaceSha256: createHash("sha256").update(sdl).digest("hex"),
    nativeSha256: createHash("sha256").update(readFileSync(options.native)).digest("hex"),
    observedFields: observedCount,
    conduitNativeFields: nativeCount,
    // The number to watch. It should fall as captures widen, and it is the
    // share of the served interface that no Hub has ever confirmed.
    conduitNativeShare: total ? Number((nativeCount / total).toFixed(4)) : 0,
    displaceable,
    unresolved,
  };
  writeFileSync(options.out, sdl);
  writeFileSync(options.report, `${JSON.stringify(report, null, 2)}\n`);
  // Go cannot embed a file outside its own package directory, so the server
  // keeps a copy. Writing it here means one command leaves both correct; a Go
  // test byte-compares them, so a copy written any other way fails the build.
  writeFileSync(resolve(HERE, "../../internal/apollographql/schema/apollo.interface.graphql"), sdl);
  console.log(
    `interface: ${observedCount} observed + ${nativeCount} Conduit-native field(s) ` +
      `(${Math.round(report.conduitNativeShare * 100)}% invented); ${displaceable.length} displaceable`,
  );
}

if (import.meta.url === `file://${process.argv[1]}`) main();

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
// Three tiers, because "not recovered" turned out to cover three different
// situations and calling them all invention overstated the invention.
//
//   OBSERVED       an authorized Hub answered for this coordinate.
//   DOCUMENTED     Apollo names it or documents it -- in the pinned OpenAPI, in
//                  an apollo-cli flag, in published documentation, or on a
//                  sibling type in the recovery record. No Hub answered for THIS
//                  coordinate, but Conduit did not make it up.
//   CONDUIT_NATIVE Conduit's own idea. No Apollo claim of any kind.
//
// The middle tier exists because documentation is evidence. Where Apollo names
// something Conduit had named itself, the invention is displaced by Apollo's
// name exactly as a capture would displace it -- the difference is only which
// evidence arrived. `verify-apollo-interface.mjs` verifies every citation
// against the pinned OpenAPI, the committed documentation manifest and the
// recovery record, so a tier cannot be claimed without the source.
//
// The bottom tier currently has no members, and that is a finding rather than a
// design: every field Conduit serves turned out to trace to some Apollo source.
// It stays in the enum because the next invention needs somewhere honest to go.
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

export function buildInterfaceSchema(observed, native, cardinality = {}) {
  const fromHub = observedFields(observed);
  // Coordinates whose cardinality the probe could not establish and the pinned
  // OpenAPI publishes. They stay OBSERVED -- a Hub answered for them -- and
  // carry the weaker source for the ONE thing the Hub did not establish.
  const documentedList = new Map(Object.entries(cardinality.coordinates ?? {}));
  const unknownCardinality = [];
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
      const cited = documentedList.get(`${typeName}.${field.name}`);
      addField(
        typeName,
        { name: field.name, arguments: field.arguments, returns, list: Boolean(cited), citedCardinality: cited },
        "OBSERVED",
      );
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
      // A citation is what separates the two lower tiers. Neither is OBSERVED:
      // no Hub answered for any of these coordinates, whatever it answered for
      // on a sibling type.
      const cited = field.publishedAs || field.documentedBy || field.patternedOn;
      const tier = cited ? "DOCUMENTED" : "CONDUIT_NATIVE";
      addField(
        typeName,
        { name: field.name, arguments: field.arguments ?? [], returns: field.returns, list: field.list === true },
        tier,
      );
    }
  }

  for (const coordinate of documentedList.keys()) {
    const [typeName, fieldName] = coordinate.split(".");
    if (!fromHub.get(typeName)?.some((field) => field.name === fieldName)) {
      // Cardinality is a claim ABOUT a recovered field. Naming a coordinate the
      // record does not contain would be adding a field through the back door,
      // under the one tier that means "a Hub answered for this".
      unknownCardinality.push(coordinate);
    }
  }

  // Only emit a field whose type this document declares, so the SDL loads.
  const declaredTypes = new Set(types.keys());
  const rendered = [];
  // Starts at one for the annotated root field, which is emitted outside the
  // type loop below. Counting it keeps this report and the Go reading of the
  // same SDL in agreement; a count that skipped it would make one of them wrong.
  let observedCount = 1;
  let documentedCount = 0;
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
      // Two provenances on one field, because two different things are being
      // claimed. `tier` says who established that the field EXISTS; `cardinality`
      // says who established that it is a list. For the page collections those
      // are different sources, and collapsing them would either overstate the
      // Hub or understate the field.
      const provenance = field.citedCardinality
        ? `@provenance(tier: ${field.tier}, cardinality: DOCUMENTED)`
        : `@provenance(tier: ${field.tier})`;
      body.push(`  ${field.name}${args}: ${returns} ${provenance}`);
      if (field.tier === "OBSERVED") observedCount += 1;
      else if (field.tier === "DOCUMENTED") documentedCount += 1;
      else nativeCount += 1;
    }
    if (body.length) rendered.push({ name: typeName, body });
  }

  const lines = [
    "# The Apollo-shaped GraphQL interface Conduit serves.",
    "#",
    "# Every field says who established it.",
    "#",
    "#   OBSERVED        an authorized Hub answered for this coordinate. Reproduced",
    "#                   from the recovery record and never edited by hand.",
    "#   DOCUMENTED      Apollo names this field or documents this concept -- in the",
    "#                   pinned OpenAPI, in an apollo-cli flag, in published",
    "#                   documentation, or on a sibling type in the recovery record.",
    "#                   No Hub answered for THIS coordinate, so it is not an Apollo",
    "#                   compatibility claim -- but Conduit did not invent it.",
    "#   CONDUIT_NATIVE  Conduit's own idea. No Apollo claim of any kind. Currently",
    "#                   unused: every served field traced to an Apollo source.",
    "#",
    "# Evidence displaces invention whichever way it arrives. A capture that",
    "# recovers a lower-tier coordinate fails the gate until the definition is",
    "# replaced; so does an Apollo name for a field Conduit had named itself.",
    "# Citations are verified against the pinned OpenAPI snapshot, the committed",
    "# documentation manifest and the recovery record by",
    "# scripts/apollo-graphql/verify-apollo-interface.mjs.",
    "#",
    `# observedFrom: ${observed.hubFingerprint}`,
    "",
    "enum ProvenanceTier {",
    "  OBSERVED",
    "  DOCUMENTED",
    "  CONDUIT_NATIVE",
    "}",
    "",
    "# tier says who established that this coordinate EXISTS. valueTier says where",
    "# the VALUE served behind it came from, and is stated only where the two",
    "# differ -- a field Apollo declares, answered from something Conduit built.",
    "# Its absence means they are the same. See ADR 0007.",
    "#",
    "# cardinality is stated only where it was established by a source other than",
    "# the one that established the field. Its absence means the field's own tier",
    "# covers it: a scalar, or a list the record itself carried.",
    "directive @provenance(",
    "  tier: ProvenanceTier!",
    "  valueTier: ProvenanceTier",
    "  cardinality: ProvenanceTier",
    ") on FIELD_DEFINITION",
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
  return { sdl, observedCount, documentedCount, nativeCount, displaceable, unresolved, unknownCardinality };
}

function main() {
  const options = parseArgs(process.argv.slice(2));
  if (options.help) {
    console.log("Usage: node scripts/apollo-graphql/build-interface-schema.mjs --out <schema> --report <report>");
    return;
  }
  const observed = JSON.parse(readFileSync(options.observed, "utf8"));
  const native = JSON.parse(readFileSync(options.native, "utf8"));
  const cardinality = JSON.parse(readFileSync(resolve(HERE, "documented-cardinality.json"), "utf8"));
  const { sdl, observedCount, documentedCount, nativeCount, displaceable, unresolved } = buildInterfaceSchema(
    observed,
    native,
    cardinality,
  );

  const total = observedCount + documentedCount + nativeCount;
  const report = {
    schemaVersion: 1,
    hubFingerprint: observed.hubFingerprint,
    interfaceSha256: createHash("sha256").update(sdl).digest("hex"),
    nativeSha256: createHash("sha256").update(readFileSync(options.native)).digest("hex"),
    observedFields: observedCount,
    documentedFields: documentedCount,
    conduitNativeFields: nativeCount,
    // The share of the served interface no Apollo source of any kind supports.
    // It is currently zero, and it is the number that must stay honest rather
    // than the number that must stay zero: a genuine Conduit invention belongs
    // here rather than under a citation that does not fit it.
    //
    // Note what it is NOT: the share no Hub has confirmed. That is much larger
    // -- everything outside observedFields -- and it is what
    // `unrecoveredShare` reports. Conflating the two is how "documented"
    // quietly becomes "verified".
    conduitNativeShare: total ? Number((nativeCount / total).toFixed(4)) : 0,
    // The share no capture established, at any tier. This is the number that
    // should fall as captures widen.
    unrecoveredShare: total ? Number(((documentedCount + nativeCount) / total).toFixed(4)) : 0,
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
    `interface: ${observedCount} observed + ${documentedCount} documented + ${nativeCount} Conduit-native field(s) ` +
      `(${Math.round(report.conduitNativeShare * 100)}% unsupported by any Apollo source); ` +
      `${displaceable.length} displaceable`,
  );
}

if (import.meta.url === `file://${process.argv[1]}`) main();

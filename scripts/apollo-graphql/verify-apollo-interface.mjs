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
const MANIFEST = resolve(ROOT, "docs/apollo/public-documentation-manifest.tsv");
const CLI = resolve(ROOT, "docs/apollo/cli-command-contract.json");
const OPENAPI = resolve(
  ROOT,
  "docs/apollo/snapshots/apollo-openapi-c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml",
);
const NATIVE = resolve(HERE, "conduit-native.json");
const CARDINALITY = resolve(HERE, "documented-cardinality.json");
// Minted values change the ANNOTATION on a recovered coordinate, not the set of
// coordinates, so this file has to be an input here as well: rebuilding without
// it derives a schema missing every valueTier and reports the committed one as
// hand-edited.
const MINTED = resolve(HERE, "conduit-minted-values.json");
const SCHEMA = resolve(ROOT, "api/graphql/apollo.interface.graphql");
const REPORT = resolve(ROOT, "docs/apollo/observed/apollo-interface-provenance.json");

const failures = [];
const fail = (message) => failures.push(message);
// Coordinates whose documentation is Beta upstream. Reported rather than
// refused: citing Beta documentation is honest, citing it silently is not.
const betaCited = [];

for (const path of [OBSERVED, NATIVE, SCHEMA, REPORT]) {
  if (!existsSync(path)) fail(`missing: ${relative(ROOT, path)}`);
}
if (failures.length) {
  for (const failure of failures) console.error(`FAIL ${failure}`);
  process.exit(1);
}

const observed = JSON.parse(readFileSync(OBSERVED, "utf8"));
const native = JSON.parse(readFileSync(NATIVE, "utf8"));
const cardinality = existsSync(CARDINALITY) ? JSON.parse(readFileSync(CARDINALITY, "utf8")) : {};
const minted = existsSync(MINTED) ? (JSON.parse(readFileSync(MINTED, "utf8")).coordinates ?? {}) : {};
const committedSchema = readFileSync(SCHEMA, "utf8");
const report = JSON.parse(readFileSync(REPORT, "utf8"));

const { sdl, observedCount, documentedCount, nativeCount, displaceable, unresolved, unknownCardinality } =
  buildInterfaceSchema(observed, native, cardinality, minted);

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

// A tier is a claim about a source, so the source is checked rather than
// trusted. A citation nobody verifies is the same decoration an unanchored
// signer pattern is: it reads as evidence and constrains nothing.
// The manifest is read for two facts, not one: whether a cited page exists, and
// how mature Apollo says it is.
//
// Maturity matters because DOCUMENTED is a claim about evidence, and Beta
// evidence is weaker in a specific, stated way -- the Health page's own callout
// says the extension "may not be available on your Apollo Hub". A field cited to
// it is not wrong, but it rests on something Apollo has not committed to, and a
// tier that reported it identically to a GA citation would flatten that away.
// This does not fail: it counts, so the number cannot grow unnoticed.
const manifestUrls = new Set();
const betaUrls = new Set();
if (existsSync(MANIFEST)) {
  for (const line of readFileSync(MANIFEST, "utf8").split("\n").slice(1)) {
    const [url, , title = ""] = line.split("\t");
    if (!url) continue;
    manifestUrls.add(url);
    if (/\[Beta\]/.test(title)) betaUrls.add(url);
  }
}
// Property names in the pinned OpenAPI, indexed BY THE TYPE THAT DECLARES THEM,
// which is what `apollo-openapi X.y` citations point at.
//
// Indexing by type rather than collecting a flat set of names is the whole
// point, and the first version of this got it wrong: it accepted any property
// name that appeared anywhere in the document, so `apollo-openapi
// Environment.productId` passed even though Environment declares only rid,
// name, environmentType and status. A citation check that does not check the
// type is not a citation check -- it only asserts that Apollo uses the word
// somewhere, which is true of almost any word worth citing.
//
// Parsed by line rather than with a YAML dependency: the snapshot is pinned, so
// its shape cannot drift under this check. The shape is
//
//     4 spaces  TypeName:
//     6 spaces    properties:
//     8 spaces      propertyName:
const openapiProperties = new Map();
const openapiArrays = new Set();
if (existsSync(OPENAPI)) {
  let currentType = null;
  let currentProperty = null;
  let inProperties = false;
  for (const line of readFileSync(OPENAPI, "utf8").split("\n")) {
    const type = line.match(/^ {4}([A-Za-z][A-Za-z0-9_]*):$/);
    if (type) {
      currentType = type[1];
      currentProperty = null;
      inProperties = false;
      openapiProperties.set(currentType, new Set());
      continue;
    }
    if (!currentType) continue;
    if (/^ {6}[a-zA-Z][a-zA-Z0-9_]*:/.test(line)) {
      // A sibling key of `properties:` (description, required, type) ends the
      // property block, so a `type:` or `required:` entry cannot be read as one.
      inProperties = /^ {6}properties:$/.test(line);
      currentProperty = null;
      continue;
    }
    const property = inProperties && line.match(/^ {8}([a-zA-Z][a-zA-Z0-9_]*):$/);
    if (property) {
      openapiProperties.get(currentType).add(property[1]);
      currentProperty = `${currentType}.${property[1]}`;
      continue;
    }
    // `type: array` sits inside the property block, two levels in. Recorded
    // because a cardinality citation has to prove the property is a LIST, not
    // merely that it exists -- otherwise any property would look like evidence
    // for a collection.
    if (currentProperty && /^ {10}type: array$/.test(line)) openapiArrays.add(currentProperty);
  }
}
// A pinned snapshot that parsed to nothing would make every citation below
// unresolvable, or -- worse, if the check were written the other way -- make
// every citation pass. Fail loudly rather than gate on an empty index.
if (existsSync(OPENAPI) && openapiProperties.size === 0) {
  fail(`${relative(ROOT, OPENAPI)} parsed to zero types; the citation index is empty and would check nothing`);
}
// The flags each captured apollo-cli command carries, so an `apollo-cli
// <command> --<flag>` citation resolves to a real flag on a real command rather
// than to a plausible-looking invocation.
const cliFlags = new Map();
if (existsSync(CLI)) {
  for (const command of JSON.parse(readFileSync(CLI, "utf8")).commands ?? []) {
    // Captured names carry a trailing " [Experimental]" marker that is a status,
    // not part of the command anyone types.
    cliFlags.set(command.command.replace(/ \[[^\]]+\]$/, ""), new Set(command.flags ?? []));
  }
}

// The three forms this gate can actually resolve. Anything else in publishedAs
// is prose, and is refused below rather than skipped.
const VERIFIABLE_CITATION =
  /apollo-openapi [A-Za-z][A-Za-z0-9_]*\.[a-zA-Z]|apollo-graphql [A-Za-z_][A-Za-z0-9_]*\.[a-zA-Z]|apollo-cli [a-z0-9][a-z0-9 _-]* --[a-z]/;

// An `apollo-graphql Type.field` citation points at the recovery record itself:
// Apollo's own GraphQL name for the same thing, established by capture on a
// sibling type. It is the strongest citation available here, so it is the one
// most worth checking -- an unverified one would let a coordinate borrow the
// record's authority without appearing in it.
const recordedCoordinates = new Set(recorded);
const checkGraphQLCitations = (where, citation, kind) => {
  for (const cited of citation.matchAll(/apollo-graphql ([A-Za-z_][A-Za-z0-9_]*\.[a-zA-Z][a-zA-Z0-9_]*)/g)) {
    if (!recordedCoordinates.has(cited[1])) {
      fail(
        `${where} cites ${cited[0]} as ${kind}, but the recovery record contains no such coordinate. ` +
          "A citation of the record must appear in the record.",
      );
    }
  }
};

for (const [typeName, declaration] of Object.entries(native.types ?? {})) {
  for (const field of declaration.fields ?? []) {
    const where = `${typeName}.${field.name}`;
    if (field.documentedBy && betaUrls.has(field.documentedBy)) {
      betaCited.push(`${where} -> ${field.documentedBy}`);
    }
    if (field.documentedBy && !manifestUrls.has(field.documentedBy)) {
      fail(
        `${where} cites documentation at ${field.documentedBy}, which is not in ` +
          "docs/apollo/public-documentation-manifest.tsv. A citation nobody can follow is not evidence.",
      );
    }
    for (const cited of (field.publishedAs ?? "").matchAll(/apollo-openapi ([A-Za-z][A-Za-z0-9_]*)\.([a-zA-Z][a-zA-Z0-9_]*)/g)) {
      const [citation, citedType, citedProperty] = cited;
      if (!openapiProperties.has(citedType)) {
        fail(
          `${where} cites ${citation}, but the pinned OpenAPI snapshot declares no schema named ` +
            `"${citedType}". Cite a type the contract actually carries.`,
        );
        continue;
      }
      if (!openapiProperties.get(citedType).has(citedProperty)) {
        fail(
          `${where} claims Apollo publishes "${citedProperty}" on ${citedType} (as ${citation}), but ` +
            `${citedType} declares only: ${[...openapiProperties.get(citedType)].sort().join(", ") || "(no properties)"}. ` +
            "The property must belong to the type cited, not merely appear somewhere in the contract.",
        );
      }
    }
    for (const cited of (field.publishedAs ?? "").matchAll(/apollo-cli ([a-z0-9][a-z0-9 _-]*?) (--[a-z][a-z0-9-]*)/g)) {
      const [citation, command, flag] = cited;
      const flags = cliFlags.get(`apollo-cli ${command}`);
      if (!flags) {
        fail(
          `${where} cites ${citation}, but docs/apollo/cli-command-contract.json records no command ` +
            `"apollo-cli ${command}". Cite a command the captured CLI contract carries.`,
        );
        continue;
      }
      if (!flags.has(flag)) {
        fail(
          `${where} claims apollo-cli ${command} publishes the flag ${flag}, but the captured contract ` +
            `records its flags as: ${[...flags].sort().join(" ")}. Do not claim a flag the CLI does not carry.`,
        );
      }
    }
    checkGraphQLCitations(where, field.publishedAs ?? "", "a published name");
    checkGraphQLCitations(where, field.patternedOn ?? "", "the pattern it follows");
    // A citation nothing above can check is prose, and prose in a citation field
    // is the decoration this gate exists to refuse. Three verifiable forms are
    // accepted -- apollo-openapi Type.property, apollo-graphql Type.field, and
    // apollo-cli <command> --<flag> -- because each resolves against a committed
    // artifact. "rid convention" and a bare "apollo-openapi status" both sat here
    // for a while reading as evidence and constraining nothing.
    if (field.publishedAs && !VERIFIABLE_CITATION.test(field.publishedAs)) {
      fail(
        `${where} cites "${field.publishedAs}", which is in no verifiable form. Use ` +
          "`apollo-openapi Type.property`, `apollo-graphql Type.field`, or `apollo-cli <command> --<flag>` " +
          "so the claim resolves against a committed artifact.",
      );
    }
    if (field.publishedAs && !field.publishedAs.includes(field.name)) {
      fail(
        `${where} cites a published name that is not this field's name: ${field.publishedAs}. ` +
          "Where Apollo publishes a name, the field must use it -- that is what displacement means.",
      );
    }
    // patternedOn is a weaker claim than publishedAs and must stay weaker. It
    // says Apollo established the SHAPE and Conduit supplied the noun, so a
    // citation carrying this field's own name is really a published name and
    // must be declared as one -- otherwise the two claims blur and the stronger
    // evidence gets filed under the weaker word.
    if (field.patternedOn) {
      if (field.patternedOn.includes(field.name)) {
        fail(
          `${where} cites its own name under patternedOn: ${field.patternedOn}. If Apollo names this field, ` +
            "say so with publishedAs; patternedOn claims only that Apollo established the shape.",
        );
      }
      if (!field.documentedBy) {
        fail(
          `${where} claims to follow an Apollo pattern but cites nothing for its own name. A pattern ` +
            "establishes the shape, not the noun; cite the noun with documentedBy or drop the claim.",
        );
      }
    }
  }
}

// Cardinality citations. A coordinate here is a claim about a field the RECORD
// already contains -- only its list-ness comes from elsewhere -- so two things
// are checked that the field-name citations do not need.
for (const coordinate of unknownCardinality) {
  fail(
    `documented-cardinality.json names ${coordinate}, which the recovery record does not contain. ` +
      "Cardinality is a claim about a recovered field; naming an unrecovered one would add a coordinate " +
      "under the one tier that means a Hub answered for it.",
  );
}
for (const [coordinate, citation] of Object.entries(cardinality.coordinates ?? {})) {
  const cited = /apollo-openapi ([A-Za-z][A-Za-z0-9_]*)\.([a-zA-Z][a-zA-Z0-9_]*)/.exec(citation);
  if (!cited) {
    fail(`${coordinate} cites "${citation}", which is not an \`apollo-openapi Type.property\` citation`);
    continue;
  }
  const [, citedType, citedProperty] = cited;
  if (!openapiProperties.has(citedType)) {
    fail(`${coordinate} cites schema "${citedType}", which the pinned OpenAPI snapshot does not declare`);
    continue;
  }
  if (!openapiProperties.get(citedType).has(citedProperty)) {
    fail(`${coordinate} cites ${citedType}.${citedProperty}, which that schema does not declare`);
    continue;
  }
  // The one that matters. A property that exists but is not an array is
  // evidence the FIELD exists, which the record already established, and no
  // evidence at all for the thing being claimed.
  if (!openapiArrays.has(`${citedType}.${citedProperty}`)) {
    fail(
      `${coordinate} cites ${citedType}.${citedProperty} as evidence of a list, but the pinned snapshot does ` +
        "not declare that property `type: array`. Cite a property that is actually a collection.",
    );
  }
}

// The report must describe this interface, not an earlier one.
if (
  report.observedFields !== observedCount ||
  report.documentedFields !== documentedCount ||
  report.conduitNativeFields !== nativeCount
) {
  fail(
    `provenance report claims ${report.observedFields} observed / ${report.documentedFields} documented / ` +
      `${report.conduitNativeFields} native; the interface derives ` +
      `${observedCount} / ${documentedCount} / ${nativeCount}`,
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
if (betaCited.length) {
  console.log(
    `NOTE ${betaCited.length} documented coordinate(s) cite a page Apollo marks [Beta], which it may not serve ` +
      `on every Hub: ${betaCited.join(", ")}`,
  );
}
// "declared", not "served". conduitNativeShare divides by the three tier counts
// -- what the SDL declares -- and twelve declared coordinates are refused rather
// than served. The old wording was true only because the numerator is zero, and
// zero is zero under either denominator; the first genuine CONDUIT_NATIVE field
// would have made this line report a declared share while calling it a served
// one, which is the conflation the interface exists to prevent.
console.log(
  `PASS Apollo interface provenance: ${observedCount} observed + ${documentedCount} documented + ` +
    `${nativeCount} Conduit-native field(s) declared; every citation resolves; ` +
    `${share}% of the declared interface has no Apollo source of any kind`,
);

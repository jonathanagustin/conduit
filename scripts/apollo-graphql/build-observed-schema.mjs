#!/usr/bin/env node
// Emits the observed Apollo SDL from a recovery record, deterministically.
//
// "Observed" is the whole claim. This is what one authorized Hub answered to a
// bounded set of questions on one date -- not Apollo's schema, not a complete
// schema, and not evidence about fields nobody asked for. The completeness
// report is emitted beside it so the two are read together.
//
// A field whose return type was not recovered is omitted from the SDL and
// listed in the report. That is the honest direction: an SDL is a contract, and
// a contract naming a type nobody established would be an invention. Omitting
// it costs coverage, which the report states; inventing it would cost the
// claim, which nothing could restate.
import { readFileSync, writeFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { resolve } from "node:path";

function usage() {
  return [
    "Usage: node scripts/apollo-graphql/build-observed-schema.mjs \\",
    "  --observed <observed.json> --out <schema.graphql> --report <completeness.json>",
  ].join("\n");
}

function parseArgs(argv) {
  const options = {};
  for (let index = 0; index < argv.length; index += 1) {
    const flag = argv[index];
    if (flag === "--help") return { help: true };
    if (!["--observed", "--out", "--report"].includes(flag)) throw new Error(`Unknown argument: ${flag}`);
    const value = argv[index + 1];
    index += 1;
    if (!value) throw new Error(`${flag} requires a value`);
    options[flag.slice(2)] = resolve(value);
  }
  for (const required of ["observed", "out", "report"]) {
    if (!options[required]) throw new Error(`--${required} is required`);
  }
  return options;
}

// Built-in GraphQL scalars need no declaration; anything else the Hub named
// does, or the document references a type it never defines.
const BUILT_IN = new Set(["Int", "Float", "String", "Boolean", "ID"]);

export function buildObservedSchema(observed) {
  const declared = new Set(observed.types.map((type) => type.name));
  const scalars = new Set();
  const omitted = [];

  const emitted = observed.types
    .map((type) => {
      const fields = [];
      for (const field of type.fields) {
        // A composite field whose type was never established cannot be typed.
        if (field.shape !== "scalar" && !field.returns) {
          omitted.push({ type: type.name, field: field.name, reason: "return type not recovered" });
          continue;
        }
        for (const argument of field.arguments) {
          if (!argument.type) {
            omitted.push({ type: type.name, field: field.name, reason: `argument '${argument.name}' type not recovered` });
          } else if (!BUILT_IN.has(argument.type)) {
            scalars.add(argument.type);
          }
        }
        if (field.arguments.some((argument) => !argument.type)) continue;

        const args = field.arguments.length
          ? `(${field.arguments.map((argument) => `${argument.name}: ${argument.type}!`).join(", ")})`
          : "";
        // A scalar whose own type was never asked for is emitted as String
        // only when the Hub itself returned no type name; otherwise the
        // recovered name wins. Unknown scalars are recorded, not guessed.
        const returns = field.returns ?? "String";
        if (!declared.has(returns) && !BUILT_IN.has(returns)) scalars.add(returns);
        fields.push(`  ${field.name}${args}: ${returns}`);
      }
      return { name: type.name, body: fields };
    })
    .filter((type) => type.body.length > 0);

  const lines = [
    "# Observed Apollo Hub GraphQL schema.",
    "#",
    "# Recovered by validation probing from one authorized Hub. This is what that",
    "# Hub answered to the candidate set in scripts/apollo-graphql/candidates.json,",
    "# not Apollo's schema and not a complete one. Fields whose types were not",
    "# established are omitted here and listed in the completeness report.",
    "#",
    `# method:          ${observed.method}`,
    `# hubFingerprint:  ${observed.hubFingerprint}`,
    `# candidatesSha256:${observed.candidatesSha256}`,
    "",
    ...[...scalars]
      .filter((scalar) => !declared.has(scalar))
      .sort()
      .map((scalar) => `scalar ${scalar}`),
    "",
    `type Query {`,
    `  ${observed.rootField}: ${observed.rootType}`,
    `}`,
    "",
    ...emitted.flatMap((type) => [`type ${type.name} {`, ...type.body, "}", ""]),
  ];
  const sdl = `${lines.join("\n").replace(/\n{3,}/g, "\n\n").trimEnd()}\n`;

  // Structural validation without a parser dependency: every type the document
  // references must be declared by it.
  const referenced = [...sdl.matchAll(/:\s*([A-Za-z_][A-Za-z0-9_]*)!?$/gm)].map((match) => match[1]);
  const definedNames = new Set([
    ...[...sdl.matchAll(/^type ([A-Za-z_][A-Za-z0-9_]*) \{/gm)].map((match) => match[1]),
    ...[...sdl.matchAll(/^scalar ([A-Za-z_][A-Za-z0-9_]*)$/gm)].map((match) => match[1]),
    ...BUILT_IN,
  ]);
  const dangling = [...new Set(referenced)].filter((name) => !definedNames.has(name)).sort();

  return { sdl, omitted, dangling, declaredTypes: [...definedNames].sort() };
}

function main() {
  const options = parseArgs(process.argv.slice(2));
  if (options.help) {
    console.log(usage());
    return;
  }
  const observed = JSON.parse(readFileSync(options.observed, "utf8"));
  const { sdl, omitted, dangling } = buildObservedSchema(observed);

  if (dangling.length) {
    console.error(`observed SDL references undeclared type(s): ${dangling.join(", ")}`);
    process.exit(1);
  }

  const emittedFields = [...sdl.matchAll(/^ {2}[a-zA-Z_]/gm)].length;
  const report = {
    schemaVersion: 1,
    hubFingerprint: observed.hubFingerprint,
    candidatesSha256: observed.candidatesSha256,
    observedSha256: createHash("sha256").update(JSON.stringify(observed)).digest("hex"),
    schemaSha256: createHash("sha256").update(sdl).digest("hex"),
    typesRecovered: observed.completeness.typesRecovered,
    fieldsRecovered: observed.completeness.fieldsRecovered,
    fieldsEmitted: emittedFields,
    fieldsOmitted: omitted.length,
    requests: observed.completeness.requests,
    omitted: omitted.sort((left, right) => `${left.type}.${left.field}`.localeCompare(`${right.type}.${right.field}`)),
    diagnostics: observed.diagnostics,
    limitations: [
      "Observed from one authorized Hub on one date; it is not Apollo's published schema.",
      "Only names in candidates.json were asked about. A field nobody asked about is absent from this record, not from the Hub.",
      "Fields whose return or argument types were not established are omitted from the SDL and listed above.",
      "This establishes no private Hub-to-Agent control wire and no mutation surface.",
    ],
  };

  writeFileSync(options.out, sdl);
  writeFileSync(options.report, `${JSON.stringify(report, null, 2)}\n`);
  console.log(`observed SDL: ${emittedFields} field(s) emitted, ${omitted.length} omitted, sha256 ${report.schemaSha256.slice(0, 12)}`);
}

if (import.meta.url === `file://${process.argv[1]}`) main();

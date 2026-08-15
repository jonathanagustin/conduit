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
  const byName = new Map(observed.types.map((type) => [type.name, type]));
  const omitted = [];
  const note = (type, field, reason) => omitted.push({ type, field, reason });

  // Which types can be emitted, computed to a fixpoint.
  //
  // GraphQL requires a type to declare at least one field, so a type the walk
  // named but recovered nothing inside cannot be emitted -- and neither can a
  // field that returns it, because the document would reference a type it never
  // declares. Dropping that field can in turn empty its owner, so this iterates
  // until nothing more changes rather than making one pass.
  //
  // Every alternative invents something. Emitting a placeholder field invents a
  // field; declaring an empty type emits invalid SDL; leaving the reference
  // dangling emits a schema that does not load. Omitting and recording is the
  // only option that costs coverage instead of truth.
  const emittable = new Set(byName.keys());
  for (;;) {
    let changed = false;
    for (const type of observed.types) {
      if (!emittable.has(type.name)) continue;
      const usable = type.fields.filter((field) => {
        if (field.arguments.some((argument) => !argument.type)) return false;
        if (field.shape === "scalar") return true;
        return Boolean(field.returns) && emittable.has(field.returns);
      });
      if (usable.length === 0) {
        emittable.delete(type.name);
        changed = true;
      }
    }
    if (!changed) break;
  }

  const scalars = new Set();
  const emitted = [];
  for (const type of observed.types) {
    if (!emittable.has(type.name)) {
      for (const field of type.fields) note(type.name, field.name, "owning type has no emittable field");
      continue;
    }
    const body = [];
    for (const field of type.fields) {
      const untypedArgument = field.arguments.find((argument) => !argument.type);
      if (untypedArgument) {
        note(type.name, field.name, `argument '${untypedArgument.name}' type not recovered`);
        continue;
      }
      if (field.shape !== "scalar") {
        if (!field.returns) {
          note(type.name, field.name, "return type not recovered");
          continue;
        }
        if (!emittable.has(field.returns)) {
          note(
            type.name,
            field.name,
            byName.has(field.returns)
              ? `return type ${field.returns} has no recovered fields`
              : `return type ${field.returns} was named but not described`,
          );
          continue;
        }
      }
      for (const argument of field.arguments) if (!BUILT_IN.has(argument.type)) scalars.add(argument.type);
      const args = field.arguments.length
        ? `(${field.arguments.map((argument) => `${argument.name}: ${argument.type}!`).join(", ")})`
        : "";
      // A scalar field's own type was never asked for; String is the neutral
      // carrier and the report says so rather than the SDL implying more.
      const returns = field.shape === "scalar" ? "String" : field.returns;
      body.push(`  ${field.name}${args}: ${returns}`);
    }
    if (body.length) emitted.push({ name: type.name, body });
  }

  const declared = new Set(emitted.map((type) => type.name));
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
    "type Query {",
    `  ${observed.rootField}: ${observed.rootType}`,
    "}",
    "",
    ...emitted.flatMap((type) => [`type ${type.name} {`, ...type.body, "}", ""]),
  ];
  const sdl = `${lines.join("\n").replace(/\n{3,}/g, "\n\n").trimEnd()}\n`;

  const referenced = [...sdl.matchAll(/:\s*([A-Za-z_][A-Za-z0-9_]*)!?$/gm)].map((match) => match[1]);
  const definedNames = new Set([
    ...[...sdl.matchAll(/^type ([A-Za-z_][A-Za-z0-9_]*) \{/gm)].map((match) => match[1]),
    ...[...sdl.matchAll(/^scalar ([A-Za-z_][A-Za-z0-9_]*)$/gm)].map((match) => match[1]),
    ...BUILT_IN,
  ]);
  const dangling = [...new Set(referenced)].filter((name) => !definedNames.has(name)).sort();

  return {
    sdl,
    omitted: omitted.sort((left, right) =>
      `${left.type}.${left.field}`.localeCompare(`${right.type}.${right.field}`),
    ),
    dangling,
    declaredTypes: [...definedNames].sort(),
  };
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

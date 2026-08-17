#!/usr/bin/env node
// Ties three statements about Conduit-minted values together so none can move
// without the others.
//
// A coordinate serving a value Conduit constructed has to say three things, in
// three places, and they are three places because each is read by someone
// different:
//
//   1. The served SDL says valueTier: CONDUIT_NATIVE, which is the only one a
//      GraphQL client ever sees.
//   2. conduit-minted-values.json says which coordinates those are and what
//      part of the value is Conduit's, which is what the generator reads.
//   3. conduit-inventions.json names the invention and the evidence that would
//      displace it, which is what a contributor reads before changing it.
//
// Nothing kept them together. Before ADR 0007 there was no (1) at all, and
// ApolloEnvironment.rid shipped a synthesized locator under a bare OBSERVED
// annotation -- telling every client an Apollo Hub had answered with a string
// Conduit had computed from a digest. The annotation was not wrong about the
// coordinate; it was silent about the value, and silence read as recovery.
//
// So this fails if any one of the three moves alone. It also refuses an entry
// for a coordinate the recovery record does not contain, because this file
// describes how a RECOVERED coordinate's value is produced and must never be a
// back door for adding one.
//
// Runs offline against committed files and needs no credentials.
import { existsSync, readFileSync } from "node:fs";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(HERE, "..");

const SDL = resolve(ROOT, "internal/apollographql/schema/apollo.interface.graphql");
const MINTED = resolve(ROOT, "scripts/apollo-graphql/conduit-minted-values.json");
const REGISTER = resolve(ROOT, "docs/evidence/conduit-inventions.json");
const OBSERVED = resolve(ROOT, "docs/apollo/observed/apollo-graphql-observed.json");

const failures = [];
const fail = (message) => failures.push(message);

for (const path of [SDL, MINTED, REGISTER, OBSERVED]) {
  if (!existsSync(path)) {
    console.error(`FAIL missing ${relative(ROOT, path)}`);
    process.exit(1);
  }
}

const minted = JSON.parse(readFileSync(MINTED, "utf8")).coordinates ?? {};
const register = JSON.parse(readFileSync(REGISTER, "utf8"));
const registered = new Set((register.inventions ?? []).map((entry) => entry.id));

// 1. What the SDL actually discloses. Read from the served file rather than
// rebuilt, so a hand-edit to the committed schema is caught rather than
// silently regenerated away.
const declared = new Set();
const disclosed = new Set();
let currentType = "";
for (const line of readFileSync(SDL, "utf8").split("\n")) {
  const header = line.match(/^type ([A-Za-z_][A-Za-z0-9_]*) \{$/);
  if (header) {
    currentType = header[1];
    continue;
  }
  if (line === "}") {
    currentType = "";
    continue;
  }
  if (!currentType) continue;
  const field = line.match(/^ {2}([a-zA-Z_][a-zA-Z0-9_]*)[^@]*@provenance\(([^)]*)\)$/);
  if (!field) continue;
  const coordinate = `${currentType}.${field[1]}`;
  declared.add(coordinate);
  if (/valueTier:\s*CONDUIT_NATIVE/.test(field[2])) disclosed.add(coordinate);
}
if (declared.size === 0) fail(`${relative(ROOT, SDL)} parsed to no fields at all; the gate is checking nothing`);

// 2. The SDL and the input file must agree exactly. Either direction failing
// means a client is being told something the repository does not believe.
for (const coordinate of disclosed) {
  if (!Object.hasOwn(minted, coordinate)) {
    fail(
      `${coordinate} discloses valueTier: CONDUIT_NATIVE in the served schema, but ` +
        `${relative(ROOT, MINTED)} does not list it. Either the value is not minted and the ` +
        `annotation overstates it, or it is and nobody said what part is Conduit's`,
    );
  }
}
for (const coordinate of Object.keys(minted)) {
  if (!disclosed.has(coordinate)) {
    fail(
      `${relative(ROOT, MINTED)} lists ${coordinate}, but the served schema does not disclose it. ` +
        `Re-run build-interface-schema.mjs -- until then clients are told a Hub answered with a value Conduit built`,
    );
  }
}

// 3. Every entry has to say what part is Conduit's and name a registered
// invention. "Conduit mints this" without saying which segment is guessed is
// the kind of disclosure that satisfies a gate and informs nobody.
const observed = JSON.parse(readFileSync(OBSERVED, "utf8"));
const recovered = new Set();
for (const type of observed.types ?? []) {
  for (const field of type.fields ?? []) recovered.add(`${type.name}.${field.name}`);
}
for (const [coordinate, entry] of Object.entries(minted)) {
  for (const field of ["mintedBy", "grammarPublishedAs", "conduitSupplies"]) {
    if (!entry[field] || String(entry[field]).trim().length < 8) {
      fail(`${coordinate} has no meaningful ${field} in ${relative(ROOT, MINTED)}`);
    }
  }
  // `invention` is optional, and that is a correction rather than a loosening.
  // "Conduit built this value" and "Conduit invented this shape" are different
  // claims: the first is true of everything in this file and is what valueTier
  // discloses; the second is stronger and belongs in the register. Requiring
  // both collapsed them, and when ApolloEntity.id's invention turned out to be a
  // misreading -- the shape was Apollo's published one all along -- the rule
  // would have demanded a replacement guess instead of allowing the honest
  // answer, which was that nothing invented remained.
  //
  // What is still required is that a named invention is a real one.
  if (entry.invention && !registered.has(entry.invention)) {
    fail(
      `${coordinate} names invention ${entry.invention}, which ${relative(ROOT, REGISTER)} does not record. ` +
        `Disclosing a value as Conduit's without registering what would displace it leaves the guess permanent`,
    );
  }
  // The recovery record is the authority on what was recovered. An entry naming
  // a coordinate outside it would be describing the value of a field no Hub
  // established, which is a CONDUIT_NATIVE tier rather than a minted value.
  if (recovered.size > 0 && !recovered.has(coordinate)) {
    fail(
      `${relative(ROOT, MINTED)} names ${coordinate}, which the recovery record does not contain. ` +
        `This file describes how a RECOVERED coordinate's value is produced; a coordinate no Hub ` +
        `established belongs at tier CONDUIT_NATIVE instead`,
    );
  }
}

if (failures.length) {
  for (const failure of failures) console.error(`FAIL ${failure}`);
  process.exit(1);
}
const named = Object.values(minted).filter((entry) => entry.invention).length;
console.log(
  `PASS minted values: ${disclosed.size} coordinate(s) disclose a Conduit-built value, ` +
    `each saying what part is Conduit's; ${named} of them additionally name a registered invention`,
);

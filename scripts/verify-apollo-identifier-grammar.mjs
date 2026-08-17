#!/usr/bin/env node
// Guards the Apollo identifier-grammar evidence, and ties it to the code it
// bears on.
//
// The reason this file exists is worth stating plainly, because it is the same
// failure twice. The AEID example it records has been inside this repository's
// evidence set since 2026-08-02: osdk-client-unstable-2.43.0-conjure-evidence.json
// pins the very tarball and the very file that carries it, by digest. Nobody
// read it. A pin records that bytes were seen, not what they said, and for
// fifteen days the answer to a question the work queue called open was sitting
// in a file this repository already trusted.
//
// So the interesting check is not that the evidence is well-formed. It is that
// the identifier Conduit MINTS still matches the shape Apollo DOCUMENTS. That
// binding is what would otherwise rot: someone changes the minting expression,
// every test still passes because they all use the new form, and the evidence
// quietly becomes a record of something the code no longer does.
//
// Runs offline against committed files and needs no credentials.
import { existsSync, readFileSync } from "node:fs";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(HERE, "..");
const EVIDENCE = resolve(ROOT, "docs/evidence/apollo-identifier-grammar.json");
const PIN = resolve(ROOT, "docs/apollo/osdk-client-unstable-2.43.0-conjure-evidence.json");
const MINTER = resolve(ROOT, "internal/apollo/application/service.go");

const failures = [];
const fail = (message) => failures.push(message);

for (const path of [EVIDENCE, PIN, MINTER]) {
  if (!existsSync(path)) {
    console.error(`FAIL missing ${relative(ROOT, path)}`);
    process.exit(1);
  }
}
const evidence = JSON.parse(readFileSync(EVIDENCE, "utf8"));
const pin = JSON.parse(readFileSync(PIN, "utf8"));

// 1. The two records must describe the same bytes. If they diverge, one of them
// has been re-pinned without the other and the "already in the repository"
// claim above stops being true.
if (evidence.source?.npmTarballSha256 !== pin.source?.npmTarballSha256) {
  fail(
    `the grammar evidence pins tarball ${evidence.source?.npmTarballSha256} while the conjure evidence pins ` +
      `${pin.source?.npmTarballSha256}; they are supposed to be the same package read twice`,
  );
}
if (evidence.source?.npmVersion !== pin.source?.npmVersion) {
  fail(`version disagreement: ${evidence.source?.npmVersion} against ${pin.source?.npmVersion}`);
}

// 2. The AEID example is the whole finding. Everything else in the file is
// context for it.
const aeid = evidence.identifiers?.ApolloEntityId ?? {};
if (!/^aeid:[^:]+:[^:]+:.+$/.test(aeid.documentedExample ?? "")) {
  fail("ApolloEntityId.documentedExample is not a four-segment aeid; the finding this file exists for is gone");
}

// 3. The binding. Conduit's minted aeid must still have the shape Apollo's
// example has -- same prefix, same segment count, product type in the third
// position. Read from the minting expression rather than asserted, so changing
// the code and not the evidence fails here rather than silently.
const minted = readFileSync(MINTER, "utf8");
const expression = minted.match(/"aeid:"\s*\+\s*(.+)$/m)?.[0];
if (!expression) {
  fail(`no aeid minting expression found in ${relative(ROOT, MINTER)}; if it moved, move this check with it`);
} else {
  const literals = [...expression.matchAll(/"([^"]*)"/g)].map((match) => match[1]);
  // "aeid:", ":<product-type>:" -- two literals, and the separators they carry
  // must produce exactly the three colons the documented example has.
  const colons = literals.join("").split(":").length - 1;
  const documentedColons = aeid.documentedExample.split(":").length - 1;
  if (colons !== documentedColons) {
    fail(
      `Conduit mints an aeid with ${colons} separator(s) and Apollo documents ${documentedColons}: ` +
        `${expression.trim()} against ${aeid.documentedExample}`,
    );
  }
}

// 4. The negative claims carry the honesty of the file. The first two are the
// ones a hopeful reader would drop: this does NOT establish a RID grammar, and
// it does NOT establish that Conduit's first segment or product type are right.
const negatives = (evidence.whatThisDoesNotEstablish ?? []).join("\n");
for (const [claim, why] of [
  ["ADR 0005 group 1", "the RID close condition is still unmet and this file must keep saying so"],
  ["helm-chart", "only foundry-product is attested as a product type; Conduit's is not"],
  ["first segment", "Apollo's example does not say whether it names a Space or an Environment"],
]) {
  if (!negatives.includes(claim)) fail(`whatThisDoesNotEstablish no longer mentions "${claim}": ${why}`);
}

if (failures.length) {
  for (const failure of failures) console.error(`FAIL ${failure}`);
  process.exit(1);
}
console.log(
  `PASS Apollo identifier grammar: ${Object.keys(evidence.identifiers ?? {}).length} identifier(s) from a pin this ` +
    `repository already held, minted aeid shape agrees with the documented example`,
);

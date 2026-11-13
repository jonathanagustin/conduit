#!/usr/bin/env node
// Fails when the console's remaining Hub contract drifts.
//
// The operator console used to render Hub status vocabularies (fleet rollout,
// plan status, notification types, and the rest) and this gate compared those
// tables to the Go constants. That console was replaced by the local packaging
// assistant. Those screens, and the tables that coloured them, are gone. The
// contract that is still shared is the tenant bootstrap: the schema version and
// the meta name are written in Go, injected into the shell, and parsed by the
// assistant before it renders. If those two strings diverge, the Hub serves a
// document the assistant refuses, which looks like a broken deployment.
//
// It checks exact equality of the two strings, in both directions of "the file
// changed". An invented version and a renamed meta are both drift.
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const read = (relative) => readFileSync(resolve(ROOT, relative), "utf8");

function capture(text, pattern, file) {
  const match = pattern.exec(text);
  if (!match) throw new Error(`could not read the contract string in ${file}`);
  return match[1];
}

const pairs = [
  {
    name: "bootstrap schema version",
    go: capture(read("internal/consoleui/bootstrap.go"), /BootstrapSchemaVersion\s*=\s*"([^"]+)"/, "internal/consoleui/bootstrap.go"),
    ui: capture(read("web/src/runtime/bootstrap.ts"), /BOOTSTRAP_SCHEMA_VERSION\s*=\s*"([^"]+)"/, "web/src/runtime/bootstrap.ts"),
  },
  {
    name: "bootstrap meta name",
    go: capture(read("internal/consoleui/bootstrap.go"), /BootstrapMetaName\s*=\s*"([^"]+)"/, "internal/consoleui/bootstrap.go"),
    ui: capture(read("web/src/runtime/bootstrap.ts"), /BOOTSTRAP_META_NAME\s*=\s*"([^"]+)"/, "web/src/runtime/bootstrap.ts"),
  },
];

const failures = [];
for (const pair of pairs) {
  if (pair.go !== pair.ui) {
    failures.push(`${pair.name}: Go says "${pair.go}" and the assistant says "${pair.ui}"`);
  }
}

if (failures.length) {
  for (const failure of failures) console.error(`FAIL console contract: ${failure}`);
  process.exit(1);
}
console.log(`PASS console contract: ${pairs.map((pair) => pair.name).join(", ")}`);

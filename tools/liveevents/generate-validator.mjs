import { createRequire } from "node:module";
import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const root = fileURLToPath(new URL("../../", import.meta.url));
const require = createRequire(path.join(root, "web/package.json"));
const Ajv = require("ajv/dist/2020.js").default;
const standalone = require("ajv/dist/standalone/index.js").default;
const schema = JSON.parse(readFileSync(path.join(root, "api/live-events/event.schema.json"), "utf8"));
// Timestamp syntax/calendar validation lives in the bounded transport parser.
// All schema structure, enum and byte-independent bounds come from this source.
const ajv = new Ajv({ strict: false, validateFormats: false, allErrors: false, code: { source: true, esm: true } });
ajv.addSchema(schema);
let code = standalone(ajv, { envelope: schema.$id });
// Same reviewed runtime helpers as the existing A2UI standalone validator.
code = code.replace(/const (func\d+) = require\("(ajv\/dist\/runtime\/(?:ucs2length|equal))"\)\.default;/gu,
  (_all, variable, module) => `import ${variable}Module from "${module}.js";const ${variable} = ${variable}Module.default ?? ${variable}Module;`);
if (code.includes("require(")) throw new Error("Unreviewed standalone runtime dependency");
code = "// Deterministic standalone validator for api/live-events/event.schema.json.\n" + code + "\n";
const target = path.join(root, "web/src/api/live-event-validator.generated.js");
if (process.argv.includes("--check")) {
  if (readFileSync(target, "utf8") !== code) throw new Error("Live event validator drift: run node tools/liveevents/generate-validator.mjs");
} else writeFileSync(target, code);

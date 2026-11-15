import { createRequire } from "node:module";
import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";
const root = fileURLToPath(new URL("../../", import.meta.url));
const require = createRequire(path.join(root, "web/package.json"));
const Ajv = require("ajv/dist/2020.js").default;
const standalone = require("ajv/dist/standalone/index.js").default;
const ajv = new Ajv({
  strict: false,
  validateFormats: false,
  code: { source: true, esm: true },
  allErrors: false,
});
const base = "https://a2ui.org/specification/v1_0/";
for (const name of readdirSync(path.join(root, "api/a2ui/upstream"))
  .filter((name) => name.endsWith(".json") && name !== "manifest.json")
  .sort()) {
  ajv.addSchema(
    JSON.parse(
      readFileSync(path.join(root, "api/a2ui/upstream", name), "utf8"),
    ),
    base + name,
  );
}
const catalog = JSON.parse(
  readFileSync(path.join(root, "api/a2ui/catalog.json"), "utf8"),
);
ajv.getSchema(base + "catalog_definition.json")(catalog) ||
  (() => {
    throw new Error("Invalid owned A2UI catalog");
  })();
ajv.addSchema(catalog, base + "catalog.json");
let code = standalone(ajv, {
  agent: base + "agent_to_renderer.json",
  renderer: base + "renderer_to_agent.json",
});
// AJV emits CJS references to its tiny runtime helpers even in ESM mode.
// Convert only the known immutable helpers; reject any new executable import.
code = code.replace(
  /const (func\d+) = require\("(ajv\/dist\/runtime\/(?:ucs2length|equal))"\)\.default;/gu,
  (_all, variable, module) =>
    `import ${variable}Module from "${module}.js";const ${variable} = ${variable}Module.default ?? ${variable}Module;`,
);
if (code.includes("require("))
  throw new Error("Unreviewed standalone runtime dependency");
const target = path.join(
  root,
  "web/src/console/operator/a2ui-validator.generated.js",
);
code =
  "// Deterministic standalone validators for the retained schemas and owned catalog.\n" +
  code +
  "\n";
if (process.argv.includes("--check")) {
  if (readFileSync(target, "utf8") !== code)
    throw new Error(
      "A2UI validator drift: run node tools/a2ui/generate-validator.mjs",
    );
} else writeFileSync(target, code);

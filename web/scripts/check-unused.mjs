// Check the browser, bridge, tooling and test import graph without treating a
// file as used merely because another unreachable file imports it.
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, extname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { parse } from "@babel/parser";

const root = resolve(fileURLToPath(new URL("..", import.meta.url)));
const extensions = [".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"];
function walk(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name);
    return entry.isDirectory() ? walk(path) : [path];
  });
}
const files = ["src", "components", "hooks", "lib", "scripts"]
  .flatMap((directory) => walk(join(root, directory)))
  .filter(
    (file) => extensions.includes(extname(file)) && !file.endsWith(".d.ts"),
  );
const configs = readdirSync(root)
  .filter((file) => /\.config\.[cm]?[jt]s$/.test(file))
  .map((file) => join(root, file));
const roots = [
  "src/embed/main.tsx", // index.html browser entry (includes development gallery)
  "src/server/main.ts", // Hub-spawned loopback ACP bridge
  "src/server/dev.mjs", // package.json development launcher
  ...files
    .map((file) => relative(root, file))
    .filter(
      (file) => file.startsWith("src/test/") || file.startsWith("scripts/"),
    ),
].map((file) => join(root, file));
const seen = new Set();
function localPath(specifier, importer) {
  if (specifier === "@conduit/ontology-client")
    return join(root, "src/api/ontology-client/index.ts");
  if (!specifier.startsWith(".") && !specifier.startsWith("@/")) return;
  const base = specifier.startsWith("@/")
    ? join(root, specifier.slice(2))
    : resolve(dirname(importer), specifier);
  const bases = [
    base,
    base.replace(/\.[cm]?js$/, ".ts"),
    base.replace(/\.[cm]?js$/, ".tsx"),
  ];
  return bases
    .flatMap((path) => [
      path,
      ...extensions.map((extension) => path + extension),
      ...extensions.map((extension) => join(path, "index" + extension)),
    ])
    .find((path) => existsSync(path) && statSync(path).isFile());
}
function visit(file) {
  if (seen.has(file)) return;
  seen.add(file);
  if (!extensions.includes(extname(file))) return;
  const source = parse(readFileSync(file, "utf8"), {
    sourceType: "unambiguous",
    plugins: ["typescript", "jsx"],
  });
  function nodeVisit(node) {
    if (!node || typeof node !== "object") return;
    let argument;
    if (
      [
        "ImportDeclaration",
        "ExportNamedDeclaration",
        "ExportAllDeclaration",
      ].includes(node.type)
    )
      argument = node.source;
    if (node.type === "ImportExpression") argument = node.source;
    if (
      node.type === "CallExpression" &&
      (node.callee.type === "Import" || node.callee.name === "require")
    )
      argument = node.arguments[0];
    if (node.type === "NewExpression" && node.callee.name === "URL")
      argument = node.arguments[0];
    if (argument?.type === "StringLiteral") {
      const target = localPath(argument.value, file);
      if (target) visit(target);
    }
    for (const child of Object.values(node)) {
      if (Array.isArray(child)) child.forEach(nodeVisit);
      else if (child?.type) nodeVisit(child);
    }
  }
  nodeVisit(source);
}
[...roots, ...configs].forEach(visit);
// Generated transport contracts remain complete even before every RPC is mounted.
// Their dedicated generation drift checks own src/api/gen.
const unused = files.filter(
  (file) => !seen.has(file) && !relative(root, file).startsWith("src/api/gen/"),
);
if (unused.length) {
  console.error(
    "Unused source modules (connect them to a real entry or remove them):\n" +
      unused.map((file) => relative(root, file)).join("\n"),
  );
  process.exitCode = 1;
} else
  console.log(
    `Source import graph: ${files.length} modules checked against declared entries, tests and generated contracts`,
  );

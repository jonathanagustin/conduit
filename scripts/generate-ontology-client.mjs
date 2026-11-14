#!/usr/bin/env node
// Generates web/src/api/ontology-client/generated.ts from the ontology metadata fixture.
//
// Usage: node scripts/generate-ontology-client.mjs [--check]
//   --check  exit 1 if the file on disk differs from the generated output.

import { readFileSync, writeFileSync, mkdirSync, existsSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HEADER =
  "// Written by scripts/generate-ontology-client.mjs from testdata/ontology/full-metadata.json. Do not edit.";

const IDENTIFIER = /^[A-Za-z_$][A-Za-z0-9_$]*$/;

const str = (value) => JSON.stringify(String(value));
const sortedKeys = (obj) => Object.keys(obj ?? {}).sort();
const key = (name) => (IDENTIFIER.test(name) ? name : str(name));
const upperFirst = (name) => name.charAt(0).toUpperCase() + name.slice(1);
const stringTuple = (values) => `[${values.map(str).join(", ")}] as const`;

function tsType(dataType) {
  switch (dataType?.type) {
    case "string":
      return "string";
    case "integer":
    case "long":
    case "double":
    case "float":
      return "number";
    case "boolean":
      return "boolean";
    default:
      return "string";
  }
}

function sortedLinks(entry) {
  return [...(entry.linkTypes ?? [])].sort((a, b) =>
    a.apiName < b.apiName ? -1 : a.apiName > b.apiName ? 1 : 0,
  );
}

// The Hub reports the owning type in a link's objectTypeApiName, not the type on
// the other side. The target is the other object type that has a link with the
// same linkTypeRid. If no other side is found, fall back to objectTypeApiName.
function linkTargetResolver(objectTypes) {
  const owners = new Map();
  for (const name of sortedKeys(objectTypes)) {
    for (const link of objectTypes[name].linkTypes ?? []) {
      if (!owners.has(link.linkTypeRid)) owners.set(link.linkTypeRid, []);
      owners.get(link.linkTypeRid).push(name);
    }
  }
  return (owner, link) => {
    const other = (owners.get(link.linkTypeRid) ?? []).find((name) => name !== owner);
    return other ?? link.objectTypeApiName;
  };
}

function objectInterface(name, entry) {
  const props = entry.objectType.properties ?? {};
  const lines = [
    `export interface ${name} {`,
    `  __apiName: ${str(name)};`,
    "  __primaryKey: string;",
    "  __title?: string;",
  ];
  for (const prop of sortedKeys(props)) {
    const display = props[prop].displayName ?? prop;
    lines.push(`  /** ${String(display).replace(/\*\//g, "*\\/")} */`);
    lines.push(`  ${key(prop)}?: ${tsType(props[prop].dataType)} | null;`);
  }
  lines.push("}");
  return lines.join("\n");
}

function objectTypesConst(objectTypes) {
  const targetOf = linkTargetResolver(objectTypes);
  const lines = ["export const OBJECT_TYPES = {"];
  for (const name of sortedKeys(objectTypes)) {
    const entry = objectTypes[name];
    const ot = entry.objectType;
    const links = sortedLinks(entry);
    lines.push(`  ${key(name)}: {`);
    lines.push(`    apiName: ${str(ot.apiName)},`);
    lines.push(`    displayName: ${str(ot.displayName)},`);
    lines.push(`    pluralDisplayName: ${str(ot.pluralDisplayName)},`);
    lines.push(`    primaryKey: ${str(ot.primaryKey)},`);
    lines.push(`    titleProperty: ${str(ot.titleProperty)},`);
    lines.push(`    properties: ${stringTuple(sortedKeys(ot.properties))},`);
    if (links.length === 0) {
      lines.push("    links: {},");
    } else {
      lines.push("    links: {");
      for (const link of links) {
        lines.push(
          `      ${key(link.apiName)}: { target: ${str(targetOf(name, link))}, cardinality: ${str(link.cardinality)} },`,
        );
      }
      lines.push("    },");
    }
    lines.push("  },");
  }
  lines.push("} as const;");
  return lines.join("\n");
}

function linkTargetsInterface(objectTypes) {
  const targetOf = linkTargetResolver(objectTypes);
  const lines = ["export interface LinkTargets {"];
  for (const name of sortedKeys(objectTypes)) {
    const links = sortedLinks(objectTypes[name]);
    if (links.length === 0) {
      lines.push(`  ${key(name)}: {};`);
      continue;
    }
    lines.push(`  ${key(name)}: {`);
    for (const link of links) {
      lines.push(`    ${key(link.apiName)}: ${str(targetOf(name, link))};`);
    }
    lines.push("  };");
  }
  lines.push("}");
  return lines.join("\n");
}

function paramsInterface(name, action) {
  const params = action.parameters ?? {};
  const lines = [`export interface ${upperFirst(name)}Params {`];
  for (const param of sortedKeys(params)) {
    const optional = params[param].required ? "" : "?";
    lines.push(`  ${key(param)}${optional}: ${tsType(params[param].dataType)};`);
  }
  lines.push("}");
  return lines.join("\n");
}

function actionTypesConst(actionTypes) {
  const lines = ["export const ACTION_TYPES = {"];
  for (const name of sortedKeys(actionTypes)) {
    const action = actionTypes[name];
    const params = action.parameters ?? {};
    const required = sortedKeys(params).filter((p) => params[p].required);
    lines.push(`  ${key(name)}: {`);
    lines.push(`    apiName: ${str(action.apiName)},`);
    lines.push(`    displayName: ${str(action.displayName)},`);
    lines.push(`    required: ${stringTuple(required)},`);
    lines.push("  },");
  }
  lines.push("} as const;");
  return lines.join("\n");
}

export function generate(metadata) {
  const objectTypes = metadata.objectTypes ?? {};
  const actionTypes = metadata.actionTypes ?? {};
  const objectNames = sortedKeys(objectTypes);
  const actionNames = sortedKeys(actionTypes);

  const blocks = [HEADER, `export const ONTOLOGY = ${str(metadata.ontology.apiName)} as const;`];

  for (const name of objectNames) {
    blocks.push(objectInterface(name, objectTypes[name]));
  }

  blocks.push(
    [
      "export interface ObjectTypes {",
      ...objectNames.map((name) => `  ${key(name)}: ${name};`),
      "}",
      "",
      "export type ObjectTypeName = keyof ObjectTypes;",
    ].join("\n"),
  );

  blocks.push(objectTypesConst(objectTypes));
  blocks.push(linkTargetsInterface(objectTypes));

  for (const name of actionNames) {
    blocks.push(paramsInterface(name, actionTypes[name]));
  }

  blocks.push(
    [
      "export interface ActionParams {",
      ...actionNames.map((name) => `  ${key(name)}: ${upperFirst(name)}Params;`),
      "}",
      "",
      "export type ActionName = keyof ActionParams;",
    ].join("\n"),
  );

  blocks.push(actionTypesConst(actionTypes));

  return blocks.join("\n\n") + "\n";
}

function main() {
  const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
  const input = resolve(root, "testdata/ontology/full-metadata.json");
  const output = resolve(root, "web/src/api/ontology-client/generated.ts");
  const metadata = JSON.parse(readFileSync(input, "utf8"));
  const next = generate(metadata);

  if (process.argv.includes("--check")) {
    const current = existsSync(output) ? readFileSync(output, "utf8") : null;
    if (current !== next) {
      console.error(
        "web/src/api/ontology-client/generated.ts is out of date. Run: node scripts/generate-ontology-client.mjs",
      );
      process.exit(1);
    }
    return;
  }

  mkdirSync(dirname(output), { recursive: true });
  writeFileSync(output, next);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main();
}

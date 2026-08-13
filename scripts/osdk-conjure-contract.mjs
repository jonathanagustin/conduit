#!/usr/bin/env node
// Derive a bounded Conjure IR contract from Palantir's generated OSDK TypeScript.
// This is intentionally a type-only converter: services, auth, errors, and
// private deployment semantics are outside Platform's Foundry adapter boundary.
import { readFileSync, readdirSync, statSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const [typescriptPath, generatedDir, selectionPath, outputPath] = process.argv.slice(2);
if (!typescriptPath || !generatedDir || !selectionPath || !outputPath) {
  throw new Error("usage: osdk-conjure-contract.mjs <typescript.js> <generated-dir> <selection.json> <output.json>");
}

const imported = await import(pathToFileURL(resolve(typescriptPath)).href);
const ts = imported.default ?? imported;
const selection = JSON.parse(readFileSync(selectionPath, "utf8"));
const packageName = selection.conjurePackage;
const opaque = new Set(selection.opaqueTypes ?? []);
const rootNames = new Set(selection.rootTypes ?? []);
const files = [];
(function walk(dir) {
  for (const entry of readdirSync(dir).sort()) {
    const path = resolve(dir, entry);
    if (statSync(path).isDirectory()) walk(path);
    else if (entry.endsWith(".ts") && !entry.endsWith(".d.ts")) files.push(path);
  }
})(resolve(generatedDir));

const program = ts.createProgram(files, {
  allowJs: false,
  module: ts.ModuleKind.NodeNext,
  moduleResolution: ts.ModuleResolutionKind.NodeNext,
  noEmit: true,
  skipLibCheck: true,
  target: ts.ScriptTarget.ES2022,
});

const declarationsByFile = new Map();
const declarationsByName = new Map();
const importsByFile = new Map();
const sourceByDeclaration = new Map();
for (const source of program.getSourceFiles()) {
  if (!files.includes(source.fileName)) continue;
  const local = new Map();
  const imports = new Map();
  source.forEachChild((node) => {
    if (ts.isImportDeclaration(node) && node.importClause?.namedBindings && ts.isNamedImports(node.importClause.namedBindings)) {
      let target = resolve(dirname(source.fileName), node.moduleSpecifier.text);
      target = target.replace(/\.js$/, ".ts");
      for (const element of node.importClause.namedBindings.elements) {
        imports.set(element.name.text, { imported: element.propertyName?.text ?? element.name.text, target });
      }
      return;
    }
    const exported = node.modifiers?.some((modifier) => modifier.kind === ts.SyntaxKind.ExportKeyword);
    if (!exported || (!ts.isInterfaceDeclaration(node) && !ts.isTypeAliasDeclaration(node))) return;
    local.set(node.name.text, node);
    sourceByDeclaration.set(node, source);
    const candidates = declarationsByName.get(node.name.text) ?? [];
    candidates.push(node);
    declarationsByName.set(node.name.text, candidates);
  });
  declarationsByFile.set(source.fileName, local);
  importsByFile.set(source.fileName, imports);
}

function resolveDeclaration(name, source) {
  const imported = importsByFile.get(source.fileName)?.get(name);
  if (imported) {
    const found = declarationsByFile.get(imported.target)?.get(imported.imported);
    if (!found) throw new Error(`cannot resolve imported type ${name} from ${source.fileName}`);
    return found;
  }
  const local = declarationsByFile.get(source.fileName)?.get(name);
  if (local) return local;
  const candidates = declarationsByName.get(name) ?? [];
  if (candidates.length !== 1) {
    throw new Error(`type ${name} from ${source.fileName} has ${candidates.length} global candidates`);
  }
  return candidates[0];
}

function rootDeclaration(name) {
  const candidates = declarationsByName.get(name) ?? [];
  if (candidates.length !== 1) throw new Error(`root type ${name} has ${candidates.length} candidates`);
  return candidates[0];
}

function declarationSource(declaration) {
  const source = sourceByDeclaration.get(declaration);
  if (!source) throw new Error(`source file is unavailable for ${declaration.name?.text ?? "unknown declaration"}`);
  return source;
}

const ANY = { type: "primitive", primitive: "ANY" };
const primitive = (name) => ({ type: "primitive", primitive: name });
const reference = (name) => ({ type: "reference", reference: { name, package: packageName } });
const optional = (itemType) => ({ type: "optional", optional: { itemType } });
const list = (itemType) => ({ type: "list", list: { itemType } });
const map = (keyType, valueType) => ({ type: "map", map: { keyType, valueType } });
const emitted = new Map();
const queued = [];

function queue(declaration) {
  const name = declaration.name.text;
  if (!emitted.has(name)) queued.push(declaration);
}

function nonNullUnionParts(node) {
  if (!ts.isUnionTypeNode(node)) return { parts: [node], nullable: false };
  const parts = node.types.filter((part) => ![
    ts.SyntaxKind.NullKeyword,
    ts.SyntaxKind.UndefinedKeyword,
  ].includes(part.kind) && !(ts.isLiteralTypeNode(part) && part.literal.kind === ts.SyntaxKind.NullKeyword));
  return { parts, nullable: parts.length !== node.types.length };
}

function typeName(node) {
  return ts.isIdentifier(node.typeName) ? node.typeName.text : node.typeName.getText();
}

function propertyName(node, source) {
  if (ts.isIdentifier(node) || ts.isStringLiteral(node) || ts.isNumericLiteral(node)) return node.text;
  return node.getText(source);
}

function convertType(node, source) {
  if (!node) return ANY;
  const { parts, nullable } = nonNullUnionParts(node);
  if (parts.length === 1 && (nullable || parts[0] !== node)) {
    return optional(convertType(parts[0], source));
  }
  if (parts.length > 1) return ANY;
  switch (node.kind) {
    case ts.SyntaxKind.StringKeyword: return primitive("STRING");
    case ts.SyntaxKind.BooleanKeyword: return primitive("BOOLEAN");
    case ts.SyntaxKind.NumberKeyword: return primitive("DOUBLE");
    case ts.SyntaxKind.AnyKeyword:
    case ts.SyntaxKind.UnknownKeyword:
    case ts.SyntaxKind.ObjectKeyword: return ANY;
    case ts.SyntaxKind.ArrayType: return list(convertType(node.elementType, source));
    case ts.SyntaxKind.ParenthesizedType: return convertType(node.type, source);
    case ts.SyntaxKind.LiteralType:
      if (ts.isStringLiteral(node.literal)) return primitive("STRING");
      if (node.literal.kind === ts.SyntaxKind.TrueKeyword || node.literal.kind === ts.SyntaxKind.FalseKeyword) return primitive("BOOLEAN");
      return ANY;
    case ts.SyntaxKind.TypeReference: {
      const name = typeName(node);
      if (name === "Array" || name === "ReadonlyArray") return list(convertType(node.typeArguments?.[0], source));
      if (name === "Record" || name === "Map" || name === "ReadonlyMap") {
        return map(convertType(node.typeArguments?.[0], source), convertType(node.typeArguments?.[1], source));
      }
      const declaration = resolveDeclaration(name, source);
      const canonicalName = declaration.name.text;
      if (!opaque.has(canonicalName)) queue(declaration);
      return opaque.has(canonicalName) ? ANY : reference(canonicalName);
    }
    default: return ANY;
  }
}

function stringEnum(node) {
  const { parts, nullable } = nonNullUnionParts(node);
  if (nullable || parts.length === 0 || !parts.every((part) => ts.isLiteralTypeNode(part) && ts.isStringLiteral(part.literal))) return null;
  return parts.map((part) => part.literal.text);
}

function unionVariants(node, source) {
  const { parts, nullable } = nonNullUnionParts(node);
  if (nullable || parts.length === 0 || !parts.every(ts.isTypeReferenceNode)) return null;
  const variants = [];
  for (const part of parts) {
    const declaration = resolveDeclaration(typeName(part), source);
    if (!ts.isInterfaceDeclaration(declaration)) return null;
    const properties = declaration.members.filter(ts.isPropertySignature);
    const sourceFile = declarationSource(declaration);
    const discriminator = properties.find((property) => property.name && propertyName(property.name, sourceFile) === "type");
    if (!discriminator?.type || !ts.isLiteralTypeNode(discriminator.type) || !ts.isStringLiteral(discriminator.type.literal)) return null;
    const variantName = discriminator.type.literal.text;
    const payload = properties.find((property) => property.name && propertyName(property.name, sourceFile) === variantName);
    if (!payload?.type) return null;
    variants.push({ fieldName: variantName, type: convertType(payload.type, sourceFile) });
  }
  return variants;
}

function emit(declaration) {
  const name = declaration.name.text;
  const typeNameValue = { name, package: packageName };
  if (opaque.has(name)) {
    return { type: "alias", alias: { typeName: typeNameValue, alias: ANY } };
  }
  if (ts.isInterfaceDeclaration(declaration)) {
    const fields = [];
    const sourceFile = declarationSource(declaration);
    for (const member of declaration.members) {
      if (!ts.isPropertySignature(member) || !member.name) continue;
      const fieldName = propertyName(member.name, sourceFile);
      let fieldType = convertType(member.type, sourceFile);
      if (member.questionToken && fieldType.type !== "optional") fieldType = optional(fieldType);
      fields.push({ fieldName, type: fieldType });
    }
    return { type: "object", object: { typeName: typeNameValue, fields } };
  }
  const enumValues = stringEnum(declaration.type);
  if (enumValues) {
    return { type: "enum", enum: { typeName: typeNameValue, values: enumValues.map((value) => ({ value })) } };
  }
  const variants = unionVariants(declaration.type, declarationSource(declaration));
  if (variants) {
    return { type: "union", union: { typeName: typeNameValue, union: variants } };
  }
  return { type: "alias", alias: { typeName: typeNameValue, alias: convertType(declaration.type, declarationSource(declaration)) } };
}

for (const name of [...rootNames].sort()) queue(rootDeclaration(name));
while (queued.length) {
  const declaration = queued.shift();
  const name = declaration.name.text;
  if (emitted.has(name)) continue;
  emitted.set(name, emit(declaration));
}

const types = [...emitted.values()].sort((left, right) => {
  const name = (value) => value.object?.typeName.name ?? value.alias?.typeName.name ?? value.enum?.typeName.name ?? value.union?.typeName.name;
  return name(left).localeCompare(name(right));
});
const output = {
  version: 1,
  errors: [],
  types,
  services: [],
  extensions: {
    platform: {
      contract: selection.contract,
      source: selection.source,
      rootTypes: [...rootNames].sort(),
      opaqueTypes: [...opaque].sort(),
      javaInventory: selection.javaInventory,
    },
  },
};
writeFileSync(outputPath, `${JSON.stringify(output, null, 2)}\n`);
console.log(`wrote ${types.length} bounded types from ${files.length} official generated TypeScript files`);

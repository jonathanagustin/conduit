#!/usr/bin/env node

import { readFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const platformRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const repositoryRoot = resolve(platformRoot, "..");
const inventoryPath = resolve(platformRoot, "docs/ui/evidence/APOLLO-ROSETTA-FEATURE-INVENTORY.json");
const catalogPath = resolve(repositoryRoot, "mcp/foundry/evidence/ui/rosetta-media-catalog.json");
const inventory = JSON.parse(readFileSync(inventoryPath, "utf8"));
const catalogBytes = readFileSync(catalogPath);
const catalog = JSON.parse(catalogBytes);

if (inventory.schemaVersion !== 1) throw new Error("unsupported Apollo inventory schema");
const catalogSha256 = createHash("sha256").update(catalogBytes).digest("hex");
if (inventory.sourceCatalogSha256 !== catalogSha256) throw new Error("Apollo inventory source catalog digest drifted");
const expected = new Map(
  catalog.rows
    .filter(row => row.product === "APOLLO")
    .map(row => [row.sha256, row]),
);
const observed = new Map();
for (const item of inventory.items) {
  if (observed.has(item.sha256)) throw new Error(`duplicate inventory digest ${item.sha256}`);
  const row = expected.get(item.sha256);
  if (row === undefined) throw new Error(`inventory digest absent from Rosetta catalog: ${item.sha256}`);
  if (!row.path.endsWith(`/${item.filename}`)) throw new Error(`filename drift for ${item.sha256}`);
  if (row.noteDate !== item.noteDate) throw new Error(`note date drift for ${item.sha256}`);
  if (!item.surface || !Array.isArray(item.features) || item.features.length === 0 ||
      !Array.isArray(item.uiPatterns) || item.uiPatterns.length === 0) {
    throw new Error(`incomplete facts inventory for ${item.sha256}`);
  }
  observed.set(item.sha256, item);
}
const missing = [...expected.keys()].filter(digest => !observed.has(digest));
if (missing.length !== 0) throw new Error(`missing ${missing.length} Apollo catalog digest(s): ${missing.join(", ")}`);
console.log(`PASS Apollo Rosetta inventory: ${observed.size}/${expected.size} media rows enumerated`);

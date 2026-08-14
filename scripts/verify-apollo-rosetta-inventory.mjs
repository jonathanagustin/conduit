#!/usr/bin/env node

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const platformRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const inventoryPath = resolve(platformRoot, "docs/ui/evidence/APOLLO-ROSETTA-FEATURE-INVENTORY.json");
const catalogPath = resolve(platformRoot, "docs/ui/evidence/APOLLO-ROSETTA-SOURCE-PROJECTION.json");
const inventory = JSON.parse(readFileSync(inventoryPath, "utf8"));
const catalog = JSON.parse(readFileSync(catalogPath, "utf8"));

if (inventory.schemaVersion !== 1) throw new Error("unsupported Apollo inventory schema");
if (catalog.schemaVersion !== 1) throw new Error("unsupported Apollo source projection schema");
if (catalog.sourceCommit !== "945f7be95accde4053c77788cb90e687103ed640") throw new Error("Apollo source projection commit drifted");
if (inventory.sourceCatalogSha256 !== catalog.sourceCatalogSha256) throw new Error("Apollo inventory source catalog coordinate drifted");
const expected = new Map(
  catalog.rows.map(row => [row.sha256, row]),
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
console.log(`PASS Apollo Rosetta inventory: ${observed.size}/${expected.size} rows match the pinned source projection`);

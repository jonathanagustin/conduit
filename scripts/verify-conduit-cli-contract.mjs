#!/usr/bin/env node

import { readFile } from "node:fs/promises";

const contractPath = "docs/reference-platform/cli-command-contract.json";
const contract = JSON.parse(await readFile(contractPath, "utf8"));
const manifestPath = "docs/reference-platform/snapshots/cli-command-sources-2026-11-14-363.tsv";
if (contract.sourceManifest !== manifestPath || contract.captured !== "2026-11-14") {
  throw new Error("Apollo CLI compatibility profile must name its reviewed source snapshot");
}
const manifest = await readFile(manifestPath, "utf8");
const expected = new Map(
  manifest
    .trim()
    .split("\n")
    .map((line) => line.split("\t"))
    .filter((fields) => fields[1] === "apollo-cli")
    .map((fields) => [fields[0], fields[5]]),
);
if (contract.schemaVersion !== 1 || !Array.isArray(contract.commands)) {
  throw new Error("Apollo CLI command contract schema is invalid");
}
if (contract.sourceCount !== expected.size || contract.commands.length !== expected.size) {
  throw new Error(`Apollo CLI command coverage is ${contract.commands.length}/${expected.size}`);
}
const seen = new Set();
for (const command of contract.commands) {
  if (seen.has(command.source)) {
    throw new Error(`duplicate Apollo CLI source ${command.source}`);
  }
  seen.add(command.source);
  if (expected.get(command.source) !== command.sourceSha256) {
    throw new Error(`Apollo CLI source hash mismatch for ${command.source}`);
  }
  if (typeof command.command !== "string" || command.command === "" || !Array.isArray(command.flags)) {
    throw new Error(`invalid Apollo CLI command entry for ${command.source}`);
  }
}

const workflow = [
  "apollo-cli environment create",
  "apollo-cli module install [Experimental]",
  "apollo-cli environment manifest generate",
  "apollo-cli product-release helm-chart init",
  "apollo-cli product-release create",
  "apollo-cli entity install [Experimental]",
];
const names = new Set(contract.commands.map((entry) => entry.command));
for (const command of workflow) {
  if (!names.has(command)) {
    throw new Error(`end-to-end workflow command is missing: ${command}`);
  }
}
const inventory = JSON.parse(await readFile("docs/reference-platform/cli-command-inventory.json", "utf8"));
const liveManifest = await readFile("docs/reference-platform/public-documentation-manifest.tsv", "utf8");
const liveSources = new Map(liveManifest.trim().split("\n").slice(1)
  .map((line) => line.split("\t")).filter((fields) => fields[1] === "apollo-cli")
  .map((fields) => [fields[0], fields[5]]));
const liveSeen = new Set();
if (inventory.schemaVersion !== 1 || inventory.sourceCount !== liveSources.size ||
    inventory.commands.length !== liveSources.size) {
  throw new Error("Apollo CLI current inventory does not cover the live manifest");
}
for (const command of inventory.commands) {
  if (liveSeen.has(command.source) || liveSources.get(command.source) !== command.sourceSha256 ||
      typeof command.command !== "string" || command.command === "" || !Array.isArray(command.flags)) {
    throw new Error(`Apollo CLI current inventory source mismatch for ${command.source}`);
  }
  liveSeen.add(command.source);
}
const additions = inventory.commands.filter((command) => !seen.has(command.source));
console.log(`Conduit CLI reviewed profile verified: ${contract.commands.length}/${expected.size} pages and ${workflow.length}/6 workflow commands`);
console.log(`Apollo CLI current inventory verified: ${liveSeen.size}/${liveSources.size} pages; ${additions.length} outside the reviewed implementation profile`);


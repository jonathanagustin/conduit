#!/usr/bin/env node

import { readFile } from "node:fs/promises";

const manifestPath = "docs/apollo/public-documentation-manifest.tsv";
const contractPath = "docs/apollo/cli-command-contract.json";
const manifest = await readFile(manifestPath, "utf8");
const expected = new Map(
  manifest
    .trim()
    .split("\n")
    .map((line) => line.split("\t"))
    .filter((fields) => fields[1] === "apollo-cli")
    .map((fields) => [fields[0], fields[5]]),
);
const contract = JSON.parse(await readFile(contractPath, "utf8"));
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
console.log(`Conduit CLI source contract verified: ${contract.commands.length}/${expected.size} pages and ${workflow.length}/6 workflow commands`);

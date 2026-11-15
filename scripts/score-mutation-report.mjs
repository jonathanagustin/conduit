#!/usr/bin/env node
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { scoreReport } from "./mutation-report-score.mjs";
const [name, path] = process.argv.slice(2);
try {
  if (!/^internal\/[a-z]+\/domain$/.test(name ?? "") || !path) throw Error("package and engine report required");
  console.log(JSON.stringify(scoreReport(resolve(import.meta.dirname, ".."), name, JSON.parse(readFileSync(path, "utf8")))));
} catch (error) {
  console.error(`FAIL mutation report: ${error.message}`);
  process.exitCode = 1;
}

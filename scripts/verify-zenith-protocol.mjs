#!/usr/bin/env node

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const configPath = resolve(root, ".zenith-protocol.json");
const failures = [];
const passes = [];
const fail = (rule, detail) => failures.push({ rule, detail });
const pass = (rule, detail) => passes.push({ rule, detail });

if (!existsSync(configPath)) {
  fail("config", ".zenith-protocol.json is missing");
} else {
  const config = JSON.parse(readFileSync(configPath, "utf8"));
  const paths = ["capabilityMap", "backlog", "assumptions", "pdrs", "metricsDoc", "metricsLedger"];
  const missing = paths.filter((key) => !existsSync(resolve(root, config[key])));
  if (missing.length) fail("ledgers-exist", `missing configured ledger(s): ${missing.join(", ")}`);
  else pass("ledgers-exist", "all configured ledgers exist");

  const metricPath = resolve(root, config.metricsLedger);
  if (existsSync(metricPath)) {
    const required = ["id", "date", "commit", "category", "metric", "value", "tier"];
    const invalid = [];
    readFileSync(metricPath, "utf8").split("\n").forEach((line, index) => {
      if (!line.trim()) return;
      try {
        const row = JSON.parse(line);
        const absent = required.filter((field) => !(field in row));
        if (absent.length) invalid.push(`line ${index + 1}: missing ${absent.join(",")}`);
      } catch (error) {
        invalid.push(`line ${index + 1}: ${error.message}`);
      }
    });
    if (invalid.length) fail("metrics-valid", invalid.join("; "));
    else pass("metrics-valid", "all metric rows contain required fields");

    try {
      execFileSync("git", ["ls-files", "--error-unmatch", config.metricsLedger], { cwd: root, stdio: "ignore" });
      const patch = execFileSync("git", ["log", "-p", "--format=", "--", config.metricsLedger], { cwd: root, encoding: "utf8" });
      const removed = patch.split("\n").filter((line) => line.startsWith("-") && !line.startsWith("---") && line.slice(1).trim());
      if (removed.length) fail("metrics-append-only", `${removed.length} committed metric line removal(s) found`);
      else pass("metrics-append-only", "no committed metric row was removed");
    } catch {
      pass("metrics-append-only", "new ledger has no committed history yet");
    }
  }

  const pdrPath = resolve(root, config.pdrs);
  if (existsSync(pdrPath)) {
    const rows = readFileSync(pdrPath, "utf8").split("\n").filter((line) => /^\|\s*P\d+\b/.test(line));
    const invalid = rows.filter((line) => !/\|\s*(PROPOSED|TRIAL|ADOPTED|REJECTED|SUPERSEDED)\s*\|/.test(line));
    if (invalid.length) fail("pdr-status", `${invalid.length} PDR row(s) have an invalid status`);
    else pass("pdr-status", `${rows.length} PDR row(s) have valid status`);
  }

  const backlogPath = resolve(root, config.backlog);
  if (existsSync(backlogPath)) {
    const proposals = readFileSync(backlogPath, "utf8").split("\n").filter((line) => /^\|\s*PH-\d+\b/.test(line));
    const ungated = proposals.filter((line) => !/\b(ACCEPT|REJECT|NOTE|QUEUED)\b/.test(line));
    if (ungated.length) fail("hypothesis-intake", `${ungated.length} proposal(s) lack a verdict`);
    else pass("hypothesis-intake", `${proposals.length} proposal(s) are gatekept`);
  }

  const capabilityPath = resolve(root, config.capabilityMap);
  if (existsSync(capabilityPath)) {
    const open = readFileSync(capabilityPath, "utf8").split("\n").filter((line) => line.startsWith("|") && /\b(OPEN|BLOCKED)\b/.test(line));
    const vague = open.filter((line) => !/`[^`]+`|\b[\w./-]+\.(go|md|mjs|sh|yaml|yml)\b|\b(error|command|API|test|gate)\b/i.test(line));
    if (vague.length) fail("open-has-instrument", `${vague.length} OPEN/BLOCKED row(s) name no instrument`);
    else pass("open-has-instrument", `${open.length} OPEN/BLOCKED row(s) name an instrument`);
  }

}

for (const result of passes) console.log(`PASS ${result.rule}: ${result.detail}`);
for (const result of failures) console.error(`FAIL ${result.rule}: ${result.detail}`);
if (failures.length) process.exit(1);

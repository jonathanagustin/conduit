#!/usr/bin/env node

import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const verifier = join(dirname(fileURLToPath(import.meta.url)), "verify-evaluator-trial.mjs");
const root = mkdtempSync(join(tmpdir(), "zenith-evaluator-"));
const sha256 = (bytes) => createHash("sha256").update(bytes).digest("hex");
const stable = (value) => Array.isArray(value) ? `[${value.map(stable).join(",")}]` : value && typeof value === "object" ? `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${stable(value[key])}`).join(",")}}` : JSON.stringify(value);
const setIdentities = (trial) => {
  const identity = structuredClone(trial);
  identity.id = "";
  identity.resolutions = [];
  trial.id = `trial_${sha256(stable(identity))}`;
  for (const resolution of trial.resolutions) {
    resolution.id = "";
    resolution.id = `resolution_${sha256(stable({...structuredClone(resolution), id:"", trialId:trial.id}))}`;
  }
};
let passed = 0;
const check = (name, mutate, shouldPass, mutateAfterIdentity = () => {}) => {
  const fixture = join(root, name);
  mkdirSync(fixture, { recursive: true });
  writeFileSync(join(fixture, "prompt.txt"), "same prompt\n");
  writeFileSync(join(fixture, "a.json"), "{}\n");
  writeFileSync(join(fixture, "b.json"), "{}\n");
  const arm = (id, finding) => ({id,provider:"provider",modelFamily:id,model:id,modelVersion:"v1",invocationId:`i-${id}`,sessionId:`s-${id}`,inputAccessStatus:"COMPLETE",residualUnknowns:[],usage:{inputTokens:1,outputTokens:1,cacheReadTokens:0,cacheWriteTokens:0},providerDurationMilliseconds:1,wallDurationMilliseconds:2,costStatus:"UNAVAILABLE",costMicros:0,costUnavailableReason:"billing unavailable",receipt:{path:`${id}.json`,sha256:sha256(readFileSync(join(fixture, `${id}.json`)))},findings:[{id:finding,modelClusterKey:"raw",summary:"claim",evidence:["line"]}]});
  const digest = "a".repeat(64);
  const trial = {apiVersion:"zenith.agentdriver.dev/evaluator-trial/v1alpha1",kind:"EvaluatorTrial",id:"trial_x",sourceRevision:"commit",snapshot:{repository:"o/r",scope:"src",commit:"commit",tree:"tree",archiveSha256:digest,id:`snap_${digest}`,secretScanRevision:"sha256:pattern",secretMatches:0},prompt:{path:"prompt.txt",sha256:sha256(readFileSync(join(fixture,"prompt.txt")) )},arms:[arm("a","a-F1"),arm("b","b-F1")],dispositions:[{findingId:"a-F1",canonicalClusterKey:"same",status:"VERIFIED_DEFECT",verifier:"gate",sourceRevision:"fix",executableGate:"test a",verifiedAt:"2026-08-08T00:00:00Z",notes:"reproduced"},{findingId:"b-F1",canonicalClusterKey:"same",status:"UNRESOLVED",verifier:"gate",sourceRevision:"fix",verifiedAt:"2026-08-08T00:00:00Z",notes:"unknown"}],resolutions:[]};
  mutate(trial);
  setIdentities(trial);
  mutateAfterIdentity(trial);
  writeFileSync(join(fixture, "trial.json"), JSON.stringify(trial));
  let ok = true;
  let output = "";
  try { output = execFileSync("node", [verifier, "--json", join(fixture,"trial.json")], {encoding:"utf8", stdio:["ignore", "pipe", "ignore"]}); } catch { ok = false; }
  if (ok !== shouldPass) throw new Error(`${name}: expected pass=${shouldPass}, got ${ok}`);
  if (ok) {
    const report = JSON.parse(output);
    if (report.status !== "PASS" || report.summary.arms !== 2 || report.metrics.length !== 2) throw new Error(`${name}: invalid measured report`);
    if (name === "valid" && (report.summary.verifiedClusters !== 1 || report.summary.unresolvedFindings !== 1 || report.metrics[0].recall.denominator !== 1)) throw new Error(`${name}: incorrect base metrics`);
    if (name === "resolve-unknown" && (report.summary.verifiedFindings !== 2 || report.summary.unresolvedFindings !== 0 || report.metrics.some((metric) => metric.uniqueVerifiedClusters !== 0))) throw new Error(`${name}: resolution was not reflected in metrics`);
  }
  passed++;
};

try {
  check("valid", () => {}, true);
  check("receipt-tamper", (trial) => { trial.arms[0].receipt.sha256 = "b".repeat(64); }, false);
  check("receipt-escape", (trial) => { trial.arms[0].receipt.path = "../a.json"; }, false);
  check("missing-snapshot-source", (trial) => { trial.snapshot.repository = ""; }, false);
  check("empty-residual", (trial) => { trial.arms[0].residualUnknowns = [""]; }, false);
  check("aggregate-overflow", (trial) => { trial.arms[0].usage.inputTokens = Number.MAX_SAFE_INTEGER; trial.arms[1].usage.inputTokens = Number.MAX_SAFE_INTEGER; }, false);
  check("unknown-free", (trial) => { trial.arms[0].costUnavailableReason = ""; }, false);
  check("missing-gate", (trial) => { delete trial.dispositions[0].executableGate; }, false);
  const disposition = (findingId, status) => ({findingId,canonicalClusterKey:"same",status,verifier:"independent reviewer",sourceRevision:"fix2",verifiedAt:"2026-08-08T01:00:00Z",notes:"reproduced"});
  const resolution = (item) => ({id:"",sourceRevision:"fix2",resolvedAt:"2026-08-08T01:00:00Z",dispositions:[item]});
  check("rewrite-resolved", (trial) => { trial.resolutions.push(resolution(disposition("a-F1", "FALSE_POSITIVE"))); }, false);
  check("resolution-tamper", (trial) => { const item = disposition("b-F1", "FALSE_POSITIVE"); trial.resolutions.push(resolution(item)); }, false, (trial) => { trial.resolutions[0].id = `resolution_${"b".repeat(64)}`; });
  check("resolution-provenance", (trial) => { const item = disposition("b-F1", "FALSE_POSITIVE"); delete item.verifier; trial.resolutions.push(resolution(item)); }, false);
  check("resolve-unknown", (trial) => { const item = disposition("b-F1", "VERIFIED_DEFECT"); item.executableGate="test b"; trial.resolutions.push(resolution(item)); }, true);
  console.log(`PASS verify-evaluator-trial fixtures=${passed}`);
} finally {
  rmSync(root, {recursive:true, force:true});
}

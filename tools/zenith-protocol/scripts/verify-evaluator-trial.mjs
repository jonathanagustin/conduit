#!/usr/bin/env node

import { createHash } from "node:crypto";
import { readFileSync, statSync } from "node:fs";
import { dirname, isAbsolute, join, normalize, resolve, sep } from "node:path";

const arguments_ = process.argv.slice(2);
const jsonOutput = arguments_.includes("--json");
const manifestPath = resolve(arguments_.find((argument) => argument !== "--json") ?? "evaluator-trial.json");
const root = dirname(manifestPath);
const failures = [];
const sha256 = (bytes) => createHash("sha256").update(bytes).digest("hex");
const fail = (message) => failures.push(message);
const isDigest = (value) => typeof value === "string" && /^[0-9a-f]{64}$/.test(value);
const isStatus = (value, allowed) => allowed.includes(value);
const isTimestamp = (value) => typeof value === "string" && Number.isFinite(Date.parse(value));
const stable = (value) => Array.isArray(value)
  ? `[${value.map(stable).join(",")}]`
  : value && typeof value === "object"
    ? `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${stable(value[key])}`).join(",")}}`
    : JSON.stringify(value);
const safePath = (value) => {
  if (typeof value !== "string" || value === "" || isAbsolute(value)) return null;
  const candidate = resolve(root, normalize(value));
  return candidate === root || candidate.startsWith(`${root}${sep}`) ? candidate : null;
};
const isRegularFile = (path) => {
  try { return statSync(path).isFile(); } catch { return false; }
};

let trial;
try {
  trial = JSON.parse(readFileSync(manifestPath, "utf8"));
} catch (error) {
  console.error(`FAIL manifest: ${error.message}`);
  process.exit(1);
}

if (trial.apiVersion !== "zenith.agentdriver.dev/evaluator-trial/v1alpha1" || trial.kind !== "EvaluatorTrial") fail("unsupported apiVersion or kind");
if (!trial.id || !trial.sourceRevision || !trial.snapshot || !Array.isArray(trial.arms) || trial.arms.length < 2) fail("trial identity, source revision, snapshot, and at least two arms are required");
const identity = structuredClone(trial);
identity.id = "";
identity.resolutions = [];
if (trial.id !== `trial_${sha256(stable(identity))}`) fail("trial id does not match canonical manifest content");
if (!trial.snapshot.repository || !trial.snapshot.scope || !trial.snapshot.commit || !trial.snapshot.tree || !isDigest(trial.snapshot.archiveSha256) || trial.snapshot.id !== `snap_${trial.snapshot.archiveSha256}` || trial.snapshot.secretMatches !== 0 || !trial.snapshot.secretScanRevision) fail("snapshot must bind repository, scope, revision, tree, archive digest, and a pinned zero-match secret scan");

const promptPath = safePath(trial.prompt?.path);
if (!promptPath || !isRegularFile(promptPath)) fail("prompt file is missing, not a regular file, or escapes the trial directory");
else if (!isDigest(trial.prompt.sha256) || sha256(readFileSync(promptPath)) !== trial.prompt.sha256) fail("prompt digest does not match bytes");

const armIds = new Set();
const findingIds = new Set();
const aggregateCounters = { inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheWriteTokens: 0, providerDurationMilliseconds: 0, wallDurationMilliseconds: 0, costMicros: 0 };
for (const arm of trial.arms ?? []) {
  if (!arm.id || armIds.has(arm.id)) fail(`duplicate or missing arm id ${JSON.stringify(arm.id)}`);
  armIds.add(arm.id);
  if (!arm.provider || !arm.modelFamily || !arm.model || !arm.modelVersion || !arm.invocationId || !arm.sessionId) fail(`arm ${arm.id} lacks immutable provider/model/invocation identity`);
  if (!isStatus(arm.inputAccessStatus, ["COMPLETE", "INCOMPLETE"])) fail(`arm ${arm.id} has invalid input access status`);
  if (arm.inputAccessStatus === "INCOMPLETE" && !(arm.residualUnknowns?.length > 0)) fail(`arm ${arm.id} incomplete access lacks residual unknowns`);
  if (!Array.isArray(arm.residualUnknowns) || arm.residualUnknowns.some((unknown) => typeof unknown !== "string" || unknown === "")) fail(`arm ${arm.id} has invalid residual unknowns`);
  if (!isStatus(arm.costStatus, ["MEASURED", "UNAVAILABLE"])) fail(`arm ${arm.id} has invalid cost status`);
  if (arm.costStatus === "UNAVAILABLE" && (arm.costMicros !== 0 || arm.costCurrency || !arm.costUnavailableReason)) fail(`arm ${arm.id} encodes unavailable cost as measured/free`);
  if (arm.costStatus === "MEASURED" && (!Number.isSafeInteger(arm.costMicros) || arm.costMicros < 0 || !/^[A-Z]{3}$/.test(arm.costCurrency ?? ""))) fail(`arm ${arm.id} has invalid measured cost`);
  if (Number.isSafeInteger(arm.costMicros)) {
    aggregateCounters.costMicros += arm.costMicros;
    if (!Number.isSafeInteger(aggregateCounters.costMicros)) fail("aggregate evaluator cost exceeds exact JSON integer range");
  }
  for (const counter of ["inputTokens", "outputTokens", "cacheReadTokens", "cacheWriteTokens"]) {
    if (!Number.isSafeInteger(arm.usage?.[counter]) || arm.usage[counter] < 0) fail(`arm ${arm.id} has invalid ${counter}`);
    else {
      aggregateCounters[counter] += arm.usage[counter];
      if (!Number.isSafeInteger(aggregateCounters[counter])) fail(`aggregate ${counter} exceeds exact JSON integer range`);
    }
  }
  if (!(arm.providerDurationMilliseconds >= 0) || !(arm.wallDurationMilliseconds >= arm.providerDurationMilliseconds)) fail(`arm ${arm.id} has invalid latency evidence`);
  else {
    aggregateCounters.providerDurationMilliseconds += arm.providerDurationMilliseconds;
    aggregateCounters.wallDurationMilliseconds += arm.wallDurationMilliseconds;
    if (!Number.isSafeInteger(aggregateCounters.providerDurationMilliseconds) || !Number.isSafeInteger(aggregateCounters.wallDurationMilliseconds)) fail("aggregate evaluator duration exceeds exact JSON integer range");
  }
  const receiptPath = safePath(arm.receipt?.path);
  if (!receiptPath || !isRegularFile(receiptPath) || !isDigest(arm.receipt.sha256) || sha256(readFileSync(receiptPath)) !== arm.receipt.sha256) fail(`arm ${arm.id} receipt digest does not match bytes, is not a regular file, or path escapes the trial directory`);
  for (const finding of arm.findings ?? []) {
    if (!finding.id || findingIds.has(finding.id) || !finding.modelClusterKey || !finding.summary || !Array.isArray(finding.evidence) || finding.evidence.length === 0 || finding.evidence.some((item) => typeof item !== "string" || item === "")) fail(`arm ${arm.id} has invalid or duplicate finding ${JSON.stringify(finding.id)}`);
    findingIds.add(finding.id);
  }
}

const dispositionIds = new Set();
for (const disposition of trial.dispositions ?? []) {
  if (!findingIds.has(disposition.findingId) || dispositionIds.has(disposition.findingId)) fail(`disposition references unknown or duplicate finding ${disposition.findingId}`);
  dispositionIds.add(disposition.findingId);
  if (!disposition.canonicalClusterKey || !isStatus(disposition.status, ["VERIFIED_DEFECT", "FALSE_POSITIVE", "UNRESOLVED"]) || !disposition.verifier || !disposition.sourceRevision || !isTimestamp(disposition.verifiedAt) || !disposition.notes) fail(`disposition ${disposition.findingId} lacks independent provenance or canonical clustering`);
  if (disposition.status === "VERIFIED_DEFECT" && !disposition.executableGate) fail(`verified finding ${disposition.findingId} lacks executable gate`);
}

const resolved = new Set([...dispositionIds].filter((id) => trial.dispositions.find((item) => item.findingId === id)?.status !== "UNRESOLVED"));
const effectiveDispositions = new Map((trial.dispositions ?? []).map((disposition) => [disposition.findingId, disposition]));
for (const resolution of trial.resolutions ?? []) {
  const resolutionIdentity = { ...structuredClone(resolution), id: "", trialId: trial.id };
  if (resolution.id !== `resolution_${sha256(stable(resolutionIdentity))}` || !resolution.sourceRevision || !isTimestamp(resolution.resolvedAt) || !(resolution.dispositions?.length > 0)) fail("resolution lacks valid content identity, provenance, timestamp, or dispositions");
  for (const disposition of resolution.dispositions ?? []) {
    if (!findingIds.has(disposition.findingId) || resolved.has(disposition.findingId) || !disposition.canonicalClusterKey || !isStatus(disposition.status, ["VERIFIED_DEFECT", "FALSE_POSITIVE"]) || !disposition.verifier || !disposition.sourceRevision || !isTimestamp(disposition.verifiedAt) || !disposition.notes) fail(`resolution illegally rewrites, references, or lacks provenance for finding ${disposition.findingId}`);
    if (disposition.status === "VERIFIED_DEFECT" && !disposition.executableGate) fail(`resolved defect ${disposition.findingId} lacks executable gate`);
    resolved.add(disposition.findingId);
    effectiveDispositions.set(disposition.findingId, disposition);
  }
}

if (dispositionIds.size !== findingIds.size) fail(`every finding requires an initial disposition: ${dispositionIds.size}/${findingIds.size}`);

if (failures.length) {
  for (const message of failures) console.error(`FAIL ${message}`);
  process.exit(1);
}

const verifiedClusterArms = new Map();
for (const arm of trial.arms) {
  for (const finding of arm.findings ?? []) {
    const disposition = effectiveDispositions.get(finding.id);
    if (disposition?.status !== "VERIFIED_DEFECT") continue;
    const arms = verifiedClusterArms.get(disposition.canonicalClusterKey) ?? new Set();
    arms.add(arm.id);
    verifiedClusterArms.set(disposition.canonicalClusterKey, arms);
  }
}
const ratio = (numerator, denominator) => denominator === 0 ? null : { numerator, denominator };
const metrics = trial.arms.map((arm) => {
  let disposedFindings = 0, verifiedFindings = 0, falsePositives = 0;
  const verifiedClusters = new Set(), uniqueVerifiedClusters = new Set();
  for (const finding of arm.findings ?? []) {
    const disposition = effectiveDispositions.get(finding.id);
    if (!disposition || disposition.status === "UNRESOLVED") continue;
    disposedFindings++;
    if (disposition.status === "FALSE_POSITIVE") falsePositives++;
    if (disposition.status === "VERIFIED_DEFECT") {
      verifiedFindings++;
      verifiedClusters.add(disposition.canonicalClusterKey);
      if (verifiedClusterArms.get(disposition.canonicalClusterKey)?.size === 1) uniqueVerifiedClusters.add(disposition.canonicalClusterKey);
    }
  }
  const recallAvailable = arm.inputAccessStatus === "COMPLETE" && verifiedClusterArms.size > 0;
  return {
    armId: arm.id, provider: arm.provider, modelFamily: arm.modelFamily, model: arm.model,
    disposedFindings, verifiedFindings, falsePositives,
    verifiedClusters: verifiedClusters.size, uniqueVerifiedClusters: uniqueVerifiedClusters.size,
    precision: ratio(verifiedFindings, disposedFindings),
    falseDiscoveryRate: ratio(falsePositives, disposedFindings),
    recallStatus: recallAvailable ? "MEASURED" : "UNAVAILABLE",
    recall: recallAvailable ? ratio(verifiedClusters.size, verifiedClusterArms.size) : null,
    recallUnavailableReason: recallAvailable ? null : arm.inputAccessStatus === "INCOMPLETE"
      ? "evaluator did not have complete access to the pinned input"
      : "batch contains no verified defect clusters",
    usage: arm.usage,
    providerDurationMilliseconds: arm.providerDurationMilliseconds,
    wallDurationMilliseconds: arm.wallDurationMilliseconds,
    costStatus: arm.costStatus,
    costMicros: arm.costMicros,
    costCurrency: arm.costCurrency ?? null,
    costUnavailableReason: arm.costUnavailableReason ?? null,
  };
});
const measuredCurrencies = new Set(trial.arms.filter((arm) => arm.costStatus === "MEASURED").map((arm) => arm.costCurrency));
const allCostMeasured = trial.arms.every((arm) => arm.costStatus === "MEASURED") && measuredCurrencies.size === 1;
const summary = {
  trialId: trial.id, arms: armIds.size, findings: findingIds.size,
  verifiedFindings: [...effectiveDispositions.values()].filter((item) => item.status === "VERIFIED_DEFECT").length,
  falsePositives: [...effectiveDispositions.values()].filter((item) => item.status === "FALSE_POSITIVE").length,
  unresolvedFindings: [...effectiveDispositions.values()].filter((item) => item.status === "UNRESOLVED").length,
  verifiedClusters: verifiedClusterArms.size,
  usage: Object.fromEntries(Object.entries(aggregateCounters).filter(([key]) => key.endsWith("Tokens"))),
  providerDurationMilliseconds: aggregateCounters.providerDurationMilliseconds,
  wallDurationMilliseconds: aggregateCounters.wallDurationMilliseconds,
  costStatus: allCostMeasured ? "MEASURED" : "UNAVAILABLE",
  costMicros: allCostMeasured ? aggregateCounters.costMicros : 0,
  costCurrency: allCostMeasured ? [...measuredCurrencies][0] : null,
  costUnavailableReason: allCostMeasured ? null : trial.arms.some((arm) => arm.costStatus === "UNAVAILABLE")
    ? "one or more arms have unavailable monetary cost"
    : "measured arm costs use different currencies",
  costUnavailableArms: trial.arms.filter((arm) => arm.costStatus === "UNAVAILABLE").map((arm) => arm.id).sort(),
};
if (jsonOutput) console.log(JSON.stringify({ status: "PASS", summary, metrics }, null, 2));
else console.log(`PASS evaluator-trial arms=${armIds.size} findings=${findingIds.size} verifiedClusters=${verifiedClusterArms.size} resolutions=${trial.resolutions?.length ?? 0}`);

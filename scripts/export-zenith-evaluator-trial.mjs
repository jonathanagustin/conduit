#!/usr/bin/env node

import { createHash } from "node:crypto";
import { existsSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { basename, dirname, join, relative, resolve } from "node:path";

const evidenceDirectory = resolve(process.argv[2] ?? "");
const batchResultPath = resolve(process.argv[3] ?? "");
const outputPath = resolve(process.argv[4] ?? "");
const checkOnly = process.argv[5] === "--check";
if (!process.argv[2] || !process.argv[3] || !process.argv[4]) {
  throw new Error("usage: export-zenith-evaluator-trial.mjs <evidence-directory> <batch-result-json> <output-json>");
}
if (dirname(outputPath) !== evidenceDirectory) {
  throw new Error("Zenith trial must be written beside its durable prompt and receipts");
}

const readJSON = (path) => JSON.parse(readFileSync(path, "utf8"));
const sha256 = (bytes) => createHash("sha256").update(bytes).digest("hex");
const stable = (value) => Array.isArray(value)
  ? `[${value.map(stable).join(",")}]`
  : value && typeof value === "object"
    ? `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${stable(value[key])}`).join(",")}}`
    : JSON.stringify(value);
const result = readJSON(batchResultPath);
const batch = result.batch;
if (!batch || !Array.isArray(batch.runs) || !Array.isArray(batch.dispositions)) {
  throw new Error("batch result lacks runs or dispositions");
}
const snapshot = readJSON(join(evidenceDirectory, "snapshot.json"));
const promptPath = join(evidenceDirectory, "prompt.txt");
const promptSHA256 = sha256(readFileSync(promptPath));
const dispositions = batch.dispositions.map((disposition) => ({
  findingId: disposition.findingId,
  canonicalClusterKey: disposition.canonicalClusterKey,
  status: disposition.status,
  verifier: disposition.verifier,
  sourceRevision: disposition.sourceRevision,
  verifiedAt: disposition.verifiedAt,
  notes: disposition.notes,
  ...(disposition.executableGate ? { executableGate: disposition.executableGate } : {}),
}));
const resolvedFindings = new Set();
const resolutionPaths = readdirSync(evidenceDirectory, { withFileTypes: true })
  .filter((entry) => entry.isDirectory() && /^store(?:-[A-Za-z0-9._-]+)?$/.test(entry.name))
  .flatMap((entry) => {
    const directory = join(evidenceDirectory, entry.name, "evaluator-resolutions");
    return existsSync(directory)
      ? readdirSync(directory).filter((name) => /^evr_[0-9a-f]{64}\.json$/.test(name)).map((name) => join(directory, name))
      : [];
  })
  .sort();
const resolutions = [];
for (const path of resolutionPaths) {
  const name = basename(path);
  const bytes = readFileSync(path);
  const resolution = JSON.parse(bytes);
  if (resolution.apiVersion !== "optimization.platform.example/evaluator/v1alpha1" || resolution.kind !== "EvaluatorResolution" || `${resolution.id}.json` !== name || !Array.isArray(resolution.dispositions)) {
    throw new Error(`${relative(evidenceDirectory, path)} is not a valid evaluator resolution`);
  }
  if (resolution.batchId !== batch.id) continue;
  for (const disposition of resolution.dispositions) {
    if (resolvedFindings.has(disposition.findingId)) throw new Error(`multiple resolutions dispose ${disposition.findingId}`);
    resolvedFindings.add(disposition.findingId);
  }
  resolutions.push({
    id: resolution.id,
    batchId: resolution.batchId,
    record: { path: relative(evidenceDirectory, path), sha256: sha256(bytes) },
    dispositions: resolution.dispositions,
  });
}
const arms = batch.runs.map((run) => {
  if (!run.model || basename(run.model) !== run.model) throw new Error("evaluator model must be a safe receipt basename");
  const receiptName = `${run.model}.receipt.json`;
  const receiptPath = join(evidenceDirectory, receiptName);
  const timing = readJSON(join(evidenceDirectory, `${run.model}.timing.json`));
  const receiptBytes = readFileSync(receiptPath);
  const receipt = JSON.parse(receiptBytes);
  if (run.readOnly !== true || run.inputSnapshotId !== snapshot.snapshotId || run.promptSha256 !== promptSHA256 || run.secretScanRevision !== snapshot.secretScanRevision || JSON.stringify(run.snapshot) !== JSON.stringify(snapshot)) {
    throw new Error(`${run.model} does not bind the exported read-only snapshot and prompt`);
  }
  if (run.receiptSha256 !== sha256(receiptBytes) || run.invocationId !== receipt.request_id || run.sessionId !== receipt.session_id || JSON.stringify(run.usage) !== JSON.stringify(receipt.usage)) {
    throw new Error(`${run.model} normalized identity or usage does not match its raw receipt`);
  }
  if (timing.model !== run.model || !Number.isSafeInteger(receipt.duration_api_ms) || !Number.isSafeInteger(timing.wallDurationMilliseconds) || receipt.duration_api_ms < 0 || timing.wallDurationMilliseconds < 0 || receipt.duration_api_ms > timing.wallDurationMilliseconds) {
    throw new Error(`${run.model} provider duration exceeds wall duration`);
  }
  return {
    id: run.id,
    provider: run.provider,
    modelFamily: run.modelFamily,
    model: run.model,
    modelVersion: run.modelVersion,
    invocationId: run.invocationId,
    sessionId: run.sessionId,
    inputAccessStatus: run.inputAccessStatus,
    residualUnknowns: run.residualUnknowns,
    usage: run.usage,
    providerDurationMilliseconds: receipt.duration_api_ms,
    wallDurationMilliseconds: timing.wallDurationMilliseconds,
    costStatus: run.costStatus,
    costMicros: run.costMicros,
    ...(run.costCurrency ? { costCurrency: run.costCurrency } : {}),
    ...(run.costUnavailableReason ? { costUnavailableReason: run.costUnavailableReason } : {}),
    receipt: { path: receiptName, sha256: run.receiptSha256 },
    findings: run.findings.map((finding) => ({
      id: finding.id,
      modelClusterKey: finding.clusterKey,
      summary: finding.summary,
      evidence: finding.evidence,
    })),
  };
});
const sourceRevisions = new Set(dispositions.map((disposition) => disposition.sourceRevision));
if (sourceRevisions.size !== 1) throw new Error("initial dispositions must share one verification revision");
const trial = {
  apiVersion: "zenith.agentdriver.dev/evaluator-trial/v1alpha1",
  kind: "EvaluatorTrial",
  id: "",
  sourceRevision: [...sourceRevisions][0],
  snapshot: {
    repository: snapshot.repository,
    scope: snapshot.path,
    commit: snapshot.commit,
    tree: snapshot.tree,
    archiveSha256: snapshot.archiveSha256,
    id: snapshot.snapshotId,
    secretScanRevision: snapshot.secretScanRevision,
    secretMatches: snapshot.secretMatches,
  },
  prompt: { path: relative(evidenceDirectory, promptPath), sha256: promptSHA256 },
  arms,
  dispositions,
  resolutions,
};
trial.id = `trial_${sha256(stable(trial))}`;
const encoded = `${JSON.stringify(trial, null, 2)}\n`;
if (checkOnly) {
  if (readFileSync(outputPath, "utf8") !== encoded) throw new Error(`${basename(outputPath)} does not match a fresh export`);
  console.log(`verified ${trial.id} from ${basename(batchResultPath)}`);
} else {
  writeFileSync(outputPath, encoded);
  console.log(`exported ${trial.id} from ${basename(batchResultPath)}`);
}

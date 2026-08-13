#!/usr/bin/env node

import { createHash } from "node:crypto";
import { readdirSync, readFileSync, writeFileSync } from "node:fs";
import { basename, join, relative, resolve } from "node:path";

const directory = resolve(process.argv[2] ?? "");
const sourceRevision = process.argv[3];
const configurationPath = process.argv[4];
if (!directory || !/^[0-9a-f]{40,64}$/.test(sourceRevision ?? "") || !configurationPath) {
  throw new Error("usage: normalize-evaluator-receipts.mjs <evidence-directory> <verification-commit> <normalization-json>");
}

const sha256 = (value) => createHash("sha256").update(value).digest("hex");
const readJSON = (name) => JSON.parse(readFileSync(join(directory, name), "utf8"));
const configuration = JSON.parse(readFileSync(resolve(configurationPath), "utf8"));
const snapshot = readJSON("snapshot.json");
const promptSHA256 = sha256(readFileSync(join(directory, "prompt.txt")));
const suffix = configuration.outputSuffix ? `-${configuration.outputSuffix}` : "";

function embeddedObject(text) {
  for (let start = text.indexOf("{"); start >= 0; start = text.indexOf("{", start + 1)) {
    let depth = 0, quoted = false, escaped = false;
    for (let end = start; end < text.length; end++) {
      const character = text[end];
      if (quoted) {
        if (escaped) escaped = false;
        else if (character === "\\") escaped = true;
        else if (character === '"') quoted = false;
      } else if (character === '"') quoted = true;
      else if (character === "{") depth++;
      else if (character === "}" && --depth === 0) {
        try {
          const parsed = JSON.parse(text.slice(start, end + 1));
          if (Array.isArray(parsed.findings) && Array.isArray(parsed.residualUnknowns)) return parsed;
        } catch {}
        break;
      }
    }
  }
  throw new Error("receipt contains no evaluator result object");
}

function familyFor(model) {
  if (model.includes("grok")) return "grok";
  if (model.includes("gemini")) return "gemini";
  if (model.includes("kimi")) return "kimi";
  if (model.includes("opus")) return "opus";
  return model.split("-")[0];
}

const receiptFiles = readdirSync(directory).filter((name) => name.endsWith(".receipt.json")).sort();
if (receiptFiles.length < 2) throw new Error("at least two evaluator receipts are required");
const runs = receiptFiles.map((receiptName) => {
  const model = receiptName.slice(0, -".receipt.json".length);
  const family = familyFor(model);
  const receiptBytes = readFileSync(join(directory, receiptName));
  const receipt = JSON.parse(receiptBytes);
  const timing = readJSON(`${model}.timing.json`);
  const result = embeddedObject(receipt.result);
  const started = new Date(timing.startedAt);
  const finished = new Date(started.getTime() + receipt.duration_api_ms);
  const inputAccessStatus = result.inputAccessStatus ?? configuration.inputAccessStatus?.[model];
  if (!["COMPLETE", "INCOMPLETE"].includes(inputAccessStatus)) throw new Error(`missing input access status for ${model}`);
  return {
    provider: configuration.provider, modelFamily: family, model,
    modelVersion: configuration.modelVersion,
    promptSha256: promptSHA256, inputSnapshotId: snapshot.snapshotId, snapshot,
    readOnly: true, secretScanRevision: snapshot.secretScanRevision,
    invocationId: receipt.request_id, sessionId: receipt.session_id, receiptSha256: sha256(receiptBytes),
    usage: receipt.usage, inputAccessStatus, residualUnknowns: result.residualUnknowns,
    startedAt: started.toISOString(), finishedAt: finished.toISOString(),
    costStatus: configuration.cost.status, costMicros: configuration.cost.micros,
    ...(configuration.cost.currency ? { costCurrency: configuration.cost.currency } : {}),
    priceSource: configuration.cost.source, priceRevision: configuration.cost.revision,
    ...(configuration.cost.unavailableReason ? { costUnavailableReason: configuration.cost.unavailableReason } : {}),
    findings: result.findings.map((finding) => ({
      id: `${family}-${finding.localId}`, clusterKey: finding.clusterKey, category: finding.category,
      target: finding.target, summary: finding.summary, confidence: finding.confidence,
      evidence: finding.evidence, hypothesis: true,
    })),
  };
});

const dispositions = runs.flatMap((run) => run.findings).map((finding) => {
  const configured = configuration.dispositions?.[finding.id] ?? {};
  const status = configured.status ?? "UNRESOLVED";
  return {
    findingId: finding.id, canonicalClusterKey: configured.canonicalClusterKey ?? configuration.canonicalClusters?.[finding.id] ?? finding.clusterKey, status,
    verifier: configured.verifier ?? "independent-reproduction",
    source: configuration.source, sourceRevision,
    ...(configured.executableGate ? { executableGate: configured.executableGate } : {}),
    verifiedAt: configuration.verifiedAt,
    notes: configured.notes ?? "Not promoted to fact: independent verification is incomplete.",
  };
});

writeFileSync(join(directory, `batch-request${suffix}.json`), `${JSON.stringify({
  storeDirectory: relative(process.cwd(), join(directory, `store${suffix}`)), runs, dispositions,
}, null, 2)}\n`);
writeFileSync(join(directory, `residual-unknowns${suffix}.json`), `${JSON.stringify(Object.fromEntries(runs.map((run) => [run.model, run.residualUnknowns])), null, 2)}\n`);
console.log(`normalized ${runs.length} receipts and ${dispositions.length} findings from ${basename(directory)}`);

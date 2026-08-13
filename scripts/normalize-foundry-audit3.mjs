#!/usr/bin/env node

import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

const boundedContracts = {
  apiGatewayRequest: { request: [], result: [] },
  authenticationCheck: { request: [], result: ["authenticationCheckResult"] },
  authorizationCheck: {
    request: ["authorizationCheckOperations"],
    result: ["authorizationCheckSucceededTargets", "authorizationCheckFailedTargets"],
  },
  requestCreate: { request: ["createdRequestAffectedResources"], result: ["createdRequestIds"] },
  requestApprove: { request: ["approvedRequestIds"], result: [] },
  requestDisapprove: { request: ["disapprovedRequestIds"], result: [] },
  requestCancel: { request: ["canceledRequestIds"], result: [] },
  requestExecute: { request: ["executedRequestIds"], result: [] },
  requestUpdate: { request: ["updatedRequestIds"], result: [] },
  createInfra: { request: ["createInfraTargets"], result: ["createdInfraResources"] },
  configureInfra: { request: ["configureInfraTargets"], result: ["configureInfraRequestId"] },
  upgradeInfra: { request: ["upgradedResources"], result: [] },
};

const sha256 = (value) => createHash("sha256").update(value).digest("hex");
const stable = (value) => {
  if (Array.isArray(value)) return value.map(stable);
  if (value && typeof value === "object") {
    return Object.fromEntries(Object.keys(value).sort().map((key) => [key, stable(value[key])]));
  }
  return value;
};
const encoded = (value) => JSON.stringify(stable(value));
const pseudonym = (value) => value == null || value === "" ? undefined : `sha256:${sha256(String(value))}`;

function objectField(value, name) {
  const candidate = value?.[name];
  if (candidate == null) return {};
  if (typeof candidate === "string") {
    try { return JSON.parse(candidate); } catch { throw new Error(`${name} is not a JSON object`); }
  }
  if (typeof candidate !== "object" || Array.isArray(candidate)) throw new Error(`${name} must be an object`);
  return candidate;
}

function summary(value) {
  const type = Array.isArray(value) ? "array" : value === null ? "null" : typeof value;
  const result = { type, sha256: sha256(encoded(value)) };
  if (Array.isArray(value) || typeof value === "string") result.length = value.length;
  else if (value && typeof value === "object") result.keys = Object.keys(value).sort();
  return result;
}

export function parseAuditInput(text) {
  const trimmed = text.trim();
  if (!trimmed) return [];
  try {
    const parsed = JSON.parse(trimmed);
    if (Array.isArray(parsed)) return parsed;
    if (Array.isArray(parsed.events)) return parsed.events;
    return [parsed];
  } catch {
    return trimmed.split(/\r?\n/).filter(Boolean).map((line, index) => {
      try { return JSON.parse(line); } catch (error) { throw new Error(`invalid JSONL line ${index + 1}: ${error.message}`); }
    });
  }
}

export function normalizeAudit3(events, { sourceBytes, capturedAt, retainOperationNames = false } = {}) {
  if (!capturedAt || Number.isNaN(Date.parse(capturedAt))) throw new Error("capturedAt must be an explicit ISO-8601 timestamp");
  const normalized = events.map((event, index) => {
    // Foundry's public wire schema uses `type`. The aliases are accepted only
    // for already-normalized exports produced by common SIEM pipelines.
    const schemaVersion = event.type ?? event.schemaVersion ?? event.schema_version ?? event.schema;
    if (schemaVersion !== "audit.3") throw new Error(`event ${index + 1}: schemaVersion must be audit.3`);
    const categories = [...new Set(event.categories ?? [])].sort();
    if (!categories.length) throw new Error(`event ${index + 1}: audit.3 requires categories`);
    const requestFields = objectField(event, "requestFields");
    const resultFields = objectField(event, "resultFields");
    for (const category of categories) {
      const contract = boundedContracts[category];
      if (!contract) continue; // preserve unknown public categories without inventing their semantics.
      for (const field of contract.request) if (!(field in requestFields)) throw new Error(`event ${index + 1}: ${category} requires requestFields.${field}`);
      for (const field of contract.result) if (!(field in resultFields)) throw new Error(`event ${index + 1}: ${category} requires resultFields.${field}`);
    }
    const requestShape = Object.fromEntries(Object.entries(requestFields).sort().map(([key, value]) => [key, summary(value)]));
    const resultShape = Object.fromEntries(Object.entries(resultFields).sort().map(([key, value]) => [key, summary(value)]));
    const record = {
      schemaVersion,
      categories,
      eventKey: pseudonym(event.eventId),
      logEntryKey: pseudonym(event.logEntryId),
      traceKey: pseudonym(event.traceId),
      environmentKey: pseudonym(event.environment),
      time: event.time ?? event.timestamp,
      service: event.service,
      requestFields: requestShape,
      resultFields: resultShape,
      recordSha256: sha256(encoded(event)),
    };
    if (retainOperationNames && Array.isArray(requestFields.operationNames)) {
      record.operationNames = requestFields.operationNames.map(String).sort();
    }
    return Object.fromEntries(Object.entries(record).filter(([, value]) => value !== undefined));
  });
  return {
    kind: "platform.foundry-audit3-shape-evidence.v1",
    capturedAt: new Date(capturedAt).toISOString(),
    sourceSha256: `sha256:${sha256(sourceBytes ?? encoded(events))}`,
    sourceRecordCount: events.length,
    valuePolicy: retainOperationNames ? "field-shapes-hashes-and-operation-names" : "field-shapes-and-hashes-only",
    events: normalized,
  };
}

function usage() {
  console.error("usage: normalize-foundry-audit3.mjs --input <json|jsonl> --output <receipt.json> --captured-at <ISO-8601> [--retain-operation-names]");
}

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  const args = process.argv.slice(2);
  const value = (flag) => { const index = args.indexOf(flag); return index < 0 ? undefined : args[index + 1]; };
  const input = value("--input"), output = value("--output"), capturedAt = value("--captured-at");
  if (!input || !output || !capturedAt) { usage(); process.exit(2); }
  const sourceBytes = readFileSync(input);
  const receipt = normalizeAudit3(parseAuditInput(sourceBytes.toString("utf8")), {
    sourceBytes, capturedAt, retainOperationNames: args.includes("--retain-operation-names"),
  });
  writeFileSync(output, `${JSON.stringify(receipt, null, 2)}\n`, { mode: 0o600 });
}

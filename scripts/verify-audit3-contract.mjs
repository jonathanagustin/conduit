#!/usr/bin/env node

import assert from "node:assert/strict";
import { normalizeAudit3, parseAuditInput } from "./normalize-foundry-audit3.mjs";

const input = JSON.stringify({
  type: "audit.3",
  categories: ["apiGatewayRequest", "requestExecute"],
  eventId: "event-sensitive", logEntryId: "line-sensitive", traceId: "trace-sensitive",
  environment: "tenant-sensitive", time: "2026-08-03T00:00:00Z", service: "gateway",
  requestFields: { operationNames: ["installProduct"], executedRequestIds: ["request-sensitive"] },
  resultFields: { executeRequestAffectedResources: ["resource-sensitive"] },
});
const receipt = normalizeAudit3(parseAuditInput(input), {
  sourceBytes: Buffer.from(input), capturedAt: "2026-08-03T00:00:00Z",
});
assert.equal(receipt.sourceRecordCount, 1);
assert.deepEqual(receipt.events[0].categories, ["apiGatewayRequest", "requestExecute"]);
assert.equal(receipt.events[0].requestFields.operationNames.type, "array");
assert.equal(JSON.stringify(receipt).includes("tenant-sensitive"), false);
assert.equal(JSON.stringify(receipt).includes("request-sensitive"), false);
assert.throws(() => normalizeAudit3([{ type: "audit.2", categories: [] }], {
  capturedAt: "2026-08-03T00:00:00Z",
}), /audit\.3/);
assert.throws(() => normalizeAudit3([{
  type: "audit.3", categories: ["requestExecute"], requestFields: {}, resultFields: {},
}], { capturedAt: "2026-08-03T00:00:00Z" }), /executedRequestIds/);
console.log("PASS audit.3 normalization, redaction, and bounded category validation");

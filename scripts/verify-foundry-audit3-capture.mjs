#!/usr/bin/env node

import assert from "node:assert/strict";
import { mkdtempSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { captureAudit3 } from "./capture-foundry-audit3.mjs";

const requests = [];
const fetchImpl = async (url, options) => {
  requests.push({ url: String(url), authorization: options.headers.Authorization });
  if (String(url).includes("/content")) {
    return new Response('{"type":"audit.3","categories":["requestExecute"]}\n');
  }
  const pageToken = new URL(url).searchParams.get("pageToken");
  return new Response(JSON.stringify(pageToken ? { data: [], nextPageToken: "page-three" } : {
    data: [{ id: "sensitive-file-id" }], nextPageToken: "page-two",
  }), { headers: { "content-type": "application/json" } });
};
const parent = mkdtempSync(join(tmpdir(), "conduit-audit3-test-"));
const output = join(parent, "capture");
const receipt = await captureAudit3({
  baseURL: "http://127.0.0.1:8080", organizationRID: "sensitive-org", startDate: "2026-08-01",
  endDate: "2026-08-02", token: "secret-token", outputDirectory: output, fetchImpl,
  capturedAt: "2026-08-03T00:00:00Z",
});
assert.equal(receipt.files.length, 1);
assert.equal(requests.length, 3);
assert.match(requests[0].url, /startDate=2026-08-01/);
assert.match(requests[0].url, /endDate=2026-08-02/);
assert.match(requests[1].url, /\/content$/);
assert.match(requests[2].url, /pageToken=page-two/);
assert.equal(JSON.stringify(receipt).includes("sensitive-org"), false);
assert.equal(JSON.stringify(receipt).includes("sensitive-file-id"), false);
assert.equal(readFileSync(join(output, receipt.files[0].name), "utf8").includes("audit.3"), true);
assert.equal(readFileSync(join(output, "capture-receipt.json"), "utf8").includes("secret-token"), false);
console.log("PASS authorized audit.3 API capture, pagination, integrity receipt, and credential redaction");

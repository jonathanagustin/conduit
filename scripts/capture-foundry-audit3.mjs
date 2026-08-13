#!/usr/bin/env node

import { createHash } from "node:crypto";
import { mkdirSync, openSync, closeSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const sha256 = (value) => createHash("sha256").update(value).digest("hex");

function requiredURL(value) {
  const url = new URL(value);
  const loopback = ["localhost", "127.0.0.1", "::1"].includes(url.hostname);
  if (url.protocol !== "https:" && !(url.protocol === "http:" && loopback)) {
    throw new Error("Foundry hostname must use HTTPS (HTTP is allowed only for loopback tests)");
  }
  url.pathname = url.pathname.replace(/\/$/, "");
  url.search = "";
  url.hash = "";
  return url;
}

async function responseBytes(response, operation) {
  if (!response.ok) throw new Error(`${operation} failed with HTTP ${response.status}`);
  return Buffer.from(await response.arrayBuffer());
}

export async function captureAudit3({
  baseURL, organizationRID, startDate, endDate, token, outputDirectory,
  fetchImpl = fetch, capturedAt = new Date().toISOString(),
}) {
  if (!organizationRID || !startDate || !token || !outputDirectory) {
    throw new Error("organizationRID, startDate, token, and outputDirectory are required");
  }
  const base = requiredURL(baseURL);
  const apiBase = base.toString().replace(/\/$/, "");
  const destination = resolve(outputDirectory);
  mkdirSync(destination, { recursive: false, mode: 0o700 });
  const authorization = { Authorization: `Bearer ${token}` };
  const files = [];
  let pageToken;
  const seenTokens = new Set();

  for (;;) {
    const listURL = new URL(`${apiBase}/api/v2/audit/organizations/${encodeURIComponent(organizationRID)}/logFiles`);
    if (pageToken) listURL.searchParams.set("pageToken", pageToken);
    else {
      listURL.searchParams.set("startDate", startDate);
      if (endDate) listURL.searchParams.set("endDate", endDate);
    }
    const listResponse = await fetchImpl(listURL, { headers: authorization });
    const listBytes = await responseBytes(listResponse, "list log files");
    let page;
    try { page = JSON.parse(listBytes.toString("utf8")); }
    catch { throw new Error("list log files returned malformed JSON"); }
    const data = Array.isArray(page.data) ? page.data : [];
    for (const item of data) {
      if (!item?.id || typeof item.id !== "string") throw new Error("log file response omitted a string id");
      const contentURL = new URL(`${apiBase}/api/v2/audit/organizations/${encodeURIComponent(organizationRID)}/logFiles/${encodeURIComponent(item.id)}/content`);
      const content = await responseBytes(await fetchImpl(contentURL, { headers: authorization }), "get log file content");
      const name = `${sha256(item.id)}.audit3`;
      const descriptor = openSync(resolve(destination, name), "wx", 0o600);
      try { writeFileSync(descriptor, content); } finally { closeSync(descriptor); }
      files.push({ idKey: `sha256:${sha256(item.id)}`, name, bytes: content.length, sha256: `sha256:${sha256(content)}` });
    }
    const next = typeof page.nextPageToken === "string" ? page.nextPageToken : "";
    if (!data.length || !next || seenTokens.has(next)) break;
    seenTokens.add(next);
    pageToken = next;
  }

  const receipt = {
    kind: "platform.foundry-audit3-raw-capture.v1",
    capturedAt: new Date(capturedAt).toISOString(),
    source: {
      api: "/api/v2/audit/organizations/{organizationRid}/logFiles",
      organizationKey: `sha256:${sha256(organizationRID)}`,
      startDate,
      ...(endDate ? { endDate } : {}),
    },
    files,
  };
  writeFileSync(resolve(destination, "capture-receipt.json"), `${JSON.stringify(receipt, null, 2)}\n`, { flag: "wx", mode: 0o600 });
  return receipt;
}

function usage() {
  console.error("usage: capture-foundry-audit3.mjs --hostname <https-url> --organization-rid <rid> --start-date <YYYY-MM-DD> [--end-date <YYYY-MM-DD>] --output-dir <new-directory> [--token-env <name>]");
}

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  const args = process.argv.slice(2);
  const value = (flag) => { const index = args.indexOf(flag); return index < 0 ? undefined : args[index + 1]; };
  const tokenEnvironment = value("--token-env") ?? "FOUNDRY_TOKEN";
  const options = {
    baseURL: value("--hostname"), organizationRID: value("--organization-rid"),
    startDate: value("--start-date"), endDate: value("--end-date"),
    outputDirectory: value("--output-dir"), token: process.env[tokenEnvironment],
  };
  if (!options.baseURL || !options.organizationRID || !options.startDate || !options.outputDirectory || !options.token) {
    usage();
    process.exit(2);
  }
  const receipt = await captureAudit3(options);
  console.log(JSON.stringify({ outputDirectory: resolve(options.outputDirectory), files: receipt.files.length }));
}

#!/usr/bin/env node

import { createHash } from "node:crypto";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { extname, join } from "node:path";

const manifestPath = process.argv[2] ??
  new URL("../../docs/reference-platform/public-documentation-manifest.tsv", import.meta.url).pathname;
const outputDirectory = process.argv[3];
if (!outputDirectory) {
  throw new Error("usage: node audit-public-documentation-images.mjs MANIFEST OUTPUT_DIRECTORY");
}

async function fetchBody(url) {
  const response = await fetch(url, {
    headers: { "user-agent": "conduit-apollo-visual-contract-audit/1" },
  });
  if (!response.ok) {
    throw new Error(`${url}: HTTP ${response.status}`);
  }
  return { body: new Uint8Array(await response.arrayBuffer()), type: response.headers.get("content-type") ?? "" };
}

const rows = (await readFile(manifestPath, "utf8")).trim().split("\n").slice(1);
const pages = rows.map((row) => row.split("\t")[0]);
const references = [];
const pageQueue = [...pages];
async function pageWorker() {
  while (pageQueue.length > 0) {
    const page = pageQueue.shift();
    const { body } = await fetchBody(page);
    const html = new TextDecoder().decode(body);
    for (const match of html.matchAll(/<img\b[^>]*>/gi)) {
      const tag = match[0];
      const source = tag.match(/\bsrc=["']([^"']+)["']/i)?.[1];
      if (!source) continue;
      const url = new URL(source.replaceAll("&amp;", "&"), page);
      if (!url.pathname.startsWith("/docs/resources/apollo/")) continue;
      url.search = "";
      url.hash = "";
      const alt = tag.match(/\balt=["']([^"']*)["']/i)?.[1] ?? "";
      references.push({ page, alt: alt.trim(), url: url.href });
    }
  }
}
await Promise.all(Array.from({ length: 12 }, () => pageWorker()));

const byURL = new Map();
for (const reference of references) {
  const current = byURL.get(reference.url) ?? { url: reference.url, references: [] };
  current.references.push({ page: reference.page, alt: reference.alt });
  byURL.set(reference.url, current);
}

await mkdir(outputDirectory, { recursive: true });
const images = [...byURL.values()].sort((left, right) => left.url.localeCompare(right.url));
const imageQueue = images.map((image, index) => ({ image, index }));
async function imageWorker() {
  while (imageQueue.length > 0) {
    const { image, index } = imageQueue.shift();
    const { body, type } = await fetchBody(image.url);
    const urlExtension = extname(new URL(image.url).pathname).toLowerCase();
    const extension = /^\.(?:png|jpe?g|gif|webp|svg)$/.test(urlExtension) ? urlExtension : ".bin";
    const name = `${String(index + 1).padStart(3, "0")}-${createHash("sha256").update(image.url).digest("hex").slice(0, 12)}${extension}`;
    await writeFile(join(outputDirectory, name), body);
    image.file = name;
    image.contentType = type;
    image.bytes = body.length;
    image.sha256 = createHash("sha256").update(body).digest("hex");
  }
}
await Promise.all(Array.from({ length: 12 }, () => imageWorker()));

await writeFile(join(outputDirectory, "inventory.json"), JSON.stringify({
  capturedAtUTC: new Date().toISOString(), pages: pages.length,
  totalReferences: references.length, renderedImageReferences: references.length,
  distinctImages: images.length, images,
}, null, 2) + "\n");
process.stdout.write(`captured ${images.length} distinct images (${references.length} references) from ${pages.length} pages into ${outputDirectory}\n`);

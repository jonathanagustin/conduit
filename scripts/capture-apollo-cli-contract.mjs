#!/usr/bin/env node

import { createHash } from "node:crypto";
import { readFile, writeFile } from "node:fs/promises";

const manifestPath = process.argv[2] ?? "docs/reference-platform/public-documentation-manifest.tsv";
const outputPath = process.argv[3] ?? "docs/reference-platform/cli-command-inventory.json";
const captured = process.env.CAPTURE_DATE;
if (!/^\d{4}-\d{2}-\d{2}$/.test(captured ?? "")) {
  throw new Error("CAPTURE_DATE=YYYY-MM-DD is required");
}

const sha256 = (value) => createHash("sha256").update(value).digest("hex");
const manifest = await readFile(manifestPath, "utf8");
const sources = manifest
  .trim()
  .split("\n")
  .map((line) => {
    const [url, category, title, words, headings, sourceSha256] = line.split("\t");
    return { url, category, title, words: Number(words), headings: Number(headings), sourceSha256 };
  })
  .filter((entry) => entry.category === "apollo-cli")
  .sort((left, right) => left.url.localeCompare(right.url));

const commands = [];
for (const source of sources) {
  const response = await fetch(source.url, { headers: { Accept: "text/html" } });
  if (!response.ok) {
    throw new Error(`${source.url}: HTTP ${response.status}`);
  }
  const html = await response.text();
  const nextData = html.match(/<script id="__NEXT_DATA__"[^>]*>(.*?)<\/script>/s);
  if (nextData === null) {
    throw new Error(`${source.url}: __NEXT_DATA__ is missing`);
  }
  const page = JSON.parse(nextData[1]);
  const markdown = page?.props?.pageProps?.markdown;
  if (typeof markdown !== "string") {
    throw new Error(`${source.url}: markdown is missing`);
  }
  const actualHash = sha256(markdown);
  if (actualHash !== source.sourceSha256) {
    throw new Error(`${source.url}: source changed: expected ${source.sourceSha256}, got ${actualHash}`);
  }
  const heading = markdown.match(/^# (.+)$/m)?.[1];
  if (heading === undefined) {
    throw new Error(`${source.url}: command heading is missing`);
  }
  const synopsis = markdown.match(/```bash\n(.*?)\n```/s)?.[1].trim() ?? "";
  const flags = [];
  for (const row of markdown.matchAll(/^\| (.*?) \| .* \|$/gm)) {
    for (const token of row[1].matchAll(/`([^`]+)`/g)) {
      if (token[1].startsWith("-") && !flags.includes(token[1])) {
        flags.push(token[1]);
      }
    }
  }
  commands.push({
    command: heading.replaceAll("\\[", "[").replaceAll("\\]", "]"),
    synopsis,
    experimental: heading.includes("\\[Experimental]"),
    deprecated: heading.includes("\\[Deprecated]"),
    flags,
    source: source.url,
    sourceSha256: actualHash,
  });
}

const contract = {
  schemaVersion: 1,
  captured,
  sourceManifest: manifestPath,
  sourceCount: sources.length,
  commands,
};
await writeFile(outputPath, `${JSON.stringify(contract, null, 2)}\n`);
console.log(`Captured ${commands.length} hash-pinned Apollo CLI reference pages to ${outputPath}`);

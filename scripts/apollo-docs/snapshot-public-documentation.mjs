#!/usr/bin/env node

import { createHash } from "node:crypto";
import { writeFile } from "node:fs/promises";

const base = "https://www.palantir.com";
const seeds = [
  "/docs/apollo/core/introduction/",
  "/docs/apollo/apollo-getting-started/getting-started-with-apollo/",
  "/docs/apollo/apollo-references/apollo-references/",
  "/docs/apollo/core/whats-new/",
];
const expectedMinimumPages = 200;

const outputPath = process.argv[2] ??
  new URL("../../docs/reference-platform/public-documentation-manifest.tsv", import.meta.url).pathname;

function normalizePath(path) {
  const withoutQuery = path.split(/[?#]/, 1)[0];
  return withoutQuery.endsWith("/") ? withoutQuery : `${withoutQuery}/`;
}

// Discovery scans the whole document rather than only its `href` attributes,
// and the crawl below follows what it finds transitively rather than stopping
// after the seeds.
//
// Both changes are regressions from the same miss. The first version read only
// `href` in the four seed pages, which is the rendered sidebar -- so it saw
// exactly the pages the navigation chooses to show. Seven `hidden_`-prefixed
// Apollo Product Specification pages are deliberately absent from that sidebar
// and were missing from the corpus for two weeks: Endpoints, Liveness and
// Readiness, Replication, Resources, Volumes, the Substitution Reference, and
// the full spec definition. They are reachable only as links in other pages'
// body text, which lives in the embedded `__NEXT_DATA__` JSON rather than in an
// anchor the sidebar rendered.
//
// A corpus that mirrors a vendor's navigation is a corpus that inherits the
// vendor's decisions about what to feature. This one has to mirror what is
// published.
function discoverApolloPaths(html) {
  const paths = new Set();
  // Deliberately not anchored to href=: body links arrive as JSON-escaped
  // Markdown inside __NEXT_DATA__, so an attribute-only pattern cannot see them.
  const pattern = /\/docs\/apollo\/[A-Za-z0-9._~\-/]*/g;
  for (const match of html.matchAll(pattern)) {
    // A traversal segment resolves outside Apollo -- one page links
    // /docs/apollo/../foundry/... -- and following it would pull unrelated
    // product documentation into an Apollo corpus.
    if (match[0].includes("..")) continue;
    const path = normalizePath(match[0]);
    if (path.split("/").filter(Boolean).length >= 4) {
      paths.add(path);
    }
  }
  return paths;
}

function sourceMarkdown(html, url) {
  const match = html.match(
    /<script[^>]+id=["']__NEXT_DATA__["'][^>]*>([\s\S]*?)<\/script>/,
  );
  if (!match) {
    throw new Error(`${url}: missing __NEXT_DATA__`);
  }
  const nextData = JSON.parse(match[1]);
  const markdown = nextData?.props?.pageProps?.markdown;
  if (typeof markdown !== "string" || markdown.length === 0) {
    throw new Error(`${url}: missing source Markdown`);
  }
  return markdown.replaceAll("\r\n", "\n");
}

function summarize(url, markdown) {
  const path = new URL(url).pathname;
  const components = path.split("/").filter(Boolean);
  const section = components[2];
  const title = markdown.match(/^#\s+(.+)$/m)?.[1]?.trim();
  if (!title) {
    throw new Error(`${url}: missing level-one title`);
  }
  const words = markdown.trim().split(/\s+/u).filter(Boolean).length;
  const headings = markdown.match(/^#{1,6}\s+.+$/gm)?.length ?? 0;
  const digest = createHash("sha256").update(markdown).digest("hex");
  return [url, section, title, words, headings, digest];
}

async function fetchText(url) {
  const response = await fetch(url, {
    headers: { "user-agent": "conduit-apollo-contract-inventory/1" },
  });
  if (!response.ok) {
    throw new Error(`${url}: HTTP ${response.status}`);
  }
  return response.text();
}

const seedBodies = await Promise.all(
  seeds.map(async (path) => [path, await fetchText(base + path)]),
);
const paths = new Set(seeds.map(normalizePath));
for (const [, html] of seedBodies) {
  for (const path of discoverApolloPaths(html)) {
    paths.add(path);
  }
}
if (paths.size < expectedMinimumPages) {
  throw new Error(
    `navigation discovery returned only ${paths.size} pages; expected at least ${expectedMinimumPages}`,
  );
}

const concurrency = 12;
const queue = [...paths].sort();
const rows = [];
// Each fetched page is also a source of links, so the crawl runs to closure
// rather than to the seeds' horizon. `paths` doubles as the visited set, which
// is what keeps a cycle -- and this documentation is full of them -- finite.
async function worker() {
  while (queue.length > 0) {
    const path = queue.shift();
    const url = base + path;
    const html = await fetchText(url);
    for (const discovered of discoverApolloPaths(html)) {
      if (!paths.has(discovered)) {
        paths.add(discovered);
        queue.push(discovered);
      }
    }
    rows.push(summarize(url, sourceMarkdown(html, url)));
  }
}
await Promise.all(Array.from({ length: concurrency }, worker));
rows.sort((left, right) => left[0].localeCompare(right[0]));

const escapeTSV = (value) => String(value).replaceAll("\t", " ").replaceAll("\n", " ");
const manifest = [
  ["url", "section", "title", "words", "headings", "content_sha256"],
  ...rows,
].map((row) => row.map(escapeTSV).join("\t")).join("\n") + "\n";
await writeFile(outputPath, manifest, "utf8");

const totalWords = rows.reduce((total, row) => total + row[3], 0);
const totalHeadings = rows.reduce((total, row) => total + row[4], 0);
process.stdout.write(
  `captured ${rows.length} pages, ${totalWords} words, ${totalHeadings} headings -> ${outputPath}\n`,
);

#!/usr/bin/env node
// Re-crawls Apollo's public documentation and fails if the committed manifest
// no longer describes it.
//
// Why this exists: the manifest is read by things that treat it as evidence.
// `scripts/apollo-graphql/verify-apollo-interface.mjs` resolves every
// `documentedBy` citation against it, so a citation "resolves" whenever the URL
// is in this file -- not when the page exists. Nothing re-crawled, so the
// corpus aged silently. By the time anyone looked it had drifted by 14 pages,
// and one of those changes added a fifth Environment connection setting to a
// contract this repository implements with four.
//
// That is the failure mode worth naming: a citation checked against a stale
// index is not checked. The index has to be checkable too.
//
// This is deliberately NOT wired into the ordinary build. It reaches a third
// party's website, so running it on every commit would be both flaky and rude.
// It skips cleanly when the network is unavailable, exactly like the cluster
// acceptance checks skip without a cluster, so it can sit in a scheduled or
// manually dispatched job without breaking anything offline.
//
//   node scripts/verify-documentation-manifest.mjs            # report drift, exit 1 on any
//   node scripts/verify-documentation-manifest.mjs --write    # refresh the manifest in place
import { createHash } from "node:crypto";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(HERE, "..");
const MANIFEST = resolve(ROOT, "docs/reference-platform/public-documentation-manifest.tsv");
const CAPTURE = resolve(ROOT, "docs/reference-platform/public-documentation-capture.json");

const base = "https://www.palantir.com";
const seeds = [
  "/docs/apollo/core/introduction/",
  "/docs/apollo/apollo-getting-started/getting-started-with-apollo/",
  "/docs/apollo/apollo-references/apollo-references/",
  "/docs/apollo/core/whats-new/",
];
// The same floor snapshot-public-documentation.mjs uses. A crawl that returns
// far fewer pages than the corpus has is a broken crawl, not a shrunken corpus,
// and must not be allowed to report every page as removed.
const expectedMinimumPages = 200;

const write = process.argv.includes("--write");
const requireNetwork = process.argv.includes("--require-network");

function normalizePath(path) {
  const withoutQuery = path.split(/[?#]/, 1)[0];
  return withoutQuery.endsWith("/") ? withoutQuery : `${withoutQuery}/`;
}

// Scans the whole document, not only `href` attributes, and the crawl below
// runs to closure. Both match snapshot-public-documentation.mjs, and for the
// same reason: an attribute-only crawl of the seeds sees the rendered sidebar,
// which omits the seven `hidden_` Apollo Product Specification pages. A drift
// gate built on the narrower discovery would have reported a corpus missing
// those pages as complete, which is the failure it exists to catch.
function discoverApolloPaths(html) {
  const paths = new Set();
  for (const match of html.matchAll(/\/docs\/apollo\/[A-Za-z0-9._~\-/]*/g)) {
    if (match[0].includes("..")) continue;
    const path = normalizePath(match[0]);
    if (path.split("/").filter(Boolean).length >= 4) paths.add(path);
  }
  return paths;
}

function sourceMarkdown(html, url) {
  const match = html.match(/<script[^>]+id=["']__NEXT_DATA__["'][^>]*>([\s\S]*?)<\/script>/);
  if (!match) throw new Error(`${url}: missing __NEXT_DATA__`);
  const markdown = JSON.parse(match[1])?.props?.pageProps?.markdown;
  if (typeof markdown !== "string" || markdown.length === 0) throw new Error(`${url}: missing source Markdown`);
  return markdown.replaceAll("\r\n", "\n");
}

function summarize(url, markdown) {
  const components = new URL(url).pathname.split("/").filter(Boolean);
  const title = markdown.match(/^#\s+(.+)$/m)?.[1]?.trim();
  if (!title) throw new Error(`${url}: missing level-one title`);
  return [
    url,
    components[2],
    title,
    markdown.trim().split(/\s+/u).filter(Boolean).length,
    markdown.match(/^#{1,6}\s+.+$/gm)?.length ?? 0,
    createHash("sha256").update(markdown).digest("hex"),
  ];
}

async function fetchText(url) {
  const response = await fetch(url, { headers: { "user-agent": "conduit-apollo-contract-inventory/1" } });
  if (!response.ok) throw new Error(`${url}: HTTP ${response.status}`);
  return response.text();
}

function readManifest(path) {
  const rows = new Map();
  for (const [index, line] of readFileSync(path, "utf8").split("\n").entries()) {
    if (index === 0 || !line.trim()) continue;
    const fields = line.split("\t");
    if (fields.length >= 6) {
      rows.set(fields[0], { section: fields[1], title: fields[2], words: fields[3], headings: fields[4], sha: fields[5] });
    }
  }
  return rows;
}

if (!existsSync(MANIFEST)) {
  console.error(`FAIL missing ${MANIFEST}`);
  process.exit(1);
}

// Reaching the site and a page still publishing content are different
// questions, and the first version of this conflated them: the whole crawl sat
// in one try/catch that reported any failure as "could not reach the site" and
// exited 0. A page that had been unpublished would therefore be announced as a
// network problem and pass.
//
// That is not hypothetical here. This documentation site answers HTTP 200 for
// paths that do not exist, serving a "Page Not Found" shell, so the status code
// cannot be used to detect removal at all. The only reliable signal is whether
// the response carries `pageProps.markdown`. Seed failures still skip -- if the
// four entry points are unreachable there is nothing to compare against -- but
// a page that answers without content is drift and is reported as such.
const contentless = [];
let crawled;
try {
  const seedBodies = await Promise.all(seeds.map((path) => fetchText(base + path)));
  const paths = new Set(seeds.map(normalizePath));
  for (const html of seedBodies) for (const path of discoverApolloPaths(html)) paths.add(path);
  if (paths.size < expectedMinimumPages) {
    // A truncated navigation would otherwise report the whole corpus as removed,
    // which reads as catastrophic drift when it is really a broken crawl.
    console.error(`FAIL navigation discovery returned only ${paths.size} pages; expected at least ${expectedMinimumPages}`);
    process.exit(1);
  }
  const queue = [...paths].sort();
  const rows = [];
  await Promise.all(
    Array.from({ length: 12 }, async () => {
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
        let markdown;
        try {
          markdown = sourceMarkdown(html, url);
        } catch {
          // Answered, but with no source Markdown: the 200-status "Page Not
          // Found" shell. Recorded rather than thrown, so one unpublished page
          // does not abort the crawl and hide every other difference.
          contentless.push(url);
          continue;
        }
        rows.push(summarize(url, markdown));
      }
    }),
  );
  crawled = rows;
} catch (error) {
  // Offline, proxied, or rate-limited. Reporting drift requires a crawl; not
  // having one is not evidence that the corpus is current, and it is not
  // evidence that it is stale either. Say so and stop.
  if (requireNetwork) {
    console.error(`FAIL documentation manifest: crawl unavailable; no current-corpus PASS (${error.message})`);
    process.exit(1);
  }
  console.log(`documentation manifest: SKIP (could not reach the documentation site: ${error.message})`);
  process.exit(0);
}

const committed = readManifest(MANIFEST);
const fresh = new Map(crawled.map((row) => [row[0], { section: row[1], title: row[2], words: String(row[3]), headings: String(row[4]), sha: row[5] }]));

for (const url of contentless) {
  console.error(
    `FAIL ${url} answers without source Markdown, which on this site is the 200-status "Page Not Found" ` +
      "shell. The page is linked but no longer published.",
  );
}

const added = [...fresh.keys()].filter((url) => !committed.has(url)).sort();
const removed = [...committed.keys()].filter((url) => !fresh.has(url)).sort();
const changed = [...fresh.keys()].filter((url) => committed.has(url) && committed.get(url).sha !== fresh.get(url).sha).sort();

if (write) {
  // Refuse to record a crawl that did not work. The page-count floor above is
  // checked against DISCOVERED paths, not against pages that actually yielded
  // source Markdown, so a site-wide failure -- every response serving the
  // 200-status "Page Not Found" shell -- would sail past it and land here with
  // an empty `crawled`. Writing then produces a header-only manifest, a
  // pageCount of 0, and today's date stamped on the capture record: the exact
  // state the freshness gate exists to detect, created by the tool meant to
  // clear it. The FAIL lines for `contentless` are printed above; they must
  // stop the write, not merely accompany it.
  if (contentless.length > 0) {
    console.error(`FAIL refusing to write: ${contentless.length} page(s) answered without source Markdown`);
    process.exit(1);
  }
  if (crawled.length < expectedMinimumPages) {
    console.error(`FAIL refusing to write: only ${crawled.length} page(s) yielded content; expected at least ${expectedMinimumPages}`);
    process.exit(1);
  }
  const header = "url\tsection\ttitle\twords\theadings\tcontent_sha256";
  const body = crawled.sort((left, right) => left[0].localeCompare(right[0])).map((row) => row.join("\t"));
  writeFileSync(MANIFEST, `${[header, ...body].join("\n")}\n`);
  // The capture record is refreshed here, in the same step, because it is a
  // statement about this crawl. Leaving it to the operator would mean the one
  // file whose whole job is to say "somebody checked recently" could be left
  // saying it when nobody had.
  const capture = JSON.parse(readFileSync(CAPTURE, "utf8"));
  capture.capturedAt = new Date().toISOString().slice(0, 10);
  capture.pageCount = crawled.length;
  writeFileSync(CAPTURE, `${JSON.stringify(capture, null, 2)}\n`);
  console.log(`documentation manifest: refreshed to ${crawled.length} page(s) (+${added.length} -${removed.length} ~${changed.length})`);
  console.log(`documentation capture: recorded ${capture.capturedAt}`);
  process.exit(0);
}

for (const url of added) console.error(`FAIL new page not in the manifest: [${fresh.get(url).section}] ${fresh.get(url).title} -- ${url}`);
for (const url of removed) console.error(`FAIL manifest page no longer published: [${committed.get(url).section}] ${committed.get(url).title} -- ${url}`);
for (const url of changed) {
  const [before, after] = [committed.get(url), fresh.get(url)];
  console.error(
    `FAIL page content changed: [${after.section}] ${after.title} -- words ${before.words}->${after.words}, ` +
      `headings ${before.headings}->${after.headings} -- ${url}`,
  );
}

if (added.length || removed.length || changed.length || contentless.length) {
  console.error(
    `\n${added.length} new, ${removed.length} removed, ${changed.length} changed, ${contentless.length} unpublished. ` +
      "Re-run with --write to refresh the " +
      "manifest, then read each changed page: a contract this repository implements may have moved. Record what " +
      "changed rather than only bumping the hashes.",
  );
  process.exit(1);
}
console.log(`PASS documentation manifest: ${fresh.size} page(s) match the published corpus exactly`);

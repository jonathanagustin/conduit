#!/usr/bin/env node

import { readFile, writeFile } from "node:fs/promises";

const manifestPath = process.argv[2] ??
  new URL("./public-documentation-manifest.tsv", import.meta.url).pathname;
const outputPath = process.argv[3];
if (!outputPath) {
  throw new Error("usage: node audit-public-documentation-media.mjs MANIFEST OUTPUT_JSON");
}

async function fetchText(url) {
  const response = await fetch(url, { headers: { "user-agent": "platform-apollo-media-contract-audit/1" } });
  if (!response.ok) throw new Error(`${url}: HTTP ${response.status}`);
  return response.text();
}

function sourceMarkdown(html, url) {
  const match = html.match(/<script[^>]+id=["']__NEXT_DATA__["'][^>]*>([\s\S]*?)<\/script>/);
  if (!match) throw new Error(`${url}: missing __NEXT_DATA__`);
  const markdown = JSON.parse(match[1])?.props?.pageProps?.markdown;
  if (typeof markdown !== "string") throw new Error(`${url}: missing source Markdown`);
  return markdown;
}

function videoURLs(text, base) {
  const result = new Set();
  for (const match of text.matchAll(/(?:href|src)=["']([^"']+)["']/gi)) {
    const decoded = match[1].replaceAll("&amp;", "&");
    let url;
    try { url = new URL(decoded, base); } catch { continue; }
    const host = url.hostname.replace(/^www\./, "");
    const isYouTube = (host === "youtube.com" && (url.pathname === "/watch" || url.pathname.startsWith("/embed/"))) || host === "youtu.be";
    const isFile = /\.(?:mp4|webm|mov|m3u8)(?:$|[?#])/i.test(url.href);
    const isPlayer = /(?:vimeo\.com|wistia\.(?:com|net))$/i.test(host);
    if (isYouTube || isFile || isPlayer) result.add(url.href);
  }
  return result;
}

function canonicalVideoURL(value) {
  const url = new URL(value);
  const host = url.hostname.replace(/^www\./, "");
  if (host === "youtu.be") return `https://www.youtube.com/watch?v=${url.pathname.slice(1)}`;
  if (host === "youtube.com" && url.pathname.startsWith("/embed/")) return `https://www.youtube.com/watch?v=${url.pathname.split("/")[2]}`;
  if (host === "youtube.com" && url.pathname === "/watch") return `https://www.youtube.com/watch?v=${url.searchParams.get("v")}`;
  url.hash = "";
  return url.href;
}

const rows = (await readFile(manifestPath, "utf8")).trim().split("\n").slice(1);
const queue = rows.map((row) => row.split("\t")[0]);
const references = [];
async function worker() {
  while (queue.length > 0) {
    const page = queue.shift();
    const html = await fetchText(page);
    const markdown = sourceMarkdown(html, page);
    const source = new Set([...videoURLs(markdown, page)].map(canonicalVideoURL));
    for (const rawURL of videoURLs(html, page)) {
      const url = canonicalVideoURL(rawURL);
      references.push({ page, rawURL, url, inSourceMarkdown: source.has(url) });
    }
  }
}
await Promise.all(Array.from({ length: 12 }, worker));

const videos = [];
for (const url of [...new Set(references.map((reference) => reference.url))].sort()) {
  const occurrences = references.filter((reference) => reference.url === url);
  const video = { url, references: occurrences.length, pages: [...new Set(occurrences.map((reference) => reference.page))].sort(), inSourceMarkdown: occurrences.some((reference) => reference.inSourceMarkdown) };
  if (new URL(url).hostname.endsWith("youtube.com")) {
    try {
      const metadata = JSON.parse(await fetchText(`https://www.youtube.com/oembed?url=${encodeURIComponent(url)}&format=json`));
      video.title = metadata.title;
      video.author = metadata.author_name;
    } catch (error) {
      video.metadataError = String(error);
    }
  }
  videos.push(video);
}

await writeFile(outputPath, JSON.stringify({
  capturedAtUTC: new Date().toISOString(), pages: rows.length, totalReferences: references.length,
  distinctVideos: videos.length, videos,
}, null, 2) + "\n");
process.stdout.write(`inventoried ${videos.length} distinct videos (${references.length} references) from ${rows.length} pages -> ${outputPath}\n`);

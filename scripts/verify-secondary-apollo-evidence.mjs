import { readFileSync, existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const evidenceDirectory = join(root, "docs", "apollo", "third-party");
const manifest = JSON.parse(readFileSync(join(evidenceDirectory, "evidence-manifest.json"), "utf8"));
if (manifest.schemaVersion !== 1 || !Array.isArray(manifest.sources) || manifest.sources.length === 0) {
  throw new Error("secondary Apollo evidence manifest must contain versioned sources");
}
const hashes = new Set();
for (const source of manifest.sources) {
  for (const field of ["id", "url", "captureDate", "authority", "htmlSha256", "note", "compatibilityDisposition", "reuse"]) {
    if (!source[field]) throw new Error(`${source.id ?? "source"} is missing ${field}`);
  }
  if (!/^https:\/\//.test(source.url) || !/^[a-f0-9]{64}$/.test(source.htmlSha256)) {
    throw new Error(`${source.id} has an invalid URL or HTML hash`);
  }
  if (source.authority === "primary" || source.compatibilityDisposition !== "no-change") {
    throw new Error(`${source.id} may not expand compatibility authority`);
  }
  if (!existsSync(join(evidenceDirectory, source.note))) throw new Error(`${source.id} note is missing`);
  if ("localMediaPath" in source) throw new Error(`${source.id} stores prohibited third-party media`);
  for (const media of source.media ?? []) {
    if (!/^[a-f0-9]{64}$/.test(media.sha256) || !Array.isArray(media.urls) || media.urls.length === 0) {
      throw new Error(`${source.id} has invalid media evidence`);
    }
    if (media.storedInRepository !== false || "localPath" in media) {
      throw new Error(`${source.id} stores media despite its reuse disposition`);
    }
    hashes.add(media.sha256);
  }
}
console.log(`PASS secondary Apollo evidence: ${manifest.sources.length} source(s), ${hashes.size} media hash(es)`);

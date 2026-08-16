#!/usr/bin/env node
// Capture the tenant's Rosetta APOLLO release-note registry as facts only.
//
// Rosetta is the platform's documentation service (`/documentation/api`). It
// serves release notes to the Foundry apps that render them, and its APOLLO
// corpus is a different and larger record than the public What's New page:
// on 2026-08-15 it carried 178 notes naming 134 Apollo CLI versions, where the
// public page named 42. That gap is the reason this exists -- the public
// changelog is a subset, so a repository that reads only the public page is
// reading an edited view of what shipped.
//
// WHAT IS AND IS NOT WRITTEN. Every note carries `title`, `description` and
// `longDescription`. None of the three is committed. What is committed is the
// note's identity and shape: id, date, type, release version, application
// identity, media count, and a SHA-256 over the full record so drift is
// detectable without retaining the text. That follows the rule the existing
// Rosetta evidence already states -- counts establish coverage, prose is not
// copied into this repository -- and it is why the artifact is a registry
// rather than a mirror.
//
// AUTH IS PER-ENDPOINT, and this is not guessable. The pagination endpoint
// takes `Authorization: Bearer`; the media endpoint takes a `PALANTIR_TOKEN`
// cookie, because media is served to an <img src> which cannot set a header.
// Sending the wrong channel yields Conjure:MissingCredentials -- "no credentials
// seen" -- which looks identical to Default:Unauthorized ("credentials seen and
// rejected") as a 401 and means the opposite thing. Only pagination is used
// here; media bytes stay where they already are, pinned by OCI digest.
//
// The tenant origin is never written. It is read from the environment and
// recorded only as a salted digest, the same way the Hub fingerprint is.
//
//   FOUNDRY_HOST=https://<tenant> PALANTIR_TOKEN=... \
//     node scripts/capture-rosetta-release-notes.mjs
import { createHash } from "node:crypto";
import { writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const OUT = resolve(HERE, "../docs/apollo/observed/apollo-rosetta-release-notes.json");

const host = (process.env.FOUNDRY_HOST || process.env.FOUNDRY_ORIGIN || "").replace(/\/$/, "");
const token = process.env.PALANTIR_TOKEN || process.env.FOUNDRY_TOKEN || "";
if (!host || !token) {
  // Absent credentials are not a failure. This reaches an authenticated tenant,
  // so it cannot run in ordinary CI, and saying so is more useful than a stack
  // trace that reads like a broken script.
  console.log("rosetta release notes: SKIP (FOUNDRY_HOST and PALANTIR_TOKEN are required)");
  process.exit(0);
}

// A salted digest, so the artifact records WHICH tenant answered without
// recording the tenant. The salt is fixed and public: the point is that the
// origin is not recoverable from the digest by reading this file, not that the
// mapping is secret from someone who already knows the origin.
const originFingerprint = createHash("sha256").update(`conduit-rosetta-origin:${host}`).digest("hex");

const notes = new Map();
let offset = 0;
for (let guard = 0; guard < 500; guard += 1) {
  const response = await fetch(`${host}/documentation/api/v2/release-notes/pagination`, {
    method: "POST",
    headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
    body: JSON.stringify({ productId: "APOLLO", applicationIds: [], pageSize: 100, ...(offset ? { offset } : {}) }),
  });
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    // The error NAME is the diagnostic; the status alone conflates three
    // different causes. It is printed because it carries no tenant data.
    console.error(`rosetta release notes: HTTP ${response.status} ${body.errorName ?? ""}`);
    process.exit(1);
  }
  const page = await response.json();
  const batch = page.releaseNotes ?? [];
  if (!batch.length) break;
  for (const note of batch) notes.set(note.id, note);
  // A nextOffset that does not advance would spin forever; treat it as the end
  // rather than as progress.
  if (page.nextOffset == null || page.nextOffset === offset) break;
  offset = page.nextOffset;
}

const rows = [...notes.values()]
  .map((note) => ({
    id: note.id,
    date: note.date,
    type: note.type,
    // Two different identity fields exist and neither is always populated:
    // `releaseVersion` is the shipped version string (`v0.814.0`, or
    // `autopilot@4.180.0` for an app rather than the CLI), and `applicationId`
    // is the surface it belongs to. Both are kept because the join differs by
    // note, and collapsing them would silently drop one form.
    releaseVersion: note.releaseVersion ?? null,
    applicationId: note.applicationId || null,
    categoryId: note.categoryId || null,
    mediaCount: (note.mediaFilenames ?? []).length,
    // Over the whole record, including the prose that is not committed, so a
    // reworded note is detectable here without the wording being retained.
    sha256: createHash("sha256").update(JSON.stringify(note)).digest("hex"),
  }))
  .sort((left, right) => (left.date === right.date ? left.id.localeCompare(right.id) : left.date.localeCompare(right.date)));

const count = (key) => {
  const tally = {};
  for (const row of rows) if (row[key]) tally[row[key]] = (tally[row[key]] ?? 0) + 1;
  return tally;
};

writeFileSync(
  OUT,
  `${JSON.stringify(
    {
      schemaVersion: 1,
      scope:
        "Facts-only registry of the tenant Rosetta APOLLO release-note corpus. Identity, dates, versions and " +
        "per-record digests only: titles, descriptions and long descriptions are deliberately not copied into " +
        "this repository, and neither is the tenant origin.",
      originFingerprint,
      noteCount: rows.length,
      byType: count("type"),
      byApplication: count("applicationId"),
      distinctReleaseVersions: new Set(rows.map((row) => row.releaseVersion).filter(Boolean)).size,
      earliest: rows[0]?.date ?? null,
      latest: rows[rows.length - 1]?.date ?? null,
      rows,
    },
    null,
    2,
  )}\n`,
);

console.log(
  `rosetta release notes: ${rows.length} APOLLO note(s), ` +
    `${new Set(rows.map((r) => r.releaseVersion).filter(Boolean)).size} distinct version(s) -> ${OUT}`,
);

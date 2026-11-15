// Preserve raw measurements and distinguish individually reviewed equivalents.
import { readFileSync, existsSync } from "node:fs";
import { createHash } from "node:crypto";
import { join } from "node:path";
export const sha256 = (value) => createHash("sha256").update(value).digest("hex");
const identity = (m) => `${m.file}:${m.line}:${m.column}:${m.type}`;
export function equivalentReview(root, name) {
  const path = join(root, "docs/operations/mutation-equivalents.json");
  if (!existsSync(path)) return null;
  const manifest = JSON.parse(readFileSync(path, "utf8"));
  if (manifest.schemaVersion !== 1) throw Error("unsupported equivalence manifest");
  const entry = manifest.packages?.[name];
  if (!entry) return null;
  if (!Array.isArray(entry.mutants) || !entry.sources) throw Error("invalid equivalence review");
  for (const [file, hash] of Object.entries(entry.sources)) {
    if (!/^[\w.-]+\.go$/.test(file) || !/^[a-f0-9]{64}$/.test(hash) || sha256(readFileSync(join(root, name, file))) !== hash) throw Error(`stale equivalent source: ${name}/${file}`);
  }
  const keys = new Set();
  for (const m of entry.mutants) {
    if (!entry.sources[m.file] || !Number.isSafeInteger(m.line) || m.line < 1 || !Number.isSafeInteger(m.column) || m.column < 1 || typeof m.type !== "string" || typeof m.reason !== "string" || m.reason.length < 40) throw Error("incomplete equivalent argument");
    const key = identity(m);
    if (keys.has(key)) throw Error("duplicate equivalent mutation");
    keys.add(key);
  }
  return { entry, keys, digest: sha256(JSON.stringify(entry)) };
}
export function quality(score, review, requireExplicit = true) {
  const count = score.equivalent ?? 0;
  if (!Number.isSafeInteger(count) || count < 0 || count > score.lived) throw Error("invalid equivalent count");
  if (review && count !== review.keys.size) throw Error("measurement omitted its source-bound equivalent review");
  if (count && (!review || review.keys.size !== count || review.digest !== score.equivalentManifestSha256)) throw Error("equivalent measurement does not match its source-bound review");
  const denominator = score.killed + score.lived - count;
  const result = denominator === 0 ? 100 : (100 * score.killed) / denominator;
  if (score.nonEquivalentEfficacy !== undefined && (!Number.isFinite(score.nonEquivalentEfficacy) || Math.abs(result - score.nonEquivalentEfficacy) > 0.01)) throw Error("non-equivalent efficacy does not follow from counts");
  if (requireExplicit && count && score.nonEquivalentEfficacy === undefined) throw Error("equivalent count has no explicit separate efficacy");
  return result;
}
export function scoreReport(root, name, report) {
  for (const file of report.files ?? []) for (const mutant of file.mutations ?? []) {
    if (mutant.status === "TIMED OUT") throw Error("timed-out mutants cannot refresh a baseline; increase the validated timeout and rerun");
  }
  for (const field of ["mutants_killed", "mutants_lived", "mutants_total"]) if (!Number.isSafeInteger(report[field]) || report[field] < 0) throw Error(`invalid engine count: ${field}`);
  const killed = report.mutants_killed, lived = report.mutants_lived;
  const efficacy = killed + lived === 0 ? 100 : (100 * killed) / (killed + lived);
  if (!Number.isFinite(report.test_efficacy) || Math.abs(efficacy - report.test_efficacy) > 0.01 || report.mutants_total < killed + lived || !Number.isFinite(report.mutations_coverage)) throw Error("inconsistent engine measurement");
  const review = equivalentReview(root, name);
  if (review) {
    const actual = new Map();
    for (const file of report.files ?? []) for (const m of file.mutations ?? []) {
      const key = identity({ ...m, file: file.file_name });
      if (actual.has(key)) throw Error("duplicate engine mutation identity");
      actual.set(key, m.status);
    }
    for (const key of review.keys) if (actual.get(key) !== "LIVED") throw Error(`reviewed equivalent is missing or no longer LIVED: ${key}`);
  }
  const result = { package: name, efficacy, killed, lived, total: report.mutants_total, coverage: report.mutations_coverage, equivalent: review?.keys.size ?? 0, equivalentManifestSha256: review?.digest ?? "" };
  result.nonEquivalentEfficacy = quality(result, review, false);
  return result;
}

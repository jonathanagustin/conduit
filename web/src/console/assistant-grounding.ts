/** Native source metadata is accepted only from Hub turn/thread documents. */
export type SourceCitation = {
  id: string;
  kind: "operator-documentation" | "ontology-observation";
  title: string;
  source: string;
  quote: string;
  excerptSha256: string;
  documentSha256: string | null;
  firstLine: number;
  lastLine: number;
  startByte: number;
  endByte: number;
  retrievedAt: string;
  auditEventId: string;
};

export type GroundingEvidence = {
  state: "available" | "unavailable";
  sources: SourceCitation[];
  truncated: boolean;
};

const encoder = new TextEncoder();
const digest = (value: unknown): value is string =>
  typeof value === "string" && /^[0-9a-f]{64}$/.test(value);
const text = (value: unknown, maximum: number): value is string =>
  typeof value === "string" &&
  value.length > 0 &&
  encoder.encode(value).length <= maximum;
const integer = (value: unknown, maximum: number): value is number =>
  typeof value === "number" &&
  Number.isSafeInteger(value) &&
  value >= 0 &&
  value <= maximum;
const object = (value: unknown): Record<string, unknown> | null =>
  value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;

function citation(value: unknown): SourceCitation | null {
  const row = object(value);
  if (
    !row ||
    !text(row["id"], 69) ||
    !/^csrc_[0-9a-f]{64}$/.test(row["id"]) ||
    row["valueTier"] !== "CONDUIT_NATIVE" ||
    !text(row["title"], 256) ||
    !text(row["source"], 512) ||
    !text(row["quote"], 512) ||
    !digest(row["excerptSha256"]) ||
    !text(row["auditEventId"], 256) ||
    !text(row["retrievedAt"], 64) ||
    !/^\d{4}-\d{2}-\d{2}T/.test(row["retrievedAt"]) ||
    !Number.isFinite(
      Date.parse(row["retrievedAt"].replace(/(\.\d{3})\d+/, "$1")),
    ) ||
    row["retrievedAt"].startsWith("0001-01-01")
  )
    return null;
  const kind = row["kind"];
  if (kind !== "operator-documentation" && kind !== "ontology-observation")
    return null;
  const first = row["firstLine"] ?? 0,
    last = row["lastLine"] ?? 0;
  const start = row["startByte"] ?? 0,
    end = row["endByte"] ?? 0;
  if (
    !integer(first, 524288) ||
    !integer(last, 524288) ||
    !integer(start, 524288) ||
    !integer(end, 524288)
  )
    return null;
  if (kind === "operator-documentation") {
    if (
      !digest(row["documentSha256"]) ||
      first < 1 ||
      last < first ||
      end < start ||
      end - start !== encoder.encode(row["quote"]).length
    )
      return null;
  } else if (
    (row["documentSha256"] !== undefined && row["documentSha256"] !== "") ||
    first !== 0 ||
    last !== 0 ||
    start !== 0 ||
    end !== 0
  )
    return null;
  return {
    id: row["id"],
    kind,
    title: row["title"],
    source: row["source"],
    quote: row["quote"],
    excerptSha256: row["excerptSha256"],
    documentSha256:
      kind === "operator-documentation"
        ? (row["documentSha256"] as string)
        : null,
    firstLine: first,
    lastLine: last,
    startByte: start,
    endByte: end,
    retrievedAt: row["retrievedAt"],
    auditEventId: row["auditEventId"],
  };
}

/** Malformed evidence is unavailable, never a successful empty retrieval. */
export function parseGrounding(value: unknown): GroundingEvidence | null {
  if (value === null || value === undefined) return null;
  const unavailable = (): GroundingEvidence => ({
    state: "unavailable",
    sources: [],
    truncated: false,
  });
  const row = object(value);
  if (
    !row ||
    row["schemaVersion"] !== "conduit.assistant-grounding/v1" ||
    !Array.isArray(row["sources"]) ||
    row["sources"].length > 8 ||
    typeof row["truncated"] !== "boolean"
  )
    return unavailable();
  const sources: SourceCitation[] = [],
    ids = new Set<string>();
  for (const raw of row["sources"]) {
    const source = citation(raw);
    if (!source || ids.has(source.id)) return unavailable();
    ids.add(source.id);
    sources.push(source);
  }
  return { state: "available", sources, truncated: row["truncated"] };
}

import { HubError } from "./hub-error.js";

export type AdminAIUsageRow = {
  day: string;
  owner: string;
  capability: string;
  transport: string;
  configuredModel: string;
  providerModel: string;
  outcome: string;
  calls: number;
  budgetedCalls: number;
  attempts: number;
  promptTokens: number | null;
  completionTokens: number | null;
  callsWithTokens: number;
  latencyMillis: number;
};

export type AdminAIUsageReport = {
  days: number;
  dailyCallBudget: number | null;
  dailyTokenBudget: number | null;
  usage: AdminAIUsageRow[];
};

export type AdminAIRunSummary = {
  runId: string;
  sessionId: string;
  owner: string;
  capability: string;
  rounds: number;
  bytes: number;
  firstAt: string;
  expiresAt: string;
};

export type AdminAITranscriptsList = {
  transcripts: string;
  retention: string;
  runs: AdminAIRunSummary[];
};

export type AdminAITranscriptExchange = {
  round: number;
  capability: string;
  auditEventId: string;
  outcome: string;
  transcript: string;
  present: boolean;
  prompt: string;
  response: string;
  sentPromptSha256: string;
  receivedResponseSha256: string;
  promptFidelity: string;
  responseFidelity: string;
  chainSha256: string;
};

export type AdminAITranscriptExport = {
  runId: string;
  sessionId: string;
  owner: string;
  verified: boolean;
  problems: string[];
  exchanges: AdminAITranscriptExchange[];
};

function unavailable(message: string): never {
  throw new HubError(502, message);
}

function asObject(value: unknown, message: string): Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value))
    return unavailable(message);
  return value as Record<string, unknown>;
}

function asCount(value: unknown, message: string): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0)
    return unavailable(message);
  return value;
}

function asNullableCount(value: unknown, message: string): number | null {
  if (value === null || value === undefined) return null;
  return asCount(value, message);
}

function asString(value: unknown, fallback = ""): string {
  return typeof value === "string" ? value : fallback;
}

export function parseAdminAIUsage(value: unknown): AdminAIUsageReport {
  const msg = "Fleet AI usage could not be read.";
  const body = asObject(value, msg);
  const days = asCount(body.days, msg);
  if (days < 1 || days > 92 || !Array.isArray(body.usage))
    return unavailable(msg);
  const usage: AdminAIUsageRow[] = body.usage.map((raw) => {
    const row = asObject(raw, msg);
    if (typeof row.day !== "string" || typeof row.owner !== "string")
      return unavailable(msg);
    return {
      day: row.day,
      owner: row.owner,
      capability: asString(row.capability, "unknown"),
      transport: asString(row.transport),
      configuredModel: asString(row.configuredModel),
      providerModel: asString(row.providerModel),
      outcome: asString(row.outcome, "unknown"),
      calls: asCount(row.calls, msg),
      budgetedCalls: asCount(row.budgetedCalls, msg),
      attempts: asCount(row.attempts, msg),
      promptTokens: asNullableCount(row.promptTokens, msg),
      completionTokens: asNullableCount(row.completionTokens, msg),
      callsWithTokens: asCount(row.callsWithTokens, msg),
      latencyMillis: asCount(row.latencyMillis, msg),
    };
  });
  return {
    days,
    dailyCallBudget: asNullableCount(body.dailyCallBudget, msg),
    dailyTokenBudget: asNullableCount(body.dailyTokenBudget, msg),
    usage,
  };
}

export function parseAdminAITranscriptsList(
  value: unknown,
): AdminAITranscriptsList {
  const msg = "AI transcript index could not be read.";
  const body = asObject(value, msg);
  if (typeof body.transcripts !== "string" || !Array.isArray(body.runs))
    return unavailable(msg);
  const runs: AdminAIRunSummary[] = body.runs.map((raw) => {
    const row = asObject(raw, msg);
    if (typeof row.runId !== "string" || !row.runId.trim())
      return unavailable(msg);
    return {
      runId: row.runId,
      sessionId: asString(row.sessionId),
      owner: asString(row.owner, "unknown"),
      capability: asString(row.capability, "unknown"),
      rounds: asCount(row.rounds, msg),
      bytes: asCount(row.bytes, msg),
      firstAt: asString(row.firstAt),
      expiresAt: asString(row.expiresAt),
    };
  });
  return {
    transcripts: body.transcripts,
    retention: asString(body.retention, "unknown"),
    runs,
  };
}

export function parseAdminAITranscriptExport(
  value: unknown,
): AdminAITranscriptExport {
  const msg = "Audited AI transcript export could not be read.";
  const body = asObject(value, msg);
  if (
    typeof body.runId !== "string" ||
    !body.runId.trim() ||
    typeof body.verified !== "boolean" ||
    !Array.isArray(body.exchanges)
  )
    return unavailable(msg);
  const exchanges: AdminAITranscriptExchange[] = body.exchanges.map((raw) => {
    const row = asObject(raw, msg);
    return {
      round: asCount(row.round, msg),
      capability: asString(row.capability, "unknown"),
      auditEventId: asString(row.auditEventId),
      outcome: asString(row.outcome),
      transcript: asString(row.transcript, "off"),
      present: Boolean(row.present),
      prompt: asString(row.prompt),
      response: asString(row.response),
      sentPromptSha256: asString(row.sentPromptSha256),
      receivedResponseSha256: asString(row.receivedResponseSha256),
      promptFidelity: asString(row.promptFidelity),
      responseFidelity: asString(row.responseFidelity),
      chainSha256: asString(row.chainSha256),
    };
  });
  const problems = Array.isArray(body.problems)
    ? body.problems.filter((item): item is string => typeof item === "string")
    : [];
  return {
    runId: body.runId,
    sessionId: asString(body.sessionId),
    owner: asString(body.owner),
    verified: body.verified,
    problems,
    exchanges,
  };
}

export async function fetchAdminAIUsage(
  days = 7,
  owner = "",
  fetchImpl: typeof fetch = fetch,
  signal?: AbortSignal,
): Promise<AdminAIUsageReport> {
  const boundedDays = Math.min(92, Math.max(1, Math.trunc(days || 7)));
  const params = new URLSearchParams({ days: String(boundedDays) });
  if (owner.trim()) params.set("owner", owner.trim());
  const response = await fetchImpl(`/v1/admin/ai/usage?${params.toString()}`, {
    method: "GET",
    credentials: "same-origin",
    cache: "no-store",
    signal: signal ?? AbortSignal.timeout(15000),
    headers: { accept: "application/json" },
  });
  if (!response.ok)
    throw new HubError(response.status, "Fleet AI usage could not be read.");
  return parseAdminAIUsage(await response.json());
}

export async function fetchAdminAITranscripts(
  owner = "",
  fetchImpl: typeof fetch = fetch,
  signal?: AbortSignal,
): Promise<AdminAITranscriptsList> {
  const params = new URLSearchParams();
  if (owner.trim()) params.set("owner", owner.trim());
  const query = params.toString();
  const response = await fetchImpl(
    `/v1/admin/ai/transcripts${query ? `?${query}` : ""}`,
    {
      method: "GET",
      credentials: "same-origin",
      cache: "no-store",
      signal: signal ?? AbortSignal.timeout(15000),
      headers: { accept: "application/json" },
    },
  );
  if (!response.ok)
    throw new HubError(
      response.status,
      "AI transcript index could not be read.",
    );
  return parseAdminAITranscriptsList(await response.json());
}

export async function fetchAdminAITranscriptExport(
  runId: string,
  fetchImpl: typeof fetch = fetch,
  signal?: AbortSignal,
): Promise<AdminAITranscriptExport> {
  const cleanId = runId.trim();
  if (!cleanId)
    throw new HubError(400, "A valid AI run identifier is required.");
  const response = await fetchImpl(
    `/v1/admin/ai/transcripts/${encodeURIComponent(cleanId)}`,
    {
      method: "GET",
      credentials: "same-origin",
      cache: "no-store",
      signal: signal ?? AbortSignal.timeout(15000),
      headers: { accept: "application/json" },
    },
  );
  if (!response.ok)
    throw new HubError(
      response.status,
      "Audited AI transcript export could not be read.",
    );
  return parseAdminAITranscriptExport(await response.json());
}

import { HubError } from "./hub-error.js";

export type OwnAIUsage = {
  owner: string;
  day: string;
  resetsAt: string;
  calls: number;
  tokens: number;
  callLimit: number | null;
  tokenLimit: number | null;
  remainingCalls: number | null;
  remainingTokens: number | null;
  missingTokenCounts: boolean;
  hasTokenCounts: boolean;
};

function unavailable(): never {
  throw new HubError(502, "AI usage could not be read.");
}

function record(value: unknown): Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value))
    return unavailable();
  return value as Record<string, unknown>;
}

function count(value: unknown): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0)
    return unavailable();
  return value;
}

function allowance(value: unknown): number | null {
  return value === null ? null : count(value);
}

/** The existing native self-usage contract; absent values are never zeroes. */
export function parseOwnAIUsage(value: unknown): OwnAIUsage {
  const body = record(value);
  const budget = record(body.budget);
  const used = record(budget.used);
  if (
    typeof body.owner !== "string" ||
    !body.owner.trim() ||
    body.owner.length > 256 ||
    body.days !== 1 ||
    !Array.isArray(body.usage) ||
    typeof budget.day !== "string" ||
    !/^\d{4}-\d{2}-\d{2}$/.test(budget.day) ||
    typeof budget.resetsAt !== "string" ||
    !Number.isFinite(Date.parse(budget.resetsAt))
  )
    return unavailable();
  const callLimit = allowance(budget.dailyCallBudget);
  const tokenLimit = allowance(budget.dailyTokenBudget);
  const remainingCalls = allowance(budget.remainingCalls);
  const remainingTokens = allowance(budget.remainingTokens);
  if (
    (callLimit === null) !== (remainingCalls === null) ||
    (tokenLimit === null) !== (remainingTokens === null) ||
    (callLimit !== null &&
      (callLimit === 0 ||
        remainingCalls === null ||
        remainingCalls > callLimit)) ||
    (tokenLimit !== null &&
      (tokenLimit === 0 ||
        remainingTokens === null ||
        remainingTokens > tokenLimit))
  )
    return unavailable();
  let missingTokenCounts = false;
  let hasTokenCounts = false;
  for (const raw of body.usage) {
    const row = record(raw);
    if (row.owner !== body.owner || row.day !== budget.day)
      return unavailable();
    const charged = count(row.budgetedCalls);
    const reported = count(row.callsWithTokens);
    const attempts = count(row.attempts);
    const prompt = allowance(row.promptTokens);
    const completion = allowance(row.completionTokens);
    hasTokenCounts ||= reported > 0;
    missingTokenCounts ||=
      reported < charged ||
      (attempts > 0 && (prompt === null || completion === null));
  }
  return {
    owner: body.owner,
    day: budget.day,
    resetsAt: budget.resetsAt,
    calls: count(used.calls),
    tokens: count(used.tokens),
    callLimit,
    tokenLimit,
    remainingCalls,
    remainingTokens,
    missingTokenCounts,
    hasTokenCounts,
  };
}

/** Reads only the authenticated caller; no owner selector or admin route. */
export async function fetchOwnAIUsage(): Promise<OwnAIUsage> {
  const response = await fetch("/v1/ai/usage?days=1", {
    method: "GET",
    credentials: "same-origin",
    cache: "no-store",
    signal: AbortSignal.timeout(15000),
    headers: { accept: "application/json" },
  });
  if (!response.ok)
    throw new HubError(response.status, "AI usage could not be read.");
  return parseOwnAIUsage(await response.json());
}

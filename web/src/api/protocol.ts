// Wire types shared by the local ACP bridge and the assistant-ui client.
// The bridge is the only place that talks to Codex. These shapes are the
// normalized snapshots the browser is allowed to see.

const SECRET =
  /(?:sk-[A-Za-z0-9]{8,}|ghp_[A-Za-z0-9]{8,}|github_pat_[A-Za-z0-9_]{8,}|AKIA[0-9A-Z]{16}|Bearer\s+[A-Za-z0-9._~+/-]{8,}|xox[baprs]-[A-Za-z0-9-]{8,})/g;

export function redact(value: string): string {
  return value.replace(SECRET, "[redacted]");
}

export function redactValue(value: unknown): unknown {
  if (typeof value === "string") return redact(value);
  if (Array.isArray(value)) return value.map((entry) => redactValue(entry));
  if (value !== null && typeof value === "object") {
    const redacted: Record<string, unknown> = {};
    for (const [key, entry] of Object.entries(value))
      redacted[key] = redactValue(entry);
    return redacted;
  }
  return value;
}

export function asText(value: unknown): string {
  if (value === null || value === undefined) return "";
  if (typeof value === "string") return redact(value);
  try {
    return redact(JSON.stringify(redactValue(value), null, 2));
  } catch {
    return "";
  }
}

export type ToolStatus = "pending" | "in_progress" | "completed" | "failed";

export type ApprovalOption = {
  optionId: string;
  name: string;
  kind: string;
};

export type ApprovalDecision =
  | { optionId: string; approved: boolean }
  | { resolution: "cancelled" };

export type ToolSnapshot = {
  toolCallId: string;
  toolName: string;
  title: string;
  status: ToolStatus;
  argsText: string;
  resultText: string;
  isError: boolean;
  approval?: {
    requestId: string;
    prompt: string;
    options: ApprovalOption[];
    decision?: ApprovalDecision;
  };
};

export type PlanEntrySnapshot = {
  content: string;
  status: string;
  priority: string;
};

export type SourceSnapshot = {
  id: string;
  url: string;
  title: string;
};

export type UsageSnapshot = {
  used: number;
  size: number;
};

export type SessionConfigChoice = {
  value: string;
  name: string;
  description?: string;
};

export type SessionConfigOption = {
  id: string;
  name: string;
  description?: string;
  category?: string;
  type: "select" | "boolean";
  currentValue: string | boolean | null;
  options?: SessionConfigChoice[];
};

export function sessionConfig(value: unknown): SessionConfigOption[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((entry) => {
    if (entry === null || typeof entry !== "object") return [];
    const record = entry as Record<string, unknown>;
    if (typeof record["id"] !== "string") return [];
    if (record["type"] !== "select" && record["type"] !== "boolean") return [];
    const current = record["currentValue"];
    const options = Array.isArray(record["options"])
      ? record["options"].flatMap((option) => {
          if (option === null || typeof option !== "object") return [];
          const item = option as Record<string, unknown>;
          if (
            typeof item["value"] !== "string" ||
            typeof item["name"] !== "string"
          )
            return [];
          const choice: SessionConfigChoice = {
            value: item["value"],
            name: item["name"],
          };
          if (typeof item["description"] === "string")
            choice.description = item["description"];
          return [choice];
        })
      : undefined;
    const option: SessionConfigOption = {
      id: record["id"],
      name: typeof record["name"] === "string" ? record["name"] : record["id"],
      type: record["type"],
      currentValue:
        typeof current === "string" || typeof current === "boolean"
          ? current
          : null,
    };
    if (typeof record["description"] === "string")
      option.description = record["description"];
    if (typeof record["category"] === "string")
      option.category = record["category"];
    if (options && options.length > 0) option.options = options;
    return [option];
  });
}

export type RunSnapshot = {
  text: string;
  reasoning: string;
  tools: ToolSnapshot[];
  plan: PlanEntrySnapshot[];
  sources: SourceSnapshot[];
  usage?: UsageSnapshot;
};

export function emptySnapshot(): RunSnapshot {
  return { text: "", reasoning: "", tools: [], plan: [], sources: [] };
}

export type AcpContent = {
  type?: string;
  text?: string;
  uri?: string;
  name?: string;
  mimeType?: string;
  data?: string;
};

export type AcpUpdate = {
  sessionUpdate?: string;
  content?: AcpContent | AcpContent[] | null;
  toolCallId?: string;
  title?: string | null;
  name?: string | null;
  kind?: string | null;
  status?: string | null;
  rawInput?: unknown;
  rawOutput?: unknown;
  contentBlocks?: AcpContent[] | null;
  locations?: { path?: string; uri?: string }[] | null;
  entries?: { content?: string; status?: string; priority?: string }[] | null;
  used?: number;
  size?: number;
};

const TOOL_STATUSES: readonly ToolStatus[] = [
  "pending",
  "in_progress",
  "completed",
  "failed",
];

function toolStatus(
  value: string | null | undefined,
  fallback: ToolStatus,
): ToolStatus {
  return TOOL_STATUSES.find((status) => status === value) ?? fallback;
}

function toolName(update: AcpUpdate, previous?: ToolSnapshot): string {
  const name = update.name ?? update.kind ?? previous?.toolName;
  return name && name.length > 0 ? name : "tool";
}

function collectSources(
  update: AcpUpdate,
  existing: SourceSnapshot[],
): SourceSnapshot[] {
  const sources = [...existing];
  const seen = new Set(sources.map((source) => source.url));
  const blocks = [
    ...(Array.isArray(update.content) ? update.content : []),
    ...(update.contentBlocks ?? []),
  ];
  for (const block of blocks) {
    if (block.type !== "resource_link" || block.uri === undefined) continue;
    if (!block.uri.startsWith("https://") && !block.uri.startsWith("http://"))
      continue;
    if (seen.has(block.uri)) continue;
    seen.add(block.uri);
    sources.push({
      id: block.uri,
      url: block.uri,
      title: redact(block.name ?? block.uri),
    });
  }
  return sources;
}

export function applyUpdate(
  snapshot: RunSnapshot,
  update: AcpUpdate,
): RunSnapshot {
  const kind = update.sessionUpdate;
  const chunk = !Array.isArray(update.content) ? update.content : undefined;
  if (kind === "agent_message_chunk" && chunk?.type === "text" && chunk.text) {
    return { ...snapshot, text: snapshot.text + redact(chunk.text) };
  }
  if (kind === "agent_thought_chunk" && chunk?.type === "text" && chunk.text) {
    return { ...snapshot, reasoning: snapshot.reasoning + redact(chunk.text) };
  }
  if (kind === "plan" && update.entries) {
    return {
      ...snapshot,
      plan: update.entries.map((entry) => ({
        content: redact(entry.content ?? ""),
        status: entry.status ?? "pending",
        priority: entry.priority ?? "medium",
      })),
    };
  }
  if (
    kind === "usage_update" &&
    typeof update.used === "number" &&
    typeof update.size === "number"
  ) {
    return { ...snapshot, usage: { used: update.used, size: update.size } };
  }
  if (kind !== "tool_call" && kind !== "tool_call_update") return snapshot;
  if (update.toolCallId === undefined || update.toolCallId.length === 0)
    return snapshot;

  const tools = snapshot.tools.map((tool) => ({ ...tool }));
  const index = tools.findIndex(
    (tool) => tool.toolCallId === update.toolCallId,
  );
  const previous = index === -1 ? undefined : tools[index];
  const next: ToolSnapshot = {
    toolCallId: update.toolCallId,
    toolName: toolName(update, previous),
    title: redact(
      update.title ?? previous?.title ?? toolName(update, previous),
    ),
    status: toolStatus(update.status, previous?.status ?? "pending"),
    argsText:
      update.rawInput === undefined
        ? (previous?.argsText ?? "")
        : asText(update.rawInput),
    resultText:
      update.rawOutput === undefined
        ? (previous?.resultText ?? "")
        : asText(update.rawOutput),
    isError:
      toolStatus(update.status, previous?.status ?? "pending") === "failed" ||
      previous?.isError === true,
  };
  if (previous?.approval) next.approval = previous.approval;
  if (index === -1) tools.push(next);
  else tools[index] = next;
  return {
    ...snapshot,
    tools,
    sources: collectSources(update, snapshot.sources),
  };
}

export const PINNED_ACP_PROTOCOL_VERSION = 1 as const;

export type GovernedRiskTier = "low_confirm" | "high_change_request";

export type EditorActionKind = "tool_call" | "file_write" | "command";

export type WorkspaceScope = {
  owner: string;
  workspaceId: string;
  branch: string;
};

export type EditorActivityEvent = {
  type: "activity";
  sessionId: string;
  threadId: string;
  runId: string;
  toolCallId: string;
  kind: EditorActionKind | "message" | "permission";
  name: string;
  status: ToolStatus;
  path?: string;
  digest?: string;
  content?: string;
  command?: string;
  riskTier?: GovernedRiskTier;
  requestId?: string;
  argsText?: string;
  resultText?: string;
  text?: string;
};

export type PermissionRequest = {
  requestId: string;
  threadId: string;
  runId: string;
  toolCallId: string;
  title: string;
  options: ApprovalOption[];
  kind?: EditorActionKind;
  riskTier?: GovernedRiskTier;
  path?: string;
  command?: string;
};

export function sanitizeApprovalOptions(
  options: readonly ApprovalOption[],
): ApprovalOption[] {
  const hasAllowOnce = options.some((option) => option.kind === "allow_once");
  const sanitized: ApprovalOption[] = [];
  for (const option of options) {
    if (option.kind === "allow_always") {
      if (hasAllowOnce) continue;
      sanitized.push({
        optionId: option.optionId,
        name: redact(option.name).replace(/always/gi, "once"),
        kind: "allow_once",
      });
      continue;
    }
    sanitized.push({
      optionId: option.optionId,
      name: redact(option.name),
      kind: option.kind,
    });
  }
  return sanitized;
}

const HIGH_RISK_PATH =
  /(?:^|\/)(?:\.\.(?:\/|$)|\.github(?:\/|$)|deploy(?:\/|$)|\.env(?:\.|$)|secrets?(?:\/|\.|$)|credentials?(?:\/|\.|$))/i;

const HIGH_RISK_COMMAND =
  /\b(?:git\s+push|gh\s+(?:pr|release|repo)|deploy|publish|helm\b|kubectl\b|terraform\b|curl\b|wget\b|ssh\b|scp\b|rsync\b|nc\b|ncat\b|sudo\b|rm\s+-[a-z]*r[a-z]*f|docker\s+push|oras\s+push)\b/i;

export function classifyPermissionRisk(input: {
  kind?: string | null | undefined;
  name?: string | null | undefined;
  title?: string | null | undefined;
  path?: string | null | undefined;
  command?: string | null | undefined;
  rawInput?: unknown;
}): GovernedRiskTier {
  const kind = (input.kind ?? "").toLowerCase();
  const name = (input.name ?? "").toLowerCase();
  if (
    kind === "deploy" ||
    kind === "publish" ||
    kind === "external" ||
    name === "deploy" ||
    name === "publish"
  ) {
    return "high_change_request";
  }
  const pathCandidate = (input.path ?? "").trim();
  if (
    pathCandidate.length > 0 &&
    (pathCandidate.startsWith("/") || HIGH_RISK_PATH.test(pathCandidate))
  ) {
    return "high_change_request";
  }
  const combined = [
    input.title ?? "",
    input.command ?? "",
    pathCandidate,
    input.rawInput === undefined ? "" : asText(input.rawInput),
  ].join("\n");
  if (HIGH_RISK_COMMAND.test(combined) || HIGH_RISK_PATH.test(combined)) {
    return "high_change_request";
  }
  return "low_confirm";
}

export function attachApproval(
  snapshot: RunSnapshot,
  request: PermissionRequest,
): RunSnapshot {
  const tools = snapshot.tools.map((tool) => ({ ...tool }));
  const approval = {
    requestId: request.requestId,
    prompt: redact(request.title),
    options: sanitizeApprovalOptions(request.options),
  };
  const index = tools.findIndex(
    (tool) => tool.toolCallId === request.toolCallId,
  );
  if (index === -1) {
    tools.push({
      toolCallId: request.toolCallId,
      toolName: "permission",
      title: approval.prompt,
      status: "pending",
      argsText: "",
      resultText: "",
      isError: false,
      approval,
    });
  } else {
    const current = tools[index];
    if (current) tools[index] = { ...current, approval };
  }
  return { ...snapshot, tools };
}

export function settleApproval(
  snapshot: RunSnapshot,
  requestId: string,
  decision: ApprovalDecision,
): RunSnapshot {
  return {
    ...snapshot,
    tools: snapshot.tools.map((tool) =>
      tool.approval?.requestId === requestId
        ? { ...tool, approval: { ...tool.approval, decision } }
        : tool,
    ),
  };
}

export type PendingPermission = PermissionRequest & {
  settled: boolean;
  resolve: (
    outcome:
      | { outcome: "cancelled" }
      | { outcome: "selected"; optionId: string },
  ) => void;
};

export type PermissionBook = {
  open(
    request: PermissionRequest,
  ): Promise<
    { outcome: "cancelled" } | { outcome: "selected"; optionId: string }
  >;
  decide(input: {
    requestId: string;
    threadId: string;
    runId: string;
    optionId: string;
  }): { ok: true } | { ok: false; status: number; error: string };
  cancelRequest(input: {
    requestId: string;
    threadId: string;
    runId: string;
  }): { ok: true } | { ok: false; status: number; error: string };
  cancelRun(runId: string): void;
  pending(requestId: string): PendingPermission | undefined;
};

export function createPermissionBook(): PermissionBook {
  const pending = new Map<string, PendingPermission>();
  return {
    open(request) {
      return new Promise((resolve) => {
        pending.set(request.requestId, { ...request, settled: false, resolve });
      });
    },
    decide(input) {
      const entry = pending.get(input.requestId);
      if (!entry || entry.settled) {
        return {
          ok: false,
          status: 409,
          error: "stale or replayed permission request",
        };
      }
      if (entry.threadId !== input.threadId || entry.runId !== input.runId) {
        return {
          ok: false,
          status: 409,
          error: "permission is bound to another run",
        };
      }
      const option = entry.options.find(
        (candidate) => candidate.optionId === input.optionId,
      );
      if (!option)
        return { ok: false, status: 400, error: "unknown permission option" };
      entry.settled = true;
      pending.delete(input.requestId);
      entry.resolve({ outcome: "selected", optionId: option.optionId });
      return { ok: true };
    },
    cancelRequest(input) {
      const entry = pending.get(input.requestId);
      if (!entry || entry.settled) {
        return {
          ok: false,
          status: 409,
          error: "stale or replayed permission request",
        };
      }
      if (entry.threadId !== input.threadId || entry.runId !== input.runId) {
        return {
          ok: false,
          status: 409,
          error: "permission is bound to another run",
        };
      }
      entry.settled = true;
      pending.delete(input.requestId);
      entry.resolve({ outcome: "cancelled" });
      return { ok: true };
    },
    cancelRun(runId) {
      for (const [requestId, entry] of pending) {
        if (entry.runId !== runId || entry.settled) continue;
        entry.settled = true;
        pending.delete(requestId);
        entry.resolve({ outcome: "cancelled" });
      }
    },
    pending(requestId) {
      return pending.get(requestId);
    },
  };
}

export function approvedOption(kind: string): boolean {
  return kind === "allow_once";
}

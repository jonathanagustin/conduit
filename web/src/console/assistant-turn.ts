import { servedModelLabel } from "./model-identity";
/*
 * The assistant's turn and thread state, as pure functions.
 *
 * A turn is reduced from the AG-UI events the Hub streams for
 * POST /v1/assistant/turns, or from the JSON turn document when it answers
 * without a stream. Nothing here touches the network, the DOM or React; the
 * store in assistant-store.ts owns those.
 *
 * Contract: docs/architecture/agent-runs.md, "Assistant turns and threads".
 */

export type HubResult = Record<string, unknown>;

export type ServedModel = {
  kind: string;
  model?: string;
  configuredModel?: string;
  providerModel?: string;
};

export type ReportedTokenUsage = {
  promptTokens: number | null;
  completionTokens: number | null;
};

/** Missing, invalid and unsafe counts remain unknown; zero is reported. */
export function reportedTokenUsage(value: unknown): ReportedTokenUsage | null {
  if (value === null || typeof value !== "object" || Array.isArray(value))
    return null;
  const fields = value as Record<string, unknown>;
  const count = (raw: unknown): number | null =>
    typeof raw === "number" && Number.isSafeInteger(raw) && raw >= 0
      ? raw
      : null;
  return {
    promptTokens: count(fields["promptTokens"]),
    completionTokens: count(fields["completionTokens"]),
  };
}

export function usageLabel(
  usage: ReportedTokenUsage | null | undefined,
): string | null {
  if (!usage) return null;
  if (usage.promptTokens === null && usage.completionTokens === null)
    return "Token usage not reported";
  return `Reported tokens: ${usage.promptTokens ?? "unknown"} input · ${usage.completionTokens ?? "unknown"} output`;
}

/** One tool call of a turn, with what the Hub ran and what it returned. */
export type ToolRow = {
  id: string;
  name: string;
  round: number;
  /** The argument JSON exactly as the Hub sent it. */
  argsText: string;
  args: Record<string, unknown>;
  /** Absent until the tool returned. */
  result?: HubResult;
  /** Answers to a Continue or Confirm the person submitted, oldest first. */
  followUps: HubResult[];
  /** A Continue or Confirm is in flight. */
  pending: boolean;
};

export type TurnError = {
  code: string;
  message: string;
  /** Whole seconds the Hub's Retry-After named on a 429. */
  retryAfter?: number;
};

export type AssistantTurn = {
  status: "running" | "complete" | "error" | "cancelled";
  /** True once RUN_STARTED arrived: the Hub reached a model call. */
  started: boolean;
  runId: string;
  threadId: string;
  round: number;
  rounds: number;
  tools: ToolRow[];
  text: string;
  label: string;
  model: ServedModel | null;
  usage?: ReportedTokenUsage | null;
  error: TurnError | null;
};

export type ChatMessage =
  | { id: string; role: "user"; text: string; mode: string; createdAt: string }
  | {
      id: string;
      role: "assistant";
      mode: string;
      createdAt: string;
      turn: AssistantTurn;
    };

const record = (value: unknown): Record<string, unknown> =>
  value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};

const text = (value: unknown): string =>
  typeof value === "string" ? value : "";

function parseObject(raw: unknown): Record<string, unknown> | null {
  if (typeof raw !== "string") return raw === undefined ? null : record(raw);
  try {
    const parsed: unknown = JSON.parse(raw);
    return parsed !== null && typeof parsed === "object"
      ? record(parsed)
      : null;
  } catch {
    return null;
  }
}

export function newTurn(): AssistantTurn {
  return {
    status: "running",
    started: false,
    runId: "",
    threadId: "",
    round: 0,
    rounds: 0,
    tools: [],
    text: "",
    label: "",
    model: null,
    usage: null,
    error: null,
  };
}

function servedModel(value: unknown): ServedModel | null {
  const fields = record(value);
  const kind = text(fields["kind"]);
  const model = text(fields["model"]);
  const configuredModel = text(fields["configuredModel"]);
  const providerModel = text(fields["providerModel"]);
  if (!kind && !model && !configuredModel && !providerModel) return null;
  return {
    kind,
    ...(model ? { model } : {}),
    ...(configuredModel ? { configuredModel } : {}),
    ...(providerModel ? { providerModel } : {}),
  };
}

function withTool(
  turn: AssistantTurn,
  id: string,
  change: (row: ToolRow) => ToolRow,
): AssistantTurn {
  return {
    ...turn,
    tools: turn.tools.map((row) => (row.id === id ? change(row) : row)),
  };
}

/**
 * Folds the turn document into a turn. The document is the Hub's own account
 * of the whole turn, so its arguments and results fill any call the stream did
 * not deliver; a call the stream already carried keeps its place.
 */
export function applyTurnDocument(
  turn: AssistantTurn,
  document: unknown,
): AssistantTurn {
  const fields = record(document);
  const calls = Array.isArray(fields["toolCalls"]) ? fields["toolCalls"] : [];
  const results = Array.isArray(fields["results"]) ? fields["results"] : [];
  let tools = turn.tools;
  results.forEach((raw, index) => {
    const call = record(calls[index]);
    const result = record(raw);
    const id = text(call["id"]) || `result_${index + 1}`;
    const known = tools.find((row) => row.id === id);
    if (known) {
      if (known.result === undefined)
        tools = tools.map((row) => (row.id === id ? { ...row, result } : row));
      return;
    }
    const args = parseObject(call["arguments"]) ?? {};
    tools = [
      ...tools,
      {
        id,
        name: text(call["name"]) || text(result["tool"]) || "tool",
        round: typeof call["round"] === "number" ? call["round"] : 0,
        argsText: JSON.stringify(args),
        args,
        result,
        followUps: [],
        pending: false,
      },
    ];
  });
  return {
    ...turn,
    tools,
    text: text(fields["text"]) || turn.text,
    label: text(fields["label"]) || turn.label,
    rounds:
      typeof fields["rounds"] === "number" ? fields["rounds"] : turn.round,
    threadId: text(fields["threadId"]) || turn.threadId,
    model: servedModel(fields["model"]) ?? turn.model,
    usage: reportedTokenUsage(fields["usage"]),
  };
}

/** One AG-UI event of an assistant turn. An unknown event changes nothing. */
export function reduceTurn(turn: AssistantTurn, event: unknown): AssistantTurn {
  const fields = record(event);
  const callId = text(fields["toolCallId"]);
  switch (fields["type"]) {
    case "RUN_STARTED":
      return {
        ...turn,
        started: true,
        runId: text(fields["runId"]),
        threadId: text(fields["threadId"]),
      };
    case "STEP_STARTED": {
      const round = /^round-(\d+)$/.exec(text(fields["stepName"]));
      return round ? { ...turn, round: Number(round[1]) } : turn;
    }
    case "TOOL_CALL_START":
      if (!callId || turn.tools.some((row) => row.id === callId)) return turn;
      return {
        ...turn,
        tools: [
          ...turn.tools,
          {
            id: callId,
            name: text(fields["toolCallName"]) || "tool",
            round: turn.round,
            argsText: "",
            args: {},
            followUps: [],
            pending: false,
          },
        ],
      };
    case "TOOL_CALL_ARGS":
      return withTool(turn, callId, (row) => {
        const argsText = row.argsText + text(fields["delta"]);
        return { ...row, argsText, args: parseObject(argsText) ?? row.args };
      });
    case "TOOL_CALL_RESULT":
      return withTool(turn, callId, (row) => ({
        ...row,
        result: parseObject(fields["content"]) ?? {
          text: text(fields["content"]),
        },
      }));
    case "TEXT_MESSAGE_CONTENT":
      return { ...turn, text: turn.text + text(fields["delta"]) };
    case "RUN_FINISHED":
      return {
        ...applyTurnDocument(turn, fields["result"]),
        status: "complete",
      };
    case "CUSTOM":
      return fields["name"] === "conduit.assistant.turn-result"
        ? applyTurnDocument(turn, fields["value"])
        : turn;
    case "RUN_ERROR":
      return {
        ...turn,
        status: "error",
        error: {
          code: text(fields["code"]) || "TURN_FAILED",
          message: text(fields["message"]),
        },
      };
    default:
      return turn;
  }
}

/**
 * The exchange readAgentStream drives (operator/agent-runs.js). The planning
 * exchange validates a RunAgentInput run; an assistant turn has neither a
 * client-chosen run id nor parent messages, so it has its own reducer and
 * shares the stream reader.
 */
export class AssistantExchange {
  turn: AssistantTurn;
  terminal = false;
  error: TurnError | null = null;
  constructor(turn: AssistantTurn = newTurn()) {
    this.turn = turn;
  }
  accept(event: unknown): this {
    if (this.terminal) return this;
    this.turn = reduceTurn(this.turn, event);
    const type = record(event)["type"];
    if (type === "RUN_FINISHED" || type === "RUN_ERROR") this.terminal = true;
    if (type === "RUN_ERROR") this.error = this.turn.error;
    return this;
  }
}

/** What a thrown value says about a failed turn, by stable code. */
export function turnErrorFrom(thrown: unknown): TurnError {
  const fields = record(thrown);
  const message = text(fields["message"]);
  if (fields["name"] === "AbortError")
    return { code: "CANCELLED", message: message || "cancelled" };
  const code = text(fields["code"]);
  const wait = fields["retryAfter"];
  const retryAfter =
    typeof wait === "number" && Number.isFinite(wait) && wait >= 0
      ? { retryAfter: wait }
      : {};
  if (code) return { code, message, ...retryAfter };
  return { code: "CONNECTION_LOST", message };
}

/** A turn that failed, keeping what already ran. */
export function failTurn(turn: AssistantTurn, thrown: unknown): AssistantTurn {
  const error = turnErrorFrom(thrown);
  const partial = record(thrown)["turn"];
  const base = partial ? applyTurnDocument(turn, partial) : turn;
  if (error.code === "CANCELLED")
    return { ...base, status: "cancelled", error: null };
  return {
    ...base,
    // A partial turn document means the Hub reached a model call.
    started: base.started || Boolean(partial),
    status: "error",
    error,
  };
}

const MODEL_DOCS =
  "docs/operations/model-http.md or docs/operations/local-model-cli.md";

/** What an operator is told for each stable failure code. */
const ERROR_TEXT: Record<string, string> = {
  MODEL_NOT_CONFIGURED: `No model is configured on this Hub, so the assistant cannot answer. An operator configures a model provider as described in ${MODEL_DOCS}. Context chips still run tools without a model.`,
  MODEL_FAILED:
    "The model call failed. Nothing further ran; tool calls already shown stand.",
  MODEL_TIMEOUT:
    "The model did not answer in time. Tool calls already shown stand.",
  MODEL_REPLY_INVALID:
    "The model's reply was not a valid assistant reply, so it was not used.",
  ROUND_CAP:
    "The model was still calling tools in its last allowed round, so the turn stopped without an answer. Tool calls already shown ran.",
  TURN_FAILED: "The Hub could not complete the turn.",
  MODEL_BUSY:
    "The model is serving as many calls as this Hub allows at once, so this turn was not started.",
  MODEL_BUDGET_EXCEEDED:
    "Your model budget for today is spent, so the Hub refused this turn. The budget resets at 00:00 UTC.",
  AUDIT_UNAVAILABLE:
    "The Hub could not write the audit record for this turn, so it stopped. Nothing unaudited ran.",
  THREADS_NOT_CONFIGURED:
    "This Hub has no conversation store. The conversation is not saved.",
  THREAD_NOT_FOUND:
    "This conversation no longer exists on the Hub. Start a new conversation.",
  THREAD_LIMIT:
    "You have reached the Hub's limit of saved conversations. Delete one, then send again.",
  THREAD_FULL:
    "This conversation has reached the Hub's message limit. Start a new conversation.",
  INVALID_REQUEST: "The Hub refused the message as invalid.",
  TOOL_INVALID: "The Hub refused the tool call as invalid.",
  UNAUTHORIZED: "You are not signed in to the Hub, or the session expired.",
  CANCELLED: "The turn was cancelled.",
  CONNECTION_LOST:
    "The connection to the Hub ended before the turn finished. Tool calls already shown may have run.",
};

/** A wait in the largest two units that matter: "45 seconds", "2 h 5 min". */
export function waitText(seconds: number): string {
  const whole = Math.max(1, Math.ceil(seconds));
  if (whole < 60) return `${whole} second${whole === 1 ? "" : "s"}`;
  const minutes = Math.ceil(whole / 60);
  if (minutes < 60) return `${minutes} minute${minutes === 1 ? "" : "s"}`;
  const rest = minutes % 60;
  return rest
    ? `${Math.floor(minutes / 60)} h ${rest} min`
    : `${minutes / 60} h`;
}

/** The message for a failure: the stable text, then what the Hub said. */
export function assistantErrorText(error: TurnError): string {
  const base = failureText(error);
  // A 429 names its wait in Retry-After; a stream failure carries none.
  return error.retryAfter === undefined
    ? base
    : `${base} The Hub asks you to retry in ${waitText(error.retryAfter)}.`;
}

function failureText(error: TurnError): string {
  const stable = ERROR_TEXT[error.code];
  const said = error.message.trim();
  if (!stable) return said || `The turn failed (${error.code}).`;
  // The Hub's own sentence is kept for the codes whose cause varies.
  const detailed = [
    "MODEL_FAILED",
    "MODEL_REPLY_INVALID",
    "TURN_FAILED",
    "INVALID_REQUEST",
    "TOOL_INVALID",
    "CONNECTION_LOST",
  ].includes(error.code);
  return detailed && said ? `${stable} The Hub said: ${said}` : stable;
}

/** Display typed identity metadata; model-name spelling carries no provenance. */
export function modelLabel(
  model: ServedModel | null | undefined,
): { text: string; caution: boolean } | null {
  if (
    !model ||
    ![model.kind, model.model, model.configuredModel, model.providerModel].some(
      (value) => value?.trim(),
    )
  )
    return null;
  return {
    text: servedModelLabel(model),
    caution: Boolean(
      model.configuredModel?.trim() && !model.providerModel?.trim(),
    ),
  };
}

/** The newest answer for a call: the last follow-up, else the first result. */
export function effectiveResult(row: ToolRow): HubResult | undefined {
  return row.followUps.at(-1) ?? row.result;
}

/**
 * The request a Continue or Confirm sends to POST /v1/assistant/tools: the
 * same tool with the same arguments the Hub returned as its elicitation, the
 * person's field values, and confirm only when the person pressed Confirm.
 * A confirm the model supplied was discarded by the Hub and is not copied.
 */
export function confirmRequest(
  mode: string,
  row: ToolRow,
  values: Record<string, string>,
  confirm: boolean,
): { mode: string; tool: string; arguments: Record<string, unknown> } {
  const result = effectiveResult(row);
  const elicited = result ? record(result["elicitation"]) : {};
  const base = Object.keys(elicited).length ? elicited : row.args;
  const { confirm: _dropped, ...args } = { ...base, ...values };
  void _dropped;
  return {
    mode: mode || "ontology",
    tool: text(result?.["tool"]) || row.name,
    arguments: confirm ? { ...args, confirm: true } : args,
  };
}

export type ToolOutcome = {
  tone: "running" | "ok" | "refused" | "confirm" | "input" | "failed";
  summary: string;
};

/** One line saying what a tool call came to. */
export function toolOutcome(row: ToolRow): ToolOutcome {
  const result = effectiveResult(row);
  if (!result) return { tone: "running", summary: "Running" };
  const reason = text(result["reason"]);
  const failure = text(result["error"]);
  if (result["truncated"] === true)
    return { tone: "ok", summary: "Result too large to keep" };
  if (failure) return { tone: "failed", summary: failure };
  if (result["needsConfirm"] === true)
    return { tone: "confirm", summary: reason || "Confirm to apply" };
  if (Array.isArray(result["fields"]) && result["fields"].length)
    return { tone: "input", summary: reason || "More detail is required" };
  const denied =
    Array.isArray(result["constraints"]) &&
    result["constraints"].some((row) => record(row)["status"] === "DENY");
  if (denied || (result["ok"] === false && reason))
    return { tone: "refused", summary: `Refused: ${reason || "no reason"}` };
  if (result["ok"] === false)
    return { tone: "failed", summary: reason || "The tool failed" };
  if (result["applied"] === true)
    return { tone: "ok", summary: reason || "Applied" };
  const changeRequest = text(result["changeRequestId"]);
  if (changeRequest)
    return {
      tone: "ok",
      summary:
        `Change request ${changeRequest} ${text(result["changeRequestStatus"])}`.trim(),
    };
  if (
    text(result["tool"]) === "query_ontology_objects" ||
    "matchedCount" in result
  )
    return {
      tone: "ok",
      summary: `${Number(result["matchedCount"] ?? 0)} matched`,
    };
  if (Array.isArray(result["hunks"]))
    return {
      tone: "ok",
      summary: `${result["hunks"].length} changed field${result["hunks"].length === 1 ? "" : "s"}`,
    };
  if (result["allowed"] === true)
    return { tone: "ok", summary: reason || "Allowed" };
  return { tone: "ok", summary: reason || "Returned" };
}

// ---------------------------------------------------------------------------
// Threads
// ---------------------------------------------------------------------------

export type ThreadSummary = {
  id: string;
  title: string;
  updatedAt: string;
  messageCount: number;
};

/**
 * ready: the Hub stores conversations. stateless: it answered that it has no
 * store, or could not be read; a single unsaved conversation is all there is.
 */
export type ThreadList = {
  status: "loading" | "ready" | "stateless";
  /** Why the conversation is not saved; empty when it is. */
  notice: string;
  threads: ThreadSummary[];
  openId: string | null;
};

export const THREADS_LOADING: ThreadList = {
  status: "loading",
  notice: "",
  threads: [],
  openId: null,
};

export function threadSummary(value: unknown): ThreadSummary | null {
  const fields = record(value);
  const id = text(fields["id"]);
  if (!id) return null;
  return {
    id,
    title: text(fields["title"]),
    updatedAt: text(fields["updatedAt"]),
    messageCount:
      typeof fields["messageCount"] === "number" ? fields["messageCount"] : 0,
  };
}

/** The list the Hub returned. `wanted` is the thread to reopen, if it is there. */
export function threadsLoaded(
  rows: readonly unknown[],
  wanted: string | null,
): ThreadList {
  const threads = rows
    .map(threadSummary)
    .filter((row): row is ThreadSummary => row !== null);
  return {
    status: "ready",
    notice: "",
    threads,
    openId: threads.some((row) => row.id === wanted) ? wanted : null,
  };
}

/** The list read failed. The conversation goes on, unsaved, and says why. */
export function threadsUnavailable(thrown: unknown): ThreadList {
  const error = turnErrorFrom(thrown);
  return {
    status: "stateless",
    notice:
      error.code === "THREADS_NOT_CONFIGURED"
        ? "This Hub has no conversation store. This conversation is not saved and is lost on reload."
        : `Saved conversations could not be read (${error.message || error.code}). This conversation is not saved.`,
    threads: [],
    openId: null,
  };
}

export function threadOpened(list: ThreadList, value: unknown): ThreadList {
  const opened = threadSummary(value);
  if (!opened || list.status !== "ready") return list;
  return {
    ...list,
    threads: [opened, ...list.threads.filter((row) => row.id !== opened.id)],
    openId: opened.id,
  };
}

export function threadSelected(
  list: ThreadList,
  id: string | null,
): ThreadList {
  if (id !== null && !list.threads.some((row) => row.id === id)) return list;
  return { ...list, openId: id };
}

export function threadDeleted(list: ThreadList, id: string): ThreadList {
  return {
    ...list,
    threads: list.threads.filter((row) => row.id !== id),
    openId: list.openId === id ? null : list.openId,
  };
}

export function threadTitle(row: ThreadSummary): string {
  return row.title.trim() || "Untitled conversation";
}

/** A stored Hub thread as the messages the thread view renders. */
export function messagesFromThread(thread: unknown): ChatMessage[] {
  const fields = record(thread);
  const threadId = text(fields["id"]);
  const stored = Array.isArray(fields["messages"]) ? fields["messages"] : [];
  return stored.map((raw, index): ChatMessage => {
    const message = record(raw);
    const id = `${threadId}:${index}`;
    const mode = text(message["mode"]);
    const createdAt = text(message["createdAt"]);
    if (message["role"] === "user")
      return { id, role: "user", text: text(message["text"]), mode, createdAt };
    const calls = Array.isArray(message["toolCalls"])
      ? message["toolCalls"]
      : [];
    const code = text(message["errorCode"]);
    const tools = calls.map((value, position): ToolRow => {
      const call = record(value);
      const args = parseObject(call["arguments"]) ?? {};
      const result = parseObject(call["result"]);
      return {
        id: text(call["id"]) || `call_${position + 1}`,
        name: text(call["name"]) || "tool",
        round: typeof call["round"] === "number" ? call["round"] : 0,
        argsText: JSON.stringify(args),
        args,
        ...(result ? { result } : {}),
        followUps: [],
        pending: false,
      };
    });
    return {
      id,
      role: "assistant",
      mode,
      createdAt,
      turn: {
        ...newTurn(),
        started: true,
        threadId,
        tools,
        text: text(message["text"]),
        usage: reportedTokenUsage(message["usage"]),
        status:
          code === "CANCELLED" ? "cancelled" : code ? "error" : "complete",
        error: code && code !== "CANCELLED" ? { code, message: "" } : null,
      },
    };
  });
}

// ---------------------------------------------------------------------------
// assistant-ui message shape
// ---------------------------------------------------------------------------

export type TurnMeta = {
  mode: string;
  label: string;
  rounds: number;
  model: ServedModel | null;
  usage?: ReportedTokenUsage | null;
  code: string;
};

type ToolPart = {
  type: "tool-call";
  toolCallId: string;
  toolName: string;
  args: Record<string, unknown>;
  argsText: string;
  result?: HubResult;
  isError?: boolean;
};
type TextPart = { type: "text"; text: string };

export type ThreadMessageShape = {
  id: string;
  role: "user" | "assistant";
  content: (ToolPart | TextPart)[];
  createdAt?: Date;
  status?:
    | { type: "running" }
    | { type: "complete"; reason: "stop" }
    | { type: "incomplete"; reason: "cancelled" }
    | { type: "incomplete"; reason: "error"; error: string };
  metadata?: { custom: { conduit: TurnMeta } };
};

function createdAt(value: string): { createdAt?: Date } {
  const time = Date.parse(value);
  return Number.isNaN(time) ? {} : { createdAt: new Date(time) };
}

/**
 * One message as assistant-ui's ThreadMessageLike. A failed turn is an
 * incomplete message with an error, which is what makes the thread's own
 * error and retry state appear; it is never written as assistant prose.
 */
export function toThreadMessage(message: ChatMessage): ThreadMessageShape {
  if (message.role === "user")
    return {
      id: message.id,
      role: "user",
      content: [{ type: "text", text: message.text }],
      ...createdAt(message.createdAt),
    };
  const { turn } = message;
  const content: (ToolPart | TextPart)[] = turn.tools.map((row) => ({
    type: "tool-call",
    toolCallId: row.id,
    toolName: row.name,
    args: row.args,
    argsText: row.argsText || JSON.stringify(row.args),
    ...(row.result === undefined ? {} : { result: row.result }),
    ...(row.result && row.result["ok"] === false ? { isError: true } : {}),
  }));
  if (turn.text) content.push({ type: "text", text: turn.text });
  const status: NonNullable<ThreadMessageShape["status"]> =
    turn.status === "running"
      ? { type: "running" }
      : turn.status === "complete"
        ? { type: "complete", reason: "stop" }
        : turn.status === "cancelled"
          ? { type: "incomplete", reason: "cancelled" }
          : {
              type: "incomplete",
              reason: "error",
              error: assistantErrorText(
                turn.error ?? { code: "TURN_FAILED", message: "" },
              ),
            };
  return {
    id: message.id,
    role: "assistant",
    content,
    status,
    ...createdAt(message.createdAt),
    metadata: {
      custom: {
        conduit: {
          mode: message.mode,
          label: turn.label,
          rounds: turn.rounds,
          model: turn.model,
          usage: turn.usage ?? null,
          code: turn.error?.code ?? "",
        },
      },
    },
  };
}

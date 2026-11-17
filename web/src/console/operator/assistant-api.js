/*
 * The assistant's Hub client: turns, tools and conversation threads
 * (docs/architecture/agent-runs.md, "Assistant turns and threads").
 *
 * Every assistant request, read or write, leaves through assistantRequest.
 * Nothing else in the console calls fetch for /v1/assistant, so a header every
 * write must carry is added in one place.
 */
import { readAgentStream } from "./agent-runs.js";

/** @typedef {{signal?: AbortSignal, fetchImpl?: typeof fetch}} RequestOptions */

/** A refusal or failure the Hub answered with, by its stable code. */
export class AssistantError extends Error {
  /**
   * @param {string} code
   * @param {string} message
   * @param {{status?: number, turn?: unknown, retryAfter?: number | null}} [detail]
   */
  constructor(code, message, detail = {}) {
    super(message);
    this.name = "AssistantError";
    this.code = code;
    this.status = detail.status ?? 0;
    /** The partial turn document, when a model call was reached. */
    this.turn = detail.turn ?? null;
    /** Whole seconds the Hub's Retry-After named, or null when it named none. */
    this.retryAfter = detail.retryAfter ?? null;
  }
}

/**
 * The wait a Retry-After header names, in whole seconds. The Hub sends
 * delta-seconds; an HTTP date is accepted too. Anything else is null.
 * @param {string | null | undefined} header
 * @param {number} [now] milliseconds since the epoch
 * @returns {number | null}
 */
export function retryAfterSeconds(header, now = Date.now()) {
  const value = typeof header === "string" ? header.trim() : "";
  if (!value) return null;
  if (/^\d+$/.test(value)) return Number(value);
  const at = Date.parse(value);
  if (Number.isNaN(at)) return null;
  return Math.max(0, Math.ceil((at - now) / 1000));
}

/**
 * The one place an assistant request is sent.
 * @param {string} method
 * @param {string} path
 * @param {{body?: unknown, accept?: string, signal?: AbortSignal, fetchImpl?: typeof fetch}} [options]
 * @returns {Promise<Response>}
 */
export function assistantRequest(method, path, options = {}) {
  const { body, accept = "application/json", signal } = options;
  const headers = { accept };
  if (body !== undefined) headers["content-type"] = "application/json";
  return (options.fetchImpl ?? fetch)(path, {
    method,
    headers,
    credentials: "same-origin",
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    ...(signal ? { signal } : {}),
  });
}

async function failure(response) {
  let detail = {};
  try {
    detail = (await response.json()) ?? {};
  } catch {
    /* A body that is not JSON carries no code; the status is all there is. */
  }
  const code =
    typeof detail.code === "string" && detail.code
      ? detail.code
      : response.status === 401
        ? "UNAUTHORIZED"
        : "TURN_FAILED";
  const message =
    typeof detail.error === "string" && detail.error
      ? detail.error
      : `the Hub returned ${response.status}`;
  return new AssistantError(code, message, {
    status: response.status,
    turn: detail.turn,
    retryAfter: retryAfterSeconds(response.headers?.get?.("retry-after")),
  });
}

async function json(method, path, options) {
  const response = await assistantRequest(method, path, options);
  if (!response.ok) throw await failure(response);
  return response.status === 204 ? null : response.json();
}

const threadPath = (id) => `/v1/assistant/threads/${encodeURIComponent(id)}`;

/** The caller's threads, newest update first. */
/** @param {RequestOptions} [options] */
export async function listThreads(options = {}) {
  const body = await json("GET", "/v1/assistant/threads", options);
  return Array.isArray(body?.threads) ? body.threads : [];
}

/** @param {string} [title] @param {RequestOptions} [options] */
export async function createThread(title = "", options = {}) {
  const thread = await json("POST", "/v1/assistant/threads", {
    ...options,
    body: title ? { title } : {},
  });
  if (typeof thread?.id !== "string" || !thread.id)
    throw new AssistantError(
      "TURN_FAILED",
      "the Hub answered a thread request without a thread id",
    );
  return thread;
}

/** @param {string} id @param {RequestOptions} [options] */
export const readThread = (id, options = {}) =>
  json("GET", threadPath(id), options);

/** @param {string} id @param {RequestOptions} [options] */
export const deleteThread = (id, options = {}) =>
  json("DELETE", threadPath(id), options);

/**
 * Run one tool a person submitted. A refusal comes back as a result with
 * ok false and the Hub's reason, never as a success.
 * @param {{mode: string, tool: string, arguments: unknown}} body
 * @param {RequestOptions} [options]
 * @returns {Promise<Record<string, unknown>>}
 */
export async function postTool(body, options = {}) {
  const response = await assistantRequest("POST", "/v1/assistant/tools", {
    ...options,
    body,
  });
  let payload = {};
  try {
    payload = (await response.json()) ?? {};
  } catch {
    /* Reported below from the status. */
  }
  if (response.ok) return payload;
  return {
    label: payload.label || "No model call",
    tool: body.tool,
    ok: false,
    code: payload.code || "",
    error: payload.error || `the Hub returned ${response.status}`,
    readOps: 0,
    writeOps: 0,
  };
}

/**
 * Send one turn and stream its events. `exchange` reduces the events and is
 * the same object readAgentStream drives for a planning run. Resolves with the
 * turn document when the Hub answered in JSON instead of a stream, which it
 * does when the turn made no model call it could report as events.
 * @param {{mode: string, message: string, context?: unknown[], threadId?: string}} body
 * @param {{accept(event: unknown): unknown, terminal: boolean, error: unknown}} exchange
 */
export async function streamTurn(body, exchange, onEvent, options = {}) {
  const response = await assistantRequest("POST", "/v1/assistant/turns", {
    ...options,
    accept: "text/event-stream",
    body,
  });
  if (!response.ok) throw await failure(response);
  if (!response.headers.get("content-type")?.startsWith("text/event-stream"))
    return response.json();
  await readAgentStream(response, exchange, onEvent);
  return null;
}

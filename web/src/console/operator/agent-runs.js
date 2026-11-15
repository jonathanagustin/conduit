import { FormSurfaces } from "./a2ui-forms.js";
import { applyPatch } from "./json-patch.js";

const copy = (value) => JSON.parse(JSON.stringify(value));
const invalid = () => {
  throw new Error("The Hub returned an invalid agent stream.");
};
const identifier = (value) =>
  typeof value === "string" && value.length > 0 && value.length <= 512;

// One explicit-start/end Hub exchange. Snapshots replace rather than append;
// transport truncation never counts as a successful run.
export class AgentExchange {
  constructor(threadId, runId) {
    this.threadId = threadId;
    this.runId = runId;
    this.started = false;
    this.terminal = false;
    this.state = {};
    this.messages = new Map();
    this.openMessages = new Set();
    this.usedMessages = new Set();
    this.tools = new Map();
    this.steps = new Set();
    this.forms = new FormSurfaces();
    this.outcome = null;
    this.error = null;
  }
  accept(event) {
    if (
      !event ||
      typeof event.type !== "string" ||
      this.terminal ||
      (!this.started && event.type !== "RUN_STARTED")
    )
      invalid();
    const message = this.messages.get(event.messageId);
    const tool = this.tools.get(event.toolCallId);
    switch (event.type) {
      case "RUN_STARTED":
        if (
          this.started ||
          event.threadId !== this.threadId ||
          event.runId !== this.runId
        )
          invalid();
        this.started = true;
        break;
      case "RUN_FINISHED":
        if (
          event.threadId !== this.threadId ||
          event.runId !== this.runId ||
          this.openMessages.size ||
          this.steps.size ||
          [...this.tools.values()].some((item) => item.status === "arguments")
        )
          invalid();
        this.outcome = copy(event.outcome || { type: "success" });
        if (!["success", "interrupt", "cancelled"].includes(this.outcome.type))
          invalid();
        this.terminal = true;
        break;
      case "RUN_ERROR":
        if (typeof event.message !== "string") invalid();
        this.error = {
          code: event.code || "AGENT_RUN_FAILED",
          message: event.message,
        };
        this.terminal = true;
        break;
      case "CUSTOM":
        if (event.name !== "a2ui") invalid();
        this.forms.accept(event.value);
        break;
      case "STATE_SNAPSHOT":
        if (
          !event.snapshot ||
          typeof event.snapshot !== "object" ||
          Array.isArray(event.snapshot)
        )
          invalid();
        this.state = copy(event.snapshot);
        break;
      case "STATE_DELTA":
        this.state = applyPatch(this.state, event.delta);
        break;
      case "MESSAGES_SNAPSHOT": {
        if (!Array.isArray(event.messages) || this.openMessages.size) invalid();
        const next = new Map();
        for (const item of event.messages) {
          if (
            !identifier(item?.id) ||
            next.has(item.id) ||
            typeof item.role !== "string"
          )
            invalid();
          next.set(item.id, copy(item));
          this.usedMessages.add(item.id);
        }
        this.messages = next;
        break;
      }
      case "TEXT_MESSAGE_START":
        if (
          !identifier(event.messageId) ||
          this.usedMessages.has(event.messageId) ||
          event.role !== "assistant"
        )
          invalid();
        this.messages.set(event.messageId, {
          id: event.messageId,
          role: "assistant",
          content: "",
        });
        this.openMessages.add(event.messageId);
        this.usedMessages.add(event.messageId);
        break;
      case "TEXT_MESSAGE_CONTENT":
        if (
          !this.openMessages.has(event.messageId) ||
          typeof event.delta !== "string"
        )
          invalid();
        message.content += event.delta;
        break;
      case "TEXT_MESSAGE_END":
        if (!this.openMessages.delete(event.messageId)) invalid();
        break;
      case "TOOL_CALL_START":
        if (
          !identifier(event.toolCallId) ||
          !identifier(event.toolCallName) ||
          tool ||
          !this.openMessages.has(event.parentMessageId)
        )
          invalid();
        this.tools.set(event.toolCallId, {
          id: event.toolCallId,
          name: event.toolCallName,
          arguments: "",
          status: "arguments",
        });
        break;
      case "TOOL_CALL_ARGS":
        if (
          tool?.status !== "arguments" ||
          typeof event.delta !== "string" ||
          tool.arguments.length + event.delta.length > 8192
        )
          invalid();
        tool.arguments += event.delta;
        break;
      case "TOOL_CALL_END":
        if (tool?.status !== "arguments") invalid();
        tool.status = "pending";
        break;
      case "TOOL_CALL_RESULT":
        if (
          tool?.status !== "pending" ||
          !identifier(event.messageId) ||
          this.usedMessages.has(event.messageId) ||
          typeof event.content !== "string"
        )
          invalid();
        tool.status = "complete";
        this.usedMessages.add(event.messageId);
        this.messages.set(event.messageId, {
          id: event.messageId,
          role: "tool",
          toolCallId: event.toolCallId,
          content: event.content,
        });
        break;
      case "STEP_STARTED":
        if (!identifier(event.stepName) || this.steps.has(event.stepName))
          invalid();
        this.steps.add(event.stepName);
        break;
      case "STEP_FINISHED":
        if (!this.steps.delete(event.stepName)) invalid();
        break;
      default:
        invalid();
    }
    return this;
  }
}

// Deliberately separate from the legacy planning SSE parser: malformed frames
// and an unterminated exchange are errors rather than silently skipped data.
export async function readAgentStream(response, exchange, onEvent) {
  if (
    !response.headers?.get("content-type")?.startsWith("text/event-stream") ||
    !response.body
  )
    invalid();
  const reader = response.body.getReader();
  const decoder = new TextDecoder("utf-8", { fatal: true });
  let buffer = "";
  let received = 0;
  try {
    for (;;) {
      const { value, done } = await reader.read();
      received += value?.byteLength || 0;
      if (received > 4 * 1024 * 1024 + 65536) invalid();
      buffer += decoder.decode(value || new Uint8Array(), { stream: !done });
      // The Hub emits LF, but accept CRLF frames as allowed by SSE.
      let boundary;
      while ((boundary = /\r?\n\r?\n/u.exec(buffer))) {
        const frame = buffer.slice(0, boundary.index);
        buffer = buffer.slice(boundary.index + boundary[0].length);
        const data = frame
          .split(/\r?\n/u)
          .filter((line) => line.startsWith("data:"))
          .map((line) => line.slice(5).replace(/^ /u, ""))
          .join("\n");
        if (!data) continue;
        const event = JSON.parse(data);
        exchange.accept(event);
        onEvent?.(exchange, event);
      }
      if (done) {
        if (buffer.trim() || !exchange.terminal)
          throw new Error(
            "The agent stream was interrupted. Recover a snapshot before continuing.",
          );
        break;
      }
    }
    if (exchange.error)
      throw Object.assign(new Error(exchange.error.message), {
        code: exchange.error.code,
      });
    return exchange;
  } finally {
    try {
      await reader.cancel();
    } finally {
      reader.releaseLock();
    }
  }
}

export async function runAgent(input, onEvent, fetchImpl = fetch, signal) {
  const response = await fetchImpl("/v1/agent-runs", {
    method: "POST",
    headers: {
      accept: "text/event-stream",
      "content-type": "application/json",
    },
    body: JSON.stringify(input),
    signal,
  });
  if (!response.ok) {
    let detail = {};
    try {
      detail = await response.json();
    } catch {
      /* Use the bounded fallback. */
    }
    throw Object.assign(
      new Error(detail.error || `The Hub answered ${response.status}.`),
      { code: detail.code || "", status: response.status },
    );
  }
  return readAgentStream(
    response,
    new AgentExchange(input.threadId, input.runId),
    onEvent,
  );
}

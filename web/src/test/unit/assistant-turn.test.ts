import { describe, expect, it } from "vite-plus/test";

import {
  AssistantExchange,
  THREADS_LOADING,
  applyTurnDocument,
  assistantErrorText,
  confirmRequest,
  failTurn,
  messagesFromThread,
  modelLabel,
  newTurn,
  reduceTurn,
  threadDeleted,
  threadOpened,
  threadSelected,
  threadsLoaded,
  threadsUnavailable,
  toThreadMessage,
  toolOutcome,
  turnErrorFrom,
  waitText,
  type AssistantTurn,
  type ToolRow,
} from "../../console/assistant-turn";

const QUERY = { objectType: "Environment" };
const RESULT = {
  tool: "query_ontology_objects",
  ok: true,
  readOps: 1,
  writeOps: 0,
  matchedCount: 1,
  entities: [{ id: "zeta", objectType: "Environment", label: "zeta" }],
};
const DOCUMENT = {
  label: "Live model call",
  text: "One environment: zeta.",
  results: [RESULT],
  toolCalls: [
    {
      id: "call_1_1",
      round: 1,
      name: "query_ontology_objects",
      arguments: QUERY,
    },
  ],
  rounds: 2,
  threadId: "ath_1",
  model: { kind: "openai-http", model: "gpt-test" },
};

/** The events the Hub writes for one tool call and an answer, in order. */
const STREAM = [
  { type: "RUN_STARTED", threadId: "ath_1", runId: "arun_1" },
  { type: "STEP_STARTED", stepName: "round-1" },
  {
    type: "TOOL_CALL_START",
    toolCallId: "call_1_1",
    toolCallName: "query_ontology_objects",
  },
  {
    type: "TOOL_CALL_ARGS",
    toolCallId: "call_1_1",
    delta: JSON.stringify(QUERY),
  },
  { type: "TOOL_CALL_END", toolCallId: "call_1_1" },
  {
    type: "TOOL_CALL_RESULT",
    toolCallId: "call_1_1",
    messageId: "call_1_1:result",
    content: JSON.stringify(RESULT),
  },
  { type: "STEP_FINISHED", stepName: "round-1" },
  { type: "STEP_STARTED", stepName: "round-2" },
  { type: "STEP_FINISHED", stepName: "round-2" },
  { type: "TEXT_MESSAGE_START", messageId: "arun_1:answer", role: "assistant" },
  {
    type: "TEXT_MESSAGE_CONTENT",
    messageId: "arun_1:answer",
    delta: "One environment: zeta.",
  },
  { type: "TEXT_MESSAGE_END", messageId: "arun_1:answer" },
  {
    type: "RUN_FINISHED",
    threadId: "ath_1",
    runId: "arun_1",
    result: DOCUMENT,
  },
];

const fold = (events: unknown[], from: AssistantTurn = newTurn()) =>
  events.reduce<AssistantTurn>(reduceTurn, from);

const tool = (over: Partial<ToolRow> = {}): ToolRow => ({
  id: "call_1_1",
  name: "governed_write",
  round: 1,
  argsText: "",
  args: {},
  followUps: [],
  pending: false,
  ...over,
});

describe("reduceTurn", () => {
  it("shows a tool call with its real arguments before its result arrives", () => {
    const turn = fold(STREAM.slice(0, 5));
    expect(turn.status).toBe("running");
    expect(turn.started).toBe(true);
    expect(turn.tools).toEqual([
      {
        id: "call_1_1",
        name: "query_ontology_objects",
        round: 1,
        argsText: JSON.stringify(QUERY),
        args: QUERY,
        followUps: [],
        pending: false,
      },
    ]);
    expect(toolOutcome(turn.tools[0]!).tone).toBe("running");
  });

  it("attaches the result to the call that produced it", () => {
    const turn = fold(STREAM.slice(0, 6));
    expect(turn.tools[0]!.result).toEqual(RESULT);
    expect(toolOutcome(turn.tools[0]!)).toEqual({
      tone: "ok",
      summary: "1 matched",
    });
  });

  it("finishes with the answer, the thread and the model that served it", () => {
    const turn = fold(STREAM);
    expect(turn).toMatchObject({
      status: "complete",
      text: "One environment: zeta.",
      label: "Live model call",
      rounds: 2,
      threadId: "ath_1",
      runId: "arun_1",
      model: { kind: "openai-http", model: "gpt-test" },
      error: null,
    });
    // The document repeats the call the stream already carried; it is not doubled.
    expect(turn.tools).toHaveLength(1);
  });

  it("ends in an error state on RUN_ERROR and keeps what already ran", () => {
    const turn = fold([
      ...STREAM.slice(0, 7),
      { type: "RUN_ERROR", code: "ROUND_CAP", message: "round cap reached" },
    ]);
    expect(turn.status).toBe("error");
    expect(turn.error).toEqual({
      code: "ROUND_CAP",
      message: "round cap reached",
    });
    expect(turn.tools[0]!.result).toEqual(RESULT);
  });

  it("ignores an event type it does not know", () => {
    const before = fold(STREAM.slice(0, 2));
    expect(reduceTurn(before, { type: "STATE_SNAPSHOT", snapshot: {} })).toBe(
      before,
    );
  });
});

describe("AssistantExchange", () => {
  it("is terminal after RUN_FINISHED and carries no error", () => {
    const exchange = new AssistantExchange();
    for (const event of STREAM) exchange.accept(event);
    expect(exchange.terminal).toBe(true);
    expect(exchange.error).toBeNull();
    expect(exchange.turn.status).toBe("complete");
  });

  it("exposes RUN_ERROR as the error the stream reader throws", () => {
    const exchange = new AssistantExchange();
    exchange.accept(STREAM[0]);
    exchange.accept({ type: "RUN_ERROR", code: "MODEL_FAILED", message: "x" });
    expect(exchange.terminal).toBe(true);
    expect(exchange.error).toEqual({ code: "MODEL_FAILED", message: "x" });
    // Nothing after the terminal event changes the turn.
    exchange.accept({ type: "TEXT_MESSAGE_CONTENT", delta: "late" });
    expect(exchange.turn.text).toBe("");
  });
});

describe("applyTurnDocument", () => {
  it("builds the tool calls from a JSON turn that was never streamed", () => {
    const turn = applyTurnDocument(newTurn(), DOCUMENT);
    expect(turn.tools).toEqual([
      {
        id: "call_1_1",
        name: "query_ontology_objects",
        round: 1,
        argsText: JSON.stringify(QUERY),
        args: QUERY,
        result: RESULT,
        followUps: [],
        pending: false,
      },
    ]);
    expect(turn.text).toBe("One environment: zeta.");
  });
});

describe("failTurn", () => {
  it("turns a pre-stream refusal into an error the thread can retry", () => {
    const turn = failTurn(newTurn(), {
      code: "MODEL_NOT_CONFIGURED",
      message: "no model",
    });
    expect(turn.status).toBe("error");
    expect(turn.started).toBe(false);
    expect(turn.error?.code).toBe("MODEL_NOT_CONFIGURED");
  });

  it("keeps the partial turn a failure carried", () => {
    const turn = failTurn(newTurn(), {
      code: "MODEL_TIMEOUT",
      message: "timed out",
      turn: { ...DOCUMENT, text: "" },
    });
    expect(turn.tools).toHaveLength(1);
    expect(turn.started).toBe(true);
  });

  it("records an aborted request as cancelled, not as an error", () => {
    const turn = failTurn(fold(STREAM.slice(0, 5)), {
      name: "AbortError",
      message: "The user aborted a request.",
    });
    expect(turn.status).toBe("cancelled");
    expect(turn.error).toBeNull();
    expect(turn.tools).toHaveLength(1);
  });

  it("names a dropped connection", () => {
    expect(failTurn(newTurn(), new Error("socket closed")).error).toEqual({
      code: "CONNECTION_LOST",
      message: "socket closed",
    });
  });
});

describe("assistantErrorText", () => {
  it("tells an operator how a model is configured, by document name", () => {
    const message = assistantErrorText({
      code: "MODEL_NOT_CONFIGURED",
      message: "model is not configured",
    });
    expect(message).toContain("docs/operations/model-http.md");
    expect(message).toContain("docs/operations/local-model-cli.md");
    expect(message).not.toContain("local model CLI is not configured");
  });

  it("has a distinct sentence for every documented code", () => {
    const codes = [
      "MODEL_NOT_CONFIGURED",
      "MODEL_FAILED",
      "MODEL_TIMEOUT",
      "MODEL_REPLY_INVALID",
      "ROUND_CAP",
      "TURN_FAILED",
      "AUDIT_UNAVAILABLE",
      "THREADS_NOT_CONFIGURED",
      "THREAD_NOT_FOUND",
      "THREAD_LIMIT",
      "THREAD_FULL",
      "INVALID_REQUEST",
      "TOOL_INVALID",
      "UNAUTHORIZED",
      "CANCELLED",
      "MODEL_BUSY",
      "MODEL_BUDGET_EXCEEDED",
    ];
    const messages = codes.map((code) =>
      assistantErrorText({ code, message: "" }),
    );
    expect(new Set(messages).size).toBe(codes.length);
    for (const message of messages)
      expect(message).not.toMatch(/^The turn failed/);
  });

  it("adds the Hub's sentence where the cause varies", () => {
    expect(
      assistantErrorText({ code: "MODEL_FAILED", message: "upstream 500" }),
    ).toContain("The Hub said: upstream 500");
    expect(
      assistantErrorText({ code: "THREAD_FULL", message: "thread is full" }),
    ).not.toContain("The Hub said");
  });

  it("states the wait a 429 named, and none when it named none", () => {
    const busy = turnErrorFrom({
      code: "MODEL_BUSY",
      message: "model is busy",
      retryAfter: 7,
    });
    expect(busy).toEqual({
      code: "MODEL_BUSY",
      message: "model is busy",
      retryAfter: 7,
    });
    expect(assistantErrorText(busy)).toContain("retry in 7 seconds");
    const spent = assistantErrorText({
      code: "MODEL_BUDGET_EXCEEDED",
      message: "",
      retryAfter: 7500,
    });
    expect(spent).toContain("budget");
    expect(spent).toContain("retry in 2 h 5 min");
    // RUN_ERROR in a stream has no Retry-After header to read.
    expect(
      assistantErrorText(
        turnErrorFrom({ code: "MODEL_BUSY", message: "", retryAfter: null }),
      ),
    ).not.toContain("retry in");
  });

  it("writes a wait in the units a person reads", () => {
    expect(waitText(0)).toBe("1 second");
    expect(waitText(59)).toBe("59 seconds");
    expect(waitText(60)).toBe("1 minute");
    expect(waitText(61)).toBe("2 minutes");
    expect(waitText(3600)).toBe("1 h");
    expect(waitText(7500)).toBe("2 h 5 min");
  });

  it("falls back to the Hub's words for a code it does not know", () => {
    expect(
      assistantErrorText({ code: "NEW_CODE", message: "new reason" }),
    ).toBe("new reason");
    expect(assistantErrorText({ code: "NEW_CODE", message: "" })).toBe(
      "The turn failed (NEW_CODE).",
    );
  });
});

describe("modelLabel", () => {
  it("names the adapter and the reported model", () => {
    expect(modelLabel({ kind: "openai-http", model: "gpt-test" })).toEqual({
      text: "openai-http · gpt-test",
      caution: false,
    });
    expect(modelLabel({ kind: "model-cli" })).toEqual({
      text: "model-cli",
      caution: false,
    });
  });

  it("marks a name the Hub calls unverified as a caution", () => {
    expect(
      modelLabel({
        kind: "model-cli",
        model: "Unverified (model CLI, CONDUIT_MODEL_NAME not set)",
      })?.caution,
    ).toBe(true);
  });

  it("shows nothing when the Hub reported no model", () => {
    expect(modelLabel(null)).toBeNull();
    expect(modelLabel({ kind: "" })).toBeNull();
  });
});

describe("confirmRequest", () => {
  const elicitation = {
    action: "approve_change",
    targetId: "cr-7",
    justification: "reviewed",
  };
  const held = tool({
    args: { ...elicitation, confirm: true },
    result: {
      tool: "governed_write",
      ok: true,
      needsConfirm: true,
      elicitation,
      reason: "confirm to apply this action",
    },
  });

  it("re-sends the same call with confirm set by the person", () => {
    expect(confirmRequest("ontology", held, {}, true)).toEqual({
      mode: "ontology",
      tool: "governed_write",
      arguments: { ...elicitation, confirm: true },
    });
  });

  it("does not carry a model-supplied confirm into a Continue", () => {
    const asking = tool({
      args: { action: "approve_change", targetId: "cr-7", confirm: true },
      result: {
        tool: "governed_write",
        ok: true,
        fields: [{ name: "justification", label: "Justification" }],
        elicitation: { action: "approve_change", targetId: "cr-7" },
      },
    });
    expect(
      confirmRequest("ontology", asking, { justification: "ok" }, false),
    ).toEqual({
      mode: "ontology",
      tool: "governed_write",
      arguments: {
        action: "approve_change",
        targetId: "cr-7",
        justification: "ok",
      },
    });
  });

  it("confirms the newest answer when a Continue came first", () => {
    const row = tool({
      args: { action: "approve_change" },
      result: { tool: "governed_write", fields: [{ name: "targetId" }] },
      followUps: [
        {
          tool: "governed_write",
          needsConfirm: true,
          elicitation: { action: "approve_change", targetId: "cr-9" },
        },
      ],
    });
    expect(confirmRequest("ontology", row, {}, true).arguments).toEqual({
      action: "approve_change",
      targetId: "cr-9",
      confirm: true,
    });
  });

  it("falls back to the call's own arguments without an elicitation", () => {
    const row = tool({
      args: { action: "add_eval_case", targetId: "run-1" },
      result: { tool: "governed_write", needsConfirm: true },
    });
    expect(confirmRequest("", row, {}, true)).toEqual({
      mode: "ontology",
      tool: "governed_write",
      arguments: { action: "add_eval_case", targetId: "run-1", confirm: true },
    });
  });
});

describe("toolOutcome", () => {
  it("states a refusal with the Hub's reason", () => {
    const reason = "no action rule governs this action, so it is refused";
    expect(
      toolOutcome(
        tool({
          result: {
            tool: "governed_write",
            ok: true,
            reason,
            constraints: [{ id: "propose_memory", status: "DENY", reason }],
          },
        }),
      ),
    ).toEqual({ tone: "refused", summary: `Refused: ${reason}` });
  });

  it("asks for confirmation, input, or reports what happened", () => {
    expect(
      toolOutcome(tool({ result: { needsConfirm: true, reason: "confirm" } }))
        .tone,
    ).toBe("confirm");
    expect(
      toolOutcome(tool({ result: { fields: [{ name: "targetId" }] } })).tone,
    ).toBe("input");
    expect(
      toolOutcome(tool({ result: { ok: false, error: "tool refused" } })),
    ).toEqual({ tone: "failed", summary: "tool refused" });
    expect(
      toolOutcome(
        tool({
          result: { ok: true, applied: true, reason: "review recorded" },
        }),
      ),
    ).toEqual({ tone: "ok", summary: "review recorded" });
    expect(
      toolOutcome(
        tool({
          result: {
            ok: true,
            allowed: true,
            changeRequestId: "cr-3",
            changeRequestStatus: "OPEN",
          },
        }),
      ).summary,
    ).toBe("Change request cr-3 OPEN");
  });

  it("reports the follow-up once the person confirmed", () => {
    expect(
      toolOutcome(
        tool({
          result: { needsConfirm: true },
          followUps: [{ ok: true, applied: true, reason: "review recorded" }],
        }),
      ).summary,
    ).toBe("review recorded");
  });
});

describe("thread list state", () => {
  const rows = [
    {
      id: "ath_2",
      title: "second",
      updatedAt: "2026-11-17T10:00:00Z",
      messageCount: 2,
    },
    {
      id: "ath_1",
      title: "",
      updatedAt: "2026-11-17T09:00:00Z",
      messageCount: 4,
    },
    { title: "no id" },
  ];

  it("starts loading with nothing open", () => {
    expect(THREADS_LOADING).toMatchObject({ status: "loading", openId: null });
  });

  it("keeps the Hub's order and reopens the remembered thread", () => {
    const list = threadsLoaded(rows, "ath_1");
    expect(list.status).toBe("ready");
    expect(list.threads.map((row) => row.id)).toEqual(["ath_2", "ath_1"]);
    expect(list.openId).toBe("ath_1");
  });

  it("opens nothing when the remembered thread is gone", () => {
    expect(threadsLoaded(rows, "ath_9").openId).toBeNull();
    expect(threadsLoaded([], null)).toMatchObject({
      status: "ready",
      threads: [],
    });
  });

  it("puts a created thread first and opens it", () => {
    const list = threadOpened(threadsLoaded(rows, null), {
      id: "ath_3",
      title: "",
      messageCount: 0,
    });
    expect(list.threads.map((row) => row.id)).toEqual([
      "ath_3",
      "ath_2",
      "ath_1",
    ]);
    expect(list.openId).toBe("ath_3");
  });

  it("selects only a thread it lists", () => {
    const list = threadsLoaded(rows, null);
    expect(threadSelected(list, "ath_2").openId).toBe("ath_2");
    expect(threadSelected(list, "ath_9")).toBe(list);
    expect(
      threadSelected(threadSelected(list, "ath_2"), null).openId,
    ).toBeNull();
  });

  it("closes the open thread when it is deleted", () => {
    const list = threadDeleted(threadsLoaded(rows, "ath_1"), "ath_1");
    expect(list.threads.map((row) => row.id)).toEqual(["ath_2"]);
    expect(list.openId).toBeNull();
    expect(threadDeleted(threadsLoaded(rows, "ath_1"), "ath_2").openId).toBe(
      "ath_1",
    );
  });

  it("falls back to one unsaved conversation and says why", () => {
    const none = threadsUnavailable({
      code: "THREADS_NOT_CONFIGURED",
      message: "no conversation store",
    });
    expect(none.status).toBe("stateless");
    expect(none.notice).toContain("no conversation store");
    expect(none.notice).toContain("not saved");
    const failed = threadsUnavailable(new Error("the Hub returned 500"));
    expect(failed.status).toBe("stateless");
    expect(failed.notice).toContain("the Hub returned 500");
    // A thread cannot be opened or created into a stateless list.
    expect(threadOpened(none, { id: "ath_1" })).toBe(none);
  });
});

describe("messagesFromThread", () => {
  const thread = {
    id: "ath_1",
    title: "which environments exist?",
    messages: [
      {
        role: "user",
        text: "which environments exist?",
        mode: "explore",
        createdAt: "2026-11-17T09:00:00Z",
      },
      {
        role: "assistant",
        text: "One environment: zeta.",
        mode: "explore",
        createdAt: "2026-11-17T09:00:07Z",
        toolCalls: [
          {
            id: "call_1_1",
            round: 1,
            name: "query_ontology_objects",
            arguments: QUERY,
            result: RESULT,
          },
        ],
      },
      {
        role: "user",
        text: "again",
        mode: "explore",
        createdAt: "2026-11-17T09:01:00Z",
      },
      {
        role: "assistant",
        text: "",
        mode: "explore",
        createdAt: "2026-11-17T09:01:05Z",
        errorCode: "ROUND_CAP",
      },
      {
        role: "user",
        text: "stop",
        mode: "explore",
        createdAt: "2026-11-17T09:02:00Z",
      },
      {
        role: "assistant",
        text: "",
        mode: "explore",
        createdAt: "2026-11-17T09:02:01Z",
        errorCode: "CANCELLED",
      },
    ],
  };

  it("restores messages, tool arguments and results from the Hub", () => {
    const messages = messagesFromThread(thread);
    expect(messages.map((message) => message.id)).toEqual([
      "ath_1:0",
      "ath_1:1",
      "ath_1:2",
      "ath_1:3",
      "ath_1:4",
      "ath_1:5",
    ]);
    const answer = messages[1]!;
    expect(answer.role === "assistant" && answer.turn).toMatchObject({
      status: "complete",
      text: "One environment: zeta.",
      tools: [{ id: "call_1_1", args: QUERY, result: RESULT }],
    });
  });

  it("restores a failed and a cancelled turn as what they were", () => {
    const messages = messagesFromThread(thread);
    const failed = messages[3]!;
    const cancelled = messages[5]!;
    expect(failed.role === "assistant" && failed.turn).toMatchObject({
      status: "error",
      error: { code: "ROUND_CAP" },
    });
    expect(cancelled.role === "assistant" && cancelled.turn.status).toBe(
      "cancelled",
    );
  });

  it("reads a thread with no messages as empty", () => {
    expect(messagesFromThread({ id: "ath_2" })).toEqual([]);
  });
});

describe("toThreadMessage", () => {
  const answer = (turn: AssistantTurn) =>
    toThreadMessage({
      id: "m2",
      role: "assistant",
      mode: "explore",
      createdAt: "2026-11-17T09:00:07Z",
      turn,
    });

  it("renders tool calls with their arguments, then the answer", () => {
    const message = answer(fold(STREAM));
    expect(message.status).toEqual({ type: "complete", reason: "stop" });
    expect(message.content).toEqual([
      {
        type: "tool-call",
        toolCallId: "call_1_1",
        toolName: "query_ontology_objects",
        args: QUERY,
        argsText: JSON.stringify(QUERY),
        result: RESULT,
      },
      { type: "text", text: "One environment: zeta." },
    ]);
    expect(message.metadata?.custom.conduit).toMatchObject({
      mode: "explore",
      label: "Live model call",
      model: { kind: "openai-http", model: "gpt-test" },
    });
  });

  it("is running while the stream is open", () => {
    expect(answer(fold(STREAM.slice(0, 5))).status).toEqual({
      type: "running",
    });
  });

  it("makes a failure an incomplete message with an error, not prose", () => {
    const message = answer(
      failTurn(newTurn(), { code: "MODEL_NOT_CONFIGURED", message: "" }),
    );
    expect(message.content).toEqual([]);
    expect(message.status).toMatchObject({
      type: "incomplete",
      reason: "error",
    });
    expect(
      message.status?.type === "incomplete" &&
        message.status.reason === "error" &&
        message.status.error,
    ).toContain("No model is configured");
    expect(message.metadata?.custom.conduit.code).toBe("MODEL_NOT_CONFIGURED");
  });

  it("marks a cancelled turn as cancelled", () => {
    expect(
      answer(failTurn(newTurn(), { name: "AbortError", message: "" })).status,
    ).toEqual({ type: "incomplete", reason: "cancelled" });
  });

  it("passes a user message through as text", () => {
    expect(
      toThreadMessage({
        id: "m1",
        role: "user",
        text: "hello",
        mode: "explore",
        createdAt: "not a date",
      }),
    ).toEqual({
      id: "m1",
      role: "user",
      content: [{ type: "text", text: "hello" }],
    });
  });
});

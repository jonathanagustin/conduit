import { describe, expect, it, vi } from "vite-plus/test";

import {
  AssistantError,
  assistantRequest,
  createThread,
  deleteThread,
  listThreads,
  postTool,
  readThread,
  retryAfterSeconds,
  streamTurn,
} from "../../console/operator/assistant-api.js";
import { AssistantExchange } from "../../console/assistant-turn";

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });

const sse = (events: unknown[]) =>
  new Response(
    ": keep-alive\n\n" +
      events.map((event) => `data: ${JSON.stringify(event)}\n\n`).join(""),
    { headers: { "content-type": "text/event-stream" } },
  );

type Call = [string, RequestInit & { headers: Record<string, string> }];

function fetching(...responses: Response[]) {
  const fetchImpl = vi.fn(async () => responses.shift()!);
  const calls = () => fetchImpl.mock.calls as unknown as Call[];
  return { fetchImpl: fetchImpl as unknown as typeof fetch, calls };
}

describe("assistantRequest", () => {
  it("is the one request shape: method, JSON body, same-origin credentials", async () => {
    const { fetchImpl, calls } = fetching(json(200, {}));
    await assistantRequest("POST", "/v1/assistant/tools", {
      body: { tool: "t" },
      fetchImpl,
    });
    expect(calls()[0]).toEqual([
      "/v1/assistant/tools",
      {
        method: "POST",
        headers: {
          accept: "application/json",
          "content-type": "application/json",
        },
        credentials: "same-origin",
        body: '{"tool":"t"}',
      },
    ]);
  });

  it("sends no body or content type on a read", async () => {
    const { fetchImpl, calls } = fetching(json(200, { threads: [] }));
    await listThreads({ fetchImpl });
    expect(calls()[0]![1]).toEqual({
      method: "GET",
      headers: { accept: "application/json" },
      credentials: "same-origin",
    });
  });
});

describe("thread routes", () => {
  it("lists, creates, reads and deletes on the documented paths", async () => {
    const { fetchImpl, calls } = fetching(
      json(200, { threads: [{ id: "ath_1" }] }),
      json(201, { id: "ath_2", messages: [] }),
      json(200, { id: "ath/2", messages: [] }),
      new Response(null, { status: 204 }),
    );
    expect(await listThreads({ fetchImpl })).toEqual([{ id: "ath_1" }]);
    expect(await createThread("", { fetchImpl })).toMatchObject({
      id: "ath_2",
    });
    await readThread("ath/2", { fetchImpl });
    expect(await deleteThread("ath/2", { fetchImpl })).toBeNull();
    expect(calls().map(([path, init]) => `${init.method} ${path}`)).toEqual([
      "GET /v1/assistant/threads",
      "POST /v1/assistant/threads",
      "GET /v1/assistant/threads/ath%2F2",
      "DELETE /v1/assistant/threads/ath%2F2",
    ]);
    expect(calls()[1]![1].body).toBe("{}");
  });

  it("reads a list without a threads array as empty", async () => {
    const { fetchImpl } = fetching(json(200, { items: [] }));
    expect(await listThreads({ fetchImpl })).toEqual([]);
  });

  it("throws the Hub's stable code", async () => {
    const { fetchImpl } = fetching(
      json(503, { code: "THREADS_NOT_CONFIGURED", error: "no store" }),
    );
    await expect(listThreads({ fetchImpl })).rejects.toMatchObject({
      name: "AssistantError",
      code: "THREADS_NOT_CONFIGURED",
      message: "no store",
      status: 503,
    });
  });

  it("refuses a created thread that has no id", async () => {
    const { fetchImpl } = fetching(json(200, {}));
    await expect(createThread("", { fetchImpl })).rejects.toBeInstanceOf(
      AssistantError,
    );
  });
});

describe("postTool", () => {
  it("returns the Hub's result", async () => {
    const { fetchImpl } = fetching(
      json(200, { tool: "governed_write", ok: true }),
    );
    expect(
      await postTool(
        { mode: "ontology", tool: "governed_write", arguments: {} },
        { fetchImpl },
      ),
    ).toEqual({ tool: "governed_write", ok: true });
  });

  it("reports a refusal as a failed result with the Hub's reason", async () => {
    const { fetchImpl } = fetching(
      json(400, { code: "TOOL_INVALID", error: "unknown tool" }),
    );
    expect(
      await postTool(
        { mode: "explore", tool: "nope", arguments: {} },
        { fetchImpl },
      ),
    ).toMatchObject({
      tool: "nope",
      ok: false,
      code: "TOOL_INVALID",
      error: "unknown tool",
    });
  });
});

describe("streamTurn", () => {
  const body = { mode: "explore", message: "hello", threadId: "ath_1" };
  const events = [
    { type: "RUN_STARTED", threadId: "ath_1", runId: "arun_1" },
    {
      type: "RUN_FINISHED",
      threadId: "ath_1",
      runId: "arun_1",
      result: { text: "hi", results: [], rounds: 1 },
    },
  ];

  it("asks for an event stream and reduces each event", async () => {
    const { fetchImpl, calls } = fetching(sse(events));
    const exchange = new AssistantExchange();
    const seen: string[] = [];
    const answer = await streamTurn(
      body,
      exchange,
      (_exchange: unknown, event: { type: string }) => seen.push(event.type),
      { fetchImpl },
    );
    expect(answer).toBeNull();
    expect(calls()[0]![1].headers.accept).toBe("text/event-stream");
    expect(calls()[0]![1].body).toBe(JSON.stringify(body));
    expect(seen).toEqual(["RUN_STARTED", "RUN_FINISHED"]);
    expect(exchange.turn).toMatchObject({ status: "complete", text: "hi" });
  });

  it("passes the abort signal so Cancel closes the request", async () => {
    const { fetchImpl, calls } = fetching(sse(events));
    const controller = new AbortController();
    await streamTurn(body, new AssistantExchange(), undefined, {
      fetchImpl,
      signal: controller.signal,
    });
    expect(calls()[0]![1].signal).toBe(controller.signal);
  });

  it("throws a pre-stream JSON failure with its code and partial turn", async () => {
    const { fetchImpl } = fetching(
      json(422, {
        code: "MODEL_NOT_CONFIGURED",
        error: "model is not configured",
        label: "No model call",
      }),
    );
    await expect(
      streamTurn(body, new AssistantExchange(), undefined, { fetchImpl }),
    ).rejects.toMatchObject({ code: "MODEL_NOT_CONFIGURED", status: 422 });
  });

  it("carries the wait a 429 names in Retry-After", async () => {
    const { fetchImpl } = fetching(
      new Response(
        JSON.stringify({ code: "MODEL_BUSY", error: "model is busy" }),
        {
          status: 429,
          headers: { "content-type": "application/json", "retry-after": "7" },
        },
      ),
    );
    await expect(
      streamTurn(body, new AssistantExchange(), undefined, { fetchImpl }),
    ).rejects.toMatchObject({ code: "MODEL_BUSY", status: 429, retryAfter: 7 });
  });

  it("carries no wait when the Hub named none", async () => {
    const { fetchImpl } = fetching(
      json(502, { code: "MODEL_FAILED", error: "upstream 500" }),
    );
    await expect(
      streamTurn(body, new AssistantExchange(), undefined, { fetchImpl }),
    ).rejects.toMatchObject({ code: "MODEL_FAILED", retryAfter: null });
  });

  it("throws RUN_ERROR with its code after the stream", async () => {
    const { fetchImpl } = fetching(
      sse([
        events[0],
        { type: "RUN_ERROR", code: "MODEL_TIMEOUT", message: "timed out" },
      ]),
    );
    const exchange = new AssistantExchange();
    await expect(
      streamTurn(body, exchange, undefined, { fetchImpl }),
    ).rejects.toMatchObject({ code: "MODEL_TIMEOUT", message: "timed out" });
    expect(exchange.turn.status).toBe("error");
  });

  it("fails a stream that ends without a terminal event", async () => {
    const { fetchImpl } = fetching(sse(events.slice(0, 1)));
    await expect(
      streamTurn(body, new AssistantExchange(), undefined, { fetchImpl }),
    ).rejects.toThrow(/interrupted/);
  });

  it("returns the turn document when the Hub answers in JSON", async () => {
    const { fetchImpl } = fetching(json(200, { text: "hi", results: [] }));
    expect(
      await streamTurn(body, new AssistantExchange(), undefined, { fetchImpl }),
    ).toEqual({ text: "hi", results: [] });
  });

  it("names a 401 with no body as UNAUTHORIZED", async () => {
    const { fetchImpl } = fetching(new Response("", { status: 401 }));
    await expect(
      streamTurn(body, new AssistantExchange(), undefined, { fetchImpl }),
    ).rejects.toMatchObject({ code: "UNAUTHORIZED" });
  });
});

describe("retryAfterSeconds", () => {
  it("reads delta-seconds", () => {
    expect(retryAfterSeconds("30")).toBe(30);
    expect(retryAfterSeconds(" 1 ")).toBe(1);
  });

  it("reads an HTTP date against the clock it is given", () => {
    const now = Date.parse("2026-11-17T23:00:00Z");
    expect(retryAfterSeconds("Wed, 18 Nov 2026 00:00:00 GMT", now)).toBe(3600);
    expect(retryAfterSeconds("Tue, 17 Nov 2026 22:00:00 GMT", now)).toBe(0);
  });

  it("answers null for a missing or unreadable header", () => {
    expect(retryAfterSeconds(null)).toBeNull();
    expect(retryAfterSeconds("")).toBeNull();
    expect(retryAfterSeconds("soon")).toBeNull();
  });
});

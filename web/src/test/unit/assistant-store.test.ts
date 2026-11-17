import { describe, expect, it, vi } from "vite-plus/test";

import {
  AssistantStore,
  OPEN_THREAD_KEY,
  type AssistantApi,
} from "../../console/assistant-store";
import type { AssistantExchange } from "../../console/assistant-turn";

const failure = (code: string, message = code) =>
  Object.assign(new Error(message), { code });

const FINISH = (threadId: string, text = "answer") => [
  { type: "RUN_STARTED", threadId, runId: "arun_1" },
  {
    type: "RUN_FINISHED",
    threadId,
    runId: "arun_1",
    result: { label: "Live model call", text, results: [], rounds: 1 },
  },
];

function memory(initial: Record<string, string> = {}) {
  const values = new Map(Object.entries(initial));
  return {
    values,
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => void values.set(key, value),
    removeItem: (key: string) => void values.delete(key),
  };
}

type Script = (
  body: Record<string, unknown>,
  exchange: AssistantExchange,
  emit: (event: unknown) => void,
  signal: AbortSignal | undefined,
) => Promise<unknown>;

function harness(over: Partial<AssistantApi> = {}, turns: Script[] = []) {
  const bodies: Record<string, unknown>[] = [];
  const api: AssistantApi = {
    listThreads: vi.fn(async () => []),
    createThread: vi.fn(async () => ({ id: "ath_new", title: "" })),
    readThread: vi.fn(async (id: string) => ({ id, messages: [] })),
    deleteThread: vi.fn(async () => null),
    postTool: vi.fn(async () => ({ ok: true })),
    streamTurn: vi.fn(async (body, exchange, onEvent, options) => {
      bodies.push(body);
      const script =
        turns.shift() ??
        (async (_body, _exchange, emit) => {
          for (const event of FINISH(String(body.threadId ?? "arun_1")))
            emit(event);
          return null;
        });
      return script(
        body,
        exchange,
        (event) => {
          exchange.accept(event);
          onEvent(exchange);
        },
        options?.signal,
      );
    }),
    ...over,
  };
  const storage = memory();
  const store = new AssistantStore({
    api,
    storage,
    context: () => ({
      mode: "explore",
      chips: [{ id: "zeta", objectType: "Environment", label: "zeta" }],
    }),
    now: () => "2026-11-17T09:00:00.000Z",
  });
  return { api, bodies, storage, store };
}

const last = (store: AssistantStore) => {
  const message = store.getState().messages.at(-1);
  if (message?.role !== "assistant") throw new Error("no assistant message");
  return message.turn;
};

describe("AssistantStore threads", () => {
  it("creates a thread on the first send and passes its id", async () => {
    const { api, bodies, storage, store } = harness();
    await store.init();
    expect(store.getState().threads.status).toBe("ready");
    expect(api.createThread).not.toHaveBeenCalled();
    await store.send("which environments exist?");
    expect(api.createThread).toHaveBeenCalledTimes(1);
    expect(bodies).toEqual([
      {
        mode: "explore",
        message: "which environments exist?",
        context: [{ id: "zeta", objectType: "Environment", label: "zeta" }],
        threadId: "ath_new",
      },
    ]);
    expect(store.getState().threads.openId).toBe("ath_new");
    expect(storage.values.get(OPEN_THREAD_KEY)).toBe("ath_new");
    await store.send("and spokes?");
    expect(api.createThread).toHaveBeenCalledTimes(1);
    expect(bodies[1]!["threadId"]).toBe("ath_new");
  });

  it("restores the open thread from the Hub after a reload", async () => {
    const { api, store, storage } = harness({
      listThreads: vi.fn(async () => [{ id: "ath_1", title: "earlier" }]),
      readThread: vi.fn(async () => ({
        id: "ath_1",
        messages: [
          { role: "user", text: "earlier question", mode: "explore" },
          { role: "assistant", text: "earlier answer", mode: "explore" },
        ],
      })),
    });
    storage.setItem(OPEN_THREAD_KEY, "ath_1");
    await store.init();
    expect(api.readThread).toHaveBeenCalledWith("ath_1");
    expect(store.getState().threads.openId).toBe("ath_1");
    expect(store.getState().messages.map((message) => message.id)).toEqual([
      "ath_1:0",
      "ath_1:1",
    ]);
    expect(last(store).text).toBe("earlier answer");
  });

  it("forgets a remembered thread the Hub no longer lists", async () => {
    const { api, store, storage } = harness();
    storage.setItem(OPEN_THREAD_KEY, "ath_gone");
    await store.init();
    expect(api.readThread).not.toHaveBeenCalled();
    expect(storage.values.has(OPEN_THREAD_KEY)).toBe(false);
  });

  it("falls back to a stateless conversation when the Hub has no store", async () => {
    const { api, bodies, store } = harness({
      listThreads: vi.fn(async () => {
        throw failure("THREADS_NOT_CONFIGURED", "no conversation store");
      }),
    });
    await store.init();
    expect(store.getState().threads.status).toBe("stateless");
    expect(store.getState().threads.notice).toContain("not saved");
    await store.send("hello");
    expect(api.createThread).not.toHaveBeenCalled();
    expect(bodies[0]).not.toHaveProperty("threadId");
    expect(last(store).status).toBe("complete");
  });

  it("re-sends without a thread when the turn itself reports no store", async () => {
    const { bodies, store } = harness({}, [
      async () => {
        throw failure("THREADS_NOT_CONFIGURED");
      },
    ]);
    await store.init();
    await store.send("hello");
    expect(bodies.map((body) => body["threadId"])).toEqual([
      "ath_new",
      undefined,
    ]);
    expect(store.getState().threads.status).toBe("stateless");
    expect(last(store).status).toBe("complete");
    expect(store.getState().messages).toHaveLength(2);
  });

  it("deletes the open thread and empties the conversation", async () => {
    const { api, store, storage } = harness({
      listThreads: vi.fn(async () => [{ id: "ath_1", title: "earlier" }]),
      readThread: vi.fn(async () => ({
        id: "ath_1",
        messages: [{ role: "user", text: "q", mode: "explore" }],
      })),
    });
    storage.setItem(OPEN_THREAD_KEY, "ath_1");
    await store.init();
    await store.deleteThread("ath_1");
    expect(api.deleteThread).toHaveBeenCalledWith("ath_1");
    expect(store.getState().threads.threads).toEqual([]);
    expect(store.getState().threads.openId).toBeNull();
    expect(store.getState().messages).toEqual([]);
    expect(storage.values.has(OPEN_THREAD_KEY)).toBe(false);
  });

  it("keeps the thread and says why when the Hub refuses the delete", async () => {
    const { store } = harness({
      listThreads: vi.fn(async () => [{ id: "ath_1", title: "earlier" }]),
      deleteThread: vi.fn(async () => {
        throw failure("TURN_FAILED", "the Hub returned 500");
      }),
    });
    await store.init();
    await store.deleteThread("ath_1");
    expect(store.getState().threads.threads).toHaveLength(1);
    expect(store.getState().threadError).toContain("the Hub returned 500");
  });

  it("starts a new conversation without creating a thread", async () => {
    const { api, store } = harness();
    await store.init();
    await store.send("first");
    store.newThread();
    expect(store.getState().messages).toEqual([]);
    expect(store.getState().threads.openId).toBeNull();
    expect(api.createThread).toHaveBeenCalledTimes(1);
    await store.send("second");
    expect(api.createThread).toHaveBeenCalledTimes(2);
  });
});

describe("AssistantStore turns", () => {
  it("updates the running message as each event arrives", async () => {
    const seen: number[] = [];
    let store!: AssistantStore;
    const made = harness({}, [
      async (_body, _exchange, emit) => {
        emit({ type: "RUN_STARTED", threadId: "ath_new", runId: "arun_1" });
        emit({
          type: "TOOL_CALL_START",
          toolCallId: "call_1_1",
          toolCallName: "query_ontology_objects",
        });
        seen.push(last(store).tools.length);
        expect(store.getState().running).toBe(true);
        expect(last(store).status).toBe("running");
        for (const event of FINISH("ath_new").slice(1)) emit(event);
        return null;
      },
    ]);
    store = made.store;
    await store.init();
    await store.send("go");
    expect(seen).toEqual([1]);
    expect(store.getState().running).toBe(false);
    expect(last(store).status).toBe("complete");
  });

  it("accepts a JSON turn when the Hub did not stream", async () => {
    const { store } = harness({}, [
      async () => ({
        label: "Fixture response",
        text: "Recorded conversation response.",
        results: [],
      }),
    ]);
    await store.init();
    await store.send("go");
    expect(last(store)).toMatchObject({
      status: "complete",
      text: "Recorded conversation response.",
      label: "Fixture response",
    });
  });

  it("cancels by aborting the request and records it as cancelled", async () => {
    let aborted = false;
    const { store } = harness({}, [
      (_body, _exchange, emit, signal) =>
        new Promise((_resolve, reject) => {
          emit({ type: "RUN_STARTED", threadId: "ath_new", runId: "arun_1" });
          signal?.addEventListener("abort", () => {
            aborted = true;
            reject(new Error("network error after abort"));
          });
        }),
    ]);
    await store.init();
    const sending = store.send("long question");
    await vi.waitFor(() => expect(last(store).started).toBe(true));
    store.cancel();
    await sending;
    expect(aborted).toBe(true);
    expect(store.getState().running).toBe(false);
    expect(last(store).status).toBe("cancelled");
  });

  it("replaces a failed attempt the Hub never started when retried", async () => {
    const { bodies, store } = harness({}, [
      async () => {
        throw failure("MODEL_NOT_CONFIGURED", "no model");
      },
    ]);
    await store.init();
    await store.send("which environments exist?");
    expect(last(store)).toMatchObject({
      status: "error",
      error: { code: "MODEL_NOT_CONFIGURED" },
    });
    await store.retry();
    expect(bodies.map((body) => body["message"])).toEqual([
      "which environments exist?",
      "which environments exist?",
    ]);
    expect(store.getState().messages).toHaveLength(2);
    expect(last(store).status).toBe("complete");
  });

  it("keeps a failed attempt the Hub recorded and appends the retry", async () => {
    const { store } = harness({}, [
      async (_body, _exchange, emit) => {
        emit({ type: "RUN_STARTED", threadId: "ath_new", runId: "arun_1" });
        emit({ type: "RUN_ERROR", code: "ROUND_CAP", message: "cap" });
        throw failure("ROUND_CAP", "cap");
      },
    ]);
    await store.init();
    await store.send("loop");
    expect(last(store).error?.code).toBe("ROUND_CAP");
    await store.retry();
    expect(store.getState().messages.map((message) => message.role)).toEqual([
      "user",
      "assistant",
      "user",
      "assistant",
    ]);
    expect(last(store).status).toBe("complete");
  });

  it("shows a thread creation failure as a retryable error", async () => {
    const createThread = vi
      .fn()
      .mockRejectedValueOnce(failure("THREAD_LIMIT", "too many threads"))
      .mockResolvedValue({ id: "ath_new", title: "" });
    const { bodies, store } = harness({ createThread });
    await store.init();
    await store.send("hello");
    expect(bodies).toEqual([]);
    expect(last(store).error?.code).toBe("THREAD_LIMIT");
    await store.retry();
    expect(bodies).toHaveLength(1);
    expect(store.getState().messages).toHaveLength(2);
  });

  it("ignores a second send while a turn is running", async () => {
    let release!: () => void;
    const { api, store } = harness({}, [
      (_body, _exchange, emit) =>
        new Promise((resolve) => {
          release = () => {
            for (const event of FINISH("ath_new")) emit(event);
            resolve(null);
          };
        }),
    ]);
    await store.init();
    const first = store.send("one");
    await vi.waitFor(() => expect(api.streamTurn).toHaveBeenCalledTimes(1));
    await store.send("two");
    release();
    await first;
    expect(api.streamTurn).toHaveBeenCalledTimes(1);
    expect(store.getState().messages).toHaveLength(2);
  });
});

describe("AssistantStore.submitTool", () => {
  const elicitation = { action: "approve_change", targetId: "cr-7" };
  const turnWithConfirm: Script = async (_body, _exchange, emit) => {
    emit({ type: "RUN_STARTED", threadId: "ath_new", runId: "arun_1" });
    emit({
      type: "RUN_FINISHED",
      threadId: "ath_new",
      runId: "arun_1",
      result: {
        text: "Waiting for your confirmation.",
        rounds: 2,
        toolCalls: [
          {
            id: "call_2_1",
            round: 2,
            name: "governed_write",
            arguments: elicitation,
          },
        ],
        results: [
          { tool: "governed_write", ok: true, needsConfirm: true, elicitation },
        ],
      },
    });
    return null;
  };

  it("posts the same call with confirm and shows the Hub's answer", async () => {
    const postTool = vi.fn(async () => ({
      tool: "governed_write",
      ok: true,
      applied: true,
      reason: "review recorded",
    }));
    const { store } = harness({ postTool }, [turnWithConfirm]);
    await store.init();
    await store.send("approve cr-7", "ontology");
    const answer = store.getState().messages.at(-1)!;
    await store.submitTool(answer.id, "call_2_1", {}, true);
    expect(postTool).toHaveBeenCalledWith({
      mode: "ontology",
      tool: "governed_write",
      arguments: { ...elicitation, confirm: true },
    });
    expect(last(store).tools[0]).toMatchObject({
      pending: false,
      followUps: [{ applied: true, reason: "review recorded" }],
    });
  });

  it("shows a refused confirm as refused", async () => {
    const postTool = vi.fn(async () => ({
      tool: "governed_write",
      ok: false,
      error: "a person cannot approve their own change request",
    }));
    const { store } = harness({ postTool }, [turnWithConfirm]);
    await store.init();
    await store.send("approve cr-7", "ontology");
    await store.submitTool(
      store.getState().messages.at(-1)!.id,
      "call_2_1",
      {},
      true,
    );
    expect(last(store).tools[0]!.followUps).toEqual([
      {
        tool: "governed_write",
        ok: false,
        error: "a person cannot approve their own change request",
      },
    ]);
  });
});

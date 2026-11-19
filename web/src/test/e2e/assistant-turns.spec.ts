import { readFileSync } from "node:fs";

import { expect, test as base, type Page } from "@playwright/test";

import { APPLICATIONS, session, stubHub } from "./hub-stub";

/*
 * The assistant against the Hub's turn and thread contract
 * (docs/architecture/agent-runs.md, "Assistant turns and threads").
 *
 * route.fulfill hands the browser a whole body at once, so it cannot show a
 * tool row arriving before its result or a request being cancelled mid-stream.
 * A turn is therefore answered inside the page (installTurnTransport) by a
 * streamed Response the test writes one event at a time, beneath the console's
 * own fetch wrapper. Threads and tools are answered in the page route from an
 * in-memory store. The stream over a real connection is the live spec's job.
 */

type Json = Record<string, unknown>;

type Turn = {
  body: Json;
  accept: string;
  /** Write one AG-UI event. */
  send(event: Json): void;
  /** End the stream. */
  end(): void;
  /** Answer with a plain JSON document instead of a stream. */
  json(status: number, body: Json, headers?: Record<string, string>): void;
  /** Resolves when the browser closed the connection before the end. */
  aborted: Promise<void>;
};

type StoredThread = {
  id: string;
  title: string;
  createdAt: string;
  updatedAt: string;
  messageCount: number;
  messages: Json[];
};

type FakeHub = {
  threads: Map<string, StoredThread>;
  /** False answers every thread route 503 THREADS_NOT_CONFIGURED. */
  threadsConfigured: boolean;
  session: Json;
  writes: { method: string; path: string; body: unknown; csrf: string }[];
  tool: (body: Json) => { status?: number; json: Json };
  nextTurn(): Promise<Turn>;
  /** What the Hub appends to a thread when a turn ends. */
  remember(threadId: string, user: string, answer: Json): void;
};

const QUERY = { objectType: "Environment" };
const QUERY_RESULT = {
  label: "Live model call",
  tool: "query_ontology_objects",
  ok: true,
  readOps: 1,
  writeOps: 0,
  durationMs: 3,
  matchedCount: 1,
  entities: [{ id: "zeta", objectType: "Environment", label: "zeta" }],
};

function turnDocument(over: Json = {}): Json {
  return {
    label: "Live model call",
    text: "One environment: zeta.",
    provenance: { Adapter: "openai-http" },
    results: [QUERY_RESULT],
    toolCalls: [
      {
        id: "call_1_1",
        round: 1,
        name: "query_ontology_objects",
        arguments: QUERY,
      },
    ],
    rounds: 2,
    model: { kind: "openai-http", model: "gpt-test" },
    ...over,
  };
}

/** The events the Hub writes up to and including the tool call's arguments. */
function openToolCall(turn: Turn, threadId: string): void {
  turn.send({ type: "RUN_STARTED", threadId, runId: "arun_1" });
  turn.send({ type: "STEP_STARTED", stepName: "round-1" });
  turn.send({
    type: "TOOL_CALL_START",
    toolCallId: "call_1_1",
    toolCallName: "query_ontology_objects",
  });
  turn.send({
    type: "TOOL_CALL_ARGS",
    toolCallId: "call_1_1",
    delta: JSON.stringify(QUERY),
  });
  turn.send({ type: "TOOL_CALL_END", toolCallId: "call_1_1" });
}

/** The rest of the same turn: the result, the answer and the terminal event. */
function finishToolCall(turn: Turn, threadId: string, document: Json): void {
  turn.send({
    type: "TOOL_CALL_RESULT",
    toolCallId: "call_1_1",
    messageId: "call_1_1:result",
    content: JSON.stringify(QUERY_RESULT),
  });
  turn.send({ type: "STEP_FINISHED", stepName: "round-1" });
  turn.send({ type: "STEP_STARTED", stepName: "round-2" });
  turn.send({ type: "STEP_FINISHED", stepName: "round-2" });
  const messageId = "arun_1:answer";
  turn.send({ type: "TEXT_MESSAGE_START", messageId, role: "assistant" });
  turn.send({
    type: "TEXT_MESSAGE_CONTENT",
    messageId,
    delta: typeof document["text"] === "string" ? document["text"] : "",
  });
  turn.send({ type: "TEXT_MESSAGE_END", messageId });
  turn.send({
    type: "RUN_FINISHED",
    threadId,
    runId: "arun_1",
    result: document,
  });
  turn.end();
}

/**
 * Installed before any page script. A turn request is answered inside the page
 * by a Response whose body is a stream the test writes to, one event at a
 * time, and whose abort signal the test observes. It sits beneath the
 * console's own fetch wrapper, so it sees the headers the Hub would see.
 */
function installTurnTransport(): void {
  type Control = {
    send(text: string): void;
    end(): void;
    json(status: number, body: string, headers: Record<string, string>): void;
  };
  const host = window as unknown as {
    __turnArrived(
      id: number,
      body: string,
      accept: string,
      csrf: string,
    ): Promise<void>;
    __turnAborted(id: number): Promise<void>;
    __turns: Map<number, Control>;
  };
  const inner = window.fetch.bind(window);
  const encoder = new TextEncoder();
  let count = 0;
  host.__turns = new Map();
  window.fetch = (input, init) => {
    const address =
      typeof input === "string"
        ? input
        : input instanceof URL
          ? input.href
          : input.url;
    const url = new URL(address, window.location.href);
    if (url.pathname !== "/v1/assistant/turns") return inner(input, init);
    const id = ++count;
    const headers = new Headers(init?.headers);
    return new Promise<Response>((resolve, reject) => {
      let controller: ReadableStreamDefaultController<Uint8Array> | null = null;
      let done = false;
      let streaming = false;
      const body = new ReadableStream<Uint8Array>({
        start(value) {
          controller = value;
        },
      });
      init?.signal?.addEventListener("abort", () => {
        if (done) return;
        done = true;
        const aborted = new DOMException(
          "The request was aborted",
          "AbortError",
        );
        reject(aborted);
        controller?.error(aborted);
        void host.__turnAborted(id);
      });
      host.__turns.set(id, {
        send(text) {
          if (done) return;
          if (!streaming) {
            streaming = true;
            resolve(
              new Response(body, {
                status: 200,
                headers: { "content-type": "text/event-stream" },
              }),
            );
          }
          controller?.enqueue(encoder.encode(text));
        },
        end() {
          if (done) return;
          done = true;
          controller?.close();
        },
        json(status, text, extra) {
          if (done) return;
          done = true;
          resolve(
            new Response(text, {
              status,
              headers: { "content-type": "application/json", ...extra },
            }),
          );
        },
      });
      void host.__turnArrived(
        id,
        typeof init?.body === "string" ? init.body : "{}",
        headers.get("accept") ?? "",
        headers.get("x-conduit-csrf") ?? "",
      );
    });
  };
}

const test = base.extend<{ hub: FakeHub }>({
  hub: async ({ page }, use) => {
    const waiting: ((turn: Turn) => void)[] = [];
    const arrived: Turn[] = [];
    const aborts = new Map<string, () => void>();
    // A reload starts the page's turn ids again; the load number keeps them apart.
    let load = 0;
    page.on("load", () => {
      load += 1;
    });
    await page.exposeFunction("__turnAborted", (id: number) => {
      aborts.get(`${load}:${id}`)?.();
    });
    await page.exposeFunction(
      "__turnArrived",
      (id: number, body: string, accept: string, csrf: string) => {
        // Calls into the page are queued so events arrive in the order sent.
        let queue: Promise<unknown> = Promise.resolve();
        const control = (method: string, ...args: unknown[]) => {
          queue = queue
            .then(() =>
              page.evaluate(
                ([turnId, name, values]) => {
                  const turns = (
                    window as unknown as {
                      __turns: Map<
                        number,
                        Record<string, (...items: unknown[]) => void>
                      >;
                    }
                  ).__turns;
                  turns
                    .get(turnId as number)
                    ?.[name as string]?.(...(values as unknown[]));
                },
                [id, method, args] as const,
              ),
            )
            .catch(() => {
              /* The page went away; there is nothing left to write to. */
            });
        };
        const parsed = JSON.parse(body) as Json;
        hub.writes.push({
          method: "POST",
          path: "/v1/assistant/turns",
          body: parsed,
          csrf,
        });
        const turn: Turn = {
          body: parsed,
          accept,
          send: (event) =>
            control("send", `data: ${JSON.stringify(event)}\n\n`),
          end: () => control("end"),
          json: (status, document, headers = {}) =>
            control("json", status, JSON.stringify(document), headers),
          aborted: new Promise((resolve) => {
            aborts.set(`${load}:${id}`, resolve);
          }),
        };
        const next = waiting.shift();
        if (next) next(turn);
        else arrived.push(turn);
      },
    );
    await page.addInitScript(installTurnTransport);

    let created = 0;
    const hub: FakeHub = {
      threads: new Map(),
      threadsConfigured: true,
      session: session(),
      writes: [],
      tool: () => ({ json: { ok: true } }),
      nextTurn: () =>
        new Promise((resolve) => {
          const turn = arrived.shift();
          if (turn) resolve(turn);
          else waiting.push(resolve);
        }),
      remember(threadId, user, answer) {
        const thread = hub.threads.get(threadId);
        if (!thread) return;
        const at = "2026-11-17T09:00:07Z";
        thread.messages.push(
          { role: "user", text: user, mode: "explore", createdAt: at },
          { role: "assistant", mode: "explore", createdAt: at, ...answer },
        );
        thread.messageCount = thread.messages.length;
        thread.title ||= user;
        thread.updatedAt = at;
      },
    };

    await stubHub(page, (_request, url) =>
      url.pathname === "/v1/session" ? { json: hub.session } : undefined,
    );
    // Registered after stubHub, so it answers the assistant routes first.
    await page.route("**/v1/assistant/**", async (route) => {
      const request = route.request();
      const method = request.method();
      const path = new URL(request.url()).pathname;
      if (method !== "GET")
        hub.writes.push({
          method,
          path,
          body: request.postDataJSON(),
          csrf: request.headers()["x-conduit-csrf"] ?? "",
        });
      if (path === "/v1/assistant/tools") {
        const answer = hub.tool(request.postDataJSON() as Json);
        await route.fulfill({
          status: answer.status ?? 200,
          json: answer.json,
        });
        return;
      }
      if (!hub.threadsConfigured) {
        await route.fulfill({
          status: 503,
          json: {
            code: "THREADS_NOT_CONFIGURED",
            error: "no conversation store is configured",
          },
        });
        return;
      }
      const summary = ({ messages: _messages, ...rest }: StoredThread) => rest;
      if (path === "/v1/assistant/threads" && method === "GET") {
        await route.fulfill({
          json: { threads: [...hub.threads.values()].reverse().map(summary) },
        });
        return;
      }
      if (path === "/v1/assistant/threads" && method === "POST") {
        created += 1;
        const thread: StoredThread = {
          id: `ath_${created}`,
          title: "",
          createdAt: "2026-11-17T09:00:00Z",
          updatedAt: "2026-11-17T09:00:00Z",
          messageCount: 0,
          messages: [],
        };
        hub.threads.set(thread.id, thread);
        await route.fulfill({ status: 201, json: thread });
        return;
      }
      const id = decodeURIComponent(path.split("/").pop() ?? "");
      const thread = hub.threads.get(id);
      if (!thread) {
        await route.fulfill({
          status: 404,
          json: { code: "THREAD_NOT_FOUND", error: "thread not found" },
        });
        return;
      }
      if (method === "DELETE") {
        hub.threads.delete(id);
        await route.fulfill({ status: 204 });
        return;
      }
      await route.fulfill({ json: thread });
    });

    await use(hub);
    // Every assistant write left through the console's wrapped fetch, which is
    // what adds the header the Hub requires on a session-cookie write.
    expect(hub.writes.filter((write) => write.csrf !== "1")).toEqual([]);
  },
});

const rail = (page: Page) => page.locator("#right-chat-rail");
const answers = (page: Page) =>
  rail(page).locator('[data-slot="aui_assistant-message-content"]');

async function openAssistant(page: Page): Promise<void> {
  await page.goto("/console/");
  await page.getByTitle("Toggle right chat panel").click();
  await expect(page.getByLabel("Message input")).toBeVisible();
}

async function send(page: Page, text: string): Promise<void> {
  await page.getByLabel("Message input").fill(text);
  await page.getByRole("button", { name: "Send message", exact: true }).click();
}

test("a turn streams its tool call, its result and the answer", async ({
  page,
  hub,
}) => {
  await openAssistant(page);
  await expect(
    rail(page).locator('[data-assistant-threads="ready"]'),
  ).toBeVisible();
  await send(page, "which environments exist?");

  const turn = await hub.nextTurn();
  // The thread was created by the first send, and the turn names it.
  expect(turn.accept).toContain("text/event-stream");
  expect(turn.body).toMatchObject({
    mode: "explore",
    message: "which environments exist?",
    threadId: "ath_1",
  });
  expect(hub.writes.map((write) => `${write.method} ${write.path}`)).toEqual([
    "POST /v1/assistant/threads",
    "POST /v1/assistant/turns",
  ]);

  // The tool call is on screen with its real arguments before any result.
  openToolCall(turn, "ath_1");
  const card = rail(page).locator(
    '[data-assistant-tool="query_ontology_objects"]',
  );
  await expect(card).toHaveAttribute("data-tool-state", "running");
  await expect(card).toHaveAttribute("data-tool-call-id", "call_1_1");
  await expect(card.locator("[data-tool-args]")).toContainText("objectType");
  await expect(card.locator("[data-tool-args]")).toContainText("Environment");
  await expect(card.locator("[data-tool-result]")).toHaveCount(0);
  await expect(answers(page)).not.toContainText("One environment");
  await expect(
    page.getByRole("button", { name: "Stop generating", exact: true }),
  ).toBeVisible();

  finishToolCall(
    turn,
    "ath_1",
    turnDocument({
      threadId: "ath_1",
      model: {
        kind: "model-cli",
        configuredModel: "cli-model",
      },
      usage: { promptTokens: 13, completionTokens: 0 },
    }),
  );
  await expect(card).toHaveAttribute("data-tool-state", "ok");
  await expect(card.locator("[data-tool-outcome]")).toHaveText("1 matched");
  await expect(card.locator("[data-tool-result]")).toContainText("zeta");
  await expect(answers(page)).toContainText("One environment: zeta.");
  await expect(rail(page).locator("[data-tool-call-id]")).toHaveCount(1);

  // Explicit configured-only provenance cannot establish the provider id.
  const meta = rail(page).locator("[data-assistant-turn-meta]");
  await expect(meta.locator("[data-assistant-usage]")).toHaveText(
    "Reported tokens: 13 input · 0 output",
  );
  await meta.screenshot({
    path: test.info().outputPath("reported-turn-usage.png"),
  });
  await expect(meta.locator("[data-assistant-model]")).toContainText(
    "Model: cli-model (operator configured; provider id unavailable)",
  );
  await expect(meta.locator("[data-assistant-model-caution]")).toHaveText(
    "Model name not verified",
  );

  // Controls with nothing behind them are not rendered.
  await expect(
    page.getByRole("button", { name: "Add Attachment" }),
  ).toHaveCount(0);
  await expect(rail(page).locator(".aui-user-action-edit")).toHaveCount(0);
  await expect(
    rail(page).getByRole("button", { name: /speak|read aloud/i }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Send message", exact: true }),
  ).toBeVisible();
});

test("a completed answer settles when reveal animation frames stop", async ({
  page,
  hub,
}) => {
  await openAssistant(page);
  await expect(
    rail(page).locator('[data-assistant-threads="ready"]'),
  ).toBeVisible();
  await send(page, "which environments exist?");
  const turn = await hub.nextTurn();
  turn.send({ type: "RUN_STARTED", threadId: "ath_1", runId: "arun_1" });
  turn.send({
    type: "TEXT_MESSAGE_START",
    messageId: "answer",
    role: "assistant",
  });
  turn.send({
    type: "TEXT_MESSAGE_CONTENT",
    messageId: "answer",
    delta: "One",
  });
  await expect(answers(page).locator(".aui-md")).toHaveText("One");

  // Model a throttled background window after a prefix is already visible.
  // Stream delivery and React tasks still run; only reveal frames are withheld.
  await page.evaluate(() => {
    window.requestAnimationFrame = () => 0;
  });
  turn.send({
    type: "TEXT_MESSAGE_CONTENT",
    messageId: "answer",
    delta: " environment: zeta.",
  });
  turn.send({ type: "TEXT_MESSAGE_END", messageId: "answer" });
  turn.send({
    type: "RUN_FINISHED",
    threadId: "ath_1",
    runId: "arun_1",
    result: turnDocument({ threadId: "ath_1", results: [], toolCalls: [] }),
  });
  turn.end();
  await expect(answers(page).locator(".aui-md")).toHaveText(
    "One environment: zeta.",
  );
  await expect(
    page.getByRole("button", { name: "Stop generating", exact: true }),
  ).toHaveCount(0);
});

test("a reported model is shown without a caution", async ({ page, hub }) => {
  await openAssistant(page);
  await send(page, "which environments exist?");
  const turn = await hub.nextTurn();
  turn.json(
    200,
    turnDocument({
      threadId: "ath_1",
      usage: { promptTokens: null, completionTokens: 0 },
    }),
  );
  const meta = rail(page).locator("[data-assistant-turn-meta]");
  await expect(meta.locator('[data-assistant-model="reported"]')).toHaveText(
    "Model: gpt-test",
  );
  await expect(meta.locator("[data-assistant-model-caution]")).toHaveCount(0);
  await expect(meta.locator("[data-assistant-usage]")).toHaveText(
    "Reported tokens: unknown input · 0 output",
  );
});

test("Stop closes the request mid-stream and the turn ends cancelled", async ({
  page,
  hub,
}) => {
  await openAssistant(page);
  await send(page, "a long question");
  const turn = await hub.nextTurn();
  openToolCall(turn, "ath_1");
  const card = rail(page).locator("[data-assistant-tool]");
  await expect(card).toHaveAttribute("data-tool-state", "running");

  await page
    .getByRole("button", { name: "Stop generating", exact: true })
    .click();
  // The server saw the connection close before it ended the stream: this is
  // what cancels the model call on the Hub.
  await turn.aborted;
  await expect(
    page.getByRole("button", { name: "Send message", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Stop generating", exact: true }),
  ).toHaveCount(0);
  // The tool call that was already shown stays; no answer was invented.
  await expect(card).toHaveCount(1);
  await expect(card).toHaveAttribute("data-tool-state", "failed");
  await expect(card.locator("[data-tool-outcome]")).toHaveText(
    "No result received",
  );
  await expect(rail(page).locator(".aui-message-error-root")).toHaveCount(0);

  // The composer works again.
  await send(page, "a second question");
  const second = await hub.nextTurn();
  expect(second.body["message"]).toBe("a second question");
  openToolCall(second, "ath_1");
  finishToolCall(second, "ath_1", turnDocument({ threadId: "ath_1" }));
  await expect(answers(page).last()).toContainText("One environment: zeta.");
});

test("a refused turn is an error with a Retry that sends again", async ({
  page,
  hub,
}) => {
  await openAssistant(page);
  await send(page, "which environments exist?");
  const refused = await hub.nextTurn();
  refused.json(422, {
    code: "MODEL_NOT_CONFIGURED",
    error: "assistant model is not configured",
    label: "No model call",
  });

  const error = rail(page).locator(".aui-message-error-root");
  await expect(error).toBeVisible();
  await expect(error).toContainText("No model is configured on this Hub");
  await expect(error).toContainText("docs/operations/model-http.md");
  await expect(error).not.toContainText("local model CLI is not configured");
  // The failure is the thread's error state, not assistant prose.
  await expect(rail(page).locator(".aui-md")).toHaveCount(0);
  await expect(rail(page).locator("[data-assistant-error-code]")).toHaveText(
    "MODEL_NOT_CONFIGURED",
  );

  await error.getByRole("button", { name: "Retry" }).click();
  const retried = await hub.nextTurn();
  expect(retried.body).toMatchObject({
    message: "which environments exist?",
    threadId: "ath_1",
  });
  openToolCall(retried, "ath_1");
  finishToolCall(retried, "ath_1", turnDocument({ threadId: "ath_1" }));
  await expect(answers(page)).toContainText("One environment: zeta.");
  await expect(rail(page).locator(".aui-message-error-root")).toHaveCount(0);
  // The attempt the Hub never started is replaced, not kept beside the retry.
  await expect(rail(page).locator('[data-role="user"]')).toHaveCount(1);
  expect(hub.writes.map((write) => write.path)).toEqual([
    "/v1/assistant/threads",
    "/v1/assistant/turns",
    "/v1/assistant/turns",
  ]);
});

test("a 429 says how long the Hub asks to wait, and Retry sends again", async ({
  page,
  hub,
}) => {
  await openAssistant(page);
  await send(page, "which environments exist?");
  const busy = await hub.nextTurn();
  busy.json(
    429,
    { code: "MODEL_BUSY", error: "model concurrency limit reached" },
    { "retry-after": "7" },
  );
  const error = rail(page).locator(".aui-message-error-root");
  await expect(error).toContainText("as many calls as this Hub allows");
  await expect(error).toContainText("retry in 7 seconds");
  await expect(rail(page).locator("[data-assistant-error-code]")).toHaveText(
    "MODEL_BUSY",
  );

  await error.getByRole("button", { name: "Retry" }).click();
  const spent = await hub.nextTurn();
  spent.json(
    429,
    { code: "MODEL_BUDGET_EXCEEDED", error: "daily model budget is spent" },
    { "retry-after": "7500" },
  );
  await expect(error).toContainText("model budget for today is spent");
  await expect(error).toContainText("retry in 2 h 5 min");
});

test("a run that ends in RUN_ERROR keeps its tool call and offers Retry", async ({
  page,
  hub,
}) => {
  await openAssistant(page);
  await send(page, "loop forever");
  const turn = await hub.nextTurn();
  openToolCall(turn, "ath_1");
  turn.send({
    type: "TOOL_CALL_RESULT",
    toolCallId: "call_1_1",
    messageId: "call_1_1:result",
    content: JSON.stringify(QUERY_RESULT),
  });
  turn.send({ type: "STEP_FINISHED", stepName: "round-1" });
  turn.send({
    type: "CUSTOM",
    name: "conduit.assistant.turn-result",
    value: { usage: { promptTokens: 8, completionTokens: null }, rounds: 1 },
  });
  turn.send({
    type: "RUN_ERROR",
    code: "ROUND_CAP",
    message: "the model was still calling tools in its last round",
  });
  turn.end();

  const error = rail(page).locator(".aui-message-error-root");
  await expect(error).toContainText("last allowed round");
  await expect(rail(page).locator("[data-assistant-usage]")).toHaveText(
    "Reported tokens: 8 input · unknown output",
  );
  await expect(rail(page).locator("[data-assistant-error-code]")).toHaveText(
    "ROUND_CAP",
  );
  await expect(
    rail(page).locator("[data-assistant-tool] [data-tool-outcome]"),
  ).toHaveText("1 matched");

  // The Hub recorded this attempt in the thread, so it stays on screen.
  await error.getByRole("button", { name: "Retry" }).click();
  const retried = await hub.nextTurn();
  expect(retried.body["message"]).toBe("loop forever");
  retried.send({ type: "RUN_STARTED", threadId: "ath_1", runId: "arun_2" });
  retried.send({
    type: "RUN_FINISHED",
    threadId: "ath_1",
    runId: "arun_2",
    result: turnDocument({ text: "Done.", results: [], toolCalls: [] }),
  });
  retried.end();
  await expect(answers(page).last()).toContainText("Done.");
  await expect(rail(page).locator('[data-role="user"]')).toHaveCount(2);
  await expect(rail(page).locator("[data-assistant-error-code]")).toHaveCount(
    1,
  );
});

test("the open thread is restored from the Hub after a reload", async ({
  page,
  hub,
}) => {
  await openAssistant(page);
  await send(page, "which environments exist?");
  const turn = await hub.nextTurn();
  openToolCall(turn, "ath_1");
  const document = turnDocument({
    threadId: "ath_1",
    usage: { promptTokens: 12, completionTokens: 3 },
  });
  hub.remember("ath_1", "which environments exist?", {
    text: document["text"],
    usage: document["usage"],
    toolCalls: [
      {
        id: "call_1_1",
        round: 1,
        name: "query_ontology_objects",
        arguments: QUERY,
        result: QUERY_RESULT,
      },
    ],
  });
  finishToolCall(turn, "ath_1", document);
  await expect(answers(page)).toContainText("One environment: zeta.");
  await expect(rail(page).locator("[data-assistant-thread-title]")).toHaveText(
    "which environments exist?",
  );

  const reads: string[] = [];
  page.on("request", (request) => {
    const path = new URL(request.url()).pathname;
    if (request.method() === "GET" && path.startsWith("/v1/assistant/"))
      reads.push(path);
  });
  await page.reload();
  await page.getByTitle("Toggle right chat panel").click();

  // Everything on screen now came from GET /v1/assistant/threads/ath_1.
  await expect(rail(page).locator('[data-role="user"]')).toContainText(
    "which environments exist?",
  );
  await expect(answers(page)).toContainText("One environment: zeta.");
  const card = rail(page).locator(
    '[data-assistant-tool="query_ontology_objects"]',
  );
  await expect(card.locator("[data-tool-args]")).toContainText("Environment");
  await expect(card.locator("[data-tool-outcome]")).toHaveText("1 matched");
  expect(reads).toContain("/v1/assistant/threads/ath_1");
  await expect(rail(page).locator("[data-assistant-usage]")).toHaveText(
    "Reported tokens: 12 input · 3 output",
  );
  const writesBefore = hub.writes.length;

  // The next message continues the same thread; no second thread is created.
  await send(page, "and how many spokes?");
  const next = await hub.nextTurn();
  expect(next.body["threadId"]).toBe("ath_1");
  expect(hub.writes.slice(writesBefore).map((write) => write.path)).toEqual([
    "/v1/assistant/turns",
  ]);
  next.json(
    200,
    turnDocument({ text: "Two spokes.", results: [], toolCalls: [] }),
  );
  await expect(answers(page).last()).toContainText("Two spokes.");
});

test("threads are listed, opened, started and deleted after confirmation", async ({
  page,
  hub,
}) => {
  for (const [id, title, text] of [
    ["ath_a", "first conversation", "first answer"],
    ["ath_b", "second conversation", "second answer"],
  ] as const) {
    hub.threads.set(id, {
      id,
      title,
      createdAt: "2026-11-17T08:00:00Z",
      updatedAt: "2026-11-17T08:00:00Z",
      messageCount: 0,
      messages: [],
    });
    hub.remember(id, title, { text });
  }
  await openAssistant(page);
  await rail(page).getByRole("button", { name: "Conversations (2)" }).click();
  const list = rail(page).getByRole("list", { name: "Saved conversations" });
  await expect(list.locator("[data-assistant-thread]")).toHaveCount(2);

  // Open one.
  await list
    .getByRole("button", { name: "first conversation", exact: true })
    .click();
  await expect(answers(page)).toContainText("first answer");
  await expect(rail(page).locator("[data-assistant-thread-title]")).toHaveText(
    "first conversation",
  );

  // New conversation empties the view and creates nothing on the Hub.
  await rail(page).getByRole("button", { name: "New conversation" }).click();
  await expect(answers(page)).toHaveCount(0);
  expect(hub.writes).toEqual([]);

  // Delete asks first; Keep changes nothing.
  await list.getByRole("button", { name: "Delete first conversation" }).click();
  await expect(list).toContainText("from the Hub? This cannot be undone.");
  await list.getByRole("button", { name: "Keep" }).click();
  expect(hub.writes).toEqual([]);
  await expect(list.locator("[data-assistant-thread]")).toHaveCount(2);

  // Deleting the open thread removes it on the Hub and empties the view.
  await list
    .getByRole("button", { name: "second conversation", exact: true })
    .click();
  await expect(answers(page)).toContainText("second answer");
  await list
    .getByRole("button", { name: "Delete second conversation" })
    .click();
  await list.getByRole("button", { name: "Delete conversation" }).click();
  await expect(list.locator("[data-assistant-thread]")).toHaveCount(1);
  await expect(answers(page)).toHaveCount(0);
  expect(hub.writes).toEqual([
    {
      method: "DELETE",
      path: "/v1/assistant/threads/ath_b",
      body: null,
      csrf: "1",
    },
  ]);
  expect([...hub.threads.keys()]).toEqual(["ath_a"]);

  // It stays deleted across a reload, and nothing is reopened.
  await page.reload();
  await page.getByTitle("Toggle right chat panel").click();
  await expect(
    rail(page).getByRole("button", { name: "Conversations (1)" }),
  ).toBeVisible();
  await expect(answers(page)).toHaveCount(0);
});

test("a Hub without a conversation store gets one unsaved conversation", async ({
  page,
  hub,
}) => {
  hub.threadsConfigured = false;
  await openAssistant(page);
  const notice = rail(page).locator('[data-assistant-threads="stateless"]');
  await expect(notice).toContainText("no conversation store");
  await expect(notice).toContainText("not saved");
  await expect(
    rail(page).getByRole("button", { name: "New conversation" }),
  ).toHaveCount(0);

  await send(page, "which environments exist?");
  const turn = await hub.nextTurn();
  expect(turn.body).not.toHaveProperty("threadId");
  openToolCall(turn, "arun_1");
  finishToolCall(turn, "arun_1", turnDocument());
  await expect(answers(page)).toContainText("One environment: zeta.");
  // No thread was created or asked for by the send.
  expect(hub.writes.map((write) => write.path)).toEqual([
    "/v1/assistant/turns",
  ]);
});

test("a held write shows Confirm, which re-sends the same call", async ({
  page,
  hub,
}) => {
  const elicitation = {
    action: "approve_change",
    targetId: "cr-7",
    justification: "reviewed the diff",
  };
  const refusal = "no action rule governs this action, so it is refused";
  hub.tool = () => ({
    json: {
      label: "No model call",
      tool: "governed_write",
      ok: true,
      readOps: 0,
      writeOps: 1,
      applied: true,
      allowed: true,
      objectId: "cr-7",
      state: "APPROVED",
      reason: "review recorded",
    },
  });
  await openAssistant(page);
  await rail(page).locator('[data-set-mode="ontology"]').click();
  await send(page, "approve cr-7 and remember it");
  const turn = await hub.nextTurn();
  expect(turn.body["mode"]).toBe("ontology");
  turn.send({ type: "RUN_STARTED", threadId: "ath_1", runId: "arun_1" });
  turn.send({
    type: "RUN_FINISHED",
    threadId: "ath_1",
    runId: "arun_1",
    result: turnDocument({
      text: "The approval is waiting for you.",
      threadId: "ath_1",
      toolCalls: [
        // Round 2: the Hub discarded the model's own confirm flag.
        {
          id: "call_2_1",
          round: 2,
          name: "governed_write",
          arguments: elicitation,
        },
        {
          id: "call_2_2",
          round: 2,
          name: "governed_write",
          arguments: { action: "propose_memory", targetId: "run-1" },
        },
      ],
      results: [
        {
          label: "Live model call",
          tool: "governed_write",
          ok: true,
          readOps: 0,
          writeOps: 0,
          risk: "low",
          needsConfirm: true,
          reason: "confirm to apply this action",
          elicitation,
        },
        {
          label: "Live model call",
          tool: "governed_write",
          ok: true,
          readOps: 0,
          writeOps: 0,
          reason: refusal,
          constraints: [
            { id: "propose_memory", status: "DENY", reason: refusal },
          ],
        },
      ],
    }),
  });
  turn.end();

  const held = rail(page).locator('[data-tool-call-id="call_2_1"]');
  await expect(held).toHaveAttribute("data-tool-state", "confirm");
  await expect(held.locator("[data-tool-args]")).toContainText("cr-7");
  await expect(held).toContainText("round 2");

  // The refused action shows the Hub's reason and offers nothing to press.
  const refused = rail(page).locator('[data-tool-call-id="call_2_2"]');
  await expect(refused).toHaveAttribute("data-tool-state", "refused");
  await expect(refused.locator("[data-tool-refusal]")).toContainText(refusal);
  await expect(refused.getByRole("button")).toHaveCount(0);

  // Nothing was written by the turn's own confirm; the person confirms.
  expect(hub.writes.map((write) => write.path)).not.toContain(
    "/v1/assistant/tools",
  );
  await held.getByRole("button", { name: "Confirm", exact: true }).click();
  await expect(held.locator("[data-tool-follow-up]")).toContainText("APPROVED");
  await expect(held.locator("[data-tool-outcome]")).toHaveText(
    "review recorded",
  );
  await expect(held.getByRole("button", { name: "Confirm" })).toHaveCount(0);
  expect(hub.writes.at(-1)).toEqual({
    method: "POST",
    path: "/v1/assistant/tools",
    csrf: "1",
    body: {
      mode: "ontology",
      tool: "governed_write",
      arguments: { ...elicitation, confirm: true },
    },
  });
});

test("a Confirm the Hub refuses is shown as refused, not as applied", async ({
  page,
  hub,
}) => {
  const elicitation = { action: "approve_change", targetId: "cr-7" };
  hub.tool = () => ({
    json: {
      tool: "governed_write",
      ok: true,
      readOps: 0,
      writeOps: 0,
      reason: "a person cannot approve their own change request",
      constraints: [
        {
          id: "approve_change",
          status: "DENY",
          reason: "a person cannot approve their own change request",
        },
      ],
    },
  });
  await openAssistant(page);
  await send(page, "approve cr-7");
  const turn = await hub.nextTurn();
  turn.json(
    200,
    turnDocument({
      text: "Waiting.",
      toolCalls: [
        {
          id: "call_1_1",
          round: 1,
          name: "governed_write",
          arguments: elicitation,
        },
      ],
      results: [
        { tool: "governed_write", ok: true, needsConfirm: true, elicitation },
      ],
    }),
  );
  const card = rail(page).locator('[data-tool-call-id="call_1_1"]');
  // The jump control must reserve footer space instead of floating over
  // message actions in a narrow assistant viewport.
  const jump = rail(page).locator(".aui-thread-scroll-to-bottom");
  await expect
    .poll(() =>
      jump.evaluate((button) => {
        const footer = button.closest(".aui-thread-viewport-footer");
        if (!footer) return false;
        const outer = footer.getBoundingClientRect();
        const inner = button.getBoundingClientRect();
        return (
          inner.top >= outer.top &&
          inner.bottom <= outer.bottom &&
          inner.left >= outer.left &&
          inner.right <= outer.right
        );
      }),
    )
    .toBe(true);
  await card.getByRole("button", { name: "Confirm", exact: true }).click();
  await expect(card).toHaveAttribute("data-tool-state", "refused");
  await expect(card.locator("[data-tool-refusal]")).toContainText(
    "a person cannot approve their own change request",
  );
  await expect(card).not.toContainText("Applied");
});

test("the rail no longer offers the memory proposal the Hub refuses", async ({
  page,
  hub,
}) => {
  void hub;
  await page.route(
    "**/conduit.packaging.v1.PackagingRunService/ListPackagingRuns",
    (route) =>
      route.fulfill({
        json: {
          configured: true,
          items: [{ runId: "run-1", phase: "blocked" }],
          total: "1",
          pageSize: 1,
          limit: 20,
        },
      }),
  );
  await page.goto("/console/#s08-staged-run");
  await expect(
    page.getByRole("heading", { name: "run-1", exact: true }),
  ).toBeVisible();
  await page.getByTitle("Toggle right chat panel").click();
  await expect(
    rail(page).locator('[data-assistant-threads="ready"]'),
  ).toBeVisible();
  await expect(
    rail(page).getByTitle("Remove PackagingRun run-1"),
  ).toBeVisible();
  await rail(page).locator('[data-set-mode="ontology"]').click();
  // A populated, governed action proves the mode and context are ready;
  // an empty flex container has no visibility guarantee across browsers.
  await expect(
    rail(page).getByRole("button", { name: "Add eval case", exact: true }),
  ).toBeVisible();
  await expect(
    rail(page).getByRole("button", { name: "Propose memory" }),
  ).toHaveCount(0);
  await expect(
    rail(page).locator('[data-assistant-action="write-memory"]'),
  ).toHaveCount(0);
});

test("the command palette lists only what the session enables", async ({
  page,
  hub,
}) => {
  hub.session = session({
    applications: APPLICATIONS.map((app) => ({
      ...app,
      enabled: ["change-requests", "plans", "teams"].includes(app.id),
    })),
    globalOperations: [],
  });
  await page.goto("/console/");
  // Absence of Fleet is also true before the session arrives. Wait for a
  // granted application before opening the palette against its capabilities.
  await expect(
    page
      .locator("#left-nav")
      .getByRole("button", { name: "Change requests", exact: true }),
  ).toBeVisible();
  await expect(
    page
      .locator("#left-nav")
      .getByRole("button", { name: "Fleet", exact: true }),
  ).toHaveCount(0);
  await page.keyboard.press("Control+k");
  const palette = page.getByRole("dialog", { name: "Command palette" });
  const row = (title: string) =>
    palette.locator(".kit-palette-row").filter({
      has: page.locator(".kit-palette-title", {
        hasText: new RegExp(`^${title}$`),
      }),
    });
  await palette.getByLabel("Search").fill("Fleet");
  await expect(palette.getByRole("listbox")).toBeVisible();
  // The Fleet area is not enabled, so neither it nor its screens are offered.
  await expect(row("Fleet")).toHaveCount(0);
  await palette.getByLabel("Search").fill("Change requests");
  await expect(row("Change requests")).not.toHaveCount(0);
  const search = await palette.getByLabel("Search").elementHandle();
  await page.evaluate(() => Reflect.get(window, "refreshHub")());
  expect(await search?.evaluate((element) => element.isConnected)).toBe(true);
  await expect(palette.getByLabel("Search")).toHaveValue("Change requests");
  await expect(row("Change requests")).not.toHaveCount(0);

  // The same Hub answer with every application on lists Fleet again.
  hub.session = session();
  await page.reload();
  await expect(
    page
      .locator("#left-nav")
      .getByRole("button", { name: "Fleet", exact: true }),
  ).toBeVisible();
  await page.keyboard.press("Control+k");
  await palette.getByLabel("Search").fill("Fleet");
  await expect(row("Fleet")).not.toHaveCount(0);
});

// Synthetic Go-generated wire data exercises the browser contract, not a live model.
const groundingFixture = JSON.parse(
  readFileSync(
    new URL(
      "../../../../internal/assistant/domain/testdata/grounding-v1.json",
      import.meta.url,
    ),
    "utf8",
  ),
) as Json;

for (const transport of ["json", "stream"] as const) {
  test(`retrieved sources survive ${transport} and thread reload`, async ({
    page,
    hub,
  }) => {
    await openAssistant(page);
    await send(page, "show retrieved references");
    const turn = await hub.nextTurn();
    const document = turnDocument({
      threadId: "ath_1",
      text: "Retrieved fixture references.",
      results: [],
      toolCalls: [],
      grounding: groundingFixture,
    });
    hub.remember("ath_1", "show retrieved references", document);
    if (transport === "json") turn.json(200, document);
    else {
      turn.send({
        type: "RUN_STARTED",
        threadId: "ath_1",
        runId: "arun_sources",
      });
      turn.send({
        type: "RUN_FINISHED",
        threadId: "ath_1",
        runId: "arun_sources",
        result: document,
      });
      turn.end();
    }
    const sources = rail(page).locator('[data-assistant-sources="available"]');
    await expect(sources).toBeVisible();
    const summary = sources.locator(":scope > summary");
    await summary.focus();
    await page.keyboard.press("Enter");
    await expect(sources.locator("blockquote").first()).toContainText(
      "<script>untrusted source text</script>",
    );
    await expect(sources.locator("script, img, a")).toHaveCount(0);
    await sources.getByText("Source evidence", { exact: true }).first().click();
    await expect(sources).toContainText("aud_fixture_doc");
    await expect(sources).toContainText("0–77 (end exclusive)");
    if (transport === "stream")
      await page.screenshot({
        path: test.info().outputPath("retrieved-sources.png"),
      });
    const writesBefore = hub.writes.length;
    await page.reload();
    await page.getByTitle("Toggle right chat panel").click();
    await expect(sources).toBeVisible();
    await sources.locator(":scope > summary").click();
    await expect(sources).toContainText("Environment:visible-fixture");
    expect(hub.writes.length).toBe(writesBefore);
  });
}

test("failed streams retain retrieved sources without reporting success", async ({
  page,
  hub,
}) => {
  await openAssistant(page);
  await send(page, "retrieve then fail");
  const turn = await hub.nextTurn();
  turn.send({
    type: "RUN_STARTED",
    threadId: "ath_1",
    runId: "arun_failed_sources",
  });
  turn.send({
    type: "CUSTOM",
    name: "conduit.assistant.turn-result",
    value: {
      grounding: { ...groundingFixture, truncated: true },
      rounds: 1,
    },
  });
  turn.send({
    type: "RUN_ERROR",
    code: "MODEL_UNAVAILABLE",
    message: "Model unavailable after retrieval",
  });
  turn.end();
  await expect(
    rail(page).getByRole("button", { name: "Retry", exact: true }),
  ).toBeVisible();
  const sources = rail(page).locator('[data-assistant-sources="available"]');
  await sources.locator(":scope > summary").click();
  await expect(sources).toContainText(
    "Additional source excerpts are not shown.",
  );
  await expect(sources.locator("[data-source-citation]")).toHaveCount(2);
});

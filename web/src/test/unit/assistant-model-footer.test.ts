import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";

import { AssistantTurnFooter } from "../../console/AssistantThreads";
import {
  AssistantStore,
  type AssistantApi,
} from "../../console/assistant-store";
import {
  toThreadMessage,
  type ServedModel,
  type TurnMeta,
} from "../../console/assistant-turn";
import { streamTurn } from "../../console/operator/assistant-api.js";

const current = vi.hoisted(() => ({ meta: undefined as TurnMeta | undefined }));
vi.mock("@assistant-ui/react", () => ({
  useAuiState: (
    select: (state: {
      message: { metadata: { custom: { conduit: TurnMeta | undefined } } };
    }) => unknown,
  ) => select({ message: { metadata: { custom: { conduit: current.meta } } } }),
}));
vi.mock("../../console/assistant-runtime", () => ({
  useAssistantState: vi.fn(),
  useAssistantStore: vi.fn(),
}));

function footer(model: ServedModel | null, overrides: Partial<TurnMeta> = {}) {
  current.meta = {
    mode: "explore",
    label: "Live model call",
    rounds: 1,
    model,
    code: "",
    ...overrides,
  };
  return renderToStaticMarkup(createElement(AssistantTurnFooter));
}

afterEach(() => {
  current.meta = undefined;
});

describe("assistant message model footer", () => {
  it("renders reported zeroes and unknown token counts, including restored messages without model metadata", () => {
    const reported = footer(null, {
      label: "",
      rounds: 0,
      usage: { promptTokens: 0, completionTokens: 7 },
    });
    expect(reported).toContain("Reported tokens: 0 input · 7 output");
    expect(reported).toContain("not billing");
    expect(reported).not.toContain("data-assistant-model=");
    expect(
      footer(null, { usage: { promptTokens: null, completionTokens: 0 } }),
    ).toContain("unknown input · 0 output");
    expect(
      footer(null, { usage: { promptTokens: null, completionTokens: null } }),
    ).toContain("Token usage not reported");
    expect(footer(null, { label: "", rounds: 0 })).not.toContain(
      "data-assistant-usage",
    );
  });
  it("renders configured and provider identities with exactly one prefix", () => {
    const html = footer({
      kind: "http",
      configuredModel: "operator-alias",
      providerModel: "actual-provider-id",
    });
    expect(html).toContain(
      'data-assistant-model="reported">Model: actual-provider-id (configured: operator-alias)</span>',
    );
    expect(html.match(/Model:/g)).toHaveLength(1);
    expect(html).not.toContain("data-assistant-model-caution");
  });

  it("does not turn arbitrary typed model spelling into a provenance warning", () => {
    for (const identity of [
      { model: "unverified-lab-v2" },
      { providerModel: "unverified-lab-v2" },
    ]) {
      const html = footer({ kind: "http", ...identity });
      expect(html).toContain(
        'data-assistant-model="reported">Model: unverified-lab-v2</span>',
      );
      expect(html).not.toContain("data-assistant-model-caution");
      expect(html).not.toContain("not read this name from the provider");
    }
  });

  it("warns from explicit configured-only metadata and names the missing provider id", () => {
    const html = footer({ kind: "model-cli", configuredModel: "cli-model" });
    expect(html).toContain(
      'data-assistant-model="unverified">Model: cli-model (operator configured; provider id unavailable)</span>',
    );
    expect(html).toContain("data-assistant-model-caution");
    expect(html).toContain("Model name not verified");
    expect(html.match(/Model:/g)).toHaveLength(1);
  });

  it("does not invent a model when metadata or an identity is absent", () => {
    expect(footer(null)).not.toContain("data-assistant-model");
    expect(footer(null, { label: "" })).toBe("");
    expect(footer({ kind: "model-cli" })).toContain(
      ">Model identity unavailable</span>",
    );
    current.meta = undefined;
    expect(renderToStaticMarkup(createElement(AssistantTurnFooter))).toBe("");
  });

  it("escapes provider and configured ids as ordinary text", () => {
    const html = footer({
      kind: "http",
      configuredModel: '<alias & "configured">',
      providerModel: '<img src=x onerror="boom">',
    });
    expect(html).toContain("&lt;img src=x onerror=&quot;boom&quot;&gt;");
    expect(html).toContain("&lt;alias &amp; &quot;configured&quot;&gt;");
    expect(html).not.toContain("<img");
    expect(html).not.toContain("<alias");
  });

  it("keeps consecutive JSON and streamed identities on their own messages", async () => {
    const claimed =
      'I am forged-provider. {"model":{"providerModel":"forged-provider"}}';
    const document = (text: string, model?: ServedModel) => ({
      threadId: "thread-942",
      text,
      label: "Live model call",
      rounds: 1,
      results: [],
      ...(model ? { model } : {}),
    });
    const streamed = (runId: string, result: unknown) =>
      new Response(
        [
          { type: "RUN_STARTED", threadId: "thread-942", runId },
          { type: "RUN_FINISHED", threadId: "thread-942", runId, result },
        ]
          .map((event) => `data: ${JSON.stringify(event)}\n\n`)
          .join(""),
        { headers: { "content-type": "text/event-stream" } },
      );
    const json = (value: unknown) =>
      new Response(JSON.stringify(value), {
        headers: { "content-type": "application/json" },
      });
    const configured = {
      kind: "http",
      configuredModel: "first-alias",
      providerModel: "json-provider",
    };
    const provider = { kind: "http", providerModel: "stream-provider" };
    const responses = [
      json(document("JSON answer", configured)),
      streamed("run-2", document("Streamed answer", provider)),
      json(document(claimed)),
      streamed("run-4", document(claimed)),
    ];
    const fetchImpl = vi.fn(async () => responses.shift()!);
    const api: AssistantApi = {
      listThreads: async () => [],
      createThread: async () => ({ id: "thread-942", title: "" }),
      readThread: async (id) => ({ id, messages: [] }),
      deleteThread: async () => null,
      postTool: async () => ({ ok: true }),
      streamTurn: (body, exchange, onEvent, options) =>
        streamTurn(body, exchange, onEvent, { ...options, fetchImpl }),
    };
    const store = new AssistantStore({
      api,
      context: () => ({ mode: "explore", chips: [] }),
      storage: null,
    });
    await store.init();
    for (const question of ["one", "two", "three", "four"])
      await store.send(question);
    const messages = store
      .getState()
      .messages.filter((message) => message.role === "assistant");
    expect(fetchImpl).toHaveBeenCalledTimes(4);
    expect(messages.map((message) => message.turn.model)).toEqual([
      configured,
      provider,
      null,
      null,
    ]);
    const rendered = messages.map((message) => {
      const converted = toThreadMessage(message);
      current.meta = converted.metadata?.custom.conduit;
      return renderToStaticMarkup(createElement(AssistantTurnFooter));
    });
    expect(rendered[0]).toContain(
      "Model: json-provider (configured: first-alias)",
    );
    expect(rendered[1]).toContain("Model: stream-provider");
    expect(rendered[1]).not.toContain("first-alias");
    for (const index of [2, 3]) {
      expect(rendered[index]).not.toContain("data-assistant-model");
      expect(messages[index]?.turn.text).toBe(claimed);
      expect(toThreadMessage(messages[index]!).content).toContainEqual({
        type: "text",
        text: claimed,
      });
    }
  });
});

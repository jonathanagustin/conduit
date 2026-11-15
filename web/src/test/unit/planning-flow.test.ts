import { PlanningAgent } from "../../console/operator/planning-agent.js";
import { describe, expect, it, vi } from "vite-plus/test";

import {
  PlanningError,
  acceptSession,
  acceptState,
  applyEvent,
  bindPlanning,
  bindingRows,
  createSession,
  describeError,
  emptyPlanning,
  openQuestions,
  parseSSE,
  planSections,
  planningMarkup,
  startTurn,
  submitAnswers,
  toolRows,
} from "../../console/operator/planning-flow.js";

const plan = {
  text: "# Build\nUse the Dockerfile.\n\n## Risks\nNone <b>known</b>.",
  sha256: "abc123",
};
const question = (over = {}) => ({
  id: "q1",
  name: "port",
  label: "Which port?",
  type: "string",
  required: true,
  ...over,
});
const session = (over = {}) => ({
  id: "s1",
  status: "ready",
  profile: { name: "onboard", version: "v3", hash: "ph" },
  intake: { repoUrl: "https://h/o/r", ref: "main" },
  needsInput: [],
  answers: [],
  plan,
  binding: null,
  events: [],
  ...over,
});

function json(status: number, body: unknown) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

describe("planning flow state", () => {
  it("starts empty with no session and no sample content", () => {
    const html = planningMarkup(emptyPlanning());
    expect(html).toContain("data-plan-composer");
    expect(html).not.toContain("data-plan-review");
    expect(html).not.toContain("data-plan-binding");
    expect(html.toLowerCase()).not.toContain("example data");
    expect(html).not.toContain("Rollout Immediately");
  });

  it("splits plan text into sections and escapes them", () => {
    expect(planSections(plan.text).map((s) => s.title)).toEqual([
      "Build",
      "Risks",
    ]);
    const html = planningMarkup({ ...emptyPlanning(), session: session() });
    expect(html).toContain("None &lt;b&gt;known&lt;/b&gt;.");
    expect(html).not.toContain("<b>known");
  });

  it("keeps Accept disabled until a plan exists and required questions are answered", () => {
    expect(acceptState(null).ok).toBe(false);
    expect(acceptState(session({ plan: null })).ok).toBe(false);
    const asking = session({ needsInput: [question()] });
    expect(acceptState(asking).ok).toBe(false);
    expect(planningMarkup({ ...emptyPlanning(), session: asking })).toMatch(
      /data-plan-accept disabled/,
    );
    const answered = session({
      needsInput: [question()],
      answers: [{ id: "q1", value: "8080" }],
    });
    expect(openQuestions(answered)).toEqual([]);
    expect(acceptState(answered).ok).toBe(true);
    expect(
      planningMarkup({ ...emptyPlanning(), session: answered }),
    ).not.toMatch(/data-plan-accept disabled/);
    expect(
      acceptState(session({ needsInput: [question({ required: false })] })).ok,
    ).toBe(true);
  });

  it("renders choices as a select and free text as an input", () => {
    const html = planningMarkup({
      ...emptyPlanning(),
      session: session({
        needsInput: [
          question({ id: "a", type: "select", options: ["x", "y"] }),
          question({ id: "b" }),
        ],
      }),
    });
    expect(html).toContain('<select class="kit-select" id="plan-q-a"');
    expect(html).toContain('<option value="y">y</option>');
    expect(html).toContain(
      '<input type="text" class="kit-input" id="plan-q-b"',
    );
  });

  it("shows the binding after Accept and hides the Accept button", () => {
    const binding = {
      profile: { name: "onboard", version: "v3", hash: "ph" },
      intake: { repoUrl: "https://h/o/r", ref: "main" },
      resolvedCommit: "c".repeat(40),
      model: "m-1",
      plan,
      acceptedAt: "2026-01-01T00:00:00Z",
    };
    const rows = Object.fromEntries(bindingRows(binding));
    expect(rows["Profile version"]).toBe("v3");
    expect(rows["Profile hash"]).toBe("ph");
    expect(rows["Commit SHA"]).toBe("c".repeat(40));
    expect(rows["Plan hash"]).toBe("abc123");
    const html = planningMarkup({
      ...emptyPlanning(),
      session: session({ status: "accepted", binding }),
    });
    expect(html).toContain("data-plan-binding");
    expect(html).not.toContain("data-plan-accept-bar");
  });

  it("shows inspection tool calls as compact rows", () => {
    const events = [
      {
        id: "e1",
        type: "inspection",
        inspection: { toolCalls: ["inspect_repo", "read_file README.md"] },
      },
      { id: "e2", type: "message", message: { role: "model", text: "hi" } },
    ];
    expect(toolRows(events).map((r) => r.call)).toEqual([
      "inspect_repo",
      "read_file README.md",
    ]);
    const html = planningMarkup({ ...emptyPlanning(), events });
    expect(html.match(/data-plan-tool/g)).toHaveLength(2);
  });

  it("maps each 409 code to its own wording and shows it", () => {
    for (const code of [
      "PLAN_REQUIRED",
      "COMMIT_NOT_PINNED",
      "NEEDS_INPUT_PENDING",
      "ALREADY_ACCEPTED",
    ]) {
      const html = planningMarkup({
        ...emptyPlanning(),
        error: new PlanningError(409, code, "x"),
      });
      expect(html).toContain(code);
      expect(html).toContain("data-plan-error");
    }
    expect(
      describeError(new PlanningError(409, "PLAN_REQUIRED", "x")),
    ).not.toBe(
      describeError(new PlanningError(409, "NEEDS_INPUT_PENDING", "x")),
    );
  });

  it("records a streamed error event", () => {
    const next = applyEvent(emptyPlanning(), {
      id: "e",
      type: "error",
      error: "model down",
      code: "MODEL_NOT_CONFIGURED",
    });
    expect(next.error?.code).toBe("MODEL_NOT_CONFIGURED");
  });
});

describe("planning flow API", () => {
  it("parses server-sent frames and keeps a partial frame", () => {
    const first = parseSSE(
      'id: 1\nevent: status\ndata: {"id":"1","type":"status"}\n\nid: 2\nevent: do',
    );
    expect(first.events).toEqual([{ id: "1", type: "status" }]);
    const second = parseSSE(
      `${first.rest}ne\ndata: {"id":"2","type":"done"}\n\n`,
    );
    expect(second.events).toEqual([{ id: "2", type: "done" }]);
  });

  it("creates a session with repoUrl and ref", async () => {
    const fetchImpl = vi.fn().mockResolvedValue(json(201, { id: "s1" }));
    await createSession("https://h/o/r", "main", fetchImpl);
    const [path, init] = fetchImpl.mock.calls[0];
    expect(path).toBe("/v1/planning-sessions");
    expect(JSON.parse(init.body)).toEqual({
      repoUrl: "https://h/o/r",
      ref: "main",
    });
  });

  it("streams turn events to the callback", async () => {
    const body = new ReadableStream({
      start(controller) {
        const enc = new TextEncoder();
        controller.enqueue(
          enc.encode(
            'event: inspection\ndata: {"id":"1","type":"inspection"}\n\nev',
          ),
        );
        controller.enqueue(
          enc.encode('ent: done\ndata: {"id":"2","type":"done"}\n\n'),
        );
        controller.close();
      },
    });
    const fetchImpl = vi.fn().mockResolvedValue(
      new Response(body, {
        headers: { "content-type": "text/event-stream" },
      }),
    );
    const seen: string[] = [];
    await startTurn(
      "s1",
      "go",
      (e: { type: string }) => seen.push(e.type),
      fetchImpl,
    );
    expect(seen).toEqual(["inspection", "done"]);
    expect(fetchImpl.mock.calls[0][1].headers.accept).toBe("text/event-stream");
  });

  it("posts answers and surfaces the 409 code from accept", async () => {
    const ok = vi.fn().mockResolvedValue(json(200, session()));
    await submitAnswers("s1", [{ id: "q1", value: "8080" }], ok);
    expect(JSON.parse(ok.mock.calls[0][1].body)).toEqual({
      answers: [{ id: "q1", value: "8080" }],
    });
    const refused = vi
      .fn()
      .mockResolvedValue(
        json(409, { error: "pending", code: "NEEDS_INPUT_PENDING" }),
      );
    await expect(acceptSession("s1", refused)).rejects.toMatchObject({
      status: 409,
      code: "NEEDS_INPUT_PENDING",
    });
  });

  it("submits human answers through the current AG-UI interrupt", async () => {
    class FakeElement {
      closest(selector: string) {
        return selector === "[data-plan-submit-answers]" ? this : null;
      }
    }
    vi.stubGlobal("Element", FakeElement);
    try {
      const asking = session({
        status: "needs_input",
        plan: null,
        needsInput: [question()],
      });
      const fetchImpl = vi.fn<typeof fetch>(async (_input, options) => {
        const input = JSON.parse(String(options?.body));
        const events = [
          { type: "RUN_STARTED", threadId: input.threadId, runId: input.runId },
          {
            type: "STATE_SNAPSHOT",
            snapshot: { session: session(), pendingClientTools: [] },
          },
          { type: "MESSAGES_SNAPSHOT", messages: [] },
          {
            type: "RUN_FINISHED",
            threadId: input.threadId,
            runId: input.runId,
            outcome: { type: "success" },
          },
        ];
        return new Response(
          events.map((event) => `data: ${JSON.stringify(event)}\n\n`).join(""),
          { headers: { "content-type": "text/event-stream" } },
        );
      });
      const agent = new PlanningAgent("s1", fetchImpl);
      agent.interrupt = { id: "current-form" };
      let click: (event: unknown) => Promise<void> = async () => {};
      const root = {
        addEventListener: (type: string, handler: typeof click) => {
          if (type === "click") click = handler;
        },
      };
      const store = {
        planning: {
          ...emptyPlanning(),
          session: asking,
          agent,
          drafts: { q1: "8080" },
        },
      };
      bindPlanning(root, store, () => {}, fetchImpl);
      await click({ target: new FakeElement() });

      expect(fetchImpl).toHaveBeenCalledTimes(1);
      expect(fetchImpl.mock.calls[0][0]).toBe("/v1/agent-runs");
      expect(
        JSON.parse(String(fetchImpl.mock.calls[0][1]?.body)).resume,
      ).toEqual([
        {
          interruptId: "current-form",
          status: "resolved",
          payload: { "question:q1": "8080" },
        },
      ]);
      expect(store.planning.session?.status).toBe("ready");
      expect(store.planning.busy).toBe("");
    } finally {
      vi.unstubAllGlobals();
    }
  });
});

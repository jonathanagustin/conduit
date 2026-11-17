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
  displayModelName,
  emptyPlanning,
  formatAcceptedTimestamp,
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

  it("keeps earlier planning thread messages when a plan arrives (#695)", () => {
    const html = planningMarkup({
      ...emptyPlanning(),
      session: session({
        messages: [
          { id: "m1", role: "user", text: "Onboard service" },
          { id: "m2", role: "model", text: "Which port should I expose?" },
        ],
      }),
      agentMessages: [
        { id: "m3", role: "user", content: "8080" },
        { id: "m4", role: "model", content: "Plan drafted for port 8080." },
      ],
    });
    expect(html).toContain("Onboard service");
    expect(html).toContain("Which port should I expose?");
    expect(html).toContain("8080");
    expect(html).toContain("Plan drafted for port 8080.");
  });

  it("shows the reported model or Configured model in binding rows, never a command (#696)", () => {
    expect(displayModelName("")).toBe("Configured model");
    expect(displayModelName("/usr/local/bin/claude-planner --json")).toBe(
      "Configured model",
    );
    expect(displayModelName("python3 scripts/planner.py")).toBe(
      "Configured model",
    );
    expect(displayModelName("claude-sonnet-4-6")).toBe("claude-sonnet-4-6");
    const rows = Object.fromEntries(
      bindingRows({
        profile: { name: "onboard", version: "v3", hash: "ph" },
        intake: { repoUrl: "https://h/o/r", ref: "main" },
        resolvedCommit: "c".repeat(40),
        model: "/bin/sh -c planner",
        plan,
        acceptedAt: "2026-04-19T19:01:02Z",
      }),
    );
    expect(rows["Model"]).toBe("Configured model");
    expect(rows["Accepted"]).toContain("ET");
    expect(rows["Accepted"]).toContain("2026-04-19T19:01:02Z");
  });

  it("formats Accepted timestamp in America/New_York ET (#697)", () => {
    expect(formatAcceptedTimestamp("2026-04-19T19:01:02Z")).toBe(
      "2026-04-19 15:01:02 ET (2026-04-19T19:01:02Z)",
    );
  });

  it("renders canonical JSON plan sections, View JSON toggle, two-column workspace, and sticky Accept metadata (#698)", () => {
    const jsonPlan = {
      text: JSON.stringify(
        {
          summary: "Containerize service",
          baseImage: "cgr.dev/chainguard/static:latest",
          buildSteps: ["go build ./cmd/app"],
          ports: [8080],
          networkAllow: ["api.internal:443"],
          sso: true,
          risks: ["Requires outbound TLS"],
        },
        null,
        2,
      ),
      sha256: "deadbeef1234567890",
    };
    const sections = planSections(jsonPlan.text);
    expect(sections.map((s) => s.title)).toEqual([
      "Summary",
      "Base image",
      "Build steps",
      "Ports",
      "Network allow",
      "SSO",
      "Risks",
    ]);
    const state = {
      ...emptyPlanning(),
      session: session({
        resolvedCommit: "1234567890abcdef1234567890abcdef12345678",
        plan: jsonPlan,
      }),
    };
    const htmlClosed = planningMarkup(state);
    expect(htmlClosed).toContain('data-plan-col="conversation"');
    expect(htmlClosed).toContain('data-plan-col="plan"');
    expect(htmlClosed).toContain("data-plan-view-json");
    expect(htmlClosed).not.toContain("data-plan-json");
    expect(htmlClosed).toContain("data-plan-accept-meta");
    expect(htmlClosed).toContain("https://h/o/r@main");
    expect(htmlClosed).toContain("commit 1234567890ab");
    expect(htmlClosed).toContain("profile onboard@v3");
    expect(htmlClosed).toContain("plan deadbeef1234");

    const htmlOpen = planningMarkup({ ...state, showPlanJson: true });
    expect(htmlOpen).toContain("data-plan-json");
    expect(htmlOpen).toContain("Hide JSON");
  });

  it("formats problem-statement commit badge without mid-word truncation and deduplicates operator/user prompt rows", () => {
    const html = planningMarkup({
      ...emptyPlanning(),
      session: session({
        profile: { name: "appgen", version: "1.0.0", hash: "ph" },
        intake: {
          repoUrl: "",
          ref: "",
          problemStatement: "Build room companion",
          appName: "excalidraw-companion",
        },
        resolvedCommit:
          "none (no source repository — problem-statement intake)",
        messages: [
          {
            id: "",
            role: "operator",
            text: "Generate non-root Go service and Zarf/UDS package",
          },
        ],
      }),
      agentMessages: [
        {
          id: "u1",
          role: "user",
          content: "Generate non-root Go service and Zarf/UDS package",
        },
        {
          id: "a1",
          role: "model",
          content:
            'Prepared generation plan. {"model":"conduit-packaging-agent-v1"}',
        },
      ],
    });
    expect(html).toContain(">commit none (appgen)<");
    expect(html).not.toContain(">commit none (no sou<");
    expect(html.match(/data-plan-message/g)).toHaveLength(2);
    expect(html).toContain("conduit-packaging-agent-v1");
    expect(html).not.toContain("{&quot;model&quot;:");
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

  it("renders Package into Git Workspace after Accept and posts to /compute", async () => {
    class FakeElement {
      closest(selector: string) {
        return selector === "[data-plan-compute]" ? this : null;
      }
    }
    vi.stubGlobal("Element", FakeElement);
    try {
      const binding = {
        profile: { name: "onboard", version: "v3", hash: "ph" },
        intake: {
          repoUrl: "https://github.com/excalidraw/excalidraw",
          ref: "main",
        },
        resolvedCommit: "c".repeat(40),
        model: "conduit-packaging-agent-v1",
        plan,
        acceptedAt: "2026-01-01T00:00:00Z",
      };
      const accepted = session({ status: "accepted", binding });
      const html = planningMarkup({
        ...emptyPlanning(),
        session: accepted,
      });
      expect(html).toContain("data-plan-compute");
      expect(html).toContain("Package into Git Workspace");

      let click: (event: unknown) => Promise<void> = async () => {};
      const root = {
        addEventListener: (type: string, handler: typeof click) => {
          if (type === "click") click = handler;
        },
      };
      const store = {
        planning: {
          ...emptyPlanning(),
          session: accepted,
        },
      };
      const fetchImpl = vi.fn<typeof fetch>(
        async () =>
          new Response(JSON.stringify(accepted), {
            status: 200,
            headers: { "content-type": "application/json" },
          }),
      );
      bindPlanning(root, store, () => {}, fetchImpl);

      await click({ target: new FakeElement() });
      expect(fetchImpl.mock.calls[0]?.[0]).toBe(
        "/v1/planning-sessions/s1/compute",
      );
      expect(store.planning.computedWorkspaceId).toBe("wsp_s1");
      expect(planningMarkup(store.planning)).toContain(
        "data-plan-computed-workspace",
      );
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("shows Needs Input after Send when the opening message is empty", async () => {
    class FakeElement {
      closest(selector: string) {
        return selector === "[data-plan-send]" ? this : null;
      }
    }
    vi.stubGlobal("Element", FakeElement);
    try {
      const created = session({
        status: "started",
        plan: null,
        resolvedCommit: "",
        needsInput: [],
      });
      const advanced = session({
        status: "needs_input",
        plan: null,
        resolvedCommit: "b".repeat(40),
        needsInput: [
          question({
            id: "node_version",
            type: "select",
            options: ["20"],
          }),
        ],
      });
      const fetchImpl = vi.fn<typeof fetch>(async (input, options) => {
        const path = String(input);
        if (
          path.endsWith("/v1/planning-sessions") &&
          options?.method === "POST"
        ) {
          return new Response(JSON.stringify(created), {
            status: 201,
            headers: { "content-type": "application/json" },
          });
        }
        const body = JSON.parse(String(options?.body));
        const events = [
          { type: "RUN_STARTED", threadId: body.threadId, runId: body.runId },
          {
            type: "STATE_SNAPSHOT",
            snapshot: { session: advanced, pendingClientTools: [] },
          },
          { type: "MESSAGES_SNAPSHOT", messages: [] },
          {
            type: "RUN_FINISHED",
            threadId: body.threadId,
            runId: body.runId,
            outcome: {
              type: "interrupt",
              interrupts: [{ id: "needs-input", reason: "needs_input" }],
            },
          },
        ];
        return new Response(
          events.map((event) => `data: ${JSON.stringify(event)}\n\n`).join(""),
          {
            headers: { "content-type": "text/event-stream" },
          },
        );
      });
      let click: (event: unknown) => Promise<void> = async () => {};
      const root = {
        addEventListener: (type: string, handler: typeof click) => {
          if (type === "click") click = handler;
        },
      };
      const store = {
        planning: {
          ...emptyPlanning(),
          repoUrl: "https://github.com/excalidraw/excalidraw",
          ref: "main",
          message: "",
        },
      };
      bindPlanning(root, store, () => {}, fetchImpl);
      await click({ target: new FakeElement() });
      const recorded = store.planning.session as {
        status?: string;
        needsInput?: Array<{ id?: string }>;
      } | null;
      expect(recorded?.status).toBe("needs_input");
      expect(recorded?.needsInput?.[0]?.id).toBe("node_version");
      expect(planningMarkup(store.planning)).toContain("data-plan-needs-input");
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("renders problem-statement intake and binding rows for the appgen work profile", async () => {
    const composerHtml = planningMarkup({
      ...emptyPlanning(),
      profile: "appgen",
      problemStatement: "Build an incident status dashboard",
      appName: "incident-status",
    });
    expect(composerHtml).toContain('data-plan-field="problemStatement"');
    expect(composerHtml).toContain('data-plan-field="appName"');

    const rows = bindingRows({
      profile: { name: "appgen", version: "1.0.0", hash: "h".repeat(64) },
      intake: {
        repoUrl: "",
        ref: "",
        problemStatement: "Build an incident status dashboard",
        appName: "incident-status",
      },
      resolvedCommit: "none (no source repository — problem-statement intake)",
      model: "Configured model",
      plan,
      acceptedAt: "2026-01-01T00:00:00Z",
    });
    expect(rows).toContainEqual(["Profile", "appgen"]);
    expect(rows).toContainEqual([
      "Problem statement",
      "Build an incident status dashboard",
    ]);
    expect(rows).toContainEqual(["Application name", "incident-status"]);
    expect(rows).toContainEqual([
      "Commit SHA",
      "none (no source repository — problem-statement intake)",
    ]);

    const appgenPlan = {
      text: JSON.stringify({
        summary: "Generate companion service",
        baseImage: "cgr.dev/chainguard/static:latest",
        startCommand: "/app",
        ports: [8080],
        healthCheckPath: "/healthz",
        sso: {
          required: true,
          notes: "Keycloak OIDC SSO via Defense Unicorns UDS Package",
        },
        generatedFiles: ["main.go", "index.html"],
        packagedFiles: ["Dockerfile", "zarf.yaml"],
        notes: ["Generated after Accept"],
      }),
      sha256:
        "sha256:9c88c9910e6d231fb53a7f902d714a6054a98f22134bc810e15c9b8a76543210",
    };
    const appgenReadyHtml = planningMarkup({
      ...emptyPlanning(),
      session: session({
        profile: { name: "appgen", version: "0.1.0", hash: "ph" },
        intake: {
          repoUrl: "",
          ref: "",
          problemStatement: "Build an incident status dashboard",
          appName: "incident-status",
        },
        resolvedCommit:
          "none (no source repository — problem-statement intake)",
        plan: appgenPlan,
        messages: [
          { id: "", role: "operator", text: "Include Keycloak OIDC SSO" },
        ],
      }),
      agentMessages: [
        { id: "m_1", role: "user", content: "Include Keycloak OIDC SSO" },
      ],
    });
    expect(appgenReadyHtml).toContain(
      "Required — Keycloak OIDC SSO via Defense Unicorns UDS Package",
    );
    expect(appgenReadyHtml).toContain("commit none (appgen)");
    expect(appgenReadyHtml).toContain("plan 9c88c9910e6d");
    expect(appgenReadyHtml).not.toContain("sha256 sha256:");
    expect(appgenReadyHtml.match(/Include Keycloak OIDC SSO/g)).toHaveLength(1);
  });

  it("renders workspace validation summary and interactive file diff tabs after compute", async () => {
    class FakeElement {
      attr: string;
      constructor(attr: string) {
        this.attr = attr;
      }
      closest(selector: string) {
        return selector === this.attr ? this : null;
      }
      getAttribute(name: string) {
        return name === "data-plan-ws-tab" ? "zarf.yaml" : "";
      }
    }
    vi.stubGlobal("Element", FakeElement);
    try {
      const binding = {
        profile: { name: "onboard", version: "v3", hash: "ph" },
        intake: {
          repoUrl: "https://github.com/excalidraw/excalidraw",
          ref: "main",
        },
        resolvedCommit: "c".repeat(40),
        model: "conduit-packaging-agent-v1",
        plan,
        acceptedAt: "2026-01-01T00:00:00Z",
      };
      const accepted = session({ status: "accepted", binding });
      let click: (event: unknown) => Promise<void> = async () => {};
      const root = {
        addEventListener: (type: string, handler: typeof click) => {
          if (type === "click") click = handler;
        },
      };
      const store = {
        planning: {
          ...emptyPlanning(),
          session: accepted,
        },
      };
      const fetchImpl = vi.fn<typeof fetch>(async (input) => {
        const url = String(input);
        if (url.endsWith("/compute")) {
          return json(202, accepted);
        }
        if (url === "/v1/dev-workspaces/wsp_s1") {
          return json(200, {
            workspaceId: "wsp_s1",
            branch: "workspace/operator/wsp_s1",
            headCommit: "7b3c91e4a52d",
            approvalId: "cr-wsp_s1-uds",
          });
        }
        if (url === "/v1/dev-workspaces/wsp_s1/diff") {
          return json(200, {
            workspaceId: "wsp_s1",
            files: [
              {
                path: "Dockerfile",
                change: "added",
                hunks: [{ header: "@@ -0,0 +1 @@", lines: ["+FROM node:20"] }],
              },
              {
                path: "zarf.yaml",
                change: "added",
                hunks: [
                  {
                    header: "@@ -0,0 +1 @@",
                    lines: ["+kind: ZarfPackageConfig"],
                  },
                ],
              },
            ],
          });
        }
        return json(404, {});
      });
      bindPlanning(root, store, () => {}, fetchImpl);

      await click({ target: new FakeElement("[data-plan-compute]") });
      const html1 = planningMarkup(store.planning);
      expect(html1).toContain("data-plan-workspace-summary");
      expect(html1).toContain("cr-wsp_s1-uds (PENDING SECOND-PERSON APPROVAL)");
      expect(html1).toContain("+FROM node:20");

      await click({ target: new FakeElement("[data-plan-ws-tab]") });
      expect(store.planning.selectedWorkspaceFile).toBe("zarf.yaml");
      const html2 = planningMarkup(store.planning);
      expect(html2).toContain("+kind: ZarfPackageConfig");
    } finally {
      vi.unstubAllGlobals();
    }
  });
});

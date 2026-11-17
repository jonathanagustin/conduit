import { afterEach, describe, expect, it, vi } from "vite-plus/test";

import { PlanningAgent } from "../../console/operator/planning-agent.js";
import {
  bindPlanning,
  emptyPlanning,
  generationStatus,
  planningLinkStep,
  planningMarkup,
  rehydrateSession,
  stopPlanning,
  syncPlanningScreen,
} from "../../console/operator/planning-flow.js";
import {
  parseScreenHash,
  planningHash,
  planningSessionFromHash,
  screenHash,
} from "../../console/operator/planning-session-link.js";

const session = (id = "ps_1") => ({
  id,
  status: "ready",
  profile: { name: "onboard" },
  intake: { repoUrl: "https://example.test/repo", ref: "main" },
  messages: [],
  needsInput: [],
});
const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
const settle = () => new Promise((resolve) => setTimeout(resolve, 0));
const asFetch = (mock: unknown) => mock as typeof fetch;
const asWindow = (value: unknown) => value as typeof globalThis;

describe("planning session hash", () => {
  it("parses a screen and its session id", () => {
    expect(parseScreenHash("#s01-intake/ps_1")).toEqual({
      screen: "s01-intake",
      param: "ps_1",
    });
    expect(parseScreenHash("#area-releases")).toEqual({
      screen: "area-releases",
      param: "",
    });
    expect(parseScreenHash("")).toEqual({ screen: "", param: "" });
    expect(parseScreenHash("#s01-intake/%E0%A4%A")).toEqual({
      screen: "s01-intake",
      param: "",
    });
  });

  it("links only the planning screen, and only to a well-formed id", () => {
    expect(planningSessionFromHash("#s01-intake/ps_1")).toBe("ps_1");
    expect(planningSessionFromHash("#s01-intake")).toBe("");
    expect(planningSessionFromHash("#area-releases/ps_1")).toBe("");
    expect(planningSessionFromHash("#s01-intake/a%2Fb")).toBe("");
    expect(planningSessionFromHash("#s01-intake/<script>")).toBe("");
  });

  it("writes the hash a parse reads back", () => {
    expect(planningHash("ps_1")).toBe("#s01-intake/ps_1");
    expect(planningSessionFromHash(planningHash("ps:1.a-b"))).toBe("ps:1.a-b");
    expect(planningHash("")).toBe("#s01-intake");
  });

  it("keeps the session in the hash across a screen switch", () => {
    const state = { planning: { session: { id: "ps_1" } } };
    expect(screenHash("s01-intake", state)).toBe("#s01-intake/ps_1");
    expect(screenHash("area-releases", state)).toBe("#area-releases");
    expect(screenHash("s01-intake", {})).toBe("#s01-intake");
  });
});

describe("planningLinkStep", () => {
  it("loads a linked session that is not on screen", () => {
    expect(planningLinkStep(emptyPlanning(), "#s01-intake/ps_1")).toEqual({
      step: "load",
      id: "ps_1",
    });
  });

  it("writes the hash for a session the URL does not name", () => {
    expect(
      planningLinkStep(
        { ...emptyPlanning(), session: session() },
        "#s01-intake",
      ),
    ).toEqual({ step: "write", hash: "#s01-intake/ps_1" });
  });

  it("does nothing when they agree, are loading, or the screen is elsewhere", () => {
    const loaded = { ...emptyPlanning(), session: session() };
    expect(planningLinkStep(loaded, "#s01-intake/ps_1").step).toBe("none");
    expect(planningLinkStep(loaded, "#area-releases").step).toBe("none");
    expect(
      planningLinkStep(
        { ...emptyPlanning(), link: { id: "ps_1", status: "lost" } },
        "#s01-intake/ps_1",
      ).step,
    ).toBe("none");
    expect(
      planningLinkStep({ ...loaded, busy: "turn" }, "#s01-intake/ps_2").step,
    ).toBe("none");
  });
});

describe("rehydrate from a deep link", () => {
  const win = (hash: string) => ({
    location: { hash },
    history: { replaceState: vi.fn() },
  });

  it("reads the session and its recorded events", async () => {
    const events = [
      {
        id: "e1",
        type: "message",
        timestamp: "t",
        message: { role: "model", text: "inspected the repository" },
      },
    ];
    const fetchImpl = vi.fn(async (path: string) => {
      if (path === "/v1/planning-sessions/ps_1") return json(session());
      if (path === "/v1/planning-sessions/ps_1/events")
        return json({ items: events });
      return json({ items: [] });
    });
    const store: Record<string, any> = {};
    const rerender = vi.fn();
    syncPlanningScreen(
      store,
      rerender,
      asFetch(fetchImpl),
      asWindow(win("#s01-intake/ps_1")),
    );
    expect(store.planning.link).toEqual({ id: "ps_1", status: "loading" });
    expect(planningMarkup(store.planning)).toContain(
      'data-plan-link="loading"',
    );
    await settle();
    expect(store.planning.session.id).toBe("ps_1");
    expect(store.planning.events).toEqual(events);
    expect(store.planning.link.status).toBe("ready");
    expect(rerender).toHaveBeenCalled();
    expect(planningMarkup(store.planning)).toContain(
      "inspected the repository",
    );
    expect(fetchImpl.mock.calls.map((call) => call[0])).toEqual(
      expect.arrayContaining([
        "/v1/planning-sessions/ps_1",
        "/v1/planning-sessions/ps_1/events",
      ]),
    );
  });

  it("states that the Hub no longer has the session on a 404", async () => {
    const fetchImpl = vi.fn(async (path: string) =>
      path.startsWith("/v1/planning-sessions/")
        ? json(
            {
              code: "SESSION_NOT_FOUND",
              error: "planning session was not found",
            },
            404,
          )
        : json({ items: [] }),
    );
    const store: Record<string, any> = {};
    syncPlanningScreen(
      store,
      () => {},
      asFetch(fetchImpl),
      asWindow(win("#s01-intake/ps_gone")),
    );
    await settle();
    expect(store.planning.link).toMatchObject({
      id: "ps_gone",
      status: "lost",
    });
    expect(store.planning.session).toBeNull();
    const html = planningMarkup(store.planning);
    expect(html).toContain('data-plan-link="lost"');
    expect(html).toContain("was not found on this Hub (it may have restarted)");
    expect(html).toContain("data-plan-link-clear");
    // No composer is offered as if the session were merely empty.
    expect(html).not.toContain("data-plan-send");
  });

  it("does not call an unreadable session lost", async () => {
    const fetchImpl = vi.fn(async (path: string) =>
      path.startsWith("/v1/planning-sessions/")
        ? json({ error: "store unavailable" }, 502)
        : json({ items: [] }),
    );
    const store: Record<string, any> = {};
    syncPlanningScreen(
      store,
      () => {},
      asFetch(fetchImpl),
      asWindow(win("#s01-intake/ps_1")),
    );
    await settle();
    expect(store.planning.link.status).toBe("failed");
    const html = planningMarkup(store.planning);
    expect(html).toContain("store unavailable");
    expect(html).not.toContain("it may have restarted");
  });

  it("keeps the session when only its events could not be read", async () => {
    const fetchImpl = vi.fn(async (path: string) =>
      path.endsWith("/events") ? json({ error: "nope" }, 502) : json(session()),
    );
    const result = await rehydrateSession("ps_1", asFetch(fetchImpl));
    expect(result.session.id).toBe("ps_1");
    expect(result.events).toEqual([]);
    expect(result.eventsError).toBeTruthy();
  });

  it("puts a new session's id in the URL", () => {
    const window = win("#s01-intake");
    const store = {
      planning: {
        ...emptyPlanning(),
        session: session(),
        generations: { status: "ready", items: [] },
      },
    };
    syncPlanningScreen(store, () => {}, asFetch(vi.fn()), asWindow(window));
    expect(window.history.replaceState).toHaveBeenCalledWith(
      null,
      "",
      "#s01-intake/ps_1",
    );
  });
});

describe("stop", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("only stops a running agent stream", () => {
    expect(stopPlanning({ ...emptyPlanning(), busy: "turn" })).toMatchObject({
      busy: "",
      stopped: true,
      error: null,
    });
    const accepting = { ...emptyPlanning(), busy: "accept" };
    expect(stopPlanning(accepting)).toBe(accepting);
  });

  it("offers Stop only while a stream is being read, and says what Stop did not do", () => {
    const running = planningMarkup({
      ...emptyPlanning(),
      session: session(),
      busy: "turn",
    });
    expect(running).toContain("data-plan-stop");
    expect(
      planningMarkup({ ...emptyPlanning(), session: session() }),
    ).not.toContain("data-plan-stop");
    const stopped = planningMarkup({
      ...emptyPlanning(),
      session: session(),
      stopped: true,
    });
    expect(stopped).toContain("data-plan-stopped");
    expect(stopped).toContain("not cancelled on the Hub");
  });

  it("passes the abort signal to the agent stream request", async () => {
    const controller = new AbortController();
    const fetchImpl = vi.fn(
      (_path: string, init: RequestInit) =>
        new Promise<Response>((_, reject) =>
          init.signal?.addEventListener("abort", () =>
            reject(new DOMException("Aborted", "AbortError")),
          ),
        ),
    );
    const agent = new PlanningAgent("ps_1", asFetch(fetchImpl));
    const pending = agent.run(
      { message: "inspect" },
      {},
      () => {},
      controller.signal,
    );
    await settle();
    expect(fetchImpl.mock.calls[0][1].signal).toBe(controller.signal);
    controller.abort();
    await expect(pending).rejects.toMatchObject({ name: "AbortError" });
    expect(agent.active).toBe(false);
    expect(agent.recoveryRequired).toBe(true);
  });

  it("aborts the stream on Stop and leaves a notice, not an error", async () => {
    let target = "[data-plan-send]";
    class FakeElement {
      closest(selector: string) {
        return selector === target ? this : null;
      }
    }
    vi.stubGlobal("Element", FakeElement);
    let signal: AbortSignal | undefined;
    const fetchImpl = vi.fn(
      (_path: string, init: RequestInit) =>
        new Promise<Response>((_, reject) => {
          signal = init.signal ?? undefined;
          init.signal?.addEventListener("abort", () =>
            reject(new DOMException("Aborted", "AbortError")),
          );
        }),
    );
    let click: (event: unknown) => Promise<void> = async () => {};
    const root = {
      addEventListener: (type: string, handler: typeof click) => {
        if (type === "click") click = handler;
      },
    };
    const store = {
      planning: { ...emptyPlanning(), session: session(), message: "inspect" },
    };
    bindPlanning(root, store, () => {}, asFetch(fetchImpl));
    const sending = click({ target: new FakeElement() });
    await settle();
    expect(store.planning.busy).toBe("turn");
    expect(signal?.aborted).toBe(false);

    target = "[data-plan-stop]";
    await click({ target: new FakeElement() });
    await sending;
    expect(signal?.aborted).toBe(true);
    expect(store.planning.busy).toBe("");
    expect(store.planning.stopped).toBe(true);
    expect(store.planning.error).toBeNull();
    expect(fetchImpl).toHaveBeenCalledTimes(1);
    expect(fetchImpl.mock.calls[0][0]).toBe("/v1/agent-runs");
  });
});

describe("app generations", () => {
  it("states a status only from recorded fields", () => {
    expect(generationStatus({ error: "build failed" })).toBe("error");
    expect(generationStatus({ needs: ["name"] })).toBe("needs input");
    expect(generationStatus({ confirmRequired: true })).toBe(
      "awaiting confirmation",
    );
    expect(
      generationStatus({
        gates: [
          { name: "a", outcome: "pass" },
          { name: "b", outcome: "fail" },
        ],
      }),
    ).toBe("gates failed");
    expect(generationStatus({ gates: [{ name: "a", outcome: "pass" }] })).toBe(
      "gates passed",
    );
    expect(generationStatus({})).toBe("recorded");
  });

  it("lists the caller's generations with gate results, escaped", () => {
    const html = planningMarkup({
      ...emptyPlanning(),
      generations: {
        status: "ready",
        items: [
          {
            id: "gen_1",
            name: "<b>orders</b>",
            gates: [
              { name: "build", outcome: "fail", reason: "no Dockerfile" },
            ],
          },
        ],
      },
    });
    expect(html).toContain('data-plan-generation="gen_1"');
    expect(html).toContain("&lt;b&gt;orders&lt;/b&gt;");
    expect(html).toContain("gates failed");
    expect(html).toContain("no Dockerfile");
  });

  it("does not fake a session list the Hub cannot produce", () => {
    const html = planningMarkup({
      ...emptyPlanning(),
      generations: { status: "ready", items: [] },
    });
    expect(html).toContain("data-plan-generations-empty");
    expect(html).toContain("no endpoint that lists them");
  });

  it("reads the list once per paint cycle and reports a refusal", async () => {
    const fetchImpl = vi.fn(async (_path: string) =>
      json({ error: "unauthorized" }, 401),
    );
    const store: Record<string, any> = {};
    const window = { location: { hash: "#s01-intake" }, history: {} };
    syncPlanningScreen(store, () => {}, asFetch(fetchImpl), asWindow(window));
    syncPlanningScreen(store, () => {}, asFetch(fetchImpl), asWindow(window));
    await settle();
    expect(fetchImpl).toHaveBeenCalledTimes(1);
    expect(fetchImpl.mock.calls[0][0]).toBe("/v1/app-generations");
    expect(store.planning.generations).toMatchObject({
      status: "failed",
      reason: "unauthorized",
    });
  });
});

import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import {
  cancelPlanningRun,
  planningOwnerKey,
  requestPlanningCancellation,
} from "../../console/operator/planning-cancellation.js";
import {
  bindPlanning,
  emptyPlanning,
  planningMarkup,
} from "../../console/operator/planning-flow.js";

const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: { "content-type": "application/json" },
  });
const receipt = (runId = "run", state = "cancellation_requested") => ({
  runId,
  state,
  valueTier: "CONDUIT_NATIVE",
});
const settle = () => new Promise((resolve) => setTimeout(resolve, 0));
const session = (id = "session") => ({
  id,
  status: "ready",
  intake: { repoUrl: "https://github.com/org/app", ref: "main" },
  messages: [],
  needsInput: [],
});
const storeFor = () =>
  ({
    reviewOperatorActor: "alice",
    planning: { ...emptyPlanning(), session: session() },
  }) as Record<string, any>;
afterEach(() => vi.unstubAllGlobals());

describe("exact planning cancellation acknowledgement", () => {
  it("sends only the selected session and run, and keeps requested separate from cancelled", async () => {
    const fetcher = vi.fn(async () => json(receipt("run/id")));
    const got = await cancelPlanningRun("session/id", "run/id", fetcher);
    expect(got.state).toBe("cancellation_requested");
    expect(fetcher).toHaveBeenCalledWith(
      "/v1/planning-sessions/session%2Fid/turns/run%2Fid/cancel",
      {
        method: "POST",
        headers: {
          "content-type": "application/json",
          accept: "application/json",
        },
        body: "{}",
      },
    );
    for (const value of [
      receipt("other"),
      receipt("run", "running"),
      { ...receipt(), valueTier: "UNKNOWN" },
      null,
    ]) {
      await expect(
        cancelPlanningRun("session", "run", async () => json(value)),
      ).rejects.toThrow("invalid cancellation acknowledgement");
    }
    await expect(
      cancelPlanningRun("session", "run", async () =>
        json({ error: "not configured" }, 501),
      ),
    ).rejects.toThrow("not configured");
  });

  it.each(["owner", "session", "new run"])(
    "discards a late cancellation response after a changed %s",
    async (change) => {
      const store = storeFor();
      let release!: (response: Response) => void;
      const fetcher = () =>
        new Promise<Response>((resolve) => {
          release = resolve;
        });
      const pending = requestPlanningCancellation(
        store,
        { sessionId: "session", runId: "run", owner: planningOwnerKey(store) },
        fetcher,
        () => {},
      );
      if (change === "owner") store.reviewOperatorActor = "bob";
      else if (change === "session")
        store.planning = { ...emptyPlanning(), session: session("other") };
      else
        store.planning = {
          ...store.planning,
          cancellation: null,
          busy: "turn",
          stopped: false,
        };
      const selected = store.planning;
      release(json(receipt("run", "cancelled")));
      await pending;
      expect(store.planning).toBe(selected);
      expect(store.planning.cancellation?.status).not.toBe("ready");
    },
  );

  it("renders request, cancellation, completed and unavailable states distinctly", () => {
    const base = { ...emptyPlanning(), session: session(), stopped: true };
    const markup = (state: string) =>
      planningMarkup({
        ...base,
        cancellation: { status: "ready", receipt: receipt("run", state) },
      });
    expect(markup("cancellation_requested")).toContain("not yet confirmed");
    expect(markup("cancellation_requested")).toContain(
      "data-plan-cancel-check",
    );
    expect(markup("cancelled")).toContain(
      "confirmed this planning turn is cancelled",
    );
    expect(markup("completed")).toContain("already completed");
    expect(
      planningMarkup({
        ...base,
        cancellation: { status: "failed", error: "<unavailable>" },
      }),
    ).toContain("&lt;unavailable&gt;");
  });
});

class ClickTarget {
  constructor(readonly selector: string) {}
  closest(selector: string) {
    return this.selector === selector ? this : null;
  }
}
function bind(store: Record<string, any>, fetcher: typeof fetch) {
  vi.stubGlobal("Element", ClickTarget);
  let click: (event: unknown) => Promise<void> = async () => {};
  bindPlanning(
    {
      addEventListener(type: string, handler: typeof click) {
        if (type === "click") click = handler;
      },
    },
    store,
    () => {},
    fetcher,
  );
  return (selector: string) => click({ target: new ClickTarget(selector) });
}

describe("Stop operation scope", () => {
  it("stops before a pending creation can start an agent run", async () => {
    const store = storeFor();
    store.planning = {
      ...emptyPlanning(),
      repoUrl: "https://github.com/org/app",
      ref: "main",
      message: "inspect",
    };
    let release!: (response: Response) => void;
    const fetcher = vi.fn(
      (_path: RequestInfo | URL, _init?: RequestInit) =>
        new Promise<Response>((resolve) => {
          release = resolve;
        }),
    );
    const click = bind(store, fetcher as typeof fetch);
    const sending = click("[data-plan-send]");
    await settle();
    await click("[data-plan-stop]");
    release(json(session("created"), 201));
    await sending;
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(fetcher.mock.calls[0][0]).toBe("/v1/planning-sessions");
    expect(store.planning.session).toBeNull();
    expect(store.planning.busy).toBe("");
    expect(store.planning.error).toBeNull();
    expect(planningMarkup(store.planning)).toContain(
      "Stopped before an agent run started",
    );
  });

  it("does not paint a late event after Stop, and cancels the captured exact run", async () => {
    const store = storeFor();
    store.planning.message = "inspect";
    let release!: (response: Response) => void;
    let input: any;
    const fetcher = vi.fn((path: string, init?: RequestInit) => {
      if (path === "/v1/agent-runs") {
        if (typeof init?.body !== "string")
          throw new Error("expected serialized run input");
        input = JSON.parse(init.body);
        return new Promise<Response>((resolve) => {
          release = resolve;
        });
      }
      return Promise.resolve(json(receipt(input.runId, "cancelled")));
    });
    const click = bind(store, fetcher as typeof fetch);
    const sending = click("[data-plan-send]");
    await settle();
    await click("[data-plan-stop]");
    release(
      new Response(
        `data: ${JSON.stringify({ type: "RUN_STARTED", threadId: input.threadId, runId: input.runId })}\n\n`,
      ),
    );
    await sending;
    expect(store.planning.agentMessages).toEqual([]);
    expect(store.planning.session.id).toBe("session");
    expect(store.planning.error).toBeNull();
    expect(store.planning.cancellation.receipt.state).toBe("cancelled");
    expect(fetcher.mock.calls[1][0]).toBe(
      `/v1/planning-sessions/session/turns/${input.runId}/cancel`,
    );
    expect(store.planning.agent.activeRun).toBeNull();
  });

  it("only stops reading during read-only snapshot recovery", async () => {
    const store = storeFor();
    const fetcher = vi.fn(
      (_path: string, init?: RequestInit) =>
        new Promise<Response>((_, reject) => {
          init?.signal?.addEventListener("abort", () =>
            reject(new DOMException("aborted", "AbortError")),
          );
        }),
    );
    const click = bind(store, fetcher as typeof fetch);
    const recovering = click("[data-plan-recover]");
    await settle();
    await click("[data-plan-stop]");
    await recovering;
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(store.planning.cancellation).toBeNull();
    expect(planningMarkup(store.planning)).toContain(
      "No model-turn cancellation was requested",
    );
  });
});

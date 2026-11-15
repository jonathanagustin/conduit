import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { createLiveHub } from "../../console/operator/live-hub.js";
import type { LiveEnvelope } from "../../api/live-events.js";
import { liveSessionIdentity } from "../../api/live-identity.js";

const encoder = new TextEncoder();
const session = (name: string) => ({
  actor: { username: name, teams: ["readers"], serviceAccount: false },
  effective: { username: name, teams: ["readers"], serviceAccount: false },
  delegated: false,
  globalOperations: [],
  resources: [],
});
const tick = async () => {
  for (let i = 0; i < 500; i++) await Promise.resolve();
};
let hub: ReturnType<typeof createLiveHub> | undefined;
afterEach(() => {
  hub?.stop();
  hub = undefined;
  vi.unstubAllGlobals();
  vi.useRealTimers();
});
function setup(streamStatus = 200) {
  vi.useFakeTimers();
  vi.stubGlobal("document", {
    getElementById: () => null,
    querySelector: () => null,
    querySelectorAll: () => [],
  });
  let owner = "alice",
    revision = 1,
    streamController!: ReadableStreamDefaultController<Uint8Array>;

  const calls: {
    path: string;
    method: string;
    signal?: AbortSignal | null;
    headers?: HeadersInit;
  }[] = [];
  const fetchImpl = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const path =
        typeof input === "string"
          ? input
          : input instanceof URL
            ? input.href
            : input.url;
      calls.push({
        path,
        method: init?.method ?? "GET",
        signal: init?.signal,
        headers: init?.headers,
      });
      if (path === "/v1/events") {
        const body = new ReadableStream<Uint8Array>({
          start(value) {
            streamController = value;
          },
        });
        return streamStatus === 200
          ? new Response(body, {
              headers: {
                "content-type": "text/event-stream",
                "cache-control": "no-store",
                "X-Conduit-Stream-ID": "s_live",
                "X-Conduit-Event-Sequence": "0",
                "X-Conduit-Event-Cursor": "c_root",
              },
            })
          : new Response("private", { status: streamStatus });
      }
      const rows =
        path === "/v1/session"
          ? session(owner)
          : path === "/v1/environments"
            ? { environments: [{ id: "dev" }] }
            : path === "/v1/fleet/installations"
              ? { installations: [] }
              : path === "/v1/products?limit=20"
                ? { products: [] }
                : path === "/v1/packaging-runs"
                  ? { items: [], configured: true }
                  : path === "/v1/change-requests"
                    ? { changeRequests: [] }
                    : path === "/v1/promotions"
                      ? { promotions: [] }
                      : path === "/v1/plans"
                        ? { plans: [{ id: `${owner}-plan`, revision }] }
                        : {};
      return new Response(JSON.stringify(rows));
    },
  );
  vi.stubGlobal("fetch", fetchImpl);
  const state: Record<string, unknown> = {};
  hub = createLiveHub({ state, render: () => {} });
  return {
    state,
    hubState: () => state.hub as { plans: { id: string; revision: number }[] },
    calls,
    fetchImpl,
    setOwner(value: string) {
      owner = value;
    },
    send(sequence: number) {
      revision++;
      const event = {
        version: 1,
        streamID: "s_live",
        sequence,
        cursor: `c_${sequence}`,
        occurredAt: "2026-11-15T12:00:00Z",
        kind: "index.invalidated",
        resource: { type: "index", id: "idx_one", revision },
        data: { areas: ["plans", "overview", "kpi"] },
      };
      streamController.enqueue(
        encoder.encode(
          `id: ${event.cursor}\nevent: ${event.kind}\ndata: ${JSON.stringify(event)}\n\n`,
        ),
      );
    },
  };
}

describe("active Hub live bridge", () => {
  it("refetches the existing read model from invalidations and pauses fallback polling while healthy", async () => {
    const fixture = setup();
    hub!.ensure();
    await tick();
    expect(hub!.liveStatus().state).toBe("live");
    expect(fixture.hubState().plans[0].revision).toBe(1);
    const received: LiveEnvelope[] = [];
    hub!.onLiveEvents((events: readonly LiveEnvelope[]) =>
      received.push(...events),
    );
    fixture.send(1);
    await tick();
    expect(fixture.hubState().plans[0].revision).toBe(2);
    expect(received).toHaveLength(1);
    expect(hub!.operationsMarkup()).toContain('data-operation-sequence="1"');
    const count = fixture.calls.length;
    await vi.advanceTimersByTimeAsync(30000);
    await tick();
    expect(fixture.calls).toHaveLength(count);
    expect(fixture.calls.every((call) => call.method === "GET")).toBe(true);
  });
  it("shows authorization failure and keeps a read-only polling fallback without reconnect loops", async () => {
    const fixture = setup(403);
    hub!.ensure();
    await tick();
    expect(hub!.liveStatus().state).toBe("auth-required");
    const count = fixture.calls.filter(
      (call) => call.path === "/v1/plans",
    ).length;
    await vi.advanceTimersByTimeAsync(30000);
    await tick();
    expect(
      fixture.calls.filter((call) => call.path === "/v1/plans").length,
    ).toBeGreaterThan(count);
    expect(
      fixture.calls.filter((call) => call.path === "/v1/events"),
    ).toHaveLength(1);
  });
  it("aborts the old stream and never reuses its cursor for a different native operator", async () => {
    const fixture = setup();
    hub!.ensure();
    await tick();
    fixture.send(1);
    await tick();
    const oldSignal = fixture.calls.find(
      (call) => call.path === "/v1/events",
    )!.signal;
    fixture.setOwner("bob");
    await hub!.refresh();
    await tick();
    expect(oldSignal!.aborted).toBe(true);
    expect(hub!.operationsMarkup()).not.toContain("data-operation-sequence");
    expect(fixture.hubState().plans[0].id).toBe("bob-plan");
    expect(fixture.state.reviewOperatorName).toBe("bob");
    expect(hub!.liveStatus().state).toBe("live");
    const requests = fixture.calls.filter((call) => call.path === "/v1/events");
    expect(new Headers(requests.at(-1)!.headers).has("Last-Event-ID")).toBe(
      false,
    );
  });
  it("canonicalizes current native identity and capability order without using a browser credential", () => {
    const first = session("alice"),
      second = session("alice");
    first.actor.teams = ["b", "a"];
    second.actor.teams = ["a", "b"];
    expect(liveSessionIdentity(first)).toBe(liveSessionIdentity(second));
    expect(liveSessionIdentity(first)).not.toBe(
      liveSessionIdentity(session("bob")),
    );
    expect(liveSessionIdentity({})).toBe("");
  });
});

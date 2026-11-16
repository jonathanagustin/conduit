import { afterEach, describe, expect, it, vi } from "vite-plus/test";

import {
  createLiveHub,
  REFRESH_READ_POSTS,
} from "../../console/operator/live-hub.js";

type Call = { path: string; method: string };

function stubHub(calls: Call[]) {
  vi.stubGlobal("document", {
    getElementById: () => null,
    querySelector: () => null,
    querySelectorAll: () => [],
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string, init?: RequestInit) => {
      const path = String(input);
      calls.push({ path, method: (init?.method || "GET").toUpperCase() });
      if (path === "/v1/bundles") {
        return new Response(JSON.stringify({ bundles: [{ id: "b1" }] }), {
          status: 200,
        });
      }
      return new Response("{}", { status: 200 });
    }),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("live hub refresh", () => {
  it("only reads: no check-in and no write routes", async () => {
    const calls: Call[] = [];
    stubHub(calls);
    const state: Record<string, unknown> = {};
    const hub = createLiveHub({ state, render: () => {} });
    await hub.refresh();
    await hub.refresh();

    expect(calls.length).toBeGreaterThan(5);
    expect(calls.some((call) => call.path.includes("/v1/sites/check-in"))).toBe(
      false,
    );
    const posts = calls.filter((call) => call.method !== "GET");
    for (const post of posts) {
      expect(post.method).toBe("POST");
      expect(REFRESH_READ_POSTS).toContain(post.path);
    }
    expect(state.hub).toBeTruthy();
  });
  it("keeps selected-run history separate and follows the history cursor", async () => {
    const calls: Call[] = [];
    stubHub(calls);
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input, init) => {
        const request = input instanceof Request ? input : null;
        const path = request ? new URL(request.url).pathname : String(input);
        calls.push({ path, method: request?.method || init?.method || "GET" });
        if (request) {
          const query = await request.json();
          expect(path).toBe(
            "/conduit.packaging.v1.PackagingRunService/ListPackagingRunEvents",
          );
          expect(request.credentials).toBe("same-origin");
          const first = query.runId === "r1";
          return new Response(
            JSON.stringify({
              configured: true,
              runId: query.runId,
              limit: 20,
              pageSize: first ? 1 : 0,
              total: first ? "2" : "0",
              nextCursor: first && !query.cursor ? "next" : "",
              items: first
                ? [
                    {
                      sequence: query.cursor ? "2" : "1",
                      at: "2026-11-16T00:00:00Z",
                      step: query.cursor ? "review" : "inspect",
                      reason: "first-run-only",
                    },
                  ]
                : [],
            }),
            { headers: { "content-type": "application/json" } },
          );
        }
        if (path === "/v1/packaging-runs")
          return new Response(
            JSON.stringify({
              items: [
                { runId: "r1", phase: "review" },
                { runId: "r2", phase: "blocked" },
              ],
            }),
          );
        return new Response("{}");
      }),
    );
    vi.stubGlobal("window", { location: { origin: "http://hub.local" } });
    const state: Record<string, any> = {};
    const live = createLiveHub({ state, render: () => {} });
    await live.refresh();
    expect(state.hub.packagingEventsByRun.r1).toHaveLength(2);
    expect(state.hub.packagingHistoryByRun.r1.total).toBe(BigInt(2));
    expect(calls.some((call) => call.path.includes("/events"))).toBe(false);
    expect(state.hub.packagingHistoryByRun.r1.complete).toBe(true);
    state.packagingRunId = "r2";
    const root = { innerHTML: "", querySelectorAll: () => [] };
    live.renderPipeline(root);
    expect(root.innerHTML).toContain("No packaging step has been recorded");
    expect(root.innerHTML).not.toContain("first-run-only");
    delete state.hub.packagingEventsByRun.r2;
    live.renderPackaging(root);
    expect(root.innerHTML).not.toContain("first-run-only");
    expect(
      calls
        .filter((call) => call.path.includes("ListPackagingRunEvents"))
        .every((call) => call.method === "POST"),
    ).toBe(true);
  });
});

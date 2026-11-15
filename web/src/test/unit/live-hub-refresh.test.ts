import { afterEach, describe, expect, it, vi } from "vitest";

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
        const path = String(input);
        calls.push({ path, method: init?.method || "GET" });
        if (path === "/v1/packaging-runs")
          return new Response(
            JSON.stringify({
              items: [
                { runId: "r1", phase: "review" },
                { runId: "r2", phase: "blocked" },
              ],
            }),
          );
        if (path === "/v1/packaging-runs/r1/events")
          return new Response(
            JSON.stringify({
              items: [
                { sequence: 1, step: "inspect", reason: "first-run-only" },
              ],
              total: 2,
              nextCursor: "next",
            }),
          );
        if (path.includes("r1/events?cursor="))
          return new Response(
            JSON.stringify({
              items: [{ sequence: 2, step: "review", outcome: "WAITING" }],
              total: 2,
            }),
          );
        if (path === "/v1/packaging-runs/r2/events")
          return new Response(JSON.stringify({ items: [], total: 0 }));
        return new Response("{}");
      }),
    );
    const state: Record<string, any> = {};
    const live = createLiveHub({ state, render: () => {} });
    await live.refresh();
    expect(state.hub.packagingEventsByRun.r1).toHaveLength(2);
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
        .filter((call) => call.path.includes("/events"))
        .every((call) => call.method === "GET"),
    ).toBe(true);
  });
});

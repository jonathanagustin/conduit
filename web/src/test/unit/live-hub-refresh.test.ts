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
});

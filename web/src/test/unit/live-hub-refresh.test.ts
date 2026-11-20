import { afterEach, describe, expect, it, vi } from "vite-plus/test";

import {
  createLiveHub,
  REFRESH_READ_POSTS,
} from "../../console/operator/live-hub.js";

type Call = { path: string; method: string };

function stubHub(calls: Call[]) {
  vi.stubGlobal("window", { location: { origin: "http://hub.local" } });
  vi.stubGlobal("document", {
    getElementById: () => null,
    querySelector: () => null,
    querySelectorAll: () => [],
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request = input instanceof Request ? input : null;
      const path = request ? new URL(request.url).pathname : String(input);
      calls.push({
        path,
        method: (request?.method || init?.method || "GET").toUpperCase(),
      });
      if (path.endsWith("/ListPackagingRuns"))
        return new Response(
          JSON.stringify({
            configured: true,
            items: [],
            total: "0",
            pageSize: 0,
            limit: 20,
          }),
          { headers: { "content-type": "application/json" } },
        );
      if (path.endsWith("/ListFleetInstallations"))
        return new Response(
          JSON.stringify({
            generatedAt: "2026-11-20T01:00:00Z",
            maxResultAgeSeconds: "120",
            installations: [],
            summary: { total: "0" },
            facets: {},
            pageSize: 0,
            limit: 20,
          }),
          { headers: { "content-type": "application/json" } },
        );
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
  it("cancels the packaging fetch without committing a partial refresh", async () => {
    const calls: Call[] = [];
    stubHub(calls);
    const original = globalThis.fetch;
    let ready!: () => void;
    const started = new Promise<void>((resolve) => {
      ready = resolve;
    });
    let request: Request | undefined;
    vi.stubGlobal(
      "fetch",
      async (input: RequestInfo | URL, init?: RequestInit) => {
        if (
          input instanceof Request &&
          input.url.endsWith("/ListPackagingRuns")
        ) {
          request = input;
          calls.push({
            path: new URL(input.url).pathname,
            method: input.method,
          });
          ready();
          return new Promise<Response>((_, reject) =>
            input.signal.addEventListener(
              "abort",
              () => reject(new DOMException("Aborted", "AbortError")),
              { once: true },
            ),
          );
        }
        return original(input, init);
      },
    );
    const previous = { packaging: [{ runId: "previous" }] };
    const state: Record<string, any> = { hub: previous };
    const hub = createLiveHub({ state, render: () => {} });
    const controller = new AbortController();
    state.hub = previous;
    const pending = hub.refresh({ signal: controller.signal });
    await started;
    controller.abort();
    await pending;
    expect(request?.signal.aborted).toBe(true);
    expect(state.hub).toBe(previous);
    expect(calls.some((call) => call.path === "/v1/packaging-runs")).toBe(
      false,
    );
  });

  it.each([
    [401, "unauthenticated"],
    [403, "permission_denied"],
    [501, "unimplemented"],
  ])(
    "does not replay a refused initial or manual list over REST (%i)",
    async (status, code) => {
      const calls: Call[] = [];
      stubHub(calls);
      const fetchHub = globalThis.fetch;
      vi.stubGlobal(
        "fetch",
        vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
          if (
            input instanceof Request &&
            new URL(input.url).pathname.endsWith("/ListPackagingRuns")
          ) {
            calls.push({
              path: new URL(input.url).pathname,
              method: input.method,
            });
            expect(input.credentials).toBe("same-origin");
            return new Response(JSON.stringify({ code, message: "refused" }), {
              status,
              headers: { "content-type": "application/json" },
            });
          }
          return fetchHub(input, init);
        }),
      );
      const state: Record<string, any> = {};
      const hub = createLiveHub({ state, render: () => {} });
      for (let refresh = 0; refresh < 2; refresh++) {
        state.hub = { packaging: [{ runId: "stale" }] };
        await hub.refresh();
        expect(state.hub.packaging).toEqual([]);
        expect(state.hub.packagingNote).toBe(
          "Hub did not return packaging runs.",
        );
      }
      expect(
        calls.filter((call) => call.path.endsWith("/ListPackagingRuns")),
      ).toHaveLength(2);
      expect(calls.some((call) => call.path === "/v1/packaging-runs")).toBe(
        false,
      );
    },
  );
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
      expect([
        ...REFRESH_READ_POSTS,
        "/conduit.packaging.v1.PackagingRunService/ListPackagingRuns",
        "/conduit.fleet.v1.FleetReadService/ListFleetInstallations",
      ]).toContain(post.path);
    }
    expect(
      calls.filter((call) => call.path.endsWith("/ListPackagingRuns")),
    ).toHaveLength(2);
    expect(calls.some((call) => call.path === "/v1/packaging-runs")).toBe(
      false,
    );
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
          if (path.endsWith("/ListPackagingRuns"))
            return new Response(
              JSON.stringify({
                configured: true,
                items: [{ runId: "r1" }, { runId: "r2" }],
                limit: 20,
                pageSize: 2,
                total: "2",
              }),
              { headers: { "content-type": "application/json" } },
            );
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

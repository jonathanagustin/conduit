import { afterEach, describe, expect, it, vi } from "vite-plus/test";

import {
  INDEX_PAGE_BOUND,
  createLiveHub,
  readPages,
  refreshEnvironmentId,
  timelineSummary,
} from "../../console/operator/live-hub.js";
import { truncationNotices } from "../../console/operator/screen-registry.js";

/** An index of `pages` pages of `size` rows; `total` only when given. */
function pagedIndex(key: string, pages: number, size: number, total?: number) {
  const urls: string[] = [];
  const read = async (url: string) => {
    urls.push(url);
    const cursor = new URL(url, "http://hub.local").searchParams.get("cursor");
    const page = cursor ? Number(cursor) : 0;
    return {
      [key]: Array.from({ length: size }, (_, row) => ({
        id: `${key}-${page}-${row}`,
      })),
      ...(total === undefined ? {} : { total }),
      ...(page + 1 < pages ? { nextCursor: String(page + 1) } : {}),
    };
  };
  return { read, urls };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("bounded index reads", () => {
  it("returns a complete index unmarked", async () => {
    const { read, urls } = pagedIndex("plans", 3, 20, 60);
    const page = await readPages("/v1/plans", "plans", read, true);
    expect(page.rows).toHaveLength(60);
    expect(page.truncated).toBe(false);
    expect(page.total).toBe(60);
    expect(urls).toEqual([
      "/v1/plans",
      "/v1/plans?cursor=1",
      "/v1/plans?cursor=2",
    ]);
  });

  it("stops at the bound with the rows read instead of failing", async () => {
    const { read, urls } = pagedIndex("plans", 9, 20, 180);
    const page = await readPages("/v1/plans", "plans", read, true);
    expect(urls).toHaveLength(INDEX_PAGE_BOUND);
    expect(page.rows).toHaveLength(INDEX_PAGE_BOUND * 20);
    expect(page.truncated).toBe(true);
    expect(page.total).toBe(180);
  });

  it("reports no total when the Hub returned none", async () => {
    const { read } = pagedIndex("installations", 9, 100);
    const page = await readPages(
      "/v1/fleet/installations?x=1",
      "installations",
      read,
      true,
    );
    expect(page.truncated).toBe(true);
    expect(page.total).toBeNull();
  });

  it("still refuses an envelope that is not an index", async () => {
    await expect(
      readPages("/v1/plans", "plans", async () => ({}), true),
    ).rejects.toThrow("Invalid Hub index");
  });
});

describe("refresh environment", () => {
  const environments = [
    { environment: { id: "edge-a" } },
    { environment: { id: "edge-b" } },
    { environment: {} },
  ];
  it("prefers a selection the Hub returned", () => {
    expect(refreshEnvironmentId(environments, ["", "edge-b"])).toBe("edge-b");
  });
  it("falls back to the first loaded environment", () => {
    expect(refreshEnvironmentId(environments, ["gone", undefined])).toBe(
      "edge-a",
    );
    expect(refreshEnvironmentId(environments)).toBe("edge-a");
  });
  it("names no environment when none is loaded", () => {
    expect(refreshEnvironmentId([], ["dev"])).toBe("");
    expect(refreshEnvironmentId(undefined, ["dev"])).toBe("");
  });
  it("states a timeline count only for the environment that was read", () => {
    const hub = { timelineEnvironment: "edge-a", timeline: "3 entries" };
    expect(timelineSummary(hub, "edge-a")).toBe("3 entries");
    expect(timelineSummary(hub, "edge-b")).toBe("not loaded");
    expect(timelineSummary({ timelineEnvironment: "edge-a" }, "edge-a")).toBe(
      "unavailable",
    );
    expect(timelineSummary({}, "edge-a")).toBe("no environment loaded");
  });
});

function stubHub(routes: Record<string, (url: URL) => unknown>) {
  const paths: string[] = [];
  vi.stubGlobal("window", { location: { origin: "http://hub.local" } });
  vi.stubGlobal("document", {
    getElementById: () => null,
    querySelector: () => null,
    querySelectorAll: () => [],
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const request = input instanceof Request ? input : null;
      const url = new URL(
        request ? request.url : String(input),
        "http://hub.local",
      );
      paths.push(url.pathname + url.search);
      if (url.pathname.endsWith("/ListPackagingRuns"))
        return new Response(
          JSON.stringify({ configured: true, items: [], total: "0" }),
          { headers: { "content-type": "application/json" } },
        );
      const route = routes[url.pathname];
      return new Response(JSON.stringify(route ? route(url) : {}));
    }),
  );
  return paths;
}

describe("refresh past the index bound", () => {
  it("keeps the plans it read and marks the index truncated", async () => {
    const paths = stubHub({
      "/v1/plans": (url) => {
        const page = Number(url.searchParams.get("cursor") || 0);
        return {
          plans: Array.from({ length: 20 }, (_, row) => ({
            id: `plan-${page}-${row}`,
            status: "APPROVED",
          })),
          total: 240,
          nextCursor: String(page + 1),
        };
      },
      "/v1/environments": () => ({
        environments: [{ environment: { id: "edge-a" } }],
        total: 1,
      }),
    });
    const state: Record<string, any> = {};
    await createLiveHub({ state, render: () => {} }).refresh();
    expect(state.hub.planNote).toBeUndefined();
    expect(state.hub.plans).toHaveLength(100);
    expect(state.hub.truncated).toEqual({
      plans: { shown: 100, total: 240 },
    });
    expect(truncationNotices(state.hub, "s16-approvals-board")).toEqual([
      "Showing the first 100 of 240 plans. The rest are not loaded in this view.",
    ]);
    expect(paths.filter((path) => path.startsWith("/v1/plans"))).toHaveLength(
      INDEX_PAGE_BOUND,
    );
  });

  it("reads the timeline and blackout of a loaded environment, never a fixed id", async () => {
    const paths = stubHub({
      "/v1/environments": () => ({
        environments: [
          { environment: { id: "edge-a" } },
          { environment: { id: "edge-b" } },
        ],
      }),
      "/v1/environments/edge-b/timeline": () => ({ timeline: [{}, {}] }),
    });
    const state: Record<string, any> = { releaseEnvironmentId: "edge-b" };
    await createLiveHub({ state, render: () => {} }).refresh();
    expect(paths).toContain("/v1/environments/edge-b/timeline");
    expect(paths).toContain("/v1/blackouts?environment=edge-b");
    expect(
      paths.some((path) => /environments\/dev\/|environment=dev$/.test(path)),
    ).toBe(false);
    expect(state.hub.timelineEnvironment).toBe("edge-b");
    expect(state.hub.timeline).toBe("2 entries");
  });

  it("skips environment-scoped reads when no environment is loaded", async () => {
    const paths = stubHub({ "/v1/environments": () => ({ environments: [] }) });
    const state: Record<string, any> = {};
    await createLiveHub({ state, render: () => {} }).refresh();
    expect(paths.some((path) => path.includes("/timeline"))).toBe(false);
    expect(paths.some((path) => path.startsWith("/v1/blackouts"))).toBe(false);
    expect(state.hub.timelineEnvironment).toBe("");
    expect(state.hub.timeline).toBe("");
    expect(timelineSummary(state.hub, "edge-a")).toBe("no environment loaded");
  });
});

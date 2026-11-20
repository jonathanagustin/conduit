import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import {
  INDEX_LIMIT,
  expandIndexRows,
  changeIndexQuery,
  indexControlsMarkup,
  indexPage,
  indexRequest,
  readIndexPage,
  routeQuery,
  screenReads,
} from "../../console/operator/index-pages.js";
import { createSettings } from "../../console/operator/settings.js";
import { kpisFromHub } from "../../console/operator/shell.js";
import { createLiveHub } from "../../console/operator/live-hub.js";
import {
  parseScreenHash,
  planningSessionFromHash,
  screenHash,
} from "../../console/operator/planning-session-link.js";

afterEach(() => vi.unstubAllGlobals());

describe("URL-owned result windows", () => {
  it("keeps opaque cursors, filters, tab and selection through a shared link", () => {
    const query = new URLSearchParams({
      "plans.cursor": "seek:/+?=",
      "plans.search": "release / 42",
      "plans.status": "FAILED",
      tab: "plans",
      plan: "plan/999",
    });
    const hash = screenHash("area-releases", { indexQuery: query.toString() });
    expect(parseScreenHash(hash)).toEqual({
      screen: "area-releases",
      param: "",
    });
    expect(routeQuery(hash).get("plan")).toBe("plan/999");
    const url = new URL(
      indexRequest("plans", routeQuery(hash)),
      "http://hub.local",
    );
    expect(Object.fromEntries(url.searchParams)).toEqual({
      view: "summary",
      limit: "20",
      cursor: "seek:/+?=",
      search: "release / 42",
      status: "FAILED",
    });
    expect(
      planningSessionFromHash("#s01-intake/session-1?plans.search=kept"),
    ).toBe("session-1");
  });

  it("resets only the changed index cursor and selection when filtering", () => {
    const before = new URLSearchParams({
      "plans.cursor": "old",
      "plans.search": "before",
      "installations.cursor": "fleet-page",
      plan: "selected",
      environment: "edge-a",
    });
    const after = changeIndexQuery(before, "plans", {
      search: "after",
      status: "FAILED",
    });
    expect(after.get("plans.cursor")).toBeNull();
    expect(after.get("plan")).toBeNull();
    expect(after.get("plans.search")).toBe("after");
    expect(after.get("installations.cursor")).toBe("fleet-page");
    expect(after.get("environment")).toBe("edge-a");
    expect(before.get("plans.cursor")).toBe("old");
  });

  it("uses the absent previous cursor for the first page when hasPrevious is true", () => {
    const html = indexControlsMarkup(
      {
        indexPages: {
          plans: {
            shown: 20,
            total: 1000,
            hasPrevious: true,
            nextCursor: "later",
            previousCursor: "",
          },
        },
      },
      "s16-approvals-board",
      "",
      String,
    );
    expect(html).toContain("Showing 20 of 1000 plans in this page.");
    expect(html).toContain('data-index-cursor="">Previous plans');
    expect(html).not.toContain('data-index-cursor="" disabled>Previous plans');
  });

  it("refuses oversized windows and malformed cursor evidence", () => {
    expect(() =>
      indexPage(
        { plans: Array.from({ length: INDEX_LIMIT + 1 }, () => ({})) },
        "plans",
        "/v1/plans",
      ),
    ).toThrow("Invalid Hub index");
    expect(() =>
      indexPage({ plans: [], nextCursor: 1 }, "plans", "/v1/plans"),
    ).toThrow("Invalid Hub cursor");
    expect(() =>
      indexPage(
        { plans: [], nextCursor: "x".repeat(2049) },
        "plans",
        "/v1/plans",
      ),
    ).toThrow("Invalid Hub cursor");
    expect(indexPage({ plans: [] }, "plans", "/v1/plans").total).toBeNull();
  });
});

describe("large native indexes", () => {
  it("can visit all 1,000 plans and installations without fetching another page eagerly", async () => {
    const plans = Array.from({ length: 1000 }, (_, n) => ({
      id: `plan-${n}`,
      environment: "edge-a",
      status: "SUCCEEDED",
    }));
    const installations = Array.from({ length: 1000 }, (_, n) => ({
      installationId: `installation-${n}`,
      environment: "edge-a",
    }));
    const requests: string[] = [];
    let inFlight = 0;
    let maximumInFlight = 0;
    const read = async (path: string) => {
      requests.push(path);
      const url = new URL(path, "http://hub.local");
      if (url.pathname.startsWith("/v1/plans/")) {
        inFlight++;
        maximumInFlight = Math.max(maximumInFlight, inFlight);
        await Promise.resolve();
        inFlight--;
        const id = decodeURIComponent(url.pathname.slice("/v1/plans/".length));
        return {
          id,
          plan: { environment: "edge-a", steps: [] },
          approvals: [],
        };
      }
      const isPlan = url.pathname === "/v1/plans";
      if (isPlan) expect(url.searchParams.get("view")).toBe("summary");
      const cursor = url.searchParams.get("cursor") || "seek:0/+?=";
      const start = Number(/^seek:(\d+)\/\+\?=$/.exec(cursor)?.[1]);
      expect(Number.isSafeInteger(start)).toBe(true);
      expect(url.searchParams.getAll("cursor")).toHaveLength(start ? 1 : 0);
      const rows = (isPlan ? plans : installations).slice(
        start,
        start + INDEX_LIMIT,
      );
      return {
        [isPlan ? "plans" : "installations"]: rows,
        ...(isPlan ? { total: 1000 } : { summary: { total: 1000 } }),
        hasPrevious: start > 0,
        ...(start > INDEX_LIMIT
          ? { previousCursor: `seek:${start - INDEX_LIMIT}/+?=` }
          : {}),
        ...(start + INDEX_LIMIT < 1000
          ? { nextCursor: `seek:${start + INDEX_LIMIT}/+?=` }
          : {}),
      };
    };
    for (const index of ["plans", "installations"]) {
      let query = new URLSearchParams();
      const visited = new Set<string>();
      let pages = 0;
      do {
        const page = await readIndexPage(index, query, read);
        pages++;
        expect(page.rows).toHaveLength(INDEX_LIMIT);
        expect(page.total).toBe(1000);
        page.rows.forEach((row) =>
          visited.add(index === "plans" ? row.id : row.installationId),
        );
        if (pages === 1 && index === "plans") {
          expect(
            requests.filter(
              (path) =>
                new URL(path, "http://hub.local").pathname === "/v1/plans",
            ),
          ).toHaveLength(1);
          expect(
            requests.filter((path) => path.startsWith("/v1/plans/")),
          ).toHaveLength(INDEX_LIMIT);
        }
        if (!page.nextCursor) break;
        query = changeIndexQuery(query, index, { cursor: page.nextCursor });
      } while (pages < 51);
      expect(pages).toBe(50);
      expect(visited.size).toBe(1000);
    }
    expect(maximumInFlight).toBeLessThanOrEqual(4);
  });
});

function installHub(answer: (url: URL) => unknown) {
  const paths: string[] = [];
  vi.stubGlobal("window", {
    location: { origin: "http://hub.local", hash: "#area-releases?tab=plans" },
  });
  vi.stubGlobal("document", {
    getElementById: () => null,
    querySelector: () => null,
    querySelectorAll: () => [],
  });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL) => {
    const url = new URL(
      input instanceof Request ? input.url : String(input),
      "http://hub.local",
    );
    paths.push(url.pathname + url.search);
    if (
      url.pathname ===
        "/conduit.fleet.v1.FleetReadService/ListFleetInstallations" &&
      input instanceof Request
    ) {
      const request = await input.json();
      const native = new URL("/v1/fleet/installations", "http://hub.local");
      for (const [key, value] of Object.entries(request))
        native.searchParams.set(key, String(value));
      const body = (await answer(native)) as Record<string, any>;
      return new Response(
        JSON.stringify({
          generatedAt: "2026-11-20T01:00:00Z",
          maxResultAgeSeconds: "120",
          facets: {},
          limit: request.limit || 100,
          pageSize: body.installations?.length || 0,
          ...body,
        }),
        { headers: { "content-type": "application/json" } },
      );
    }
    return new Response(JSON.stringify(await answer(url)), {
      headers: { "content-type": "application/json" },
    });
  });
  return paths;
}

describe("visible-screen reads", () => {
  it("removes the previous page immediately while a new query loads", async () => {
    let release!: () => void;
    const waiting = new Promise<void>((resolve) => {
      release = resolve;
    });
    installHub(async (url) => {
      if (url.pathname === "/v1/plans") {
        if (url.searchParams.get("search")) await waiting;
        return {
          plans: [
            {
              id: url.searchParams.get("search") || "before",
              plan: { environment: "edge-a" },
            },
          ],
          total: 1,
        };
      }
      return {};
    });
    const state: Record<string, any> = { screen: "rm03-plan-detail" };
    const hub = createLiveHub({ state, render: () => {} });
    try {
      hub.ensure();
      await vi.waitFor(() => expect(state.hub?.plans?.[0]?.id).toBe("before"));
      state.indexQuery = "plans.search=after";
      hub.ensure();
      expect(state.hub.loading).toBe(true);
      expect(state.hub.plans).toBeUndefined();
      release();
      await vi.waitFor(() => expect(state.hub?.plans?.[0]?.id).toBe("after"));
    } finally {
      release();
      hub.stop();
    }
  });

  it("loads reported health and declared Plan policies for the promotion panel", async () => {
    const installation = {
      environment: "edge-a",
      installationId: "app",
      product: "app.example:api",
      observedVersion: "1.0.0",
      namespace: "",
      desiredVersion: "",
      desiredReleaseDigest: "",
      observedReleaseDigest: "",
      desiredConfigDigest: "",
      observedConfigDigest: "",
      rolloutStatus: "",
      configStatus: "",
      lifecycle: "",
      observationAgeSeconds: 0,
      stale: false,
      health: "HEALTHY",
    };
    const desiredState = {
      productSettings: [
        {
          product: "app.example:api",
          promotionPipeline: {
            stages: [
              {
                fromChannel: "DEV",
                toChannel: "STAGING",
                policy: { minimumHealthyEntities: 1 },
              },
            ],
          },
        },
      ],
    };
    const paths = installHub((url) => {
      if (url.pathname === "/v1/fleet/installations")
        return { installations: [installation], summary: { total: 1 } };
      if (url.pathname === "/v1/plans")
        return {
          plans: [{ id: "policy-plan", environment: "edge-a" }],
          total: 1,
        };
      if (url.pathname === "/v1/plans/policy-plan")
        return {
          id: "policy-plan",
          plan: { environment: "edge-a" },
          desiredState,
        };
      return {};
    });
    const state: Record<string, any> = { screen: "rm06-promotion-pipe" };
    await createLiveHub({ state, render: () => {} }).refresh();
    expect(
      paths.some(
        (path) =>
          path === "/conduit.fleet.v1.FleetReadService/ListFleetInstallations",
      ),
    ).toBe(true);
    expect(paths.some((path) => path.startsWith("/v1/plans?"))).toBe(true);
    expect(state.hub.installations).toEqual([installation]);
    expect(state.hub.plans[0].desiredState).toEqual(desiredState);
  });

  it("does not load delivery indexes for Work Intake", async () => {
    const paths = installHub(() => ({}));
    const state: Record<string, any> = { screen: "s01-intake" };
    await createLiveHub({ state, render: () => {} }).refresh();
    expect(paths).toEqual(["/v1/session", "/v1/spaces"]);
    expect(screenReads("s01-intake").size).toBe(0);
  });

  it("reads one summary page and the exact linked plan outside it", async () => {
    const paths = installHub((url) => {
      if (url.pathname === "/v1/plans")
        return {
          plans: [{ id: "plan-0", environment: "edge-a", status: "SUCCEEDED" }],
          total: 1000,
          nextCursor: "seek:20",
        };
      if (url.pathname.startsWith("/v1/plans/"))
        return {
          id: url.pathname.slice(10),
          plan: { environment: "edge-a", steps: [] },
        };
      if (url.pathname === "/v1/change-requests")
        return { changeRequests: [], total: 0 };
      return {};
    });
    const state: Record<string, any> = {
      screen: "rm03-plan-detail",
      releasePlanId: "plan-999",
      indexQuery: "plan=plan-999",
    };
    await createLiveHub({ state, render: () => {} }).refresh();
    expect(state.hub.plans.map((row: any) => row.id)).toEqual([
      "plan-0",
      "plan-999",
    ]);
    expect(state.hub.indexPages.plans.shown).toBe(1);
    expect(state.hub.indexPages.plans.selectedOutside).toBe(true);
    expect(paths.filter((path) => path.startsWith("/v1/plans?"))).toHaveLength(
      1,
    );
    expect(paths).toContain("/v1/plans/plan-999");
    expect(
      paths.some(
        (path) =>
          path.startsWith("/v1/products") ||
          path.startsWith("/v1/fleet/installations"),
      ),
    ).toBe(false);
  });

  it("retries the same failed filter and cursor window", async () => {
    let fail = true;
    const paths = installHub((url) => {
      if (url.pathname === "/v1/plans")
        return fail ? {} : { plans: [], total: 1000 };
      if (url.pathname === "/v1/change-requests") return { changeRequests: [] };
      return {};
    });
    const state: Record<string, any> = {
      screen: "rm03-plan-detail",
      indexQuery:
        "plans.cursor=exact%2Fcursor&plans.status=FAILED&plans.search=needle",
    };
    const hub = createLiveHub({ state, render: () => {} });
    await hub.refresh();
    expect(state.hub.indexPages.plans.error).toContain(
      "Retry keeps its filters and cursor",
    );
    const requested = state.hub.indexPages.plans.url;
    fail = false;
    await hub.refresh();
    expect(paths.filter((path) => path.startsWith("/v1/plans?"))).toEqual([
      requested,
      requested,
    ]);
    expect(state.hub.indexPages.plans.error).toBe("");
  });

  it("does not publish a page whose route changed while it was loading", async () => {
    let release!: () => void;
    const waiting = new Promise<void>((resolve) => {
      release = resolve;
    });
    let started!: () => void;
    const ready = new Promise<void>((resolve) => {
      started = resolve;
    });
    installHub(async (url) => {
      if (url.pathname === "/v1/plans") {
        started();
        await waiting;
        return {
          plans: [{ id: "old", plan: { environment: "old" } }],
          total: 1,
        };
      }
      return {};
    });
    const state: Record<string, any> = { screen: "rm03-plan-detail" };
    const published: string[][] = [];
    const hub = createLiveHub({
      state,
      render: () => {
        published.push((state.hub?.plans || []).map((row: any) => row.id));
      },
    });
    const old = hub.refresh();
    await ready;
    state.screen = "s01-intake";
    const current = hub.refresh();
    release();
    await Promise.all([old, current]);
    expect(state.hub.plans).toBeNull();
    expect(published.flat()).not.toContain("old");
  });
});

describe("honest page totals and lazy settings", () => {
  it("does not present unread, filtered, or partial indexes as complete header metrics", () => {
    const hub: Record<string, any> = {
      pagedIndexes: true,
      indexPages: {},
      changes: [{ status: "PENDING" }],
    };
    const metric = () =>
      kpisFromHub(hub).find((item: any) => item.id === "changes");
    expect(metric()?.live).toBe(false);
    const page = {
      shown: 1,
      total: 1000,
      nextCursor: "more",
      hasPrevious: false,
    };
    hub.indexPages.changes = page;
    expect(metric()?.live).toBe(false);
    Object.assign(page, { total: 1, nextCursor: "", filtered: true });
    expect(metric()?.live).toBe(false);
    Object.assign(page, { filtered: false, selectedOutside: true });
    expect(metric()?.live).toBe(false);
    Object.assign(page, { selectedOutside: false });
    expect(metric()).toMatchObject({ live: true, value: "1" });
  });

  it("defers data-source probes until that settings screen is opened", async () => {
    const paths = installHub(() => ({}));
    const state: Record<string, any> = {};
    const settings = createSettings({ state, render: () => {} });
    settings.extendState();
    settings.ensureLoaded();
    await vi.waitFor(() => expect(state.settingsLoaded).toBe(true));
    expect(
      paths.some(
        (path) =>
          path.startsWith("/v1/environments") ||
          path.includes("ListPackagingRuns") ||
          path === "/v1/bundle-imports",
      ),
    ).toBe(false);
    settings.renderers["adm04-data-sources"]({ innerHTML: "" });
    await vi.waitFor(() =>
      expect(paths).toContain("/v1/environments?view=summary"),
    );
    await vi.waitFor(() =>
      expect(paths.some((path) => path.includes("ListPackagingRuns"))).toBe(
        true,
      ),
    );
  });
});

it("generated Environment indexes hydrate configuration even without a report", async () => {
  const read = vi.fn(async (path: string) => ({
    environment: {
      id: decodeURIComponent(path.split("/").at(-1)!),
      labels: { region: "east" },
      namespaces: [{ name: "owned" }],
    },
  }));
  const result = await expandIndexRows(
    "environments",
    [{ environment: { id: "never-reported" } }],
    read,
    true,
  );
  expect(read).toHaveBeenCalledExactlyOnceWith(
    "/v1/environments/never-reported",
  );
  expect(result[0].environment.labels).toEqual({ region: "east" });
  expect(result[0].environment.namespaces).toEqual([{ name: "owned" }]);
  expect(result[0]).not.toHaveProperty("reported");
  await expect(
    expandIndexRows(
      "environments",
      [{ environment: { id: "expected" } }],
      async () => ({ environment: { id: "other" } }),
      true,
    ),
  ).rejects.toThrow("Environment details unavailable");
});

it("retains empty Environment wording with an exact known zero total", () => {
  const page = indexPage(
    { environments: [], total: BigInt(0) },
    "environments",
    "/v1/environments?view=summary",
  );
  expect(page.total).toBe("0");
  const markup = (total: string) =>
    indexControlsMarkup(
      { indexPages: { environments: { ...page, total } } },
      "flt-environments",
      new URLSearchParams(),
      (value: unknown) => String(value),
    );
  expect(markup("0")).toContain("Showing 0 environments in this page.");
  expect(markup("100")).toContain(
    "Showing 0 of 100 environments in this page.",
  );
});

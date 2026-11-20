import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { createLiveHub } from "../../console/operator/live-hub.js";
import {
  activeSpaceId,
  changeSpaceSelection,
  readSpaceSelection,
  spaceScopedPath,
  spaceSelectorMarkup,
} from "../../console/operator/space-scope.js";

const SPACE = `space_${"a".repeat(32)}`;
const OTHER = `space_${"b".repeat(32)}`;

describe("server Space scope", () => {
  it.each([
    "/v1/environments",
    "/v1/products",
    "/v1/plans",
    "/v1/fleet/installations",
    "/v1/fleet/upgrades",
    "/v1/change-requests",
    "/v1/promotions",
    "/v1/bundles",
    "/v1/environments/dev",
    "/v1/environments/dev/entities",
    "/v1/plans/p1",
    "/v1/change-requests/c1",
    "/v1/promotions/pro1",
    "/v1/bundles/b1",
    "/v1/products/com.example%3Aapp/releases",
    "/v1/products/com.example%3Aapp/releases/1.0.0",
  ])("carries persisted selection on %s", (path) => {
    expect(
      new URL(
        spaceScopedPath(path, SPACE),
        "http://hub.local",
      ).searchParams.get("spaceId"),
    ).toBe(SPACE);
    const scoped = spaceScopedPath(`${path}?search=hello&cursor=opaque`, SPACE);
    const query = new URL(scoped, "http://hub.local").searchParams;
    expect(query.get("search")).toBe("hello");
    expect(query.get("cursor")).toBe("opaque");
    expect(query.getAll("spaceId")).toEqual([SPACE]);
    expect(spaceScopedPath(path, "")).toBe(path);
  });

  it.each([
    "/v1/channel-subscriptions",
    "/v1/packaging-runs",
    "/v1/rollouts",
    "/v1/dev-workspaces",
    "/v1/authorization-audit",
    "/v1/environments/dev/timeline",
  ])("leaves explicit unscoped context %s alone", (path) => {
    expect(spaceScopedPath(path, SPACE)).toBe(path);
  });

  it("retains missing and invalid remembered selections until explicitly cleared", () => {
    expect(activeSpaceId(SPACE)).toBe(SPACE);
    expect(activeSpaceId(OTHER)).toBe(OTHER);
    expect(activeSpaceId("invalid")).toBe("invalid");
    expect(
      readSpaceSelection({
        getItem: () => "invalid",
        length: 1,
        clear: vi.fn(),
        key: vi.fn(),
        removeItem: vi.fn(),
        setItem: vi.fn(),
      }),
    ).toBe("invalid");
    const html = spaceSelectorMarkup(
      { spaces: [], spaceNote: "Unavailable" },
      OTHER,
    );
    expect(html).toContain(`value="${OTHER}" selected`);
    expect(html).toContain("SPACE UNAVAILABLE");
    expect(html).not.toContain('value="" selected');
    expect(html).not.toContain('aria-label="Space" disabled');
    expect(html).toContain('<option value="">All Spaces</option>');
  });

  it("discloses scoped and global contexts without changing semantic styles", () => {
    const html = spaceSelectorMarkup(
      { spaces: [{ id: SPACE, name: 'a"<b>' }], pagedIndexes: true },
      SPACE,
    );
    expect(html).toContain("a&quot;&lt;b&gt;");
    expect(html).toContain(
      "Scoped by the Hub: environments, products, promotions, bundles",
    );
    expect(html).toContain(
      "Not Space-scoped: channels, packaging, workspaces, rollouts, audit",
    );
  });

  it("resets all affected cursors and linked selections while preserving independent filters and planning state", () => {
    const query = new URLSearchParams({
      space: SPACE,
      tab: "plan",
      "plans.search": "app",
      "products.search": "product",
      plan: "p1",
      change: "c1",
      environment: "dev",
      session: "planning-session",
    });
    for (const index of [
      "environments",
      "products",
      "plans",
      "installations",
      "changes",
      "promotions",
    ])
      query.set(`${index}.cursor`, `old-${index}`);
    const state: any = {
      indexQuery: query.toString(),
      spaceId: SPACE,
      releaseEnvironmentId: "dev",
      envDetailId: "dev",
      releasePlanId: "p1",
      crReview: { selectedId: "c1", draft: { rationale: "old" } },
    };
    changeSpaceSelection(state, OTHER);
    const next = new URLSearchParams(state.indexQuery);
    expect([...next.keys()].filter((key) => key.endsWith(".cursor"))).toEqual(
      [],
    );
    expect(next.get("space")).toBe(OTHER);
    expect(next.get("plans.search")).toBe("app");
    expect(next.get("products.search")).toBe("product");
    expect(next.get("session")).toBe("planning-session");
    expect(next.get("tab")).toBe("plan");
    for (const key of ["plan", "change", "environment"])
      expect(next.has(key)).toBe(false);
    expect(state.releasePlanId).toBe("");
    expect(state.crReview.selectedId).toBe("");
  });
});

describe("Space-scoped refresh", () => {
  afterEach(() => vi.unstubAllGlobals());
  function install(spacesAvailable = true) {
    const requests = Object.assign([] as URL[], {
      fleetBodies: [] as Record<string, unknown>[],
      environmentBodies: [] as Record<string, unknown>[],
    });
    vi.stubGlobal("window", { location: { origin: "http://hub.local" } });
    vi.stubGlobal("document", {
      getElementById: () => null,
      querySelector: () => null,
      querySelectorAll: () => [],
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const url = new URL(
          input instanceof Request ? input.url : String(input),
          "http://hub.local",
        );
        requests.push(url);
        const json = (body: unknown, status = 200) =>
          new Response(JSON.stringify(body), { status });
        if (
          url.pathname ===
            "/conduit.fleet.v1.FleetReadService/ListFleetInstallations" &&
          input instanceof Request
        ) {
          expect(input.method).toBe("POST");
          const body = await input.json();
          requests.fleetBodies.push(body);
          if (body.spaceId && body.spaceId !== SPACE)
            return new Response(
              JSON.stringify({
                code: "permission_denied",
                message: "unknown or denied Space",
              }),
              { status: 403, headers: { "content-type": "application/json" } },
            );
          return new Response(
            JSON.stringify({
              generatedAt: "2026-11-20T01:00:00Z",
              maxResultAgeSeconds: "120",
              installations: [
                { environment: "later-environment", installationId: "app" },
              ],
              summary: { total: "1" },
              facets: {},
              pageSize: 1,
              limit: body.limit || 100,
            }),
            { headers: { "content-type": "application/json" } },
          );
        }
        if (
          url.pathname ===
            "/conduit.environments.v1.EnvironmentReadService/ListEnvironmentSummaries" &&
          input instanceof Request
        ) {
          expect(input.method).toBe("POST");
          const body = await input.json();
          requests.environmentBodies.push(body);
          if (body.spaceId && body.spaceId !== SPACE)
            return new Response(
              JSON.stringify({
                code: "permission_denied",
                message: "unknown or denied Space",
              }),
              { status: 403, headers: { "content-type": "application/json" } },
            );
          return new Response(
            JSON.stringify({
              environments: [
                {
                  environment: { id: "page-first" },
                  spoke: { state: "UNREACHABLE" },
                },
              ],
              total: "40",
              pageSize: 1,
              limit: body.limit ?? 100,
              nextCursor: "next-environments",
            }),
            { headers: { "content-type": "application/json" } },
          );
        }
        if (url.pathname === "/v1/spaces")
          return spacesAvailable
            ? json({ spaces: [{ id: SPACE }] })
            : json({ error: "unavailable" }, 503);
        const selected = url.searchParams.get("spaceId");
        if (selected && selected !== SPACE)
          return json({ error: "unknown or denied Space" }, 403);
        if (url.pathname === "/v1/environments")
          return json({
            environments: [
              { environment: { id: "page-first" }, reported: { entities: [] } },
            ],
            total: 40,
            nextCursor: "next-environments",
          });
        if (url.pathname === "/v1/environments/linked")
          return json({ error: "outside selected Space" }, 404);
        // This Plan's Environment is deliberately NOT in the loaded Environment
        // page. Persisted server membership, never that page, decides inclusion.
        if (url.pathname === "/v1/plans")
          return json({
            plans: [{ id: "p1", plan: { environment: "later-environment" } }],
            total: 1,
          });
        if (url.pathname === "/v1/fleet/installations")
          return json({
            installations: [{ environment: "later-environment" }],
            summary: { total: 1 },
          });
        if (url.pathname === "/v1/products")
          return json({ products: [], total: 0 });
        if (url.pathname === "/v1/change-requests")
          return json({ changeRequests: [], total: 0 });
        return json({});
      }),
    );
    return requests;
  }

  it("carries selected Space on the live upgrade refresh", async () => {
    const requests = install();
    const state: any = {
      spaceId: SPACE,
      screen: "s17-spokes-audit",
      indexQuery: `space=${SPACE}`,
    };
    await createLiveHub({ state, render: () => {} }).refresh();
    expect(
      requests
        .find((url) => url.pathname === "/v1/fleet/upgrades")
        ?.searchParams.getAll("spaceId"),
    ).toEqual([SPACE]);
  });

  it("asks the server for selected indexes without post-filtering a loaded page", async () => {
    const requests = install();
    const state: any = {
      spaceId: SPACE,
      screen: "s14-env-detail",
      indexQuery: `space=${SPACE}`,
    };
    await createLiveHub({ state, render: () => {} }).refresh();
    for (const path of ["/v1/plans", "/v1/products"]) {
      const request = requests.find((url) => url.pathname === path);
      expect(request?.searchParams.get("spaceId"), path).toBe(SPACE);
    }
    expect(requests.fleetBodies).toEqual([{ spaceId: SPACE, limit: 20 }]);
    expect(requests.environmentBodies).toEqual([{ spaceId: SPACE, limit: 20 }]);
    expect(requests.some((url) => url.pathname === "/v1/environments")).toBe(
      false,
    );
    expect(
      requests.some((url) => url.pathname === "/v1/fleet/installations"),
    ).toBe(false);
    expect(state.hub.plans[0].plan.environment).toBe("later-environment");
    expect(state.hub.installations).toHaveLength(1);
    expect(state.hub.indexPages.environments.total).toBe("40");
  });

  it.each([false, true])(
    "never widens a missing remembered Space when Space listing succeeds=%s",
    async (listed) => {
      const requests = install(listed);
      const state: any = {
        spaceId: OTHER,
        screen: "flt-environments",
        indexQuery: `space=${OTHER}`,
      };
      await createLiveHub({ state, render: () => {} }).refresh();
      expect(requests.environmentBodies.length).toBeGreaterThan(0);
      expect(requests.fleetBodies.length).toBeGreaterThan(0);
      expect(
        [...requests.environmentBodies, ...requests.fleetBodies].every(
          (body) => body.spaceId === OTHER,
        ),
      ).toBe(true);
      expect(
        requests.some((url) =>
          ["/v1/environments", "/v1/fleet/installations"].includes(
            url.pathname,
          ),
        ),
      ).toBe(false);
      expect(state.hub.environments).toEqual([]);
      expect(state.hub.indexPages.environments.error).toContain(
        "did not return",
      );
      expect(state.spaceId).toBe(OTHER);
    },
  );

  it("sends the selection on linked reads and does not substitute another Environment", async () => {
    const requests = install();
    const state: any = {
      spaceId: SPACE,
      screen: "s14-env-detail",
      releaseEnvironmentId: "linked",
      envDetailId: "linked",
      indexQuery: `space=${SPACE}&environment=linked`,
    };
    await createLiveHub({ state, render: () => {} }).refresh();
    expect(
      requests
        .find((url) => url.pathname === "/v1/environments/linked")
        ?.searchParams.get("spaceId"),
    ).toBe(SPACE);
    expect(
      state.hub.environments.some(
        (row: any) => row.environment.id === "linked",
      ),
    ).toBe(false);
    expect(state.hub.timelineEnvironment).toBe("");
    expect(
      requests.some(
        (url) =>
          url.pathname.endsWith("/timeline") ||
          url.pathname === "/v1/blackouts",
      ),
    ).toBe(false);
  });
});

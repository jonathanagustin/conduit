import { afterEach, expect, it, vi } from "vite-plus/test";
import { createLiveHub } from "../../console/operator/live-hub.js";
import { mountSpaceSelector } from "../../console/operator/space-scope.js";
import { indexPage } from "../../console/operator/index-pages.js";

afterEach(() => vi.unstubAllGlobals());

it("reads the fleet total from the native authorized summary envelope", () => {
  const page = indexPage(
    {
      installations: Array.from({ length: 20 }, (_, n) => ({
        installationId: `i-${n}`,
      })),
      summary: { total: 1000 },
      total: 0,
      nextCursor: "more",
      pageSize: 20,
      limit: 20,
      hasPrevious: false,
    },
    "installations",
    "/v1/fleet/installations?limit=20",
  );
  expect(page.total).toBe(1000);
  expect(
    indexPage(
      { installations: [], summary: { total: 0 } },
      "installations",
      "/v1/fleet/installations",
    ).total,
  ).toBe(0);
  expect(
    indexPage(
      { installations: [], total: 1000 },
      "installations",
      "/v1/fleet/installations",
    ).total,
  ).toBeNull();
});

function install(answer: (url: URL) => unknown) {
  const requests: URL[] = [];
  vi.stubGlobal("window", {
    location: { origin: "http://hub.local", hash: "" },
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
    requests.push(url);
    return new Response(JSON.stringify(await answer(url)), {
      headers: { "content-type": "application/json" },
    });
  });
  return requests;
}

it.each([true, false])(
  "scopes timeline and blackout to an authorized linked Environment (available=%s)",
  async (available) => {
    const requests = install((url) => {
      if (url.pathname === "/v1/session") return {};
      if (url.pathname === "/v1/spaces") return { spaces: [] };
      if (url.pathname === "/v1/environments")
        return {
          environments: [
            { environment: { id: "page-first" }, reported: { entities: [] } },
          ],
          total: 1000,
          nextCursor: "more",
        };
      if (url.pathname === "/v1/environments/linked-999")
        return available
          ? { environment: { id: "linked-999" }, reported: { entities: [] } }
          : { error: "unavailable" };
      if (url.pathname === "/v1/products") return { products: [], total: 0 };
      if (url.pathname === "/v1/plans") return { plans: [], total: 0 };
      if (url.pathname === "/v1/fleet/installations")
        return { installations: [], summary: { total: 0 } };
      if (url.pathname.endsWith("/timeline")) return { timeline: [] };
      if (url.pathname === "/v1/blackouts")
        return { blocked: true, reason: url.searchParams.get("environment") };
      return {};
    });
    const state: any = {
      screen: "s14-env-detail",
      releaseEnvironmentId: "linked-999",
      envDetailId: "linked-999",
      indexQuery: "environment=linked-999",
    };
    await createLiveHub({ state, render: () => {} }).refresh();
    if (!available) {
      expect(state.hub.indexPages.environments.selectionError).toContain(
        "unavailable",
      );
      expect(state.hub.timelineEnvironment).toBe("");
      expect(
        requests.some(
          (url) =>
            url.pathname.endsWith("/timeline") ||
            url.pathname === "/v1/blackouts",
        ),
      ).toBe(false);
      return;
    }
    expect(
      state.hub.environments.some(
        (row: any) => row.environment.id === "linked-999",
      ),
    ).toBe(true);
    expect(requests.map((url) => url.pathname)).toContain(
      "/v1/environments/linked-999/timeline",
    );
    expect(state.hub.timelineEnvironment).toBe("linked-999");
    expect(state.hub.blackoutReason).toBe("linked-999");
  },
);

it("resets the environment cursor when the actual Space selector changes", async () => {
  const first = "space_" + "a".repeat(32),
    second = "space_" + "b".repeat(32);
  const requests = install((url) => {
    if (url.pathname === "/v1/spaces")
      return { spaces: [{ id: first }, { id: second }] };
    if (url.pathname === "/v1/environments")
      return { environments: [], total: 0 };
    if (url.pathname === "/v1/fleet/installations")
      return { installations: [], summary: { total: 0 } };
    return {};
  });
  const state: any = {
    screen: "flt-environments",
    spaceId: first,
    releaseEnvironmentId: "old-space-environment",
    envDetailId: "old-space-environment",
    indexQuery: `space=${first}&environments.cursor=after-old-space-key&environment=old-space-environment&installations.environment=old-space-environment&installations.cursor=old-installations&plans.cursor=keep-independent-plan-page`,
  };
  const hub = createLiveHub({ state, render: () => {} });
  await hub.refresh();
  let changed!: (event: any) => void;
  const anchor = { parentElement: { insertBefore: () => {} } };
  const host: any = {
    dataset: {},
    addEventListener: (_name: string, handler: (event: any) => void) => {
      changed = handler;
    },
  };
  const doc = {
    getElementById: (id: string) =>
      id === "example-data-badge" ? anchor : null,
    createElement: () => host,
  };
  mountSpaceSelector(doc, state, () => hub.refresh());
  changed({ target: { id: "space-selector", value: second } });
  await vi.waitFor(() =>
    expect(
      requests.some(
        (url) =>
          url.pathname === "/v1/environments" &&
          url.searchParams.get("spaceId") === second,
      ),
    ).toBe(true),
  );
  const next = requests.find(
    (url) =>
      url.pathname === "/v1/environments" &&
      url.searchParams.get("spaceId") === second,
  )!;
  expect(next.searchParams.has("cursor")).toBe(false);
  expect(state.releaseEnvironmentId).toBe("");
  expect(state.envDetailId).toBe("");
  expect(new URLSearchParams(state.indexQuery).has("environment")).toBe(false);
  expect(new URLSearchParams(state.indexQuery).get("space")).toBe(second);
  expect(new URLSearchParams(state.indexQuery).has("plans.cursor")).toBe(false);
  expect(
    new URLSearchParams(state.indexQuery).has("installations.environment"),
  ).toBe(false);
  expect(
    new URLSearchParams(state.indexQuery).has("installations.cursor"),
  ).toBe(false);
});

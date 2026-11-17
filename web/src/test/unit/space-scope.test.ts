import { afterEach, describe, expect, it, vi } from "vite-plus/test";

import { createLiveHub } from "../../console/operator/live-hub.js";
import {
  activeSpaceId,
  applySpaceScope,
  spaceScopedPath,
  spaceSelectorMarkup,
} from "../../console/operator/space-scope.js";

const SPACE = `space_${"a".repeat(32)}`;
const OTHER = `space_${"b".repeat(32)}`;

describe("space scope", () => {
  it("sends spaceId only to the index route that reads it", () => {
    expect(spaceScopedPath("/v1/environments", SPACE)).toBe(
      `/v1/environments?spaceId=${SPACE}`,
    );
    expect(spaceScopedPath("/v1/environments?view=summary", SPACE)).toBe(
      `/v1/environments?view=summary&spaceId=${SPACE}`,
    );
    expect(spaceScopedPath("/v1/plans", SPACE)).toBe("/v1/plans");
    expect(spaceScopedPath("/v1/environments/dev/timeline", SPACE)).toBe(
      "/v1/environments/dev/timeline",
    );
    expect(spaceScopedPath("/v1/environments", "")).toBe("/v1/environments");
  });

  it("scopes nothing for a Space the Hub does not list", () => {
    const spaces = [{ id: SPACE, name: "team-a" }];
    expect(activeSpaceId(SPACE, spaces)).toBe(SPACE);
    expect(activeSpaceId(OTHER, spaces)).toBe("");
    expect(activeSpaceId("team-a", spaces)).toBe("");
    expect(activeSpaceId(SPACE, [])).toBe("");
  });

  it("narrows only rows that name an environment, and leaves the rest whole", () => {
    const next = applySpaceScope(
      {
        environments: [{ environment: { id: "dev" } }],
        installations: [{ environment: "dev" }, { environment: "prod" }],
        plans: [
          { id: "p1", plan: { environment: "dev" } },
          { id: "p2", plan: { environment: "prod" } },
        ],
        changes: [{ id: "c1", environment: "prod" }],
        products: [{ product: "a" }, { product: "b" }],
        promotions: [{ id: "pro" }],
      },
      SPACE,
    );
    expect(next.installations).toEqual([{ environment: "dev" }]);
    expect(next.plans.map((row: { id: string }) => row.id)).toEqual(["p1"]);
    expect(next.changes).toEqual([]);
    expect(next.products).toHaveLength(2);
    expect(next.promotions).toHaveLength(1);
    expect(next.spaceScope).toEqual({ spaceId: SPACE, environments: 1 });
  });

  it("names what is and is not scoped, escaped", () => {
    const html = spaceSelectorMarkup(
      { spaces: [{ id: SPACE, name: 'a"<b>' }] },
      SPACE,
    );
    expect(html).toContain("a&quot;&lt;b&gt;");
    expect(html).toContain("data-space-scope");
    expect(html).toContain("Not scoped by the Hub API: products");
    expect(spaceSelectorMarkup({ spaces: [] }, "")).toContain("disabled");
    expect(
      spaceSelectorMarkup(
        { spaces: [], spaceNote: "Hub did not return Spaces" },
        "",
      ),
    ).toContain("Unavailable");
  });
});

describe("space-scoped refresh", () => {
  afterEach(() => vi.unstubAllGlobals());

  function stub(paths: string[]) {
    vi.stubGlobal("window", { location: { origin: "http://hub.local" } });
    vi.stubGlobal("document", {
      getElementById: () => null,
      querySelector: () => null,
      querySelectorAll: () => [],
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = input instanceof Request ? input.url : String(input);
        paths.push(path);
        const json = (body: unknown) =>
          new Response(JSON.stringify(body), { status: 200 });
        if (path === "/v1/spaces")
          return json({ spaces: [{ id: SPACE, name: "team-a" }] });
        if (path.startsWith("/v1/environments?spaceId="))
          return json({ environments: [{ environment: { id: "dev" } }] });
        if (path === "/v1/environments")
          return json({
            environments: [
              { environment: { id: "dev" } },
              { environment: { id: "prod" } },
            ],
          });
        if (path === "/v1/fleet/installations")
          return json({
            installations: [{ environment: "dev" }, { environment: "prod" }],
          });
        if (path === "/v1/plans")
          return json({
            plans: [
              { id: "p1", plan: { environment: "dev" } },
              { id: "p2", plan: { environment: "prod" } },
            ],
          });
        return json({});
      }),
    );
  }

  it("asks the Hub for the Space's environments and narrows the rows that name them", async () => {
    const paths: string[] = [];
    stub(paths);
    const state: Record<string, any> = { spaceId: SPACE };
    await createLiveHub({ state, render: () => {} }).refresh();
    expect(paths).toContain(`/v1/environments?spaceId=${SPACE}`);
    expect(paths).not.toContain("/v1/environments");
    expect(paths).toContain("/v1/plans");
    expect(state.hub.plans.map((row: { id: string }) => row.id)).toEqual([
      "p1",
    ]);
    expect(state.hub.installations).toEqual([{ environment: "dev" }]);
    expect(state.hub.spaceScope).toEqual({ spaceId: SPACE, environments: 1 });
  });

  it("reads everything unscoped when no Space is selected", async () => {
    const paths: string[] = [];
    stub(paths);
    const state: Record<string, any> = { spaceId: "" };
    await createLiveHub({ state, render: () => {} }).refresh();
    expect(paths).toContain("/v1/environments");
    expect(state.hub.plans).toHaveLength(2);
    expect(state.hub.spaceScope).toBeUndefined();
  });
});

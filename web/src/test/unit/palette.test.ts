// @vitest-environment node
import { describe, expect, it } from "vitest";

import {
  filterPalette,
  highlight,
  paletteItems,
  paletteResultsMarkup,
} from "../../console/operator/palette.js";

const screens = [
  {
    id: "pkg-runs",
    short: "Packaging runs",
    subtitle: "RUNS",
    parentLabel: "Packaging",
  },
];
const hub = {
  environments: [{ environment: { id: "dev", tier: "dev" } }],
  products: [{ product: "sample-app", releases: [{}, {}] }],
  packaging: [
    { runId: "sample-app-1", phase: "blocked", outcome: "TIMED_OUT" },
  ],
  changes: [{ id: "cr-1", title: "Raise memory", status: "PENDING_APPROVALS" }],
};

describe("command palette", () => {
  it("lists views, Hub objects and actions", () => {
    const items = paletteItems(screens, hub);
    const objects = items.filter((i) => i.category === "Objects");
    expect(objects.map((o) => o.title)).toEqual([
      "dev",
      "sample-app",
      "sample-app-1",
      "Raise memory",
    ]);
    expect(objects[0].target).toEqual({
      screen: "flt-environments",
      env: "dev",
    });
    expect((objects[3].target as { change?: string }).change).toBe("cr-1");
    expect(items.some((i) => i.category === "Actions")).toBe(true);
  });

  it("has no objects when the Hub returned none", () => {
    const items = paletteItems(screens, undefined);
    expect(items.filter((i) => i.category === "Objects")).toEqual([]);
  });

  it("filters by category and query", () => {
    const items = paletteItems(screens, hub);
    expect(
      filterPalette(items, "sample", "Objects").map((i) => i.title),
    ).toEqual(["sample-app", "sample-app-1"]);
    expect(filterPalette(items, "sample", "Views")).toEqual([]);
    expect(filterPalette(items, "", "Actions").length).toBe(3);
  });

  it("highlights the match and escapes the rest", () => {
    expect(highlight("<a>dev", "DEV")).toBe("&lt;a&gt;<mark>dev</mark>");
    expect(highlight("x", "")).toBe("x");
  });

  it("marks the selected row and says when nothing matches", () => {
    const items = paletteItems(screens, hub);
    const html = paletteResultsMarkup(items.slice(0, 2), { selected: 1 });
    expect(html).toContain('aria-selected="true"');
    expect(html).toContain("pickPaletteItem(1)");
    expect(paletteResultsMarkup([], { query: "<q>" })).toContain(
      'Nothing matches "&lt;q&gt;"',
    );
  });
});

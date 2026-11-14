// @vitest-environment node
import { describe, expect, it } from "vitest";

import {
  applyFacets,
  countBy,
  FACETS,
  facetsMarkup,
} from "../../console/operator/env-facets.js";

const rows = [
  {
    environment: { id: "dev", maintenance: {} },
    spoke: { state: "connected", agentId: "a1" },
    reported: {
      entities: [{ health: "HEALTHY", rolloutStatus: "UP_TO_DATE" }],
    },
  },
  {
    environment: { id: "prod-<1>", maintenance: { window: "02:00-05:00" } },
    spoke: { state: "stale" },
    reported: {
      entities: [
        { health: "HEALTHY", rolloutStatus: "UP_TO_DATE" },
        { health: "DEGRADED", rolloutStatus: "BEHIND" },
      ],
    },
  },
  { environment: { id: "edge" }, spoke: { state: "stale" } },
];

const facet = (key: string) => FACETS.find((item) => item.key === key)!;

describe("environment facets", () => {
  it("counts values largest first", () => {
    expect(countBy(rows, facet("spoke").value)).toEqual([
      { group: "stale", count: 2 },
      { group: "connected", count: 1 },
    ]);
  });

  it("reads the worst health, rollout, and maintenance", () => {
    expect(rows.map(facet("health").value)).toEqual([
      "HEALTHY",
      "DEGRADED",
      "No report",
    ]);
    expect(rows.map(facet("rollout").value)).toEqual([
      "Up to date",
      "Behind",
      "No report",
    ]);
    expect(rows.map(facet("maintenance").value)).toEqual([
      "No window",
      "Window set",
      "No window",
    ]);
  });

  it("filters on every selected value", () => {
    expect(
      applyFacets(rows, { spoke: "stale" }).map((row) => row.environment.id),
    ).toEqual(["prod-<1>", "edge"]);
    expect(
      applyFacets(rows, { spoke: "stale", health: "DEGRADED" }),
    ).toHaveLength(1);
    expect(applyFacets(rows, {})).toHaveLength(3);
  });

  it("renders escaped rows, an empty match, and an empty Hub", () => {
    const html = facetsMarkup(rows, { spoke: "stale" });
    expect(html).toContain("prod-&lt;1&gt;");
    expect(html).toContain("2 of 3");
    expect(html).toContain('aria-pressed="true"');
    expect(facetsMarkup(rows, { spoke: "gone" })).toContain(
      "No environment matches",
    );
    expect(facetsMarkup([], {})).toContain("The Hub returned no environments.");
  });
});

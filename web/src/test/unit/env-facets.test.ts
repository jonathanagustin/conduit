// @vitest-environment node
import { describe, expect, it } from "vitest";

import {
  applyFacets,
  countBy,
  FACETS,
  envDetailMarkup,
  facetsMarkup,
  installationDrift,
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

describe("environment detail", () => {
  const row = {
    environment: { id: "prod-east", maintenance: { window: "02:00-05:00" } },
    spoke: { state: "connected" },
    reported: {
      observedAt: "2026-11-14T10:00:00Z",
      entities: [
        {
          product: "atlas",
          version: "2.8.2",
          namespace: "atlas",
          health: "DEGRADED",
          rolloutStatus: "BEHIND",
          configStatus: "IN_SYNC",
        },
      ],
    },
  };
  const installations = [
    {
      environment: "prod-east",
      product: "atlas",
      desiredVersion: "2.8.4",
      observedVersion: "2.8.2",
      observedAt: "2026-11-14T10:00:00Z",
    },
    { environment: "dev", product: "atlas", desiredVersion: "2.8.4" },
  ];

  it("classifies an installation from its own desired and observed fields", () => {
    expect(installationDrift(installations[0])).toBe("Drifted");
    expect(installationDrift(installations[1])).toBe("No report");
    expect(
      installationDrift({ desiredVersion: "1", observedVersion: "1" }),
    ).toBe("In sync");
  });

  it("shows only this environment's installations and counts its drift", () => {
    const html = envDetailMarkup(row, installations);
    expect(html).toContain("data-env-back");
    expect(html).toContain("1 of 1 drifted");
    expect(html).toContain("2.8.4");
    expect(
      html.match(/<tbody>[\s\S]*<\/tbody>/)?.[0].match(/<tr>/g),
    ).toHaveLength(1);
  });

  it("lists reported workloads and the maintenance spec on their tabs", () => {
    expect(envDetailMarkup(row, [], { tab: "workloads" })).toContain(
      "DEGRADED",
    );
    expect(envDetailMarkup(row, [], { tab: "constraints" })).toContain(
      "02:00-05:00",
    );
  });

  it("says so when the environment has left the list", () => {
    expect(envDetailMarkup(undefined, [])).toContain("no longer in the Hub");
  });

  it("opens the detail from the list", () => {
    expect(facetsMarkup([row], {})).toContain('data-env-open="prod-east"');
  });
});

describe("zero-time reports", () => {
  it("treats Go's zero time as never observed", () => {
    expect(
      installationDrift({
        desiredVersion: "0.1.0",
        observedAt: "0001-01-01T00:00:00Z",
      }),
    ).toBe("No report");
  });
});

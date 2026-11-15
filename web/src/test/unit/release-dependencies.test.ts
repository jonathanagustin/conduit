import { expect, it } from "vitest";
import {
  catalogCoordinates,
  potentialDependents,
  dependencyMarkup,
  upgradeLagMarkup,
} from "../../console/operator/release-dependencies.js";
it("bounds and deduplicates visible catalog coordinates", () => {
  const result = catalogCoordinates([
    { product: "a", releases: [{ version: "1" }, { version: "1" }] },
    {
      product: "b",
      releases: Array.from({ length: 40 }, (_, i) => ({ version: String(i) })),
    },
  ]);
  expect(result.rows).toHaveLength(32);
  expect(result.truncated).toBe(true);
  expect(result.rows.filter((row) => row.product === "a")).toHaveLength(1);
});
it("computes bounded potential reachability through optional edges and cycles without resolving ranges", () => {
  const records = [
    { product: "a", dependencies: [{ product: "b", optional: false }] },
    { product: "c", dependencies: [{ product: "a", optional: true }] },
    { product: "b", dependencies: [{ product: "c" }] },
    { product: "unavailable", dependencies: null },
  ];
  expect(potentialDependents(records, "b")).toEqual(["a", "c"]);
  expect(potentialDependents(records, "absent")).toEqual([]);
});
it("escapes constraint graph data and preserves unknown selected and empty metadata limits", () => {
  const rows = [
    {
      id: "root",
      product: "<root>",
      version: "1",
      dependencies: [
        {
          product: "<dependency>",
          minimumVersion: "1",
          maximumVersion: "2",
          optional: true,
        },
      ],
    },
  ];
  const html = dependencyMarkup(rows);
  expect(html).toContain("&lt;dependency&gt;");
  expect(html).toContain("stroke-dasharray");
  expect(html).toContain("does not resolve version ranges");
  expect(dependencyMarkup(rows, "missing")).toContain("No selected release");
  expect(dependencyMarkup([{ ...rows[0], dependencies: [] }])).toContain(
    "does not distinguish an empty dependency set",
  );
  expect(dependencyMarkup([{ ...rows[0], dependencies: null }])).toContain(
    "metadata is unavailable",
  );
});
it("does not turn missing upgrade evidence into an empty fleet or invent causes", () => {
  expect(upgradeLagMarkup([], false)).toContain("unavailable");
  expect(upgradeLagMarkup([], true)).toContain("no version-lag rows");
  expect(
    upgradeLagMarkup(
      [{ environment: "prod", product: "a", reason: "<recorded>" }],
      true,
    ),
  ).toContain("&lt;recorded&gt;");
});

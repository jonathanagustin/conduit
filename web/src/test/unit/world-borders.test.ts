// @vitest-environment node
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { expect, it } from "vitest";
import source from "../../console/operator/assets/world-borders-110m.source.json";

it("keeps the reviewed static basemap bytes and numeric geometry bounded", () => {
  const bytes = readFileSync(
    new URL(
      "../../console/operator/assets/world-borders-110m.json",
      import.meta.url,
    ),
  );
  expect(createHash("sha256").update(bytes).digest("hex")).toBe(
    source.outputSha256,
  );
  const rings = JSON.parse(bytes.toString()) as number[][][];
  expect(rings.length).toBe(source.rings);
  expect(rings.reduce((sum, ring) => sum + ring.length, 0)).toBe(source.points);
  for (const ring of rings)
    for (const point of ring) {
      expect(point).toHaveLength(2);
      expect(
        Number.isFinite(point[0]) && point[0]! >= -180 && point[0]! <= 180,
      ).toBe(true);
      expect(
        Number.isFinite(point[1]) && point[1]! >= -90 && point[1]! <= 90,
      ).toBe(true);
    }
});

import {
  visibleAuditResources,
  scopedNotificationEvents,
} from "../../console/operator/audit-event-stream.js";
// @vitest-environment node
import { describe, it, expect } from "vite-plus/test";
import { readFileSync } from "node:fs";
import {
  proofDigest,
  verifyProjection,
} from "../../console/operator/audit-proof.mjs";
import { modelProvenance } from "../../console/operator/audit-ledger.js";
const fixture = JSON.parse(
  readFileSync(
    new URL(
      "../../../../internal/governance/domain/testdata/audit-projection.json",
      import.meta.url,
    ),
    "utf8",
  ),
);
describe("native audit projection digest verification", () => {
  it("recomputes native Go fixture digests including UTF-8 and uint64 length framing", async () => {
    expect(await proofDigest(fixture[0])).toBe(fixture[0].digest);
    expect(await verifyProjection(fixture)).toMatchObject({
      ok: true,
      checked: 2,
    });
  });
  it("rejects altered payload bytes, displayed identity, resource, origin and digest", async () => {
    for (const mutation of [
      {
        payload: Buffer.from(
          Buffer.from(fixture[0].payload, "base64").toString("utf8") + " ",
        ).toString("base64"),
      },
      { actor: "forged" },
      { resourceRid: "environment/other" },
      { origin: "LEGACY_IMPORT" },
      { digest: "f".repeat(64) },
      { payload: "not-base64" },
    ])
      expect(
        (await verifyProjection([{ ...fixture[0], ...mutation }, fixture[1]]))
          .ok,
      ).toBe(false);
  });
  it("does not verify an empty projection, a missing prefix or an inexact sequence", async () => {
    expect((await verifyProjection([])).ok).toBe(false);
    expect((await verifyProjection([fixture[1]])).ok).toBe(false);
    expect(
      (await verifyProjection([{ ...fixture[0], sequence: 2 ** 53 }])).ok,
    ).toBe(false);
  });
});

it("does not infer a live model call from adapter configuration or hashes", () => {
  expect(modelProvenance(fixture[0])).toMatchObject({
    live: "no",
    input: "a".repeat(64),
    output: "b".repeat(64),
  });
  expect(
    modelProvenance({
      context: { adapter: "configured-cli", promptSha256: "a".repeat(64) },
    }).live,
  ).toBe("unknown");
  expect(modelProvenance({ context: { liveModelCall: true } }).live).toBe(
    "yes",
  );
  expect(
    modelProvenance({ context: { liveModelCall: false, promptSha256: "bad" } }),
  ).toMatchObject({ live: "no", input: "unavailable" });
});

it("takes provenance from digest-covered bytes when they are returned", () => {
  expect(
    modelProvenance({
      ...fixture[0],
      context: { liveModelCall: true, adapter: "forged" },
    }).live,
  ).toBe("no");
  expect(
    modelProvenance({ payload: "invalid", context: { liveModelCall: true } })
      .live,
  ).toBe("unknown");
});

it("restricts event pages to the selected resource and bounds visible scopes", () => {
  expect(visibleAuditResources([...fixture, ...fixture])).toEqual([
    "environment/east",
    "environment/west",
  ]);
  expect(() =>
    scopedNotificationEvents(
      { resourceRid: "environment/west", events: [] },
      "environment/east",
    ),
  ).toThrow("did not match");
  expect(() =>
    scopedNotificationEvents(
      {
        resourceRid: "environment/east",
        events: [{ id: "x", resourceRid: "environment/west" }],
      },
      "environment/east",
    ),
  ).toThrow("did not match");
  const event = { id: "x", resourceRid: "environment/east" };
  expect(
    scopedNotificationEvents(
      { resourceRid: "environment/east", events: [event, event] },
      "environment/east",
    ),
  ).toHaveLength(1);
});

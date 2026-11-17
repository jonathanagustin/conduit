// @vitest-environment node
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vite-plus/test";
import { newestAuditEntries } from "../../console/operator/audit-order.js";
import { ledgerMarkup } from "../../console/operator/audit-ledger.js";
import { verifyProjection } from "../../console/operator/audit-proof.mjs";
import { readAuditPages } from "../../console/operator/live-hub.js";
import { overviewMarkup } from "../../console/operator/command-center.js";
import { dockSurfacesMarkup } from "../../console/operator/shell.js";

describe("descending audit feed", () => {
  it("keeps the newest returned records in projection order across pages", async () => {
    const paths: string[] = [];
    const page = await readAuditPages(async (path: string) => {
      paths.push(path);
      const url = new URL(path, "http://hub.local");
      expect(url.searchParams.get("order")).toBe("desc");
      expect(url.searchParams.get("view")).toBe("chain");
      return url.searchParams.has("cursor")
        ? { order: "desc", entries: [{ id: "older", sequence: 3 }], total: 3 }
        : {
            order: "desc",
            entries: [
              { id: "newest", sequence: 1 },
              { id: "middle", sequence: 2 },
            ],
            total: 3,
            nextCursor: "opaque&cursor",
          };
    });
    expect(page.rows.map((row) => row.id)).toEqual([
      "newest",
      "middle",
      "older",
    ]);
    expect(page).toMatchObject({ order: "desc", total: 3, truncated: false });
    expect(
      new URL(paths[1], "http://hub.local").searchParams.get("cursor"),
    ).toBe("opaque&cursor");
  });

  it("refuses absent or changing order confirmation from older Hubs", async () => {
    await expect(
      readAuditPages(async () => ({ entries: [], total: 0 })),
    ).rejects.toThrow("confirm descending audit order");
    let pages = 0;
    await expect(
      readAuditPages(async () => ({
        order: pages++ === 0 ? "desc" : "asc",
        entries: [{ id: "unconfirmed" }],
        nextCursor: "next",
      })),
    ).rejects.toThrow("confirm descending audit order");
  });
});

describe("audit display order", () => {
  const entries = Object.freeze(
    Array.from({ length: 12 }, (_, index) =>
      Object.freeze({
        sequence: index + 1,
        id: `event-${index}`,
        actor: `actor-${String(index).padStart(2, "0")}`,
        action: "recorded-operation",
        decision: "ALLOW",
        previousDigest: "a".repeat(64),
        digest: "b".repeat(64),
      }),
    ),
  );

  it("shows the beginning of descending reads and the end of ascending reads", () => {
    expect(
      newestAuditEntries(entries, "desc", 2).map((entry) => entry.id),
    ).toEqual(["event-0", "event-1"]);
    expect(
      newestAuditEntries(entries, "asc", 2).map((entry) => entry.id),
    ).toEqual(["event-11", "event-10"]);
    const ledger = ledgerMarkup(entries, { order: "desc", pageSize: 2 });
    expect(ledger.indexOf("actor-00")).toBeLessThan(ledger.indexOf("actor-01"));
    expect(ledger).not.toContain("actor-02");
    expect(entries[0].sequence).toBe(1);
  });

  it("uses the same newest records in the overview and live dock", () => {
    const hub = { audit: entries, auditOrder: "desc" };
    const overview = overviewMarkup(hub);
    const dock = dockSurfacesMarkup({ hub }).live;
    for (const html of [overview, dock]) {
      expect(html.indexOf("actor-00")).toBeLessThan(html.indexOf("actor-01"));
      expect(html).not.toContain("actor-11");
    }
    expect(overview).not.toContain("actor-06");
    expect(dock).not.toContain("actor-08");
  });

  it("shows an escaped unavailable audit note instead of empty or stale live rows", () => {
    for (const audit of [[], entries]) {
      const html = dockSurfacesMarkup({
        hub: {
          audit,
          auditOrder: "desc",
          auditNote: 'Audit unavailable: <img src=x onerror="alert(1)">',
        },
      }).live;
      expect(html).toContain("Audit unavailable:");
      expect(html).toContain("&lt;img src=x");
      expect(html).not.toContain("<img");
      expect(html).not.toContain("No audit entry is recorded.");
      expect(html).not.toContain("actor-00");
      expect(html).not.toContain("recorded-operation");
    }
    expect(dockSurfacesMarkup({ hub: { audit: [] } }).live).toContain(
      "No audit entry is recorded.",
    );
    const populated = dockSurfacesMarkup({
      hub: { audit: entries, auditOrder: "desc" },
    }).live;
    expect(populated).toContain("actor-00");
    expect(populated).not.toContain("Audit unavailable:");
  });

  it("leaves the native fixture's proof sequence and exact payload inputs intact", async () => {
    const fixture = JSON.parse(
      readFileSync(
        new URL(
          "../../../../internal/governance/domain/testdata/audit-projection.json",
          import.meta.url,
        ),
        "utf8",
      ),
    );
    const before = JSON.stringify(fixture);
    ledgerMarkup(fixture, { order: "desc" });
    newestAuditEntries(fixture, "desc");
    expect(JSON.stringify(fixture)).toBe(before);
    expect(await verifyProjection(fixture)).toMatchObject({
      ok: true,
      checked: 2,
    });
  });
});

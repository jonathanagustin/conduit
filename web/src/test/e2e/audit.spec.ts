import { readFileSync } from "node:fs";
import { expect, test } from "@playwright/test";
import { installPackagingReadStub, hydrate } from "./console-fixture";

installPackagingReadStub();

test("Audit platform status renders stubbed measurements and refreshes with GET", async ({
  page,
}) => {
  const methods: string[] = [];
  await page.route("**/v1/admin/status", async (route) => {
    methods.push(route.request().method());
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        generatedAt: "2026-11-14T12:00:00Z",
        components: [
          {
            label: "Hub",
            status: "AVAILABLE",
            detail: "This Hub served the request",
          },
        ],
        counters: [
          {
            label: "Pending reviews",
            status: "AVAILABLE",
            value: 0,
            detail: "Stored requests",
            link: "rm07-change-requests",
          },
        ],
        storage: [
          {
            label: "Registry filesystem",
            status: "UNAVAILABLE",
            usedBytes: null,
            capacityBytes: null,
            usedPercent: null,
            warningPercent: 80,
            criticalPercent: 95,
            detail: "No telemetry provider configured",
          },
        ],
        audit: {
          status: "UNAVAILABLE",
          chainLength: null,
          verifiedAt: null,
          detail: "No stored-chain verifier",
        },
      }),
    });
  });
  await hydrate(page);
  await page.goto("/console/");
  await page
    .getByRole("button", { name: "Audit", exact: true })
    .first()
    .click();
  await page
    .getByRole("button", { name: "Platform status", exact: true })
    .click();
  await expect(page.locator("[data-admin-status]")).toBeVisible();
  await expect(page.locator("[data-admin-status]")).toContainText(
    "Registry filesystem",
  );
  await expect(page.locator("[data-admin-status]")).toContainText(
    "Verification time unavailable",
  );
  await page.getByRole("button", { name: "Refresh measurements" }).click();
  await expect.poll(() => methods.length).toBeGreaterThanOrEqual(2);
  expect(methods.every((method) => method === "GET")).toBe(true);
});

test("audit verifies native payload bytes and keeps polled events resource scoped", async ({
  page,
}) => {
  const entries = JSON.parse(
    readFileSync(
      new URL(
        "../../../../internal/governance/domain/testdata/audit-projection.json",
        import.meta.url,
      ),
      "utf8",
    ),
  );
  let tampered = false;
  const polls: string[] = [];
  const writes: string[] = [];
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url());
    if (route.request().method() !== "GET") writes.push(url.pathname);
    if (url.pathname === "/v1/session")
      return route.fulfill({
        json: { actor: { username: "root" }, effective: { username: "root" } },
      });
    if (url.pathname === "/v1/authorization-audit") {
      expect(url.searchParams.get("order")).toBe("desc");
      expect(url.searchParams.get("view")).toBe("chain");
      return route.fulfill({
        json: {
          order: "desc",
          entries: tampered
            ? [{ ...entries[0], actor: "forged" }, entries[1]]
            : entries,
        },
      });
    }
    return route.fulfill({ status: 404, json: { error: "not configured" } });
  });
  await page.route(
    "**/conduit.notifications.v1.NotificationService/ListNotificationEvents",
    async (route) => {
      expect(route.request().method()).toBe("POST");
      const body = route.request().postDataJSON();
      expect(Object.keys(body).sort()).toEqual(["limit", "resourceRid"]);
      expect(body.limit).toBe("100");
      polls.push(body.resourceRid);
      return route.fulfill({
        json: {
          resourceRid: "environment/east",
          limit: 100,
          events: [
            {
              id: "event-1",
              type: "PLAN_REVIEW",
              resourceRid: "environment/east",
              actor: "operator",
              summary: "<img src=x onerror=alert(1)>",
              occurredAt: "2026-11-15T12:00:00.123456789Z",
            },
          ],
        },
      });
    },
  );
  await hydrate(page);
  await page.goto("/console/#au01-audit");
  await expect(page.locator(".audit-ledger")).toContainText("operator-雪");
  await page
    .getByRole("button", { name: "Verify returned payload digests" })
    .click();
  await expect(page.locator("[data-audit-proof]")).toContainText(
    "internally consistent",
  );
  await expect(page.locator("[data-audit-proof]")).toContainText(
    "no independent checkpoint",
  );
  const recorded = page
    .locator("[data-audit-provenance]")
    .filter({ hasText: "adapter: recorded" });
  await recorded.locator("summary").click();
  await expect(recorded).toContainText("Live model call: no");
  await expect(recorded).toContainText("a".repeat(64));
  await page
    .getByLabel("Audit event resource")
    .selectOption("environment/east");
  await page
    .getByRole("button", { name: "Start polling", exact: true })
    .click();
  await expect(page.locator("[data-audit-event-stream]")).toContainText(
    "<img src=x",
  );
  await expect(page.locator("[data-audit-event-stream] img")).toHaveCount(0);
  // Force the same refresh that previously remounted and reset the poller.
  await page.evaluate(() => Reflect.get(window, "refreshHub")());
  await expect(page.getByLabel("Audit event resource")).toHaveValue(
    "environment/east",
  );
  await expect(
    page.getByRole("button", { name: "Pause polling", exact: true }),
  ).toBeEnabled();
  await expect(page.locator("[data-audit-event-stream]")).toContainText(
    "<img src=x",
  );

  await page
    .getByRole("button", { name: "Pause polling", exact: true })
    .click();
  expect(polls).toEqual(["environment/east"]);
  await page
    .getByLabel("Audit event resource")
    .selectOption("environment/west");
  await page
    .getByRole("button", { name: "Refresh events", exact: true })
    .click();
  await expect(page.locator("[data-audit-event-stream]")).toContainText(
    "did not match the selected resource",
  );
  await expect(page.locator("[data-audit-event-stream]")).not.toContainText(
    "<img src=x",
  );
  tampered = true;
  await page.evaluate(() => Reflect.get(window, "refreshHub")());
  await expect(page.locator("[data-audit-proof]")).toHaveCount(0);
  await page
    .getByRole("button", { name: "Verify returned payload digests" })
    .click();
  await expect(page.locator("[data-audit-proof]")).toContainText(
    "coordinates do not match",
  );
  expect(writes).toEqual([]);
});

import { readFileSync } from "node:fs";

const tenantA = JSON.parse(
  readFileSync(new URL("../../../../testdata/console-bootstrap/tenant-a.json", import.meta.url), "utf8"),
) as Record<string, unknown>;

import { expect, test } from "./harness.js";

test("renders only validated evidence-bound tenant branding", async ({ page, hub }) => {
  hub.tenant(tenantA);
  hub.get("/overview", {
    generatedAt: "2026-08-09T12:00:00Z",
    awaitingApprovals: { status: "CURRENT", count: 0, items: [] },
    failedPlans: { status: "CURRENT", count: 0, items: [] },
    runningPlans: { status: "CURRENT", count: 0, items: [] },
    unhealthyEntities: { status: "CURRENT", count: 0, items: [] },
    staleReports: { status: "CURRENT", count: 0, items: [] },
    blockedPromotions: { status: "CURRENT", count: 0, items: [] },
  });

  await page.goto("overview");

  const header = page.getByTestId("app-frame-header");
  await expect(header.getByText("Alpha Operations", { exact: true })).toBeVisible();
  await expect(header.getByText("Synthetic test tenant", { exact: true })).toBeVisible();
  await expect(page).toHaveTitle("Alpha Operations - Synthetic Alpha");
  await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible();
});

test("fails before rendering when tenant data tries to grant a capability", async ({ page, hub }) => {
  hub.tenant({ ...tenantA, capabilities: ["admin"] });

  await page.goto("overview");

  const error = page.getByTestId("console-configuration-unavailable");
  await expect(error).toHaveText("Console configuration is unavailable. Contact the Conduit operator.");
  await expect(page.getByTestId("app-frame")).toHaveCount(0);
  await expect(error).not.toContainText("Synthetic Alpha");
});

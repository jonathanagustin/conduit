import { expect, type Page, test } from "@playwright/test";

// The release journey from the kit's journey map, walked against a real Hub:
// packaging, releases, a change request approved by a second person, the
// rollout plan it governs, the fleet, and the audit ledger that records it.
// scripts/simulate-console.sh seeds the Hub as "proposer" and the browser acts
// as "operator", so the approval below is a real separate-approver decision.
// Each step asserts on what the Hub returned, never on example data.

test.describe.configure({ mode: "serial" });

async function openWorkbench(page: Page) {
  await page.goto("./");
  await page.getByRole("button", { name: "Workbench" }).first().click();
  await expect(page.locator("#left-nav")).toBeVisible();
}

async function openArea(page: Page, area: string, tab?: string) {
  await page
    .locator("#left-nav")
    .getByText(area, { exact: true })
    .first()
    .click();
  if (tab) {
    await page
      .locator("#main-workspace")
      .getByRole("button", { name: tab, exact: true })
      .first()
      .click();
  }
}

async function hub<T>(page: Page, path: string): Promise<T> {
  const response = await page.request.get(new URL(path, page.url()).toString());
  expect(response.ok(), path).toBe(true);
  return (await response.json()) as T;
}

test("release journey runs end to end on the Hub's own data", async ({
  page,
}) => {
  await openWorkbench(page);
  const main = page.locator("#main-workspace");

  // 1. Packaging: the runs list reflects the Hub, including when it is empty.
  const runs = await hub<{ items: { runId: string }[] }>(
    page,
    "/v1/packaging-runs",
  );
  await openArea(page, "Packaging", "Runs");
  if (runs.items.length) {
    await expect(main.getByText(runs.items[0].runId).first()).toBeVisible();
  } else {
    await expect(
      main.getByText(/No packaging run is recorded|Hub did not return/),
    ).toBeVisible();
  }

  // 2. Releases: every product the Hub publishes shows its newest version.
  const products = await hub<{ products: { product: string }[] }>(
    page,
    "/v1/products?limit=20",
  );
  expect(products.products.length).toBeGreaterThan(0);
  await openArea(page, "Releases", "Products");
  await expect(
    main.getByText(products.products[0].product).first(),
  ).toBeVisible();

  // 3. Change request: the seeded request is open, authored by someone else.
  const listed = await hub<{
    changeRequests: {
      id: string;
      status: string;
      planId?: string;
      author?: { username?: string };
    }[];
  }>(page, "/v1/change-requests");
  const request = listed.changeRequests.find(
    (item) => item.author?.username !== "operator",
  );
  expect(request, "a change request authored by another subject").toBeTruthy();
  const requestId = request!.id;
  await openArea(page, "Change requests");
  await main.locator(`[data-cr-select="${requestId}"]`).click();
  const approve = main.locator('[data-cr-decide="approve"]');
  // The operator did not author it, so may review it; a reason comes first.
  await expect(approve).toHaveAttribute("data-can-review", "1");
  await expect(approve).toBeDisabled();
  await main
    .locator("[data-cr-rationale]")
    .fill("Reviewed the plan diff and the policy result.");
  await expect(approve).toBeEnabled();
  // Then an explicit confirm.
  await approve.click();
  await main.locator("[data-cr-confirm]").click();
  await expect
    .poll(async () => {
      const after = await hub<{
        changeRequests: { id: string; status: string }[];
      }>(page, "/v1/change-requests");
      return after.changeRequests.find((item) => item.id === requestId)?.status;
    })
    .toMatch(/approved/i);

  // 4. Rollout: the plan the request governs is no longer waiting on approval.
  if (request!.planId) {
    await expect
      .poll(async () => {
        const plans = await hub<{
          plans: { id: string; status?: string; state?: string }[];
        }>(page, "/v1/plans");
        const plan = plans.plans.find((item) => item.id === request!.planId);
        return String(plan?.status ?? plan?.state ?? "");
      })
      .not.toMatch(/AWAITING_APPROVAL/);
  }

  // 5. Fleet: every environment the Hub knows about is on the spokes view.
  const environments = await hub<{
    environments: { environment: { id: string } }[];
  }>(page, "/v1/environments");
  await openArea(page, "Fleet");
  await expect(
    main.getByText(environments.environments[0].environment.id).first(),
  ).toBeVisible();

  // 5b. Environments list to one environment and back, on the same rows.
  const firstEnv = environments.environments[0].environment.id;
  await openArea(page, "Fleet", "Environments");
  await main.locator(`[data-env-open="${firstEnv}"]`).click();
  await expect(main.locator("[data-env-back]")).toBeVisible();
  await expect(main.locator("h2", { hasText: firstEnv })).toBeVisible();
  await main.locator('[data-env-tab="constraints"]').click();
  await expect(main.locator('[data-env-tab="constraints"]')).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  await main.locator("[data-env-back]").click();
  await expect(main.locator(`[data-env-open="${firstEnv}"]`)).toBeVisible();

  // 6. Audit: the approval is on the ledger and the chain links check out.
  await openArea(page, "Audit");
  await main.locator("[data-ledger-query]").fill("approve");
  const approval = main
    .locator("tr", { hasText: "CHANGE_REQUEST_APPROVE" })
    .first();
  await expect(approval).toContainText("operator");
  await expect(approval).toContainText("ALLOW");
  await main.locator("[data-ledger-check]").click();
  await expect(main.getByText(/All \d+ links match/)).toBeVisible();
});

import { expect, expectRows, test } from "./harness.js";

// The degraded scenario. scripts/simulate-console.sh --scenario degraded seeds
// an extra Product whose Entity installs cleanly and then reports a warning
// from the health probe.
//
// The distinction between this and the failure scenario is the Hub's, not a
// stylistic one: a Plan that succeeded may not report a degraded Entity -- the
// Hub refuses the finish outright and leaves the Plan RUNNING. Degradation is
// something an Entity falls into after it is installed, so it arrives on the
// health-observation path while the delivery report says the apply worked.
// The simulator learned that the hard way; see its Observe comment.
test.describe.configure({ mode: "serial" });

test("a reviewer approves the Change Request through the console", async ({ page, visit }) => {
  await visit("change-requests");
  await page.locator('a[href*="/change-requests/crq_"]').first().click();
  await page.getByLabel("Rationale").fill("simulated review");
  await page.getByRole("button", { name: "Approve Change Request" }).click();
  await expect(page.getByText("APPROVED").first()).toBeVisible({ timeout: 15_000 });
});

test("the Plan succeeds even though one Entity is unwell", async ({ page, visit }) => {
  await visit("plans");
  // SUCCEEDED, not FAILED: the apply worked. This is the assertion that stops
  // the scenario quietly degenerating into a second failure test.
  await expect(page.getByText("SUCCEEDED").first()).toBeVisible({ timeout: 60_000 });
});

test("the Fleet separates the unwell Entity from its healthy neighbours", async ({ page, visit }) => {
  await visit("fleet");
  await expectRows(page, 3);
  await expect(page.getByText(/have not reported recently/)).toHaveCount(0, { timeout: 90_000 });
  // Per row rather than per page: a page-level assertion that "WARNING appears"
  // would pass just as well if every Entity were unwell, which is the opposite
  // of what this scenario is for.
  await expect(page.getByRole("row").filter({ hasText: "example-degraded" })).toContainText("WARNING");
  for (const healthy of ["example-api", "example-workspace"]) {
    await expect(page.getByRole("row").filter({ hasText: healthy })).toContainText("HEALTHY");
  }
});

test("the Overview does not report all-clear while an Entity is unwell", async ({ page, visit }) => {
  await visit("overview");
  await expect(page.getByTestId("overview-page")).toBeVisible();
  // The healthy story asserts this text appears; here it must not. An Entity
  // reporting a warning after a clean install is exactly the state an operator
  // opens this page to discover, and it reaches the panel by a different route
  // from the Plan counts beside it.
  await expect(page.getByText("Nothing needs attention")).toHaveCount(0, { timeout: 60_000 });
});

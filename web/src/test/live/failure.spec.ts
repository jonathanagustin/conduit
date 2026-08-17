import { expect, seededPlan, test } from "./harness.js";

// The failure scenario. scripts/simulate-console.sh --scenario failure seeds an
// extra Product whose coordinate carries the marker the simulated Agent matches
// on, so its apply fails for real: the Worker records a failed task, rolls the
// earlier step back, and the Hub marks the Plan FAILED.
//
// Nothing here is reachable from the healthy scenario, which is why this is a
// separate file rather than another case in console.spec.ts -- a Hub carrying a
// failed Plan cannot also satisfy "nothing needs attention".
//
// This is also the part of the console that fixtures serve worst. The failed
// view renders an attribution, a task timeline, resource diagnostics and a
// rollback outcome, all of which are produced by the Agent's own failure path;
// a stub asserts only that the console can draw a shape someone typed out.
test.describe.configure({ mode: "serial" });

test("a reviewer approves the doomed Change Request through the console", async ({ page, visit }) => {
  await visit("change-requests");
  await page.locator('a[href*="/change-requests/crq_"]').first().click();
  await page.getByLabel("Rationale").fill("simulated review");
  await page.getByRole("button", { name: "Approve Change Request" }).click();
  await expect(page.getByText("APPROVED").first()).toBeVisible({ timeout: 15_000 });
});

test("the Plan fails and the console attributes it to the Entity that failed", async ({ page, visit }) => {
  await visit("plans");
  await expect(page.getByText("FAILED").first()).toBeVisible({ timeout: 60_000 });

  await page.getByRole("link", { name: seededPlan }).click();
  await expect(page.getByText("This Plan failed")).toBeVisible();
  // The attribution names the Entity rather than the step number. An operator
  // reading this screen is trying to find out what to look at.
  await expect(page.getByText(/attributed to example-doomed/)).toBeVisible();
});

test("the failed Plan carries the diagnostics the Agent reported", async ({ page, visit }) => {
  await visit(`plans/${seededPlan}`);
  // The resource diagnostic travels from the Agent's DiagnosticError through the
  // Hub to this view. Asserting it is what makes the simulator's failure path
  // worth having: a plain error would render an empty diagnostics section and
  // this test would still find a FAILED Plan.
  await expect(page.getByText("Deployment/example-doomed")).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText("simulated: no healthy replicas")).toBeVisible();
  // And the step that had already succeeded was unwound, which is the Worker's
  // rollback path rather than anything the Hub decided.
  await expect(page.getByText("ROLLED_BACK")).toBeVisible();
});

test("the Overview counts the failed Plan", async ({ page, visit }) => {
  await visit("overview");
  // A different projection over the same state, as in the healthy story. Here
  // the all-clear must NOT appear -- a console that rendered "nothing needs
  // attention" over a failed Plan would be the worst kind of wrong.
  await expect(page.getByText("Nothing needs attention")).toHaveCount(0);
  await expect(page.locator('a[href*="/plans/pln_"]').first()).toBeVisible({ timeout: 60_000 });
});

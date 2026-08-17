import { expect, expectRows, seededPlan, test } from "./harness.js";

// One desired state, one Team and one approval policy were posted to a real
// Hub. Everything below is what the Hub derived from them and the console then
// rendered -- no fixture is involved at any point, so a renamed field, a query
// parameter the Hub ignores, an authorization filter, or a nil slice encoded as
// null fails here rather than shipping.

test("the Plans screen lists the Plan the Hub minted", async ({ page, visit }) => {
  await visit("plans");
  await expectRows(page);
  expect(seededPlan, "the harness did not report a seeded Plan").not.toEqual("");
  await expect(page.getByRole("cell", { name: seededPlan })).toBeVisible();
  // The environment and status are the Hub's own projection of the Plan. The
  // seed named an environment and some Releases; AWAITING_APPROVAL is the Hub's
  // verdict about them.
  await expect(page.getByRole("cell", { name: "dev-use2", exact: true })).toBeVisible();
  await expect(page.getByText("AWAITING_APPROVAL").first()).toBeVisible();
});

test("the Change Requests screen shows a request genuinely awaiting a reviewer", async ({ page, visit }) => {
  await visit("change-requests");
  await expectRows(page);
  // The seed never asked for a Change Request; proposing a Plan creates one.
  // PENDING_APPROVALS rather than ERROR is the part worth asserting: it means
  // the seeded policy was in force when the request was evaluated, so a real
  // reviewer requirement is outstanding rather than the request having failed
  // for want of any policy at all.
  await expect(page.getByText(/^crq_/).first()).toBeVisible();
  await expect(page.getByText("PENDING_APPROVALS").first()).toBeVisible();
});

test("the Fleet screen lists the installations the desired state implies", async ({ page, visit }) => {
  await visit("fleet");
  await expectRows(page, 2);
  // Two Entities were seeded, and the fleet projection is derived from them
  // rather than from reported state -- nothing has reported yet, which is why
  // both read as stale below.
  await expect(page.getByRole("cell", { name: "example-api", exact: true })).toBeVisible();
  await expect(page.getByRole("cell", { name: "example-workspace", exact: true })).toBeVisible();
  await expect(page.getByText(/have not reported recently/)).toBeVisible();
});

test("the Overview agrees with the Change Requests screen about the same Hub", async ({ page, visit }) => {
  await visit("overview");
  // The overview is a different projection over the same state, built by a
  // different code path from the screen above. Two surfaces disagreeing about
  // one Hub is a class of defect this repository has shipped before, and only a
  // live run can see it: the first version of this simulation seeded no
  // approval policy, and the console correctly showed a Plan AWAITING_APPROVAL
  // beside an Overview counting zero awaiting approval.
  await expect(page.getByTestId("overview-page")).toBeVisible();
  // Matched on the link target rather than its text: the title is composed by
  // the Hub ("Reconcile dev-use2 generation ..."), and asserting on wording
  // would make this a test of that sentence instead of a test that the panel
  // reached a Change Request at all.
  const awaiting = page.locator('a[href*="/change-requests/crq_"]');
  await expect(awaiting.first()).toBeVisible({ timeout: 15_000 });
  // And the fleet's own count, which comes from the same projection the Fleet
  // screen renders.
  await expect(page.getByText(/installations not reporting recently/)).toBeVisible();
});

test("the Teams screen shows the Team the seed created", async ({ page, visit }) => {
  await visit("teams");
  await expectRows(page);
  await expect(page.getByText("Reviewers").first()).toBeVisible();
});

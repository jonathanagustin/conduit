import { expect, expectRows, seededPlan, test } from "./harness.js";

// One desired state, one Team and one approval policy were posted to a real
// Hub, and a simulated Agent is polling it. Everything below is what the Hub
// derived from them and the console then rendered -- no fixture is involved at
// any point, so a renamed field, a query parameter the Hub ignores, an
// authorization filter, or a nil slice encoded as null fails here rather than
// shipping.
//
// Serial, and in this order, because these tests share one Hub and the story
// they tell is a sequence: the seeded state, the approval that a reviewer
// performs through the console, and the state the Agent then produces. Running
// them in parallel would have the approval land underneath the assertions about
// what has not been approved yet.
test.describe.configure({ mode: "serial" });

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
  await expect(page.locator('a[href*="/change-requests/crq_"]').first()).toBeVisible({ timeout: 15_000 });
});

test("the Teams screen shows the Team the seed created", async ({ page, visit }) => {
  await visit("teams");
  await expectRows(page);
  await expect(page.getByText("Reviewers").first()).toBeVisible();
});

// The approval, performed the way an operator performs it. Everything after
// this point is state no seed produced: it exists because a reviewer clicked a
// button and a simulated Agent then did the work.
test("a reviewer approves the Change Request through the console", async ({ page, visit }) => {
  await visit("change-requests");
  await page.locator('a[href*="/change-requests/crq_"]').first().click();

  // The Hub requires a rationale, and the console disables the button without
  // one -- so this also asserts the console is enforcing the same rule.
  const approve = page.getByRole("button", { name: "Approve Change Request" });
  await expect(approve).toBeDisabled();
  await page.getByLabel("Rationale").fill("simulated review");
  await expect(approve).toBeEnabled();
  await approve.click();

  // The reviewer is not the author. The harness bootstraps two administrators
  // and seeds as the other one precisely so this click is not refused: the Hub
  // will not let the author of a Change Request approve it.
  await expect(page.getByText("APPROVED").first()).toBeVisible({ timeout: 15_000 });
});

// Terminal states only from here. Asserting that a Plan is briefly RUNNING
// would be a race against the simulated Agent's poll interval, and this suite
// runs with retries disabled so that races are visible rather than retried
// away.
test("the approved Plan is executed by the Agent and reaches SUCCEEDED", async ({ page, visit }) => {
  await visit("plans");
  await expect(page.getByText("SUCCEEDED").first()).toBeVisible({ timeout: 60_000 });
});

test("the Fleet reports healthy installations once the Agent has run", async ({ page, visit }) => {
  await visit("fleet");
  await expectRows(page, 2);
  // Before an Agent existed this screen could only ever show UNKNOWN,
  // OUT_OF_DATE and stale, because reported state reaches the Hub solely
  // through a lease token minted for an approved Plan. Nothing short of an
  // Agent can produce what is asserted here.
  // Waited on first, and deliberately not on a health label: the filter panel
  // renders "HEALTHY" and "UNHEALTHY" as options whether or not anything
  // reported, so a getByText wait on those is satisfied instantly and leaves
  // the assertions that matter running against a page that has not refetched.
  // The stale callout disappearing is the honest signal that a report landed.
  await expect(page.getByText(/have not reported recently/)).toHaveCount(0, { timeout: 90_000 });
  for (const entity of ["example-api", "example-workspace"]) {
    const row = page.getByRole("row").filter({ hasText: entity });
    await expect(row).toContainText("HEALTHY");
    await expect(row).toContainText("UP_TO_DATE");
    // The observed version is what the Agent reported, echoed from the step it
    // applied. Had it invented its own the projection would render drift.
    await expect(row).toContainText("0.1.0");
  }
});

test("the Overview clears once nothing needs attention", async ({ page, visit }) => {
  await visit("overview");
  // The all-clear state is unreachable without an Agent: it requires no stale
  // report, no unhealthy Entity, and no Change Request awaiting approval at
  // once. It is the strongest single assertion that the whole loop closed.
  await expect(page.getByText("Nothing needs attention")).toBeVisible({ timeout: 60_000 });
});

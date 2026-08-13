import { expect, test } from "./harness.js";

/** Change Requests -- P0: "What exactly am I being asked to approve?"
 *
 * The screen has to answer that before it offers a button, so most of these
 * tests are about what is shown rather than what is clicked. */

function changeRequest(overrides: Record<string, unknown> = {}) {
  return {
    id: "crq_upgrade",
    planId: "pln_deploy",
    environment: "prod-use1",
    title: "Upgrade payments to 2.4.1",
    description: "Rolls the payments Entity forward one patch release.",
    author: { username: "author" },
    status: "PENDING_APPROVALS",
    requirementsSatisfied: false,
    changes: [
      {
        id: "chg-1",
        type: "UPDATE_ENTITY",
        installationId: "payments",
        diff: { version: { before: "2.4.0", after: "2.4.1" } },
      },
    ],
    evaluations: [
      { changeId: "chg-1", policyId: "review", outcome: "REQUIRE_TEAM", reason: "production" },
    ],
    requirements: [
      { id: "req-1", team: "reviewers", reason: "production change", satisfiedBy: [] },
    ],
    createdAt: "2026-08-07T12:00:00Z",
    updatedAt: "2026-08-07T12:00:00Z",
    revision: 1,
    ...overrides,
  };
}

test.describe("Change Requests", () => {
  test("puts requests awaiting approval before settled ones", async ({ page, hub }) => {
    hub.get("/change-requests", {
      changeRequests: [
        changeRequest({ id: "crq_done", status: "APPROVED", updatedAt: "2026-08-07T13:00:00Z" }),
        changeRequest({ id: "crq_waiting", status: "PENDING_APPROVALS" }),
      ],
    });
    await page.goto("change-requests");

    // The queue exists to surface what needs a person. Recency alone would bury
    // the only row that does.
    const rows = page.getByRole("row");
    await expect(rows.nth(1)).toContainText("crq_waiting");
    await expect(rows.nth(2)).toContainText("crq_done");
  });

  test("counts the outstanding reviewer requirements", async ({ page, hub }) => {
    hub.get("/change-requests", { changeRequests: [changeRequest()] });
    await page.goto("change-requests");

    await expect(page.getByRole("row", { name: /crq_upgrade/ })).toContainText("1");
  });

  test("shows who must review, what the policies said, and what changes", async ({ page, hub }) => {
    hub.get("/change-requests/crq_upgrade", changeRequest());
    await page.goto("change-requests/crq_upgrade");

    await expect(page.getByRole("heading", { name: "Who must review" })).toBeVisible();
    await expect(page.getByText("reviewers")).toBeVisible();
    await expect(page.getByText("outstanding")).toBeVisible();

    await expect(page.getByRole("heading", { name: "What the policies said" })).toBeVisible();
    await expect(page.getByText("REQUIRE_TEAM")).toBeVisible();

    await expect(page.getByRole("heading", { name: "What changes" })).toBeVisible();
    const diff = page.getByRole("row", { name: /version/ });
    await expect(diff).toContainText("2.4.0");
    await expect(diff).toContainText("2.4.1");
  });

  test("requires a rationale before any decision can be taken", async ({ page, hub }) => {
    // The Hub records a review permanently and it cannot be edited afterwards.
    // "Approved" with no reason is not an answer to what was being approved.
    hub.get("/change-requests/crq_upgrade", changeRequest());
    await page.goto("change-requests/crq_upgrade");

    await expect(page.getByRole("button", { name: "Approve" })).toBeDisabled();
    await expect(page.getByRole("button", { name: "Reject" })).toBeDisabled();

    await page.getByLabel("Rationale").fill("checked the rollout plan");

    await expect(page.getByRole("button", { name: "Approve" })).toBeEnabled();
    await expect(page.getByRole("button", { name: "Reject" })).toBeEnabled();
  });

  test("whitespace alone is not a rationale", async ({ page, hub }) => {
    hub.get("/change-requests/crq_upgrade", changeRequest());
    await page.goto("change-requests/crq_upgrade");

    await page.getByLabel("Rationale").fill("   ");

    await expect(page.getByRole("button", { name: "Approve" })).toBeDisabled();
  });

  test("sends an approval and shows the Hub's resulting state", async ({ page, hub }) => {
    hub.get("/change-requests/crq_upgrade", changeRequest());
    hub.post("/change-requests/crq_upgrade/approve", changeRequest({ status: "APPROVED" }));
    await page.goto("change-requests/crq_upgrade");

    await page.getByLabel("Rationale").fill("checked the rollout plan");
    await page.getByRole("button", { name: "Approve" }).click();

    await expect
      .poll(() => hub.requests().some((request) => request.includes("POST") && request.includes("/approve")))
      .toBe(true);
    // The body, not just the fact of a request. The Hub sets
    // DisallowUnknownFields, so an extra field fails the call -- and asserting
    // only that a POST happened is what let a union of every action's fields
    // ship as the body for all four.
    expect(hub.bodies()).toEqual([{ rationale: "checked the rollout plan" }]);
  });

  test("sends each action the body its endpoint accepts, and nothing more", async ({
    page,
    hub,
  }) => {
    hub.get("/change-requests/crq_upgrade", changeRequest({ status: "CANCELLED" }));
    hub.post("/change-requests/crq_upgrade/reopen", changeRequest());
    await page.goto("change-requests/crq_upgrade");
    await page.getByLabel("Rationale").fill("reopening");
    await page.getByRole("button", { name: "Reopen" }).click();

    // Reopen takes an EMPTY body: the Hub calls requireEmptyBody and rejects any
    // field at all, including the rationale the form collected.
    await expect.poll(() => hub.bodies().length).toBe(1);
    expect(hub.bodies()[0]).toEqual({});
  });

  test("sends cancel a reason rather than a rationale", async ({ page, hub }) => {
    hub.get("/change-requests/crq_upgrade", changeRequest());
    hub.post("/change-requests/crq_upgrade/cancel", changeRequest({ status: "CANCELLED" }));
    await page.goto("change-requests/crq_upgrade");
    await page.getByLabel("Rationale").fill("superseded");
    await page.getByRole("button", { name: "Cancel request" }).click();

    await expect.poll(() => hub.bodies().length).toBe(1);
    expect(hub.bodies()[0]).toEqual({ reason: "superseded" });
  });

  test("reports a refused review instead of appearing to succeed", async ({ page, hub }) => {
    hub.get("/change-requests/crq_upgrade", changeRequest());
    await page.goto("change-requests/crq_upgrade");

    // No POST stub is registered, so the harness answers 501. What matters is
    // that the screen says the review was not recorded rather than clearing the
    // form and looking done.
    await page.getByLabel("Rationale").fill("not mine to approve");
    await page.getByRole("button", { name: "Approve" }).click();

    await expect(page.getByText("That review was not recorded")).toBeVisible();
    await expect(page.getByLabel("Rationale")).toHaveValue("not mine to approve");
  });

  test("shows an evaluation failure rather than an empty approval screen", async ({ page, hub }) => {
    hub.get(
      "/change-requests/crq_upgrade",
      changeRequest({
        status: "ERROR",
        error: "all enabled policies returned no_op for change chg-1",
        evaluations: [],
        requirements: [],
      }),
    );
    await page.goto("change-requests/crq_upgrade");

    await expect(page.getByText("This request could not be evaluated")).toBeVisible();
    await expect(page.getByText(/returned no_op/)).toBeVisible();
  });

  test("lists the reviews already recorded against a request", async ({ page, hub }) => {
    hub.get(
      "/change-requests/crq_upgrade",
      changeRequest({
        reviews: [{ subject: "reviewer", decision: "APPROVE", rationale: "looks right" }],
      }),
    );
    await page.goto("change-requests/crq_upgrade");

    // Scoped to the reviews list: "reviewer" also appears as the "reviewers"
    // team in the requirements table above, and an unscoped match is ambiguous.
    const reviews = page.getByRole("listitem").filter({ hasText: "looks right" });
    await expect(reviews).toContainText("APPROVE");
    await expect(reviews).toContainText("reviewer");
  });
});

test.describe("consequence of approving", () => {
  const PLAN = {
    id: "pln_deploy",
    desiredStateDigest: "sha256:a",
    plan: {
      environment: "prod-use1",
      generation: "g",
      steps: [
        { position: 1, action: "RECONCILE", installationId: "payments", entity: "payments", product: "app.example:payments", version: "2.4.1", namespace: "payments" },
        { position: 2, action: "RECONCILE", installationId: "search", entity: "search", product: "app.example:search", version: "8.1.0", namespace: "search" },
      ],
    },
    status: "AWAITING_APPROVAL",
    requiredApprovals: 1,
    createdAt: "2026-08-07T12:00:00Z",
    updatedAt: "2026-08-07T12:00:00Z",
    revision: 1,
  };

  test("says what approving will execute, before offering the button", async ({ page, hub }) => {
    // A reviewer who cannot see this is approving a title and a diff.
    hub.get("/change-requests/crq_upgrade", changeRequest());
    hub.get("/plans/pln_deploy", PLAN);
    await page.goto("change-requests/crq_upgrade");

    const consequence = page.getByText(/Approving releases/);
    await expect(consequence).toContainText("pln_deploy");
    await expect(consequence).toContainText("prod-use1");
    await expect(consequence).toContainText("2 steps");
    await expect(consequence).toContainText("payments, search");
  });

  test("says the consequence is unknown rather than staying silent", async ({ page, hub }) => {
    // Not knowing what will execute is itself worth saying: it is the
    // difference between an informed approval and a blind one.
    hub.get("/change-requests/crq_upgrade", changeRequest());
    hub.fails("/plans/pln_deploy", 500, "the plan store is unavailable");
    await page.goto("change-requests/crq_upgrade");

    await expect(page.getByText(/consequence of approving is unknown/)).toBeVisible();
  });

  test("says so when a request governs no Plan", async ({ page, hub }) => {
    hub.get("/change-requests/crq_upgrade", changeRequest({ planId: "" }));
    await page.goto("change-requests/crq_upgrade");

    await expect(page.getByText(/governs no Plan/)).toBeVisible();
  });
});

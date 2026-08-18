import { expect, indexPage, test } from "./harness.js";

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
  // The Hub sends null, not [], for a list it has none of: governance.go tags
  // changes, evaluations and requirements without `omitempty`, so a nil Go
  // slice serializes as null. Every fixture here used [], and against a real
  // Hub the screen died on the first request whose policy raised no
  // requirement -- "Cannot read properties of null (reading 'filter')", a blank
  // page, and a green test suite.
  //
  // Serving the Hub's actual shape is the point. A fixture that is easier to
  // write than the truth is how the suite stayed green through a crash.
  test("survives the null lists a Hub sends for an empty policy evaluation", async ({ page, hub }) => {
    hub.get(
      "/change-requests",
      indexPage("changeRequests", [
        changeRequest({ id: "crq_bare", changes: null, evaluations: null, requirements: null }),
      ]),
    );
    await page.goto("change-requests");

    await expect(page.getByRole("row").nth(1)).toContainText("crq_bare");
    // "none" outstanding, which is the screen's own wording for a computed
    // zero. The distinction that matters is that it computed one at all: the
    // crash happened inside outstandingRequirements, before anything rendered.
    await expect(page.getByRole("row").nth(1)).toContainText("none");
  });

  test("opens a request whose lists are all null", async ({ page, hub }) => {
    hub.get(
      "/change-requests/crq_bare",
      changeRequest({ id: "crq_bare", changes: null, evaluations: null, requirements: null }),
    );
    await page.goto("change-requests/crq_bare");

    await expect(page.getByText("Upgrade payments to 2.4.1")).toBeVisible();
  });

  test("puts requests awaiting approval before settled ones", async ({ page, hub }) => {
    hub.get("/change-requests", indexPage("changeRequests", [
        changeRequest({ id: "crq_done", status: "APPROVED", updatedAt: "2026-08-07T13:00:00Z" }),
        changeRequest({ id: "crq_waiting", status: "PENDING_APPROVALS" }),
      ]));
    await page.goto("change-requests");

    // The queue exists to surface what needs a person. Recency alone would bury
    // the only row that does.
    const rows = page.getByRole("row");
    await expect(rows.nth(1)).toContainText("crq_waiting");
    await expect(rows.nth(2)).toContainText("crq_done");
  });

  test("counts the outstanding reviewer requirements", async ({ page, hub }) => {
    hub.get("/change-requests", indexPage("changeRequests", [changeRequest()]));
    await page.goto("change-requests");

    await expect(page.getByRole("row", { name: /crq_upgrade/ })).toContainText("1");
  });

  test("keeps search and opaque traversal in the URL across reload and retry", async ({ page, hub }) => {
    hub.get(
      "/change-requests",
      indexPage("changeRequests", [changeRequest({ id: "crq_first" })], {
        total: 2,
        pageSize: 1,
        limit: 1,
        nextCursor: "change-next",
      }),
    );
    hub.get(
      "/change-requests",
      indexPage("changeRequests", [changeRequest({ id: "crq_second" })], {
        total: 2,
        pageSize: 1,
        limit: 1,
        hasPrevious: true,
      }),
      { cursor: "change-next" },
    );

    await page.goto("change-requests?search=upgrade");
    await page.getByTestId("change-requests-pagination").getByRole("button", { name: "Next" }).click();
    await expect(page).toHaveURL(/search=upgrade.*cursor=change-next|cursor=change-next.*search=upgrade/);
    await expect(page.getByRole("row", { name: /crq_second/ })).toBeVisible();

    const beforeReload = hub.requests().filter((request) => request.includes("cursor=change-next")).length;
    await page.reload();
    await expect(page.getByRole("row", { name: /crq_second/ })).toBeVisible();
    expect(hub.requests().filter((request) => request.includes("cursor=change-next")).length).toBeGreaterThan(beforeReload);

    hub.fails("/change-requests", 500, "change request store unavailable", { cursor: "change-next" });
    await page.reload();
    await expect(page.getByText(/Change Requests could not be read/i)).toBeVisible();
    hub.get(
      "/change-requests",
      indexPage("changeRequests", [changeRequest({ id: "crq_second" })], {
        total: 2,
        pageSize: 1,
        limit: 1,
        hasPrevious: true,
      }),
      { cursor: "change-next" },
    );
    await page.getByRole("button", { name: "Try again" }).click();
    await expect(page.getByRole("row", { name: /crq_second/ })).toBeVisible();
    await expect(page).toHaveURL(/cursor=change-next/);

    await page.getByTestId("change-requests-pagination").getByRole("button", { name: "Previous" }).click();
    await expect(page).not.toHaveURL(/cursor=/);
    await expect(page).toHaveURL(/search=upgrade/);
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
    // Reopen is the author's action; the default operator is not the author and
    // is no longer shown the button, so the session has to name one.
    hub.session("author");
    hub.get("/change-requests/crq_upgrade", changeRequest({ status: "CANCELLED" }));
    hub.post("/change-requests/crq_upgrade/reopen", changeRequest());
    await page.goto("change-requests/crq_upgrade");
    await page.getByRole("button", { name: "Reopen" }).click();

    // Reopen takes no body at all: requireEmptyBody reads bytes, so "{}" -- the
    // serialisation of {} -- is a 400, not an empty object. The stub used to
    // JSON.parse the POST and treat {} as empty, which is how this test stayed
    // green while a real Hub refused the click.
    await expect.poll(() => hub.bodies().length).toBe(1);
    expect(hub.bodies()[0]).toBeNull();
  });

  test("reopens without asking for a rationale it cannot send", async ({ page, hub }) => {
    // The endpoint has no field for one, so demanding one was a toll on the way
    // to an action that discards it -- under a placeholder promising the text
    // was recorded against your name.
    hub.session("author");
    hub.get("/change-requests/crq_upgrade", changeRequest({ status: "REJECTED" }));
    hub.post("/change-requests/crq_upgrade/reopen", changeRequest());
    await page.goto("change-requests/crq_upgrade");

    await expect(page.getByLabel("Rationale")).toHaveValue("");
    await expect(page.getByRole("button", { name: "Reopen" })).toBeEnabled();
    await expect(page.getByText(/Reopening records no rationale/)).toBeVisible();
  });

  test("keeps a rationale reopen never sent rather than clearing it", async ({ page, hub }) => {
    // Clearing the box on success is the last place the discarded text could
    // still look consumed: the operator sees an empty form and a reopened
    // request, and concludes the two were connected.
    hub.session("author");
    hub.get("/change-requests/crq_upgrade", changeRequest({ status: "CANCELLED" }));
    hub.post("/change-requests/crq_upgrade/reopen", changeRequest());
    await page.goto("change-requests/crq_upgrade");

    await page.getByLabel("Rationale").fill("mistake, reopening");
    await page.getByRole("button", { name: "Reopen" }).click();

    await expect.poll(() => hub.bodies().length).toBe(1);
    expect(hub.bodies()[0]).toBeNull();
    await expect(page.getByLabel("Rationale")).toHaveValue("mistake, reopening");
  });

  test("sends cancel a reason rather than a rationale", async ({ page, hub }) => {
    hub.session("author");
    hub.get("/change-requests/crq_upgrade", changeRequest());
    hub.post("/change-requests/crq_upgrade/cancel", changeRequest({ status: "CANCELLED" }));
    await page.goto("change-requests/crq_upgrade");
    await page.getByLabel("Rationale").fill("superseded");
    await page.getByRole("button", { name: "Cancel request" }).click();

    await expect.poll(() => hub.bodies().length).toBe(1);
    expect(hub.bodies()[0]).toEqual({ reason: "superseded" });
  });

  // Cancel and Reopen are the author's, and only from the statuses each
  // transition can leave: governance.go Reopen admits REJECTED, CANCELLED and
  // ERROR, and the cancel route in http.go admits PENDING_APPROVALS and
  // APPROVED. Offering them to a reviewer made two controls that demanded a
  // rationale and then returned 403 -- and the stub answered 200, so nothing
  // here noticed.
  test("does not offer a reviewer the author-only actions", async ({ page, hub }) => {
    hub.get("/change-requests/crq_upgrade", changeRequest());
    await page.goto("change-requests/crq_upgrade");

    await expect(page.getByRole("button", { name: "Approve" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Cancel request" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Reopen" })).toHaveCount(0);
  });

  test("does not offer a reviewer Reopen on a settled request either", async ({ page, hub }) => {
    hub.get("/change-requests/crq_upgrade", changeRequest({ status: "REJECTED" }));
    await page.goto("change-requests/crq_upgrade");

    await expect(page.getByTestId("change-request-detail")).toBeVisible();
    await expect(page.getByRole("button", { name: "Reopen" })).toHaveCount(0);
  });

  test("offers the author only the transition the status permits", async ({ page, hub }) => {
    hub.session("author");
    hub.get("/change-requests/crq_upgrade", changeRequest());
    await page.goto("change-requests/crq_upgrade");

    // Pending: cancellable, and there is nothing to reopen.
    await expect(page.getByRole("button", { name: "Cancel request" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Reopen" })).toHaveCount(0);
  });

  test("offers the author Reopen, and not Cancel, once a request is settled", async ({
    page,
    hub,
  }) => {
    hub.session("author");
    hub.get("/change-requests/crq_upgrade", changeRequest({ status: "CANCELLED" }));
    await page.goto("change-requests/crq_upgrade");

    await expect(page.getByRole("button", { name: "Reopen" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Cancel request" })).toHaveCount(0);
  });

  test("reports a refusal the Hub still makes rather than appearing to succeed", async ({
    page,
    hub,
  }) => {
    // The console gates on what it can see, and what it can see is one fetch
    // old: a request cancelled by its author in another tab is still PENDING
    // here. The Hub is the authority, so its refusal has to reach the screen.
    hub.session("author");
    hub.get("/change-requests/crq_upgrade", changeRequest());
    hub.postFails("/change-requests/crq_upgrade/cancel", 403, "forbidden");
    await page.goto("change-requests/crq_upgrade");

    await page.getByLabel("Rationale").fill("superseded");
    await page.getByRole("button", { name: "Cancel request" }).click();

    await expect(page.getByText("That review was not recorded")).toBeVisible();
    await expect(page.getByText("forbidden")).toBeVisible();
    await expect(page.getByLabel("Rationale")).toHaveValue("superseded");
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

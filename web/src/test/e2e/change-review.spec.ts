import { expect, test } from "@playwright/test";
import { APPLICATIONS } from "./hub-stub";
import { installPackagingReadStub, hydrate } from "./console-fixture";

installPackagingReadStub();

test("change review binds confirmation to request, revision and Hub identity", async ({
  page,
}) => {
  let operator = "reviewer",
    revision = 1,
    statusB = "PENDING_APPROVALS";
  const reviews: unknown[] = [];
  const requests = () => [
    {
      id: "review-a",
      title: "Review A",
      revision: 1,
      status: "PENDING_APPROVALS",
      author: { username: "writer" },
      environment: "prod",
      changes: [
        {
          id: "change-a",
          type: "UPDATE",
          diff: { "limits.memory": { before: "256Mi", after: "512Mi" } },
        },
      ],
      requirements: [
        { kind: "TEAM", team: "reviewers", reason: "independent review" },
      ],
      requirementsSatisfied: false,
    },
    {
      id: "review-b",
      title: "Review B",
      revision,
      status: statusB,
      author: { username: "writer" },
      environment: "prod",
      changes: [
        {
          id: "change-b",
          type: "UPDATE",
          diff: {
            "limits.memory": { before: "64Mi", after: "128Mi" },
            password: {
              before: "do-not-expose-old",
              after: "do-not-expose-new",
            },
          },
        },
      ],
      requirements: [
        { kind: "TEAM", team: "reviewers", reason: "independent review" },
      ],
      evaluations: [
        {
          policyId: "native-review",
          outcome: "REVIEW",
          reason: "independent reviewer required",
        },
      ],
      requirementsSatisfied: statusB === "APPROVED",
    },
  ];
  await page.route("**/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/v1/session")
      return route.fulfill({
        json: {
          actor: { username: operator },
          effective: { username: operator },
          applications: APPLICATIONS,
        },
      });
    if (path === "/v1/change-requests")
      return route.fulfill({ json: { changeRequests: requests() } });
    if (path === "/v1/change-requests/review-b/approve") {
      reviews.push({ path, body: route.request().postDataJSON() });
      statusB = "APPROVED";
      return route.fulfill({ json: requests()[1] });
    }
    if (path === "/v1/plans")
      return route.fulfill({
        json: {
          plans: [
            {
              id: "native-review-plan",
              status: "AWAITING_APPROVAL",
              requiredApprovals: 2,
              approvals: [],
              plan: { environment: "prod", steps: [] },
            },
            {
              id: "native-recorded-plan",
              status: "RECORDED",
              plan: { environment: "prod", steps: [] },
            },
          ],
        },
      });
    return route.fulfill({ json: {} });
  });
  const refresh = () =>
    page.evaluate(() =>
      (window as unknown as { refreshHub: () => Promise<void> }).refreshHub(),
    );
  await hydrate(page);
  await page.goto("/console/");
  await page
    .getByRole("button", { name: "Change requests", exact: true })
    .first()
    .click();
  let root = page.locator("[data-cr-root]");
  await root.locator("[data-cr-rationale]").fill("Reviewed A");
  await root.locator('[data-cr-decide="approve"]').click();
  await expect(root.locator("[data-cr-confirm]")).toBeVisible();
  await root.locator('[data-cr-filter="pending"]').click();
  await expect(root.locator("[data-cr-confirm]")).toHaveCount(0);
  await expect(root.locator("[data-cr-rationale]")).toHaveValue("");
  await root.locator('[data-cr-select="review-b"]').click();
  await expect(page.locator("[data-object-header]")).toContainText("Review B");
  await expect(root.locator("[data-cr-diff]")).toContainText("128Mi");
  await expect(root.locator("[data-cr-diff]")).not.toContainText(
    "do-not-expose",
  );
  await root.locator("[data-cr-rationale]").fill("Reviewed B");
  await root.locator('[data-cr-decide="approve"]').click();
  revision = 2;
  await refresh();
  await expect(root.locator("[data-cr-confirm]")).toHaveCount(0);
  await expect(root.locator("[data-cr-rationale]")).toHaveValue("");
  await expect(root).toContainText("Request context changed");
  expect(reviews).toEqual([]);
  operator = "writer";
  await refresh();
  await expect(root.locator("[data-cr-decide]")).toHaveCount(0);
  await expect(root).toContainText("You wrote this request");
  operator = "";
  await refresh();
  await expect(root).toContainText("identity is unavailable");
  await expect(root.locator("[data-cr-decide]")).toHaveCount(0);
  operator = "reviewer";
  await refresh();
  await page.locator('[data-area-tab="gate"]').click();
  await expect(page.locator("[data-cr-gate]")).toContainText("review-b");
  await expect(page.locator("[data-cr-gate]")).toContainText(
    "author cannot approve",
  );
  await page.locator('[data-open-cr-review="review-b"]').click();
  await root
    .locator("[data-cr-rationale]")
    .fill("Reviewed the current B revision");
  await root.locator('[data-cr-decide="approve"]').click();
  await root.locator("[data-cr-confirm]").click();
  await expect(root).toContainText("no longer in the visible filtered index");
  expect(reviews).toEqual([
    {
      path: "/v1/change-requests/review-b/approve",
      body: { rationale: "Reviewed the current B revision" },
    },
  ]);
  await root.locator('[data-cr-filter="all"]').click();
  await root.locator('[data-cr-select="review-b"]').click();
  await page.evaluate(() =>
    (window as unknown as { switchScreen: (id: string) => void }).switchScreen(
      "s10-diff",
    ),
  );
  await expect(page.locator("[data-cr-diff]")).toContainText("128Mi");
  await expect(page.locator("#example-data-badge")).toHaveText("LIVE");
  await expect(page.locator("#area-body")).not.toContainText("do-not-expose");
  await page.locator('[data-area-tab="approvals"]').click();
  await expect(page.locator("[data-approvals-board]")).toContainText(
    "Recorded only",
  );
  // The Approvals board lists Hub plans and must say so (#822).
  await expect(page.locator("#example-data-badge")).toHaveText("LIVE");
  await expect(page.locator("#example-data-badge")).toHaveAttribute(
    "data-mode",
    "live",
  );
  await expect(page.locator("[data-approvals-board]")).toContainText(
    "native-recorded-plan",
  );
  await page.locator('[data-review-plan="native-review-plan"]').click();
  await expect(page.locator("[data-release-plan-inspector]")).toContainText(
    "native-review-plan",
  );
});

test("settings-only refresh preserves a focused change-review draft", async ({
  page,
}) => {
  await page.route("**/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/v1/session")
      return route.fulfill({
        json: {
          actor: { username: "reviewer" },
          effective: { username: "reviewer" },
          applications: APPLICATIONS,
        },
      });
    if (path === "/v1/operator-identity")
      return route.fulfill({ json: { enabled: false, providers: [] } });
    if (path === "/v1/teams") return route.fulfill({ json: { teams: [] } });
    if (path === "/v1/resource-grants")
      return route.fulfill({ json: { resourceGrants: [] } });
    if (path === "/v1/approval-policies")
      return route.fulfill({ json: { approvalPolicies: [] } });
    if (path === "/v1/change-requests")
      return route.fulfill({
        json: {
          changeRequests: [
            {
              id: "review-focus",
              revision: 1,
              status: "PENDING_APPROVALS",
              author: { username: "writer" },
              environment: "east",
              changes: [],
            },
          ],
        },
      });
    return route.fulfill({ status: 404, json: { error: "not configured" } });
  });
  await hydrate(page);
  await page.goto("/console/#adm02-org-control-panel");
  await expect(page.locator("[data-org-snapshot]")).toBeVisible();
  await page
    .getByRole("button", { name: "Change requests", exact: true })
    .click();
  const rationale = page.locator("[data-cr-rationale]");
  await rationale.fill("Independent review draft");
  await page.evaluate(() => Reflect.get(window, "refreshOrganization")());
  await expect(rationale).toBeFocused();
  await expect(rationale).toHaveValue("Independent review draft");
});

import { expect, test } from "@playwright/test";
import { APPLICATIONS } from "./hub-stub";
import { installPackagingReadStub, hydrate } from "./console-fixture";

installPackagingReadStub();

test("Intelligence stages only scoped confirmed proposals and distinguishes evaluation evidence", async ({
  page,
}) => {
  const selections: unknown[] = [];
  const feedback: unknown[] = [];
  const forbidden: string[] = [];
  const cases = [
    {
      id: "case-pass",
      scenario: "within ceiling",
      boundObject: "recorded-case",
      value: "512Mi",
      expectedOutcome: "PASS",
      source: "gate rule",
    },
    {
      id: "case-fail",
      scenario: "required failure",
      boundObject: "recorded-case",
      value: "8Gi",
      expectedOutcome: "FAIL_REQUIRED",
      source: "gate rule",
    },
  ];
  let created = false;
  await page.route("**/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (
      route.request().method() !== "GET" &&
      (/\/(approve|leases|start)$/.test(path) ||
        path.includes("model-proposal"))
    )
      forbidden.push(path);
    if (path === "/v1/session")
      return route.fulfill({
        json: {
          actor: { username: "intel-writer" },
          effective: { username: "intel-writer" },
          applications: APPLICATIONS,
        },
      });
    if (path === "/v1/fleet/installations")
      return route.fulfill({
        json: {
          installations: [
            {
              environment: "scoped-prod",
              installationId: "native-api",
              product: "native-product",
              desiredVersion: "1",
            },
            {
              environment: "foreign-observed",
              installationId: "observed-only",
              product: "unknown",
            },
          ],
        },
      });
    if (path === "/v1/intelligence/logic")
      return route.fulfill({
        json: {
          adapter: "recorded",
          timeout: "45s",
          gateRule: "memory <= 1Gi",
          label: "No model call",
        },
      });
    if (path === "/v1/intelligence/courses")
      return route.fulfill({
        json: {
          label: "No model call",
          options: [
            {
              id: "coa-512",
              title: "512Mi option",
              value: "512Mi",
              outcome: "PASS",
              blocked: false,
              reason: "within ceiling",
              tradeoff: "bounded memory",
              label: "No model call",
            },
            {
              id: "coa-8gi",
              title: "8Gi option",
              value: "8Gi",
              outcome: "FAIL_REQUIRED",
              blocked: true,
              reason: "exceeds ceiling",
              tradeoff: "blocked",
              label: "No model call",
            },
          ],
        },
      });
    if (path === "/v1/intelligence/courses/select") {
      const body = route.request().postDataJSON();
      selections.push(body);
      created = true;
      return route.fulfill({
        json: {
          valueTier: "CONDUIT_NATIVE",
          changeRequestId: "intel-request",
          status: "PENDING_APPROVALS",
          environment: body.Environment,
          installationId: body.InstallationID,
          productId: body.ProductID,
          value: body.Value,
        },
      });
    }
    if (path === "/v1/change-requests")
      return route.fulfill({
        json: {
          changeRequests: created
            ? [
                {
                  id: "intel-request",
                  title: "Scoped memory proposal",
                  targetKind: "CONDUIT_NATIVE_INTELLIGENCE_COURSE",
                  environment: "scoped-prod",
                  author: { username: "intel-writer" },
                  status: "PENDING_APPROVALS",
                  changes: [
                    {
                      id: "memory",
                      diff: { "limits.memory": { after: "512Mi" } },
                    },
                  ],
                  requirements: [{ kind: "TEAM", team: "release-reviewers" }],
                },
              ]
            : [],
        },
      });
    if (path === "/v1/intelligence/evals")
      return route.fulfill({ json: { cases } });
    if (path === "/v1/intelligence/evals/run")
      return route.fulfill({
        json: {
          label: "No model call",
          results: cases.map((item) => ({
            case: item,
            columns: [
              {
                adapter: "recorded",
                outcome: item.expectedOutcome,
                passed: true,
                reason: "recorded gate",
                label: "No model call",
              },
            ],
          })),
        },
      });
    if (path === "/v1/intelligence/evals/cases") {
      const body = route.request().postDataJSON();
      feedback.push(body);
      const added = {
        id: "feedback-native",
        scenario: body.Scenario,
        boundObject: body.BoundObject,
        value: body.Value,
        expectedOutcome: body.ExpectedOutcome,
        source: "operator feedback",
      };
      cases.push(added);
      return route.fulfill({ status: 201, json: added });
    }
    if (path === "/v1/intelligence/events")
      return route.fulfill({
        json: {
          label: "Example data",
          note: "These rules do not execute.",
          observed: [
            {
              runId: "visible-run",
              phase: "BLOCKED",
              outcome: "FAIL_REQUIRED",
            },
          ],
          rules: [
            {
              name: "Example proposal rule",
              executes: false,
              label: "Example data",
            },
          ],
        },
      });
    return route.fulfill({ json: {} });
  });
  await hydrate(page);
  await page.goto("/console/");
  await page
    .getByRole("button", { name: "Intelligence", exact: true })
    .first()
    .click();
  const studio = page.locator("[data-intel-studio]");
  await expect(studio.locator('[name="target"] option')).toHaveCount(1);
  await expect(
    studio.locator('[name="value"] option[value="coa-8gi"]'),
  ).toBeDisabled();
  await expect(
    studio.getByRole("button", { name: "Create change request" }),
  ).toBeDisabled();
  await studio.locator("[data-intel-confirm]").check();
  await studio.getByRole("button", { name: "Create change request" }).click();
  await expect(studio.locator("[data-intel-note]")).toContainText(
    "Request intel-request",
  );
  expect(selections).toEqual([
    {
      Value: "512Mi",
      Environment: "scoped-prod",
      InstallationID: "native-api",
      ProductID: "native-product",
    },
  ]);
  await studio
    .getByRole("button", { name: "Open associated change request" })
    .click();
  await expect(page.locator("[data-cr-root]")).toContainText(
    "Scoped memory proposal",
  );
  await expect(page.locator("[data-cr-decide]")).toHaveCount(0);
  await page
    .getByRole("button", { name: "Intelligence", exact: true })
    .first()
    .click();
  await page.locator('[data-area-tab="evals"]').click();
  await expect(page.locator("[data-intel-evals]")).toContainText(
    "No observed adapter matches",
  );
  await page.getByRole("button", { name: "Run supplied cases" }).click();
  await expect(page.locator("[data-intel-evals]")).toContainText(
    "2 / 2 expectations matched",
  );
  await expect(page.locator("[data-intel-evals]")).toContainText(
    "not a deployment gate pass",
  );
  const add = page.locator("[data-intel-add-case]");
  await add.locator('[name="scenario"]').fill("new feedback");
  await add.locator('[name="value"]').fill("1Gi");
  await add.locator('[name="boundObject"]').fill("owned-case");
  await add.getByRole("button", { name: "Add feedback case" }).click();
  expect(feedback).toHaveLength(0);
  await add.locator("[data-case-confirm]").check();
  await add.getByRole("button", { name: "Add feedback case" }).click();
  await expect(page.locator("[data-intel-evals]")).toContainText(
    "feedback-native",
  );
  expect(feedback).toHaveLength(1);
  await page.locator('[data-area-tab="courses"]').click();
  await expect(studio).toContainText("Courses of action");
  await studio.locator("[data-intel-confirm]").check();
  await studio.getByRole("button", { name: "Create change request" }).click();
  await expect(studio.locator("[data-intel-note]")).toContainText(
    "Submission already attempted",
  );
  expect(selections).toHaveLength(1);
  await page.locator('[data-area-tab="events"]').click();
  await expect(page.locator("[data-intel-events]")).toContainText(
    "visible-run",
  );
  await expect(
    page
      .locator("[data-intel-events]")
      .getByRole("button", { name: "Pause unavailable" }),
  ).toBeDisabled();
  expect(forbidden).toEqual([]);
});

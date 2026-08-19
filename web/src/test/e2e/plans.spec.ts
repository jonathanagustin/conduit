import { expect, test } from "./harness.js";
import { PlanDetailPom, PlanIndexPom } from "./pages/index.js";

/** Plans and Plan detail -- P0 use cases "What will or did execute?" and the
 * Plan index. The failure presentation follows captures 052 and 053 in
 * docs/ui/upstream-screen-evidence.tsv: a failed Plan shows the timeline and
 * the raw Agent output, and a failed Helm rollout names the Kubernetes
 * resources that did not become ready. */

function planRecord(overrides: Record<string, unknown> = {}) {
  return {
    id: "pln_deploy",
    desiredStateDigest: "sha256:aaa",
    plan: {
      environment: "prod-use1",
      generation: "0123456789abcdef",
      steps: [
        {
          position: 1,
          action: "RECONCILE",
          installationId: "payments",
          entity: "payments",
          product: "app.example:payments",
          version: "2.4.1",
          namespace: "payments",
        },
      ],
    },
    status: "SUCCEEDED",
    requiredApprovals: 2,
    approvals: [{ subject: "reviewer" }],
    createdAt: "2026-08-07T12:00:00Z",
    updatedAt: "2026-08-07T12:05:00Z",
    revision: 3,
    ...overrides,
  };
}

function planSummary(overrides: Record<string, unknown> = {}) {
  return {
    id: "pln_deploy",
    environment: "prod-use1",
    status: "SUCCEEDED",
    stepCount: 1,
    approvalCount: 1,
    requiredApprovals: 2,
    createdAt: "2026-08-07T12:00:00Z",
    updatedAt: "2026-08-07T12:05:00Z",
    revision: 3,
    ...overrides,
  };
}

function planPage(plans: unknown[], pagination: Record<string, unknown> = {}) {
  return { plans, total: plans.length, pageSize: plans.length, limit: 100, hasPrevious: false, ...pagination };
}

test.describe("Plans", () => {
  test("lists Plans with their status and approval progress", async ({ page, hub }) => {
    hub.get("/plans", planPage([planSummary()]));
    await page.goto("plans");

    const row = page.getByRole("row", { name: /pln_deploy/ });
    await expect(row).toBeVisible();
    await expect(row).toContainText("prod-use1");
    await expect(row).toContainText("SUCCEEDED");
    // One of two approvals: an operator has to see the shortfall, not just a status.
    await expect(row).toContainText("1 of 2");
  });

  test("says so when there are no Plans", async ({ page, hub }) => {
    hub.get("/plans", planPage([]));
    await page.goto("plans");

    await expect(page.getByText("No Plans have been created.")).toBeVisible();
  });

  test("opens a Plan from the list", async ({ page, hub }) => {
    hub.get("/plans", planPage([planSummary()]));
    hub.get("/plans/pln_deploy", planRecord());
    await page.goto("plans");

    await page.getByRole("link", { name: "pln_deploy" }).click();

    await expect(page.getByTestId("plan-detail")).toBeVisible();
    await expect(page).toHaveURL(/\/console\/plans\/pln_deploy$/);
  });

  test("keeps submitted search and opaque traversal in the URL", async ({ page, hub }) => {
    hub.get("/plans", planPage([planSummary()], { total: 2, nextCursor: "opaque-next" }));
    hub.get("/plans", planPage([planSummary({ id: "pln_second" })], { total: 2, hasPrevious: true }), { cursor: "opaque-next" });
    const plans = new PlanIndexPom(page);
    await page.goto("plans");

    await plans.search.fill("payments");
    await plans.search.press("Enter");
    await expect(page).toHaveURL(/search=payments/);
    await expect.poll(() => hub.requests().at(-1)).toContain("search=payments");

    await plans.next.click();
    await expect(page).toHaveURL(/cursor=opaque-next/);
    await expect(plans.root.getByRole("link", { name: "pln_second" })).toBeVisible();
    await expect.poll(() => hub.requests().at(-1)).toContain("view=summary");
  });

  test("preserves search context when the Plan index fails and retries", async ({ page, hub }) => {
    hub.fails("/plans", 500, "plan projection unavailable");
    const plans = new PlanIndexPom(page);
    await page.goto("plans?search=payments");

    await expect(page.getByRole("heading", { name: 'Plans matching "payments" could not be read' })).toBeVisible();
    await expect(page.getByText("plan projection unavailable")).toBeVisible();

    hub.get("/plans", planPage([planSummary()]));
    await plans.retry.click();
    await expect(plans.root.getByRole("link", { name: "pln_deploy" })).toBeVisible();
    await expect.poll(() => hub.requests().at(-1)).toContain("search=payments");
  });

  test("shows the ordered steps a Plan will execute", async ({ page, hub }) => {
    hub.get("/plans/pln_deploy", planRecord());
    await page.goto("plans/pln_deploy");

    const step = page.getByRole("row", { name: /payments/ });
    await expect(step).toContainText("RECONCILE");
    await expect(step).toContainText("app.example:payments");
    await expect(step).toContainText("2.4.1");
  });

  test("says a Plan has not executed rather than showing an empty timeline", async ({
    page,
    hub,
  }) => {
    hub.get("/plans/pln_deploy", planRecord({ status: "AWAITING_APPROVAL" }));
    await page.goto("plans/pln_deploy");

    await expect(page.getByText(/has not executed/)).toBeVisible();
  });

  test("attributes a failure and keeps the tasks behind it visible", async ({ page, hub }) => {
    // Upstream shows a run reaching 100% with a mid-list failure and later
    // tasks succeeding. A failed task must not truncate the timeline.
    hub.get(
      "/plans/pln_deploy",
      planRecord({
        status: "FAILED",
        failure: "helm upgrade failed",
        failedInstallationId: "payments",
        execution: {
          startedAt: "2026-08-07T12:00:00Z",
          finishedAt: "2026-08-07T12:04:00Z",
          tasks: [
            { sequence: 1, phase: "VALIDATE", status: "SUCCEEDED" },
            {
              sequence: 2,
              phase: "APPLY",
              status: "FAILED",
              installationId: "payments",
              failure: "0/3 nodes are available: 3 Insufficient memory.",
              agentOutput: "Error: UPGRADE FAILED",
              resources: [
                {
                  apiVersion: "apps/v1",
                  kind: "Deployment",
                  namespace: "payments",
                  name: "payments-api",
                  reason: "ProgressDeadlineExceeded",
                  message: "ReplicaSet did not become ready before the deadline.",
                },
                {
                  apiVersion: "v1",
                  kind: "Pod",
                  namespace: "payments",
                  name: "payments-api-7f6d9",
                  reason: "Unschedulable",
                  message: "0/3 nodes are available: 3 Insufficient memory.",
                },
              ],
            },
            { sequence: 3, phase: "OBSERVE", status: "SUCCEEDED" },
          ],
        },
      }),
    );
    await page.goto("plans/pln_deploy");
    const detail = new PlanDetailPom(page);

    await expect(page.getByText("This Plan failed")).toBeVisible();
    await expect(page.getByText(/attributed to payments/)).toBeVisible();
    await expect(
      page.locator(".bp6-callout").getByText("0/3 nodes are available: 3 Insufficient memory."),
    ).toBeVisible();
    const diagnostics = detail.resourceDiagnostics("APPLY");
    await expect(diagnostics).toBeVisible();
    await expect(diagnostics).toContainText("Deployment/payments-api (apps/v1)");
    await expect(diagnostics).toContainText("ProgressDeadlineExceeded");
    await expect(diagnostics).toContainText("Pod/payments-api-7f6d9 (v1)");
    await expect(diagnostics).toContainText("Unschedulable");
    await expect(diagnostics).toContainText("payments");
    await page.setViewportSize({ width: 390, height: 844 });
    await expect
      .poll(() => diagnostics.evaluate((element) => element.scrollWidth <= element.clientWidth))
      .toBe(true);
    // The task after the failure is still listed.
    await expect(detail.task("OBSERVE")).toBeVisible();
  });

  test("frames the diagnostics as a real Blueprint panel with a 4px corner", async ({
    page,
    hub,
  }) => {
    // The panel is a Blueprint Section now, not a styled <details>, so its 4px
    // corner comes from --bp-surface-border-radius rather than a radius rule on
    // a raw element. Asserting the computed corner guards that the primitive,
    // not the removed CSS, is what draws the panel.
    hub.get(
      "/plans/pln_deploy",
      planRecord({
        status: "FAILED",
        execution: {
          tasks: [
            {
              sequence: 1,
              phase: "APPLY",
              status: "FAILED",
              resources: [{ kind: "Pod", name: "payments-api", reason: "Unschedulable" }],
            },
          ],
        },
      }),
    );
    await page.goto("plans/pln_deploy");
    const detail = new PlanDetailPom(page);

    const panel = detail.task("APPLY").locator(".console-plan-detail__diagnostics");
    await expect(panel).toHaveClass(/bp6-section/);
    await expect
      .poll(() => panel.evaluate((element) => getComputedStyle(element).borderTopLeftRadius))
      .toBe("4px");
  });

  test("keeps raw Agent output collapsed until asked for", async ({ page, hub }) => {
    // The output is bounded and redacted by the Hub, but it is still noise on a
    // screen whose question is "what happened"; upstream collapses it too.
    hub.get(
      "/plans/pln_deploy",
      planRecord({
        status: "FAILED",
        execution: {
          tasks: [
            { sequence: 1, phase: "APPLY", status: "FAILED", agentOutput: "Error: UPGRADE FAILED" },
          ],
        },
      }),
    );
    await page.goto("plans/pln_deploy");
    const detail = new PlanDetailPom(page);

    await expect(page.getByText("Error: UPGRADE FAILED")).toBeHidden();
    await detail.planOutput.click();
    await expect(page.getByText("Error: UPGRADE FAILED")).toBeVisible();
  });

  test("reports a cancelled Plan with who cancelled it and why", async ({ page, hub }) => {
    hub.get(
      "/plans/pln_deploy",
      planRecord({
        status: "CANCELLED",
        cancellation: { subject: "operator-one", reason: "superseded by a newer generation" },
      }),
    );
    await page.goto("plans/pln_deploy");

    await expect(page.getByText("This Plan was cancelled")).toBeVisible();
    await expect(page.getByText(/superseded by a newer generation/)).toBeVisible();
  });

  test("distinguishes a Plan that is not visible from one that failed to load", async ({
    page,
    hub,
  }) => {
    hub.fails("/plans/pln_deploy", 403, "forbidden");
    await page.goto("plans/pln_deploy");

    await expect(page.getByText("Not visible to you")).toBeVisible();
  });
});

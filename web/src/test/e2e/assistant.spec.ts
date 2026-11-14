import { expect, test, type Page, type Route } from "@playwright/test";

const BOOTSTRAP_MARKER = "<!--conduit-console-bootstrap-->";
const BOOTSTRAP = {
  schemaVersion: "conduit.console.bootstrap/v1",
  tenant: { id: "conduit", displayName: "Conduit" },
  brand: {
    title: "Conduit",
    subtitle: "Control plane",
    accent: "#2d72d2",
    icon: "dashboard",
  },
  provenance: {
    claimClass: "conduit-extension",
    evidenceRef: "conduit://console/default",
  },
};

function escapeAttribute(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&#34;")
    .replaceAll("'", "&#39;");
}

async function hydrate(page: Page): Promise<void> {
  const meta = `<meta name="x-conduit-console-bootstrap" content="${escapeAttribute(JSON.stringify(BOOTSTRAP))}" />`;
  await page.route("**/console/**", async (route: Route) => {
    if (route.request().resourceType() !== "document") {
      await route.fallback();
      return;
    }
    const response = await route.fetch();
    await route.fulfill({
      response,
      body: (await response.text()).replace(BOOTSTRAP_MARKER, meta),
    });
  });
}

test("refuses to start when the Hub did not render a tenant bootstrap", async ({
  page,
}) => {
  await page.goto("/console/");
  await expect(
    page.getByTestId("console-configuration-unavailable"),
  ).toBeVisible();
});

test("opens straight to the operator workbench", async ({ page }) => {
  const assistantBridge: string[] = [];
  page.on("request", (request) => {
    if (new URL(request.url()).pathname.startsWith("/api/assistant")) {
      assistantBridge.push(request.url());
    }
  });
  await hydrate(page);
  await page.goto("/console/");
  await expect(page).toHaveTitle("Conduit Console");
  await expect(page.locator("#operator-root")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Command Center" }).first(),
  ).toBeVisible();
  await expect(page.getByText("Starter Template")).toHaveCount(0);
  await expect(page.getByText("View Source")).toHaveCount(0);
  await expect(page.getByText(/bridge is unreachable/i)).toHaveCount(0);
  await page.waitForLoadState("networkidle");
  expect(assistantBridge).toEqual([]);
});

test.describe("workbench", () => {
  test("shows the workbench from hub routes", async ({ page }) => {
    test.setTimeout(60_000);
    let approved = false;
    await page.route("**/health/live", (route) =>
      route.fulfill({ json: { ok: true } }),
    );
    await page.route("**/v1/environments**", (route) => {
      const url = route.request().url();
      if (url.includes("reported-state")) {
        return route.fulfill({
          json: {
            agentId: "example-spoke",
            observedAt: "2026-11-14T00:00:00Z",
            entities: [{ name: "sample" }],
          },
        });
      }
      if (/\/v1\/environments\/[^/?]+/.test(url)) {
        return route.fulfill({
          json: {
            environment: {
              id: "lab",
              generation: "4",
              connectionSetting: "spoke",
              defaultReleaseChannel: "stable",
              labels: { tier: "example" },
              maintenance: {
                noDowntime: { timeZone: "UTC", intervals: ["Mon 00:00-04:00"] },
              },
              config: { replicas: "2" },
            },
          },
        });
      }
      return route.fulfill({
        json: {
          environments: [
            {
              environment: { id: "lab", connectionSetting: "spoke" },
              reported: { environment: "lab" },
            },
          ],
          total: 1,
        },
      });
    });
    await page.route("**/v1/products**", (route) => {
      if (route.request().url().includes("view=full")) {
        return route.fulfill({
          json: {
            products: [
              {
                product: "sample-app",
                releases: [
                  { version: "1.2.0", channels: ["candidate", "stable"] },
                ],
              },
            ],
            total: 1,
          },
        });
      }
      return route.fulfill({
        json: { products: [{ product: "sample-app" }], total: 1 },
      });
    });
    await page.route("**/v1/fleet/installations**", (route) =>
      route.fulfill({
        json: {
          installations: [
            {
              installationId: "sample-a",
              environment: "lab",
              product: "sample-app",
              health: "HEALTHY",
            },
            {
              installationId: "sample-b",
              environment: "lab",
              product: "sample-app",
              health: "ERROR",
            },
          ],
        },
      }),
    );
    await page.route("**/v1/change-requests**", (route) =>
      route.fulfill({
        json: {
          changeRequests: [
            {
              id: "change-1",
              title: "Example change",
              status: "IN_REVIEW",
              environment: "lab",
              requirementsSatisfied: false,
              changes: [{ diff: { replicas: { before: "1", after: "2" } } }],
              reviews: [{ decision: "comment", rationale: "Example review" }],
            },
          ],
        },
      }),
    );
    await page.route("**/v1/promotions**", (route) =>
      route.fulfill({
        json: {
          promotions: [
            {
              id: "promo-1",
              product: "sample-app",
              version: "1.2.0",
              fromChannel: "candidate",
              toChannel: "stable",
              status: "BLOCKED",
              reasons: ["Example: maintenance window is closed"],
            },
          ],
        },
      }),
    );
    await page.route("**/v1/plans**", async (route) => {
      const url = route.request().url();
      if (route.request().method() === "POST" && url.includes("/approvals")) {
        approved = true;
        await route.fulfill({ json: { id: "plan-1", status: "APPROVED" } });
        return;
      }
      await route.fulfill({
        json: {
          plans: [
            approved
              ? {
                  id: "plan-1",
                  status: "APPROVED",
                  requiredApprovals: 1,
                  approvals: [{ subject: "operator" }],
                  plan: { environment: "lab", steps: [{}] },
                }
              : {
                  id: "plan-1",
                  status: "AWAITING_APPROVAL",
                  requiredApprovals: 1,
                  plan: { environment: "lab", steps: [{}] },
                },
            {
              id: "plan-failed",
              status: "FAILED",
              failure: "Example rollout stopped",
              requiredApprovals: 1,
              plan: { environment: "lab", steps: [{}] },
              execution: {
                tasks: [
                  {
                    phase: "apply",
                    status: "FAILED",
                    agentOutput: "example output",
                  },
                ],
              },
            },
          ],
          total: 1,
        },
      });
    });
    await page.route("**/v1/packaging-runs**", async (route) => {
      const url = route.request().url();
      if (url.includes("/events")) {
        await route.fulfill({
          json: {
            configured: true,
            items: [
              {
                sequence: 1,
                at: "2026-11-14T00:00:00Z",
                step: "review",
                outcome: "waiting",
                reason: "example gate",
              },
            ],
            total: 1,
          },
        });
        return;
      }
      if (
        route.request().method() === "GET" &&
        !url.includes("sample-app-1") &&
        !url.includes("missing")
      ) {
        await route.fulfill({
          json: {
            configured: true,
            items: [
              {
                configured: true,
                phase: "review",
                runId: "sample-app-1",
                planId: "plan-1",
                tier: "CONDUIT_NATIVE",
              },
            ],
            total: 1,
          },
        });
        return;
      }
      await route.fallback();
    });
    await page.route(
      "**/*PackagingRunService/GetPackagingRun",
      async (route) => {
        const body = route.request().postDataJSON() as { runId?: string };
        const missing = body.runId === "missing";
        await route.fulfill({
          json: missing
            ? { configured: false, tier: "CONDUIT_NATIVE", runId: "missing" }
            : {
                configured: true,
                phase: "review",
                runId: "sample-app-1",
                planId: "plan-1",
                tier: "CONDUIT_NATIVE",
                sourceDigest: "sha256:example",
                artifactDigest: "sha256:artifact-example",
                proposalRevision: "rev-1",
                targetPlanRef: "lab/plan-1",
              },
        });
      },
    );
    await hydrate(page);
    await page.goto("/console/");
    await expect(page.getByText("LIVE", { exact: true })).toHaveCount(0);
    await expect(page.getByText("EXAMPLE DATA", { exact: true })).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Command Center" }).first(),
    ).toBeVisible();
    await expect(
      page.locator("#header-kpi-strip").getByText("Convergence").first(),
    ).toBeVisible();
    await page.evaluate(() => {
      const view = window as unknown as {
        switchScreen: (id: string) => void;
        toggleChatRail: () => void;
      };
      view.switchScreen("area-packaging");
    });
    await expect(page.getByText("sample-app-1").first()).toBeVisible();
    await page.evaluate(() => {
      history.pushState(null, "", "#s08-staged-run");
      window.dispatchEvent(new HashChangeEvent("hashchange"));
    });
    await expect(page.getByRole("button", { name: "Timeline" })).toBeVisible();
    await page.evaluate(() => {
      (
        window as unknown as { switchScreen: (id: string) => void }
      ).switchScreen("s16-approvals-board");
    });
    await expect(page.getByText("PLN-2026-0418").first()).toBeVisible();
    await page.evaluate(() => {
      (
        window as unknown as { switchScreen: (id: string) => void }
      ).switchScreen("rm09-recalls");
    });
    await expect(
      page.getByText("No recall is recorded.").first(),
    ).toBeVisible();
    await page.evaluate(() => {
      (
        window as unknown as { switchScreen: (id: string) => void }
      ).switchScreen("rm10-ramped-rollout");
    });
    await expect(page.getByText("Canary pacing").first()).toBeVisible();
    await page.evaluate(() => {
      (
        window as unknown as { switchScreen: (id: string) => void }
      ).switchScreen("rm11-debug-terminal");
    });
    await expect(
      page.getByText("session brokering not implemented").first(),
    ).toBeVisible();
    await page.evaluate(() => {
      (
        window as unknown as { switchScreen: (id: string) => void }
      ).switchScreen("pt01-proposal-trace");
    });
    await expect(page.getByText("Recorded proposal").first()).toBeVisible();
    await page.evaluate(() => {
      (
        window as unknown as { switchScreen: (id: string) => void }
      ).switchScreen("og01-ontology-graph");
    });
    await expect(page.getByText("reportingSpoke").first()).toBeVisible();
    await expect(
      page
        .locator("#main-workspace")
        .getByText(/Example data|Ontology API/)
        .first(),
    ).toBeVisible();
    await page.evaluate(() => {
      (
        window as unknown as { switchScreen: (id: string) => void }
      ).switchScreen("adm01-user-prefs");
    });
    await expect(page.getByText("Personal API tokens").first()).toBeVisible();
    await page.evaluate(() => {
      (
        window as unknown as { switchScreen: (id: string) => void }
      ).switchScreen("adm02-org-control-panel");
    });
    await expect(
      page.getByRole("radio", { name: "Organizations" }).first(),
    ).toBeVisible();
    await page.evaluate(() => {
      (
        window as unknown as { switchScreen: (id: string) => void }
      ).switchScreen("adm03-auth-signin");
    });
    await expect(
      page.getByText("Sign-in is not enabled on this Hub").first(),
    ).toBeVisible();
    await page.evaluate(() => {
      (window as unknown as { toggleChatRail: () => void }).toggleChatRail();
    });
    await expect(page.getByLabel("Message input")).toBeVisible();
  });
});

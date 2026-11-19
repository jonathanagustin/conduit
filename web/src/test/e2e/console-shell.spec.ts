import { expect, test } from "@playwright/test";
import { APPLICATIONS, approvalReceipt } from "./hub-stub";
import { installPackagingReadStub, hydrate } from "./console-fixture";

installPackagingReadStub();

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
    // The KPI strip is part of the wide layout; compact controls have their own regression.
    await page.setViewportSize({ width: 1920, height: 1080 });
    let approved = false;
    await page.route("**/health/live", (route) =>
      route.fulfill({ json: { ok: true } }),
    );
    await page.route("**/v1/session**", (route) =>
      route.fulfill({ json: { applications: APPLICATIONS } }),
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
    await page.route(
      "**/conduit.plans.v1.PlanApprovalService/ApprovePlan",
      async (route) => {
        approved = true;
        await route.fulfill({
          json: approvalReceipt("plan-1", "APPROVED", "operator"),
        });
      },
    );
    await page.route("**/v1/plans**", async (route) => {
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
      "**/conduit.packaging.v1.PackagingRunService/ListPackagingRuns",
      (route) =>
        route.fulfill({
          json: {
            configured: true,
            items: [
              {
                runId: "sample-app-1",
                phase: "review",
                planId: "plan-1",
                tier: "CONDUIT_NATIVE",
              },
            ],
            total: "1",
            pageSize: 1,
            limit: 20,
          },
        }),
    );
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
    // The Command Center opens on its live overview; the example fleet is
    // its own tab and says so.
    const badge = page.locator("#example-data-badge");
    await expect(badge).toHaveText("LIVE");
    await expect(
      page.getByRole("heading", { name: "Waiting on a decision" }),
    ).toBeVisible();
    await page.evaluate(() => {
      (
        window as unknown as { switchScreen: (id: string) => void }
      ).switchScreen("area-command");
    });
    await page.getByRole("button", { name: "Example fleet" }).click();
    await expect(badge).toHaveText("UNAVAILABLE");
    await page
      .locator("#main-workspace")
      .getByRole("button", { name: "Show example data", exact: true })
      .click();
    await expect(badge).toHaveText("EXAMPLE DATA");
    await page.evaluate(() => {
      (
        window as unknown as { switchScreen: (id: string) => void }
      ).switchScreen("area-packaging");
    });
    await expect(badge).toHaveText("LIVE");
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
    await expect(
      page
        .locator("[data-approvals-board]")
        .getByText("plan-1", { exact: true }),
    ).toBeVisible();
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
    await expect(page.locator("[data-release-waves]")).toBeVisible();
    await expect(page.locator("[data-release-calendar]")).toContainText(
      "Resolved scheduling data is unavailable",
    );
    await expect(page.getByText("Canary pacing")).toHaveCount(0);
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
    await expect(
      page.getByText("Metadata unavailable; no example data substituted", {
        exact: true,
      }),
    ).toBeVisible();
    await expect(
      page.locator('svg[aria-label="Ontology graph"] g'),
    ).toHaveCount(0);
    await expect(
      page
        .locator("#main-workspace")
        .getByText(
          /Native metadata|Metadata unavailable|No object types returned/,
        )
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
      page.getByText("Identity configuration is unavailable.").first(),
    ).toBeVisible();
    await page.evaluate(() => {
      (window as unknown as { toggleChatRail: () => void }).toggleChatRail();
    });
    await expect(page.getByLabel("Message input")).toBeVisible();
  });
});

test("a view whose renderer throws is contained and the shell stays usable", async ({
  page,
}) => {
  const records: Record<string, unknown>[] = [];
  page.on("console", (message) => {
    if (message.type() !== "error") return;
    void message
      .args()[0]
      ?.jsonValue()
      .then((value) => {
        if (value?.event === "conduit.console.client_error")
          records.push(value);
      })
      .catch(() => {});
  });
  // Transport validation refuses malformed rows. Plant a renderer failure
  // after the valid page loads to exercise the separate containment boundary.
  await page.route("**/v1/**", (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/v1/session")
      return route.fulfill({ json: { applications: APPLICATIONS } });
    return path === "/v1/plans"
      ? route.fulfill({
          json: {
            plans: [
              {
                id: "render-check",
                status: "SUCCEEDED",
                plan: { environment: "edge-a", steps: [] },
              },
            ],
          },
        })
      : route.fulfill({ json: {} });
  });
  await hydrate(page);
  await page.goto("/console/");
  await page.evaluate(() =>
    (window as unknown as { switchScreen: (id: string) => void }).switchScreen(
      "s16-approvals-board",
    ),
  );
  await expect(page.locator("[data-approvals-board]")).toContainText(
    "render-check",
  );
  const breakView = () =>
    page.evaluate(() => {
      const app = window as unknown as {
        state: { hub: { plans: unknown[] } };
        renderMainWorkspace(): void;
      };
      app.state.hub.plans = [null];
      app.renderMainWorkspace();
    });
  await breakView();
  const failure = page.locator('[data-render-failure="s16-approvals-board"]');
  await expect(failure).toContainText("This view failed to render");
  await expect(failure.locator("[data-render-failure-screen]")).toHaveText(
    "s16-approvals-board",
  );
  await expect(failure.getByRole("button", { name: "Reload" })).toBeVisible();
  await expect(page.locator("[data-approvals-board]")).toHaveCount(0);
  // A failed view is not a Hub read and must not carry the live label.
  await expect(page.locator("#example-data-badge")).toHaveText("UNAVAILABLE");
  await expect
    .poll(
      () =>
        records.filter(
          (record) =>
            record.kind === "render" && record.screen === "s16-approvals-board",
        ).length,
    )
    .toBeGreaterThan(0);
  // The contained failure is the only client error this session logged.
  expect(records.map((record) => record.kind)).toEqual(["render"]);
  expect(Object.keys(records[0]!).sort()).toEqual([
    "build",
    "event",
    "kind",
    "message",
    "name",
    "screen",
    "stack",
  ]);
  expect(String(records[0]!.build)).toMatch(/^index-[\w-]+\.js$/);

  // The area tabs beside the failed view still switch.
  await page.locator('[data-area-tab="gate"]').click();
  await expect(page.locator("#area-body")).toContainText(
    "Action constraint gate",
  );
  await expect(page.locator("[data-render-failure]")).toHaveCount(0);
  // And so does the left navigation.
  await page
    .getByRole("button", { name: "Audit", exact: true })
    .first()
    .click();
  await expect(page.locator('[data-area-tab="chain"]')).toBeVisible();
  await expect(page.locator("[data-render-failure]")).toHaveCount(0);

  // Reload returns to a working console.
  await page.evaluate(() =>
    (window as unknown as { switchScreen: (id: string) => void }).switchScreen(
      "s16-approvals-board",
    ),
  );
  await expect(page.locator("[data-approvals-board]")).toContainText(
    "render-check",
  );
  await breakView();
  await Promise.all([
    page.waitForEvent("load"),
    failure.getByRole("button", { name: "Reload" }).click(),
  ]);
  await expect(page.locator("#operator-root")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Command Center" }).first(),
  ).toBeVisible();
});

test("engineer focus preserves one live conversation, draft and pending request", async ({
  page,
}, testInfo) => {
  await page.setViewportSize({ width: 1440, height: 1024 });
  await hydrate(page);
  await page.addInitScript(() =>
    localStorage.setItem("conduit.console.dockWidth", "420"),
  );
  const writes: string[] = [];
  // The first send opens a Hub thread, then sends the turn into it.
  const TURN_WRITES = ["/v1/assistant/threads", "/v1/assistant/turns"];
  let calls = 0;
  let release!: () => void;
  const responseReady = new Promise<void>((resolve) => {
    release = resolve;
  });
  let failed = 0;
  page.on("requestfailed", (request) => {
    if (new URL(request.url()).pathname === "/v1/assistant/turns") failed++;
  });
  await page.route("**/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (route.request().method() !== "GET") writes.push(path);
    if (
      path === "/v1/assistant/threads" &&
      route.request().method() === "POST"
    ) {
      await route.fulfill({
        status: 201,
        json: { id: "ath_1", title: "", messageCount: 0, messages: [] },
      });
      return;
    }
    if (path === "/v1/assistant/turns") {
      calls++;
      await responseReady;
      await route.fulfill({
        json: {
          label: "Fixture response",
          text: "Recorded conversation response.",
          model: {
            configuredModel: "requested-alias",
            providerModel: "served-version",
          },
          results: [],
        },
      });
      return;
    }
    await route.fulfill({
      json: { items: [], plans: [], environments: [], bundles: [], events: [] },
    });
  });
  await page.goto("/console/#s09-source");
  const studio = page.locator("[data-workspace-studio]");
  await expect(studio).toBeVisible();
  const entry = studio.getByRole("button", {
    name: "Open engineer workspace",
    exact: true,
  });
  await entry.click();
  const root = page.locator("#operator-root");
  const rail = page.locator("#right-chat-rail");
  const composer = page.getByLabel("Message input");
  await expect(root).toHaveAttribute("data-engineer-focus", "true");
  await expect(composer).toBeVisible();
  const mount = await page.locator("#assistant-thread-mount").elementHandle();
  const input = await composer.elementHandle();
  await composer.fill("Unsent engineering notes");
  await page
    .getByRole("button", { name: "Return to workbench", exact: true })
    .click();
  await expect(entry).toBeFocused();
  await expect(composer).toHaveValue("Unsent engineering notes");
  expect(await rail.evaluate((el) => el.getBoundingClientRect().width)).toBe(
    420,
  );
  await rail
    .getByRole("button", { name: "Open engineer workspace", exact: true })
    .click();
  expect(
    await mount!.evaluate(
      (el) => el === document.querySelector("#assistant-thread-mount"),
    ),
  ).toBe(true);
  expect(
    await input!.evaluate(
      (el) => el === document.querySelector('[aria-label="Message input"]'),
    ),
  ).toBe(true);
  const guidance = rail.locator("[data-engineer-guidance]");
  await expect(guidance).toBeVisible();
  const capabilities = guidance.getByText("Capabilities", { exact: true });
  await capabilities.focus();
  await capabilities.press("Enter");
  await expect(
    guidance.getByText("Inspect ontology objects", { exact: true }),
  ).toBeVisible();
  await expect(
    guidance.getByText("Propose governed action", { exact: true }),
  ).toHaveCount(0);
  await guidance.getByText("Skills", { exact: true }).click();
  await expect(
    guidance.getByText("No skills inventory is available for this workspace."),
  ).toBeVisible();
  await guidance.getByText("Docs", { exact: true }).click();
  await expect(
    guidance.getByRole("link", { name: "Workspace Studio scope", exact: true }),
  ).toHaveAttribute("rel", "noopener noreferrer");
  await expect(composer).toHaveValue("Unsent engineering notes");
  expect(calls).toBe(0);
  expect(writes).toEqual([]);
  expect(
    await page
      .locator("#main-workspace")
      .evaluate((el) => Boolean(el.parentElement?.inert)),
  ).toBe(true);
  await composer.fill("Inspect <img src=x> recorded source");
  await page.getByRole("button", { name: "Send message", exact: true }).click();
  await expect.poll(() => calls).toBe(1);
  await capabilities.press("Enter");
  await capabilities.press("Enter");
  expect(failed).toBe(0);
  expect(writes).toEqual(TURN_WRITES);
  await page
    .getByRole("button", { name: "Return to workbench", exact: true })
    .click();
  await rail
    .getByRole("button", { name: "Open engineer workspace", exact: true })
    .click();
  expect(
    await mount!.evaluate(
      (el) => el === document.querySelector("#assistant-thread-mount"),
    ),
  ).toBe(true);
  release();
  await expect(
    rail
      .locator('[data-slot="aui_assistant-message-content"]')
      .getByText("Recorded conversation response.", { exact: false }),
  ).toBeVisible();
  await expect(
    rail.locator('[data-slot="aui_assistant-message-content"]'),
  ).toContainText("Model: served-version (configured: requested-alias)");
  await expect(rail.locator("[data-engineer-outline] summary")).toContainText(
    "1 user turn",
  );
  await rail.locator("[data-engineer-outline] summary").click();
  await rail
    .getByRole("button", {
      name: "1. Inspect <img src=x> recorded source",
      exact: true,
    })
    .click();
  await expect(
    rail.locator('[data-message-id][data-role="user"]'),
  ).toBeFocused();
  await expect(rail.locator('img[src="x"]')).toHaveCount(0);
  expect(calls).toBe(1);
  expect(failed).toBe(0);
  expect(writes).toEqual(TURN_WRITES);
  await composer.fill("Keep this draft across Studio navigation");
  await page.setViewportSize({ width: 375, height: 812 });
  const bounds = await rail.boundingBox();
  expect(bounds!.x).toBeGreaterThanOrEqual(-0.01);
  expect(bounds!.width).toBeLessThanOrEqual(375.01);
  await expect(composer).toBeVisible();
  await rail
    .locator("[data-engineer-guidance]")
    .getByText("Capabilities", { exact: true })
    .click();
  await rail
    .locator("[data-engineer-guidance]")
    .getByText("Docs", { exact: true })
    .click();
  await expect(
    rail.getByRole("link", { name: "Identity and governed actions" }),
  ).toBeVisible();
  await expect(composer).toBeVisible();
  await expect(composer).toHaveValue(
    "Keep this draft across Studio navigation",
  );
  const expandedBounds = await rail.boundingBox();
  expect(expandedBounds!.x).toBeGreaterThanOrEqual(-0.01);
  expect(expandedBounds!.x + expandedBounds!.width).toBeLessThanOrEqual(375.01);
  await page.screenshot({
    path: testInfo.outputPath("engineer-guidance-narrow.png"),
  });
  await page.evaluate(() => {
    const event = new KeyboardEvent("keydown", {
      key: "Escape",
      bubbles: true,
      cancelable: true,
    });
    event.preventDefault();
    window.dispatchEvent(event);
  });
  await expect(root).toHaveAttribute("data-engineer-focus", "true");
  await page.keyboard.press("Escape");
  await expect(root).toHaveAttribute("data-engineer-focus", "false");
  expect(
    await page
      .locator("#main-workspace")
      .evaluate((el) => Boolean(el.parentElement?.inert)),
  ).toBe(false);
  await page.setViewportSize({ width: 1364, height: 1024 });
  await rail
    .getByRole("button", { name: "Open engineer workspace", exact: true })
    .click();
  await page
    .getByRole("navigation", { name: "Workspace studio" })
    .getByRole("button", { name: "Preview", exact: true })
    .click();
  await expect(root).toHaveAttribute("data-engineer-focus", "false");
  await expect(page.locator("[data-workspace-studio]")).toBeVisible();
  await expect(composer).toHaveValue(
    "Keep this draft across Studio navigation",
  );
  expect(
    await page.evaluate(() =>
      localStorage.getItem("conduit.console.dockWidth"),
    ),
  ).toBe("420");
  expect(writes).toEqual(TURN_WRITES);
  await rail
    .getByRole("button", { name: "Open engineer workspace", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Close assistant panel", exact: true })
    .click();
  await expect(root).toHaveAttribute("data-engineer-focus", "false");
  await expect(rail).toBeHidden();
  expect(
    await page
      .locator("#main-workspace")
      .evaluate((el) => Boolean(el.parentElement?.inert)),
  ).toBe(false);
  await page.getByTitle("Toggle right chat panel").click();
  await expect(composer).toHaveValue(
    "Keep this draft across Studio navigation",
  );
  await expect(rail.locator("[data-engineer-outline] summary")).toContainText(
    "1 user turn",
  );
  expect(writes).toEqual(TURN_WRITES);
});

test("operator badge shows only a complete native session and clears on refusal", async ({
  page,
}, testInfo) => {
  await hydrate(page);
  const name = '<img src=x onerror="attack()">';
  let phase: "refused" | "valid" | "malformed" = "refused";
  const writes: string[] = [];
  await page.route("**/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (route.request().method() !== "GET") writes.push(path);
    if (path === "/v1/session") {
      if (phase === "refused")
        return route.fulfill({ status: 401, json: { error: "not signed in" } });
      return route.fulfill({
        json:
          phase === "malformed"
            ? { actor: { username: "unverified" } }
            : { actor: { username: "robot" }, effective: { username: name } },
      });
    }
    return route.fulfill({
      json: {
        environments: [],
        installations: [],
        products: [],
        changeRequests: [],
        plans: [],
        promotions: [],
        items: [],
      },
    });
  });
  await page.goto("/console/#area-packaging");
  const badge = page.locator("#operator-chip"),
    label = page.locator("#operator-name");
  await expect(badge).toHaveAttribute("role", "img");
  await expect(badge).toHaveAttribute(
    "aria-label",
    "Operator identity unavailable",
  );
  await expect(label).toHaveText("Identity unavailable");
  await expect(page.locator("#operator-initials")).toHaveText("?");
  phase = "valid";
  await page.evaluate(() => Reflect.get(window, "refreshHub")());
  await expect(label).toHaveText(name);
  await expect(badge).toHaveAttribute("aria-label", `Operator: ${name}`);
  await expect(badge.locator("img")).toHaveCount(0);
  await page.setViewportSize({ width: 375, height: 812 });
  await expect(badge).toBeVisible();
  const bounds = await badge.boundingBox();
  expect(bounds!.x).toBeGreaterThanOrEqual(0);
  expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(375);
  const retry = page.getByRole("button", { name: "Retry live", exact: true });
  await expect(retry).toBeVisible();
  const retryBounds = await retry.boundingBox();
  expect(retryBounds!.x).toBeGreaterThanOrEqual(0);
  expect(retryBounds!.x + retryBounds!.width).toBeLessThanOrEqual(375);

  await page.screenshot({
    path: testInfo.outputPath("operator-identity-mobile.png"),
  });
  await page.keyboard.press("Control+,");
  const settingsIdentity = page.locator("#settings-operator-identity");
  await expect(settingsIdentity).toBeVisible();
  await expect(settingsIdentity).toContainText(name);
  await expect(settingsIdentity.locator("img")).toHaveCount(0);
  phase = "malformed";
  await page.evaluate(() => Reflect.get(window, "refreshHub")());
  await expect(badge).toHaveAttribute(
    "aria-label",
    "Operator identity unavailable",
  );
  await expect(settingsIdentity).toHaveText(
    "Native operator identity is unavailable.",
  );
  phase = "valid";
  await page.evaluate(() => Reflect.get(window, "refreshHub")());
  await expect(label).toHaveText(name);
  await expect(settingsIdentity).toContainText(name);
  phase = "refused";
  await page.evaluate(() => Reflect.get(window, "refreshHub")());
  await expect(badge).toHaveAttribute(
    "aria-label",
    "Operator identity unavailable",
  );
  await expect(label).toHaveText("Identity unavailable");
  await expect(settingsIdentity).toHaveText(
    "Native operator identity is unavailable.",
  );
  expect(writes).toEqual([]);
});

test("personal token examples require opt-in and discard simulated credentials on exit", async ({
  page,
}) => {
  await hydrate(page);
  const writes: string[] = [];
  page.on("request", (request) => {
    if (
      request.method() !== "GET" &&
      new URL(request.url()).pathname.startsWith("/v1/")
    )
      writes.push(request.url());
  });
  await page.route("**/v1/**", (route) => route.fulfill({ json: {} }));
  await page.goto("/console/");
  await page.evaluate(() =>
    Reflect.get(window, "switchScreen")("adm01-user-prefs"),
  );
  const workspace = page.locator("#main-workspace");
  await expect(
    workspace.getByText("This Hub does not issue personal tokens."),
  ).toBeVisible();
  await expect(workspace.getByText("Workstation", { exact: true })).toHaveCount(
    0,
  );
  await expect(
    workspace.getByRole("button", {
      name: "Create example token",
      exact: true,
    }),
  ).toHaveCount(0);
  await workspace
    .getByRole("button", { name: "Show token example data", exact: true })
    .click();
  await expect(workspace.getByRole("status")).toContainText(
    "do not issue or revoke credentials",
  );
  await expect(
    workspace.getByText("Workstation", { exact: true }),
  ).toBeVisible();
  await workspace
    .getByLabel("Name", { exact: true })
    .fill("Browser simulation only");
  await workspace
    .getByRole("button", { name: "Create example token", exact: true })
    .click();
  const row = workspace
    .getByRole("row")
    .filter({ hasText: "Browser simulation only" });
  await expect(row).toContainText("Active");
  await row
    .getByRole("button", { name: "Revoke example", exact: true })
    .click();
  await expect(row).toContainText("Revoked");
  await workspace
    .getByRole("button", { name: "Exit token example mode", exact: true })
    .click();
  await expect(
    workspace.getByText("Browser simulation only", { exact: true }),
  ).toHaveCount(0);
  await expect(workspace.locator("#pref-token-name")).toHaveCount(0);
  await workspace
    .getByRole("button", { name: "Show token example data", exact: true })
    .click();
  await expect(
    workspace.getByText("Browser simulation only", { exact: true }),
  ).toHaveCount(0);
  await expect(workspace.locator("#pref-token-name")).toHaveValue("");
  await page.reload();
  await page.waitForFunction(
    () => typeof Reflect.get(window, "switchScreen") === "function",
  );
  await page.evaluate(() =>
    Reflect.get(window, "switchScreen")("adm01-user-prefs"),
  );
  await expect(
    workspace.getByRole("button", {
      name: "Show token example data",
      exact: true,
    }),
  ).toBeVisible();
  expect(writes).toEqual([]);
});

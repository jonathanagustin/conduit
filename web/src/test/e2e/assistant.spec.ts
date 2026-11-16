import { readFileSync } from "node:fs";
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

test.beforeEach(async ({ page }) => {
  await page.route(
    "**/conduit.packaging.v1.PackagingRunService/ListPackagingRuns",
    (route) =>
      route.fulfill({
        json: {
          configured: true,
          items: [],
          total: "0",
          pageSize: 0,
          limit: 20,
        },
      }),
  );
});

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

test("Audit platform status renders stubbed measurements and refreshes with GET", async ({
  page,
}) => {
  const methods: string[] = [];
  await page.route("**/v1/admin/status", async (route) => {
    methods.push(route.request().method());
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        generatedAt: "2026-11-14T12:00:00Z",
        components: [
          {
            label: "Hub",
            status: "AVAILABLE",
            detail: "This Hub served the request",
          },
        ],
        counters: [
          {
            label: "Pending reviews",
            status: "AVAILABLE",
            value: 0,
            detail: "Stored requests",
            link: "rm07-change-requests",
          },
        ],
        storage: [
          {
            label: "Registry filesystem",
            status: "UNAVAILABLE",
            usedBytes: null,
            capacityBytes: null,
            usedPercent: null,
            warningPercent: 80,
            criticalPercent: 95,
            detail: "No telemetry provider configured",
          },
        ],
        audit: {
          status: "UNAVAILABLE",
          chainLength: null,
          verifiedAt: null,
          detail: "No stored-chain verifier",
        },
      }),
    });
  });
  await hydrate(page);
  await page.goto("/console/");
  await page
    .getByRole("button", { name: "Audit", exact: true })
    .first()
    .click();
  await page
    .getByRole("button", { name: "Platform status", exact: true })
    .click();
  await expect(page.locator("[data-admin-status]")).toBeVisible();
  await expect(page.locator("[data-admin-status]")).toContainText(
    "Registry filesystem",
  );
  await expect(page.locator("[data-admin-status]")).toContainText(
    "Verification time unavailable",
  );
  await page.getByRole("button", { name: "Refresh measurements" }).click();
  await expect.poll(() => methods.length).toBeGreaterThanOrEqual(2);
  expect(methods.every((method) => method === "GET")).toBe(true);
});

test("Command Center uses recorded sites and drafts context without writes", async ({
  page,
}, testInfo) => {
  const mutations: string[] = [];
  const external: string[] = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.pathname.startsWith("/v1/") && request.method() !== "GET")
      mutations.push(`${request.method()} ${url.pathname}`);
    if (!["127.0.0.1", "localhost"].includes(url.hostname))
      external.push(url.origin);
  });
  const environment = (id: string, labels: Record<string, string> = {}) => ({
    environment: { id, labels },
    spoke: { state: "live", agentId: `agent-${id}` },
    reported: { agentId: `agent-${id}`, observedAt: new Date().toISOString() },
  });
  const changes = [
    {
      id: "cc-other",
      title: "Other review",
      status: "IN_REVIEW",
      author: { username: "author" },
      changes: [],
      reviews: [],
    },
    {
      id: "cc-change",
      title: "Scale review",
      status: "IN_REVIEW",
      author: { username: "author" },
      changes: [],
      reviews: [],
    },
  ];
  await page.route("**/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    const bodies: Record<string, unknown> = {
      "/v1/session": {
        actor: { username: "reader" },
        effective: { username: "reader" },
      },
      "/v1/environments": {
        environments: [
          environment("cc-east", {
            "conduit.latitude": "0",
            "conduit.longitude": "0",
          }),
          environment("cc-west", {
            "conduit.latitude": "35",
            "conduit.longitude": "-105",
          }),
          environment("cc-unmapped"),
        ],
      },
      "/v1/fleet/installations": {
        installations: [
          { environment: "cc-east", desiredVersion: "2", observedVersion: "1" },
        ],
      },
      "/v1/dev-workspaces": {
        items: [
          {
            workspaceId: "cc-workspace",
            profileId: "packaging",
            status: "running",
          },
        ],
      },
      "/v1/change-requests": { changeRequests: changes },
      "/v1/products": { products: [] },
      "/v1/plans": { plans: [] },
      "/v1/promotions": { promotions: [] },
      "/v1/packaging-runs": { items: [] },
      "/v1/authorization-audit": {
        entries: [{ sequence: 1, actor: "reader", action: "plan:read" }],
      },
    };
    await route.fulfill({ json: bodies[path] || {} });
  });
  await hydrate(page);
  await page.goto("/console/");
  await expect(
    page.getByRole("heading", { name: "Sites and connectivity" }),
  ).toBeVisible();
  await expect(
    page.getByText("2 with declared coordinates · 1 without a mapped location"),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Active workspace profiles" }),
  ).toBeVisible();
  await expect(
    page.getByText("1 recorded active workspace", { exact: true }),
  ).toBeVisible();
  await expect(page.locator(".kit-cc-pin")).toHaveCount(1);
  const labels = page.getByRole("button", { name: "Labels", exact: true });
  await labels.focus();
  await page.keyboard.press("Space");
  await expect(labels).toHaveAttribute("aria-pressed", "false");
  await expect(labels).toBeFocused();
  await expect(page.locator(".kit-cc-pin text")).toHaveCount(0);
  await page.getByRole("button", { name: "cc-west", exact: true }).click();
  await expect(
    page.getByRole("dialog", { name: "Environment cc-west" }),
  ).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(
    page.locator('.kit-cc-pin[data-cc-site="cc-west"]'),
  ).toBeFocused();
  await page.getByRole("button", { name: "cc-unmapped", exact: true }).click();
  const object = page.getByRole("dialog", { name: "Environment cc-unmapped" });
  await expect(
    object.getByText("No valid coordinates are recorded for this environment."),
  ).toBeVisible();
  await object.getByRole("button", { name: "COA", exact: true }).click();
  const composer = page.getByLabel("Message input");
  await expect(composer).toHaveValue(
    /course of action for Environment cc-unmapped/,
  );
  await composer.fill("My unsent notes");
  await object
    .getByRole("button", { name: "Share in chat", exact: true })
    .click();
  await expect(composer).toHaveValue(
    /My unsent notes\n\nReference for discussion: Environment cc-unmapped/,
  );
  await composer.fill("x".repeat(4000));
  await object.getByRole("button", { name: "Ask", exact: true }).click();
  await expect(composer).toHaveValue("x".repeat(4000));
  await expect(
    page.getByText(
      "Context could not be added. Review the existing assistant draft first.",
    ),
  ).toBeVisible();
  await composer.fill("Keep these notes");
  await object.getByRole("button", { name: "Ask", exact: true }).click();
  await expect(composer).toHaveValue(
    /Keep these notes\n\nHelp me understand Environment cc-unmapped/,
  );
  await expect(
    page.getByText(
      "Context could not be added. Review the existing assistant draft first.",
    ),
  ).toHaveCount(0);
  await page.screenshot({
    path: testInfo.outputPath("command-center-geography.png"),
    fullPage: false,
  });
  await page
    .getByRole("button", {
      name: "Review ChangeRequest cc-change",
      exact: true,
    })
    .click();
  await expect(page.locator('[data-cr-select="cc-change"]')).toHaveAttribute(
    "aria-current",
    "true",
  );
  expect(mutations).toEqual([]);
  expect(external).toEqual([]);
});

test("Work Intake uses agent form tools and resumes with the human's answer", async ({
  page,
}) => {
  await hydrate(page);
  const session = {
    id: "ps_browser",
    status: "needs_input",
    intake: { repoUrl: "https://github.com/org/app", ref: "main" },
    profile: { name: "onboard", version: "1", hash: "b".repeat(64) },
    needsInput: [
      {
        id: "port",
        name: "port",
        label: "Which port?",
        type: "string",
        required: true,
      },
    ],
    answers: [],
    messages: [],
    events: [],
    plan: null,
    binding: null,
    resolvedCommit: "a".repeat(40),
  };
  await page.route("**/v1/planning-sessions", (route) =>
    route.fulfill({ status: 201, json: session }),
  );
  const requests: Record<string, any>[] = [];
  await page.route("**/v1/agent-runs", async (route) => {
    const input = route.request().postDataJSON();
    requests.push(input);
    const first = requests.length === 1;
    const resumed = Boolean(input.resume);
    const rejected = requests.length === 3;
    const current = rejected
      ? {
          ...session,
          needsInput: [
            {
              ...session.needsInput[0],
              validationError: "Choose another port.",
            },
          ],
        }
      : resumed && !rejected
        ? {
            ...session,
            status: "ready",
            answers: [
              { id: "port", value: input.resume[0].payload["question:port"] },
            ],
            plan: { text: "Build reviewed input.", sha256: "c".repeat(64) },
          }
        : session;
    const events: unknown[] = [
      { type: "RUN_STARTED", threadId: input.threadId, runId: input.runId },
    ];
    if (first)
      events.push(
        { type: "TEXT_MESSAGE_START", messageId: "request", role: "assistant" },
        {
          type: "TOOL_CALL_START",
          toolCallId: "prefill",
          toolCallName: "fill_field",
          parentMessageId: "request",
        },
        {
          type: "TOOL_CALL_ARGS",
          toolCallId: "prefill",
          delta: '{"field":"question:port","value":"8080"}',
        },
        { type: "TOOL_CALL_END", toolCallId: "prefill" },
        { type: "TEXT_MESSAGE_END", messageId: "request" },
      );
    events.push(
      {
        type: "STATE_SNAPSHOT",
        snapshot: {
          session: current,
          toolCalls: [],
          pendingClientTools: first
            ? [
                {
                  id: "prefill",
                  name: "fill_field",
                  arguments: { field: "question:port", value: "8080" },
                  eligible: false,
                },
              ]
            : [],
        },
      },
      {
        type: "MESSAGES_SNAPSHOT",
        messages: [{ id: "user", role: "user", content: "Inspect" }],
      },
      {
        type: "RUN_FINISHED",
        threadId: input.threadId,
        runId: input.runId,
        outcome: first
          ? { type: "success", pendingToolCallIds: ["prefill"] }
          : resumed && !rejected
            ? { type: "success" }
            : {
                type: "interrupt",
                interrupts: [{ id: "form-current", reason: "needs_input" }],
              },
      },
    );
    if (!resumed || rejected)
      events.splice(events.length - 1, 0, {
        type: "CUSTOM",
        name: "a2ui",
        value: {
          version: "1.0",
          createSurface: {
            surfaceId: "surface-" + input.runId,
            catalogId: "conduit:planning-forms/v1",
            dataModel: { answers: { port: "" } },
            components: [
              {
                id: "root",
                component: "Column",
                children: ["question:port", "submit"],
              },
              {
                id: "question:port",
                component: "TextField",
                label:
                  'Which port? <img src=x onerror="window.formInjected=true">',
                ...(rejected ? { error: "Choose another port." } : {}),
                value: { "@path": "/answers/port" },
                "@checks": [
                  {
                    condition: {
                      "@call": "required",
                      args: { value: { "@path": "/answers/port" } },
                    },
                    message: "Enter the port.",
                  },
                ],
              },
              {
                id: "submit",
                component: "Button",
                label: "Submit answers",
                action: {
                  event: {
                    name: "submit_needs_input",
                    context: { "question:port": { "@path": "/answers/port" } },
                  },
                },
              },
            ],
          },
        },
      });
    await route.fulfill({
      contentType: "text/event-stream",
      body: events
        .map((event) => `data: ${JSON.stringify(event)}\n\n`)
        .join(""),
    });
  });
  await page.goto("/console/#s01-intake");
  await page.getByLabel("Repository URL").fill("https://github.com/org/app");
  await page.getByLabel("Ref", { exact: true }).fill("main");
  await page.evaluate(() => Reflect.get(window, "refreshHub")());
  await expect(page.getByLabel("Ref", { exact: true })).toBeFocused();
  await expect(page.getByLabel("Ref", { exact: true })).toHaveValue("main");
  await page.getByLabel("Message", { exact: true }).fill("Inspect");
  await page.locator("[data-plan-send]").click();
  await expect(page.locator('[data-plan-answer="port"]')).toHaveValue("8080");
  await expect(page.locator("[data-plan-submit-answers]")).toBeEnabled();
  await expect(page.locator("[data-plan-needs-input] img")).toHaveCount(0);
  expect(
    await page.evaluate(() => Reflect.get(window, "formInjected")),
  ).toBeUndefined();
  await page.locator('[data-plan-answer="port"]').fill("");
  await page.locator("[data-plan-submit-answers]").click();
  await expect(page.locator('[data-plan-answer="port"]')).toBeFocused();
  await expect(page.locator('[data-plan-answer="port"]')).toHaveAttribute(
    "aria-invalid",
    "true",
  );
  expect(requests).toHaveLength(2);
  await page.locator('[data-plan-answer="port"]').fill("9090");
  await page.locator("[data-plan-submit-answers]").click();
  await expect(page.locator("[data-plan-needs-input]")).toContainText(
    "Choose another port.",
  );
  await page.locator("[data-plan-submit-answers]").click();
  expect(requests).toHaveLength(3);
  await page.locator('[data-plan-answer="port"]').fill("9091");
  await page.locator("[data-plan-submit-answers]").click();
  await expect(page.locator("[data-plan-review]")).toContainText(
    "Build reviewed input.",
  );
  expect(requests).toHaveLength(4);
  expect(requests[3].resume[0].payload).toEqual({ "question:port": "9091" });
  expect(requests[1].messages.at(-1)).toMatchObject({
    role: "tool",
    toolCallId: "prefill",
  });
  expect(requests[2].forwardedProps.conduit.a2uiAction.action).toMatchObject({
    name: "submit_needs_input",
    sourceComponentId: "submit",
    context: { "question:port": "9090" },
  });
  expect(requests[2].resume).toEqual([
    {
      interruptId: "form-current",
      status: "resolved",
      payload: { "question:port": "9090" },
    },
  ]);
  await expect(page.locator("[data-plan-binding]")).toHaveCount(0);
});

test("packaging graph and staged chunks keep the selected run and paged history", async ({
  page,
}) => {
  await hydrate(page);
  const calls: { path: string; method: string }[] = [];
  await page.route("**/v1/packaging-runs**", async (route) => {
    const url = new URL(route.request().url());
    calls.push({
      path: url.pathname + url.search,
      method: route.request().method(),
    });
    if (url.pathname === "/v1/packaging-runs") {
      await route.fulfill({
        json: {
          items: [
            {
              runId: "run-a",
              configured: true,
              phase: "review",
              outcome: "WAITING",
              sourceDigest: "sha256:source-a",
            },
            {
              runId: "run-b",
              configured: true,
              phase: "blocked",
              outcome: "FAIL_REQUIRED",
              artifactDigest: "sha256:artifact-b",
            },
          ],
          total: 2,
        },
      });
      return;
    }
    await route.fulfill({ json: { items: [], total: 0 } });
  });
  await page.route(
    "**/conduit.packaging.v1.PackagingRunService/ListPackagingRuns",
    async (route) => {
      calls.push({
        path: new URL(route.request().url()).pathname,
        method: route.request().method(),
      });
      expect(route.request().postDataJSON()).toEqual({});
      await route.fulfill({
        json: {
          configured: true,
          items: [
            {
              runId: "run-a",
              phase: "review",
              outcome: "WAITING",
              sourceDigest: "sha256:source-a",
            },
            {
              runId: "run-b",
              phase: "blocked",
              outcome: "FAIL_REQUIRED",
              artifactDigest: "sha256:artifact-b",
            },
          ],
          total: "2",
          pageSize: 2,
          limit: 20,
        },
      });
    },
  );
  const historyQueries: { runId: string; cursor?: string }[] = [];
  await page.route(
    "**/conduit.packaging.v1.PackagingRunService/ListPackagingRunEvents",
    async (route) => {
      expect(route.request().method()).toBe("POST");
      const query = route.request().postDataJSON();
      historyQueries.push(query);
      const first = query.runId === "run-a";
      const next = Boolean(query.cursor);
      await route.fulfill({
        json: {
          configured: true,
          runId: query.runId,
          pageSize: 1,
          limit: 20,
          total: first ? "2" : "1",
          nextCursor: first && !next ? "cursor-a" : "",
          items: [
            {
              sequence: first && next ? "2" : "1",
              at: "2026-11-15T09:00:00.123456789Z",
              step: first ? (next ? "review" : "assess") : "build",
              outcome: first ? (next ? "WAITING" : "PASS") : "FAIL_REQUIRED",
              reason: first
                ? next
                  ? "waiting-a"
                  : "a-only-output"
                : "b-only-failed",
            },
          ],
        },
      });
    },
  );
  await page.goto("/console/#s07-pipeline");
  await expect(page.locator("[data-packaging-pipeline]")).toBeVisible();
  await expect(
    page.locator("[data-stage-graph] [data-packaging-stage]"),
  ).toHaveCount(2);
  await expect(page.locator("#example-data-badge")).toHaveText("LIVE");
  await expect(page.locator("[data-stage-preview]")).toContainText("waiting-a");
  await page.locator("[data-packaging-stage='0']").click();
  await expect(page.locator("[data-stage-preview]")).toContainText(
    "a-only-output",
  );
  await page.locator("[data-packaging-run]").selectOption("run-b");
  await expect(page.locator("[data-object-header]")).toContainText("run-b");
  await expect(page.locator("[data-object-header]")).not.toContainText("run-a");
  await expect(
    page.locator("[data-stage-graph] [data-packaging-stage]"),
  ).toHaveCount(1);
  await expect(page.locator("[data-stage-preview]")).toContainText(
    "b-only-failed",
  );
  await expect(page.locator("[data-stage-preview]")).not.toContainText(
    "a-only-output",
  );
  await page
    .getByRole("button", { name: "Open staged run", exact: true })
    .click();
  await expect(page.locator("[data-stage-chunk]")).toHaveCount(1);
  await expect(page.locator("[data-stage-chunk]")).toContainText(
    "b-only-failed",
  );
  await page
    .getByRole("button", { name: "Artifacts & evidence", exact: true })
    .click();
  await expect(page.locator("[data-staged-run]")).toContainText(
    "No downloadable artifact reference",
  );
  await expect(page.locator("[data-staged-run]")).not.toContainText("RUN-8844");
  await page
    .getByRole("button", { name: "Open pipeline graph", exact: true })
    .click();
  await expect(page.locator("[data-packaging-run]")).toHaveValue("run-b");
  const beforeManual = calls.filter((call) =>
    call.path.endsWith("/ListPackagingRuns"),
  ).length;
  await page.evaluate(() =>
    (window as unknown as { refreshHub: () => Promise<void> }).refreshHub(),
  );
  expect(
    calls.filter((call) => call.path.endsWith("/ListPackagingRuns")).length,
  ).toBe(beforeManual + 1);
  await expect(page.locator("[data-packaging-run]")).toHaveValue("run-b");
  expect(historyQueries).toContainEqual({ runId: "run-a", cursor: "cursor-a" });
  expect(calls.some((call) => call.path.includes("/events"))).toBe(false);
  expect(calls.some((call) => call.path === "/v1/packaging-runs")).toBe(false);
  expect(
    calls.filter((call) => call.path.endsWith("/ListPackagingRuns")).length,
  ).toBeGreaterThan(0);
  expect(
    calls.every(
      (call) =>
        call.method ===
        (call.path.endsWith("/ListPackagingRuns") ? "POST" : "GET"),
    ),
  ).toBe(true);
});

test("release convergence uses reported Hub evidence in the releases area", async ({
  page,
}) => {
  await page.route("**/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/v1/fleet/installations") {
      await route.fulfill({
        json: {
          installations: [
            {
              environment: "prod-native",
              installationId: "service-a",
              product: "native-product",
              desiredVersion: "2.0.0",
              observedVersion: "2.0.0",
              desiredReleaseDigest: "sha256:desired",
              observedReleaseDigest: "sha256:other",
              desiredConfigDigest: "sha256:config",
              observedConfigDigest: "sha256:config",
              observedAt: "2026-11-15T10:00:00Z",
              stale: false,
              lifecycle: "INSTALLED",
              health: "HEALTHY",
              configStatus: "APPLIED",
              rolloutStatus: "UP_TO_DATE",
            },
          ],
        },
      });
      return;
    }
    await route.fulfill({ json: {} });
  });
  await hydrate(page);
  await page.goto("/console/");
  await page
    .getByRole("button", { name: "Releases", exact: true })
    .first()
    .click();
  await page.locator('[data-area-tab="convergence"]').click();
  const matrix = page.locator("[data-release-convergence]");
  await expect(matrix).toContainText("native-product");
  await expect(matrix).toContainText("diverged");
  await expect(matrix).not.toContainText("edge-east-01");
});

test("release waves show recorded gates and reasons without synthetic calendar data", async ({
  page,
}) => {
  await page.route("**/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    await route.fulfill({
      json:
        path === "/v1/rollouts"
          ? {
              rollouts: [
                {
                  id: "native-wave-run",
                  product: "native-service",
                  version: "3",
                  status: "paused",
                  pausedBy: "reviewer",
                  waves: [
                    {
                      name: "canary-native",
                      environments: ["dev"],
                      gate: "health",
                      status: "passed",
                    },
                    {
                      name: "fleet-native",
                      environments: ["prod"],
                      gate: "approval",
                      status: "blocked",
                      reason: "Waiting for separate approver",
                    },
                  ],
                },
              ],
            }
          : {},
    });
  });
  await hydrate(page);
  await page.goto("/console/");
  await page
    .getByRole("button", { name: "Releases", exact: true })
    .first()
    .click();
  await page.locator('[data-area-tab="waves"]').click();
  const waves = page.locator("[data-release-waves]");
  await expect(waves).toContainText("native-wave-run");
  await expect(waves).toContainText("Gate: approval");
  await expect(waves).toContainText("Waiting for separate approver");
  await expect(waves).toContainText("scheduling is unavailable");
});

test("plan inspector loads the selected environment timeline and preserves audit evidence limits", async ({
  page,
}) => {
  const requests: string[] = [];
  await page.route("**/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    requests.push(path);
    if (path === "/v1/plans")
      return route.fulfill({
        json: {
          plans: [
            {
              id: "native-plan-a",
              status: "FAILED",
              failure: "Recorded denial",
              plan: { environment: "env-a", steps: [] },
            },
            {
              id: "native-plan-b",
              status: "SUCCEEDED",
              plan: { environment: "env-b", steps: [] },
            },
          ],
        },
      });
    if (path === "/v1/environments/env-a/timeline")
      return route.fulfill({
        json: {
          timeline: [
            {
              at: "2026-11-15",
              kind: "audit",
              summary: "native-audit-a",
              verified: true,
              digest: "sha256:link",
              sequence: 7,
            },
          ],
        },
      });
    if (path === "/v1/environments/env-b/timeline")
      return route.fulfill({
        json: {
          timeline: [
            {
              at: "2026-11-15",
              kind: "plan",
              summary: "native-plan-event-b",
              verified: true,
            },
          ],
        },
      });
    return route.fulfill({ json: {} });
  });
  await hydrate(page);
  await page.goto("/console/");
  await page
    .getByRole("button", { name: "Releases", exact: true })
    .first()
    .click();
  await page.locator('[data-area-tab="plans"]').click();
  const inspector = page.locator("[data-release-plan-inspector]");
  await expect(inspector).toContainText("Recorded denial");
  await expect(inspector).toContainText("Hub-verified chain link 7");
  await inspector.locator("[data-release-plan]").selectOption("native-plan-b");
  await expect(inspector).toContainText("native-plan-event-b");
  await expect(inspector).not.toContainText("native-audit-a");
  await expect(inspector).not.toContainText("Hub-verified chain link");
  expect(requests).toContain("/v1/environments/env-b/timeline");
});

test("environment hub scopes header, installations, blackout and timeline to selection", async ({
  page,
}) => {
  const proposals: unknown[] = [];
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/v1/fleet/propose-fix") {
      proposals.push(route.request().postDataJSON());
      return route.fulfill({ json: { changeRequestId: "native-fix-request" } });
    }
    if (url.pathname === "/v1/environments")
      return route.fulfill({
        json: {
          environments: [
            { environment: { id: "env-a" } },
            {
              environment: {
                id: "env-b",
                maintenance: {
                  downtime: {
                    timeZone: "UTC",
                    intervals: ["Monday 02:00-04:00"],
                  },
                },
              },
            },
          ],
        },
      });
    if (url.pathname === "/v1/fleet/installations")
      return route.fulfill({
        json: {
          installations: [
            { environment: "env-a", product: "other-service" },
            {
              environment: "env-b",
              product: "selected-service",
              installationId: "service-b",
              desiredVersion: "2",
              observedVersion: "1",
              desiredReleaseDigest: "sha256:wanted",
              observedReleaseDigest: "sha256:old",
              desiredConfigDigest: "sha256:config",
              observedConfigDigest: "sha256:config",
              observedAt: "2026-11-15T10:00:00Z",
              stale: false,
            },
          ],
        },
      });
    if (url.pathname === "/v1/blackouts")
      return route.fulfill({
        json: {
          environment: url.searchParams.get("environment"),
          blocked: true,
          reason: `hold-${url.searchParams.get("environment")}`,
        },
      });
    if (url.pathname.endsWith("/timeline"))
      return route.fulfill({
        json: {
          timeline: [
            {
              kind: "plan",
              at: "2026-11-15",
              summary: `event-${url.pathname.split("/")[3]}`,
              verified: false,
            },
          ],
        },
      });
    return route.fulfill({ json: {} });
  });
  await hydrate(page);
  await page.goto("/console/");
  await page
    .getByRole("button", { name: "Releases", exact: true })
    .first()
    .click();
  await page.locator("[data-release-environment]").selectOption("env-b");
  const hub = page.locator("[data-release-environment-hub]");
  await expect(hub).toContainText("selected-service");
  await expect(hub).not.toContainText("other-service");
  await expect(hub).toContainText("hold-env-b");
  await expect(hub).toContainText("event-env-b");
  await expect(hub).not.toContainText("hold-env-a");
  await expect(hub).not.toContainText("event-env-a");
  await expect(page.locator("[data-object-header]")).toContainText("env-b");
  await expect(hub).toContainText("Monday 02:00-04:00");
  await hub.getByRole("button", { name: "Propose fix for service-b" }).click();
  await expect(hub).toContainText("native-fix-request");
  expect(proposals).toEqual([
    { environment: "env-b", installationId: "service-b" },
  ]);
});

test("release dependencies display bounded native constraints and recorded upgrade lag", async ({
  page,
}) => {
  const writes: string[] = [];
  await page.route("**/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (route.request().method() !== "GET") writes.push(path);
    if (path === "/v1/products")
      return route.fulfill({
        json: {
          products: [
            { product: "native-service", releases: [{ version: "2" }] },
            { product: "native-library", releases: [{ version: "1" }] },
          ],
        },
      });
    if (path === "/v1/products/native-service/dependencies/2")
      return route.fulfill({
        json: {
          dependencies: [
            {
              product: "native-library",
              minimumVersion: "1",
              maximumVersion: "2",
              optional: true,
            },
          ],
        },
      });
    if (path === "/v1/products/native-library/dependencies/1")
      return route.fulfill({ json: { dependencies: [] } });
    if (path === "/v1/fleet/upgrades")
      return route.fulfill({
        json: {
          upgrades: [
            {
              environment: "prod-native",
              installationId: "service-a",
              product: "native-service",
              desiredVersion: "2",
              observedVersion: "1",
              reason: "observed version differs",
            },
          ],
        },
      });
    return route.fulfill({ json: {} });
  });
  await hydrate(page);
  await page.goto("/console/");
  await page
    .getByRole("button", { name: "Releases", exact: true })
    .first()
    .click();
  await page.locator('[data-area-tab="dependencies"]').click();
  const graph = page.locator("[data-release-dependencies]");
  await expect(graph.locator("svg")).toBeVisible();
  await expect(graph).toContainText("native-library · optional");
  await graph
    .locator("[data-release-dependency]")
    .selectOption(JSON.stringify(["native-library", "1"]));
  await expect(graph).toContainText("native-service");
  await expect(graph).toContainText(
    "does not distinguish an empty dependency set",
  );
  await expect(page.locator("[data-release-upgrade-lag]")).toContainText(
    "observed version differs",
  );
  expect(
    writes.filter(
      (path) =>
        path.startsWith("/v1/products/") || path === "/v1/fleet/upgrades",
    ),
  ).toEqual([]);
});

test("release planning, ramp and windows use recorded data with explicit calendar limits", async ({
  page,
}) => {
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/v1/environments")
      return route.fulfill({
        json: {
          environments: [
            {
              environment: {
                id: "native-env",
                maintenance: {
                  downtime: {
                    timeZone: "UTC",
                    intervals: ["Tuesday 03:00-05:00"],
                  },
                  overrides: [
                    {
                      type: "DOWNTIME",
                      startsAt: "2026-11-15T03:00:00Z",
                      endsAt: "2026-11-15T04:00:00Z",
                      reason: "Recorded maintenance approval",
                      approvedBy: "reviewer",
                    },
                  ],
                },
              },
            },
          ],
        },
      });
    if (url.pathname === "/v1/plans")
      return route.fulfill({
        json: {
          plans: [
            {
              id: "native-planning-record",
              status: "PENDING_APPROVAL",
              plan: { environment: "native-env", steps: [] },
            },
          ],
        },
      });
    if (url.pathname === "/v1/rollouts")
      return route.fulfill({
        json: {
          rollouts: [
            {
              id: "native-ramp-record",
              product: "service",
              version: "1",
              status: "paused",
              waves: [
                {
                  name: "native-canary",
                  environments: ["native-env"],
                  gate: "approval",
                  status: "blocked",
                  reason: "Waiting for review",
                },
              ],
            },
          ],
        },
      });
    if (url.pathname === "/v1/blackouts")
      return route.fulfill({
        json: {
          environment: url.searchParams.get("environment"),
          blocked: false,
        },
      });
    if (url.pathname.endsWith("/timeline"))
      return route.fulfill({ json: { timeline: [] } });
    return route.fulfill({ json: {} });
  });
  await hydrate(page);
  await page.goto("/console/");
  await page
    .getByRole("button", { name: "Releases", exact: true })
    .first()
    .click();
  await page.locator('[data-area-tab="planning"]').click();
  await expect(page.locator("[data-release-plan-inspector]")).toContainText(
    "native-planning-record",
  );
  await expect(page.locator("[data-release-calendar]")).toContainText(
    "Resolved scheduling data is unavailable",
  );
  await expect(page.locator("#area-body")).not.toContainText("prod-root-2026");
  await page.locator('[data-area-tab="ramp"]').click();
  await expect(page.locator("[data-release-waves]")).toContainText(
    "native-ramp-record",
  );
  await expect(page.locator("[data-release-waves]")).toContainText(
    "Waiting for review",
  );
  await expect(page.locator("[data-release-calendar]")).toContainText(
    "No Gantt bars",
  );
  await page.locator('[data-area-tab="windows"]').click();
  await expect(page.locator("[data-release-environment-hub]")).toContainText(
    "Tuesday 03:00-05:00",
  );
  await expect(page.locator("[data-release-calendar]")).toContainText(
    "A governed scheduling proposal",
  );
  await expect(
    page.getByRole("img", { name: "Recorded maintenance override interval" }),
  ).toBeVisible();
  await expect(page.locator("[data-release-calendar]")).toContainText(
    "Recorded maintenance approval",
  );
});

test("window scheduler creates only a confirmed preview-bound change request", async ({
  page,
}) => {
  const proposals: Record<string, any>[] = [];
  const forbidden: string[] = [];
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url());
    if (
      route.request().method() !== "GET" &&
      /\/(approve|leases|start)$/.test(url.pathname)
    )
      forbidden.push(url.pathname);
    if (url.pathname === "/v1/environments")
      return route.fulfill({
        json: { environments: [{ environment: { id: "scheduled-prod" } }] },
      });
    if (url.pathname === "/v1/plans")
      return route.fulfill({
        json: {
          plans: [
            {
              id: "scheduled-plan",
              status: "AWAITING_APPROVAL",
              plan: {
                environment: "scheduled-prod",
                steps: [{ entity: "api", installationId: "api-1" }],
              },
            },
          ],
        },
      });
    if (url.pathname.endsWith("/timeline"))
      return route.fulfill({ json: { timeline: [] } });
    if (url.pathname === "/v1/blackouts")
      return route.fulfill({
        json: {
          environment: url.searchParams.get("environment"),
          blocked: false,
        },
      });
    if (url.pathname === "/v1/temporary-maintenance-overrides") {
      const body = route.request().postDataJSON();
      proposals.push(body);
      return route.fulfill({
        json: {
          valueTier: "CONDUIT_NATIVE",
          dryRun: body.dryRun,
          requests: [
            {
              changeRequestId: "scheduled-request",
              ...body.entities[0],
              startsAt: body.startsAt,
              endsAt: new Date(
                Date.parse(body.startsAt) +
                  Number(body.duration.slice(0, -1)) * 60000,
              ).toISOString(),
              type: body.downtime ? "DOWNTIME" : "NO_DOWNTIME",
              status: "PENDING_APPROVALS",
            },
          ],
        },
      });
    }
    return route.fulfill({ json: {} });
  });
  await hydrate(page);
  await page.goto("/console/");
  await page
    .getByRole("button", { name: "Releases", exact: true })
    .first()
    .click();
  await page.locator('[data-area-tab="windows"]').click();
  const form = page.locator("[data-window-scheduler]");
  const save = form.getByRole("button", { name: "Create change request" });
  await expect(save).toBeDisabled();
  await form
    .locator('[name="start"]')
    .fill(new Date(Date.now() + 2 * 3600000).toISOString().slice(0, 16));
  await form.locator('[name="reason"]').fill("Reviewed maintenance");
  await form.getByRole("button", { name: "Preview change request" }).click();
  await expect(form.locator("[data-window-message]")).toContainText(
    "Preview scheduled-request",
  );
  await form.locator('[name="minutes"]').fill("90");
  await expect(save).toBeDisabled();
  const bar = form.locator("[data-draft-bar]");
  await bar.focus();
  await bar.press("ArrowRight");
  await bar.press("Shift+ArrowRight");
  await expect(form.locator('[name="minutes"]')).toHaveValue("105");
  const before = await form.locator('[name="start"]').inputValue();
  const box = await bar.boundingBox();
  expect(box).not.toBeNull();
  await page.mouse.move(box!.x + 2, box!.y + 20);
  await page.mouse.down();
  await page.mouse.move(box!.x + 45, box!.y + 20);
  await page.mouse.up();
  await expect(form.locator('[name="start"]')).not.toHaveValue(before);
  await form.getByRole("button", { name: "Preview change request" }).click();
  await expect(form.locator("[data-window-message]")).toContainText(
    "Preview scheduled-request",
  );
  await form.locator("[data-confirm-window]").check();
  await save.click();
  await expect(form.locator("[data-window-message]")).toContainText(
    "Change request scheduled-request",
  );
  expect(proposals).toHaveLength(3);
  expect(proposals.map((row) => row.dryRun)).toEqual([true, true, false]);
  expect({
    ...proposals[1],
    dryRun: false,
    expectedChangeRequestId: "scheduled-request",
  }).toEqual(proposals[2]);
  expect(proposals[2]!.entities).toEqual([
    { environment: "scheduled-prod", entity: "api" },
  ]);
  expect(forbidden).toEqual([]);
  await form.getByRole("button", { name: "Preview change request" }).click();
  await expect(form.locator("[data-window-message]")).toContainText(
    "Preview scheduled-request",
  );
  await form.locator("[data-confirm-window]").check();
  await save.click();
  await expect(form.locator("[data-window-message]")).toContainText(
    "Submission already attempted",
  );
  expect(proposals.filter((row) => !row.dryRun)).toHaveLength(1);
});

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
  await expect(page.locator("#area-body")).not.toContainText("do-not-expose");
  await page.locator('[data-area-tab="approvals"]').click();
  await expect(page.locator("[data-approvals-board]")).toContainText(
    "Recorded only",
  );
  await expect(page.locator("[data-approvals-board]")).toContainText(
    "native-recorded-plan",
  );
  await page.locator('[data-review-plan="native-review-plan"]').click();
  await expect(page.locator("[data-release-plan-inspector]")).toContainText(
    "native-review-plan",
  );
});

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

test("spoke operations uses recorded workloads and opens the exact environment", async ({
  page,
}) => {
  const writes: string[] = [];
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url());
    if (route.request().method() !== "GET") writes.push(url.pathname);
    if (url.pathname === "/v1/environments")
      return route.fulfill({
        json: {
          environments: [
            {
              environment: { id: "east" },
              spoke: { state: "stale", agentId: "agent-east" },
              reported: {
                agentId: "agent-east",
                entities: [
                  {
                    installationId: "east-app",
                    product: "east-product",
                    namespace: "east-ns",
                    version: "1",
                    health: "UNHEALTHY",
                  },
                ],
              },
            },
            {
              environment: {
                id: "west",
                namespaces: [
                  {
                    name: "west-declared",
                    rid: "namespace/west",
                    deletionProtected: true,
                  },
                ],
              },
              spoke: { state: "live", agentId: "agent-west" },
              reported: {
                agentId: "agent-west",
                entities: [
                  {
                    installationId: "west-app",
                    product: "west-product",
                    namespace: "west-ns",
                    version: "2",
                    health: "HEALTHY",
                    healthDetails: {
                      nodes: [
                        {
                          nodeId: "west-pod-observed",
                          result: "HEALTHY",
                          containers: [
                            {
                              containerId: "west-container",
                              liveness: true,
                              readiness: false,
                            },
                          ],
                        },
                      ],
                    },
                  },
                ],
              },
            },
          ],
        },
      });
    if (url.pathname === "/v1/fleet/installations")
      return route.fulfill({ json: { installations: [] } });
    if (url.pathname.endsWith("/timeline"))
      return route.fulfill({ json: { timeline: [] } });
    if (url.pathname === "/v1/blackouts")
      return route.fulfill({
        json: {
          environment: url.searchParams.get("environment"),
          blocked: false,
        },
      });
    return route.fulfill({ status: 404, json: { error: "not configured" } });
  });
  await hydrate(page);
  await page.goto("/console/#s17-spokes-audit");
  await expect(page.locator("[data-spoke-operations]")).toContainText(
    "east-product",
  );
  await expect(page.locator("[data-spoke-operations]")).toContainText(
    "Spoke: stale",
  );
  await expect(page.locator("[data-spoke-operations]")).not.toContainText(
    "Optical Diode",
  );
  await page.locator('[data-spoke-environment="west"]').click();
  await expect(
    page.locator("#main-workspace [data-spoke-workloads]"),
  ).toContainText("west-product");
  await expect(
    page.locator("#main-workspace [data-spoke-workloads]"),
  ).not.toContainText("east-product");
  await expect(
    page.locator("#main-workspace [data-spoke-namespaces]"),
  ).toContainText("west-declared");
  await expect(
    page.locator("#main-workspace [data-spoke-instances]"),
  ).toContainText("west-pod-observed");
  await expect(
    page.locator("#main-workspace [data-spoke-instances]"),
  ).toContainText("readiness no");
  expect(writes).toEqual([]);
});

test("offline bundles bind verified envelope, physical-copy confirmation and report-back", async ({
  page,
}) => {
  const writes: Record<string, any>[] = [];
  const envelope = (id: string) => ({
    manifest: { id },
    manifestDigest: `sha256:${id}`,
  });
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/v1/session")
      return route.fulfill({
        json: {
          actor: { username: "alice" },
          effective: { username: "alice" },
        },
      });
    if (url.pathname === "/v1/environments")
      return route.fulfill({
        json: {
          environments: [
            { environment: { id: "east" } },
            { environment: { id: "west" } },
          ],
        },
      });
    if (url.pathname === "/v1/bundles")
      return route.fulfill({
        json: {
          bundles: [
            { id: "bundle-a", manifestDigest: "sha256:bundle-a" },
            { id: "bundle-b", manifestDigest: "sha256:bundle-b" },
          ],
        },
      });
    if (
      url.pathname.startsWith("/v1/bundles/") &&
      route.request().method() === "GET"
    )
      return route.fulfill({ json: envelope(url.pathname.split("/").pop()!) });
    if (url.pathname === "/v1/bundles/verification") {
      const input = route.request().postDataJSON();
      return route.fulfill({
        json: {
          bundleId: input.manifest.id,
          manifestDigest: input.manifestDigest,
          signature: { status: "verified", keyId: "key-1" },
          sbom: [],
        },
      });
    }
    if (url.pathname === "/v1/offline-bundles") {
      const input = route.request().postDataJSON();
      writes.push(input);
      const handoff = {
        id: "handoff-a",
        bundleId: "bundle-a",
        digest: "sha256:bundle-a",
        site: "west",
        takenBy: "alice",
        status: input.action === "handoff" ? "handed_off" : "pending_check_in",
      };
      return route.fulfill({
        status: input.action === "handoff" ? 201 : 202,
        json: {
          handoff,
          reason:
            input.action === "report"
              ? "report-back is pending until the site checks in"
              : "offline bundle handed off",
        },
      });
    }
    return route.fulfill({ status: 404, json: { error: "not configured" } });
  });
  await hydrate(page);
  await page.goto("/console/#rm08-bundle-preview");
  await expect(page.locator(".cnd-topbar")).toContainText("alice");
  await page
    .getByLabel("Offline bundle", { exact: true })
    .selectOption("bundle-a");
  await expect(page.locator("[data-offline-signature]")).toContainText(
    "Verified by Hub",
  );
  await expect(page.locator("[data-offline-preview]")).toContainText(
    "bundle-a",
  );
  await expect(page.locator("[data-offline-preview]")).not.toContainText(
    "bundle-b",
  );
  await page.evaluate(() => Reflect.get(window, "refreshHub")());
  await expect(page.getByLabel("Offline bundle", { exact: true })).toHaveValue(
    "bundle-a",
  );
  await expect(page.locator("[data-offline-signature]")).toContainText(
    "Verified by Hub",
  );
  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "Download signed envelope" }).click();
  expect((await download).suggestedFilename()).toBe("bundle-a.json");
  const form = page.locator("[data-offline-handoff]");
  await page.getByLabel("Offline bundle site").selectOption("east");
  await form.getByRole("checkbox").check();
  await expect(form.getByRole("button")).toBeEnabled();
  await page.getByLabel("Offline bundle site").selectOption("west");
  await expect(form.getByRole("button")).toBeDisabled();
  await form.getByRole("checkbox").check();
  await form.getByRole("button").click();
  await expect(page.locator("[data-offline-note]")).toContainText(
    "Operator handoff recorded",
  );
  expect(writes).toEqual([
    { action: "handoff", bundleId: "bundle-a", site: "west", confirm: true },
  ]);
  const report = page.locator("[data-offline-report]");
  await report.getByRole("combobox").selectOption("installed");
  await report.getByRole("checkbox").check();
  await report.getByRole("button").click();
  await expect(page.locator("[data-offline-note]")).toContainText(
    "pending_check_in",
  );
  expect(writes[1]).toEqual({
    action: "report",
    id: "handoff-a",
    outcome: "installed",
  });
  await report.getByRole("combobox").selectOption("failed");
  await report.getByRole("checkbox").check();
  await expect(report.getByRole("button")).toBeDisabled();
  expect(writes).toHaveLength(2);
});

test("audit verifies native payload bytes and keeps polled events resource scoped", async ({
  page,
}) => {
  const entries = JSON.parse(
    readFileSync(
      new URL(
        "../../../../internal/governance/domain/testdata/audit-projection.json",
        import.meta.url,
      ),
      "utf8",
    ),
  );
  let tampered = false;
  const polls: string[] = [];
  const writes: string[] = [];
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url());
    if (route.request().method() !== "GET") writes.push(url.pathname);
    if (url.pathname === "/v1/session")
      return route.fulfill({
        json: { actor: { username: "root" }, effective: { username: "root" } },
      });
    if (url.pathname === "/v1/authorization-audit")
      return route.fulfill({
        json: {
          entries: tampered
            ? [{ ...entries[0], actor: "forged" }, entries[1]]
            : entries,
        },
      });
    return route.fulfill({ status: 404, json: { error: "not configured" } });
  });
  await page.route(
    "**/conduit.notifications.v1.NotificationService/ListNotificationEvents",
    async (route) => {
      expect(route.request().method()).toBe("POST");
      const body = route.request().postDataJSON();
      expect(Object.keys(body).sort()).toEqual(["limit", "resourceRid"]);
      expect(body.limit).toBe("100");
      polls.push(body.resourceRid);
      return route.fulfill({
        json: {
          resourceRid: "environment/east",
          limit: 100,
          events: [
            {
              id: "event-1",
              type: "PLAN_REVIEW",
              resourceRid: "environment/east",
              actor: "operator",
              summary: "<img src=x onerror=alert(1)>",
              occurredAt: "2026-11-15T12:00:00.123456789Z",
            },
          ],
        },
      });
    },
  );
  await hydrate(page);
  await page.goto("/console/#au01-audit");
  await expect(page.locator(".audit-ledger")).toContainText("operator-雪");
  await page
    .getByRole("button", { name: "Verify returned payload digests" })
    .click();
  await expect(page.locator("[data-audit-proof]")).toContainText(
    "internally consistent",
  );
  await expect(page.locator("[data-audit-proof]")).toContainText(
    "no independent checkpoint",
  );
  const recorded = page
    .locator("[data-audit-provenance]")
    .filter({ hasText: "adapter: recorded" });
  await recorded.locator("summary").click();
  await expect(recorded).toContainText("Live model call: no");
  await expect(recorded).toContainText("a".repeat(64));
  await page
    .getByLabel("Audit event resource")
    .selectOption("environment/east");
  await page
    .getByRole("button", { name: "Start polling", exact: true })
    .click();
  await expect(page.locator("[data-audit-event-stream]")).toContainText(
    "<img src=x",
  );
  await expect(page.locator("[data-audit-event-stream] img")).toHaveCount(0);
  // Force the same refresh that previously remounted and reset the poller.
  await page.evaluate(() => Reflect.get(window, "refreshHub")());
  await expect(page.getByLabel("Audit event resource")).toHaveValue(
    "environment/east",
  );
  await expect(
    page.getByRole("button", { name: "Pause polling", exact: true }),
  ).toBeEnabled();
  await expect(page.locator("[data-audit-event-stream]")).toContainText(
    "<img src=x",
  );

  await page
    .getByRole("button", { name: "Pause polling", exact: true })
    .click();
  expect(polls).toEqual(["environment/east"]);
  await page
    .getByLabel("Audit event resource")
    .selectOption("environment/west");
  await page
    .getByRole("button", { name: "Refresh events", exact: true })
    .click();
  await expect(page.locator("[data-audit-event-stream]")).toContainText(
    "did not match the selected resource",
  );
  await expect(page.locator("[data-audit-event-stream]")).not.toContainText(
    "<img src=x",
  );
  tampered = true;
  await page.evaluate(() => Reflect.get(window, "refreshHub")());
  await expect(page.locator("[data-audit-proof]")).toHaveCount(0);
  await page
    .getByRole("button", { name: "Verify returned payload digests" })
    .click();
  await expect(page.locator("[data-audit-proof]")).toContainText(
    "coordinates do not match",
  );
  expect(writes).toEqual([]);
});

test("organization keeps empty and denied snapshots native and read only", async ({
  page,
}) => {
  let mode = "empty";
  const writes: string[] = [];
  await page.route("**/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (route.request().method() !== "GET") writes.push(path);
    if (path === "/v1/session")
      return route.fulfill({
        json: {
          actor: { username: "alice" },
          effective: { username: "alice", teams: [] },
          applications: [],
        },
      });
    if (path === "/v1/operator-identity")
      return route.fulfill({
        status: mode === "denied" ? 503 : 200,
        json:
          mode === "denied"
            ? { error: "unavailable" }
            : { enabled: false, providers: [] },
      });
    if (
      ["/v1/teams", "/v1/resource-grants", "/v1/approval-policies"].includes(
        path,
      )
    ) {
      if (mode === "denied")
        return route.fulfill({ status: 403, json: { error: "forbidden" } });
      if (path === "/v1/teams")
        return route.fulfill({
          json: {
            teams:
              mode === "empty"
                ? []
                : [
                    {
                      name: "live-team",
                      displayName: "Live Team",
                      memberUsers: ["one", "two"],
                      memberGroups: ["group"],
                    },
                  ],
          },
        });
      if (path === "/v1/resource-grants")
        return route.fulfill({ json: { resourceGrants: [] } });
      return route.fulfill({ json: { approvalPolicies: [] } });
    }
    if (path === "/v1/environments")
      return route.fulfill({ json: { environments: [] } });
    return route.fulfill({ status: 404, json: { error: "not configured" } });
  });
  await page.route("**/health/ready", (route) =>
    route.fulfill({ json: { status: "ok" } }),
  );
  await hydrate(page);
  await page.goto("/console/#adm02-org-control-panel");
  await expect(page.locator("[data-org-snapshot]")).toContainText("read only");
  await expect(
    page.locator(
      "#main-workspace input, #main-workspace select, #main-workspace textarea",
    ),
  ).toHaveCount(0);
  await page
    .getByRole("radio", { name: "Teams and roles", exact: true })
    .click();
  await expect(page.locator("[data-org-directory-status]")).toContainText(
    "0 stored teams returned",
  );
  await expect(page.locator("#main-workspace")).not.toContainText(
    "release-managers",
  );
  mode = "populated";
  await page
    .getByRole("button", { name: "Refresh snapshot", exact: true })
    .click();
  const row = page.getByRole("row").filter({ hasText: "Live Team" });
  await expect(row.getByRole("cell").nth(1)).toHaveText("2");
  await expect(row.getByRole("cell").nth(2)).toHaveText("1");
  mode = "denied";
  await page
    .getByRole("button", { name: "Refresh snapshot", exact: true })
    .click();
  await expect(page.locator("[data-org-directory-status]")).toContainText(
    "unavailable",
  );
  await expect(page.locator("#main-workspace")).not.toContainText("Live Team");
  await expect(page.locator("#main-workspace")).not.toContainText(
    "Example data",
  );
  await page
    .getByRole("radio", { name: "Identity provider", exact: true })
    .click();
  await expect(page.locator("#main-workspace")).toContainText(
    "metadata is unavailable",
  );
  expect(writes).toEqual([]);
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
          applications: [],
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

test("workspace studio drops stale reads and confirms real metadata coordinates", async ({
  page,
}) => {
  const a = {
    workspaceId: "workspace-a",
    title: "Workspace A",
    owner: "alice",
    environmentId: "east",
    status: "STOPPED",
    proposalRevision: "rev-a",
    sourceDigest: "sha256:a",
  };
  let b = {
    ...a,
    workspaceId: "workspace-b",
    title: "Workspace B",
    proposalRevision: "rev-b",
    sourceDigest: "sha256:b",
    templateVersion: "v1",
    toolVersions: "tools-v1",
  };
  await page.route(
    "**/conduit.packaging.v1.PackagingRunService/ListPackagingRuns",
    (route) =>
      route.fulfill({
        json: {
          configured: true,
          items: [{ runId: "unrelated-packaging-run", phase: "review" }],
          total: "1",
          pageSize: 1,
          limit: 20,
        },
      }),
  );
  const writes: { path: string; body: any }[] = [];
  let releaseA!: () => void;
  const gateA = new Promise<void>((resolve) => {
    releaseA = resolve;
  });
  let aStarted = false;
  await page.route("**/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/v1/session")
      return route.fulfill({
        json: {
          actor: { username: "alice" },
          effective: { username: "alice" },
        },
      });
    if (path === "/v1/operator-identity")
      return route.fulfill({ json: { enabled: false, providers: [] } });
    if (path === "/v1/packaging-runs")
      return route.fulfill({
        json: {
          items: [{ runId: "unrelated-packaging-run", phase: "review" }],
        },
      });
    if (path === "/v1/dev-workspaces")
      return route.fulfill({ json: { items: [a, b] } });
    if (path === "/v1/dev-workspaces/workspace-a/tree") {
      aStarted = true;
      await gateA;
      return route.fulfill({
        json: {
          entries: [{ path: "foreign-a.md", kind: "file", digest: "sha256:a" }],
        },
      });
    }
    if (path === "/v1/dev-workspaces/workspace-b/tree")
      return route.fulfill({
        json: {
          entries: [{ path: "b.md", kind: "file", digest: "sha256:file-b" }],
        },
      });
    if (path === "/v1/dev-workspaces/workspace-b/file")
      return route.fulfill({
        json: {
          path: "b.md",
          digest: "sha256:file-b",
          content: "<img src=x onerror=alert(1)>",
        },
      });
    if (
      path === "/v1/dev-workspaces/workspace-b" &&
      route.request().method() === "GET"
    )
      return route.fulfill({ json: b });
    if (
      path === "/v1/dev-workspaces/workspace-b/runtime" &&
      route.request().method() === "GET"
    )
      return route.fulfill({
        json: {
          workspaceId: b.workspaceId,
          owner: "alice",
          status: b.status,
          origin: "recorded",
        },
      });
    if (path === "/v1/dev-workspaces/workspace-b/logs")
      return route.fulfill({
        json: { lines: [], origin: "recorded", status: b.status },
      });
    if (
      [
        "/v1/dev-workspaces/workspace-b/runtime",
        "/v1/dev-workspaces/workspace-b/runtime/stop",
      ].includes(path) &&
      route.request().method() === "POST"
    ) {
      writes.push({ path, body: route.request().postDataJSON() });
      b = { ...b, status: path.endsWith("/stop") ? "STOPPED" : "RUNNING" };
      return route.fulfill({
        json: {
          workspaceId: b.workspaceId,
          owner: "alice",
          status: b.status,
          origin: "recorded",
        },
      });
    }
    if (path === "/v1/dev-workspaces/workspace-b/upgrade") {
      const body = route.request().postDataJSON();
      writes.push({ path, body });
      b = { ...b, ...body, proposalRevision: "rev-upgraded" };
      return route.fulfill({ json: b });
    }
    if (path === "/v1/dev-workspaces/workspace-b/submit") {
      const body = route.request().postDataJSON();
      writes.push({ path, body });
      return route.fulfill({
        status: 202,
        json: {
          changeRequestId: "separate-workspace-review",
          status: "PENDING_APPROVALS",
        },
      });
    }
    return route.fulfill({ status: 404, json: { error: "not configured" } });
  });
  await hydrate(page);
  await page.goto("/console/#s09-source");
  await page
    .getByRole("button", { name: "Workspace A · STOPPED", exact: true })
    .click();
  await expect.poll(() => aStarted).toBe(true);
  await page
    .getByRole("button", { name: "Workspace B · STOPPED", exact: true })
    .click();
  await expect(page.locator("[data-workspace-detail]")).toContainText(
    "workspace-b",
  );
  await expect(page.locator("[data-area-object-header]")).toContainText(
    "workspace-b",
  );
  await expect(page.locator("[data-area-object-header]")).not.toContainText(
    "unrelated-packaging-run",
  );
  await expect(page.locator("[data-workspace-studio]")).toContainText("b.md");
  const staleTreeResponse = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname ===
      "/v1/dev-workspaces/workspace-a/tree",
  );
  releaseA();
  await (await staleTreeResponse).finished();
  await page.evaluate(
    () =>
      new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
      ),
  );
  await expect(
    page.getByRole("button", { name: "b.md · file", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "b.md · file", exact: true }).click();
  await expect(page.locator("[data-workspace-studio]")).toContainText(
    "<img src=x",
  );
  await expect(page.locator("[data-workspace-studio] img")).toHaveCount(0);
  await expect(page.locator("[data-workspace-studio]")).not.toContainText(
    "foreign-a.md",
  );
  await page
    .getByRole("button", { name: "Review and lifecycle", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Stage metadata upgrade", exact: true })
    .click();
  expect(writes).toEqual([]);
  const form = page.locator("[data-ws-confirm]");
  const digest = "sha256:" + "c".repeat(64);
  await form.getByLabel("Reviewed source digest").fill(digest);
  await form.getByLabel("Template version").fill("v2");
  await form.getByLabel("Tool versions").fill("tools-v2");
  await form.getByRole("checkbox").check();
  await form.getByLabel("Template version").fill("v3");
  await expect(
    form.getByRole("button", { name: "Confirm workspace action", exact: true }),
  ).toBeDisabled();
  await form.getByRole("checkbox").check();
  await form
    .getByRole("button", { name: "Confirm workspace action", exact: true })
    .click();
  await expect(page.locator("[data-workspace-detail]")).toContainText(digest);
  expect(writes[0].body).toEqual({
    sourceDigest: digest,
    templateVersion: "v3",
    toolVersions: "tools-v2",
  });
  await expect(page.locator("[data-workspace-studio]")).not.toContainText(
    "<img src=x",
  );
  await page
    .getByRole("button", { name: "Stage separate review", exact: true })
    .click();
  await form
    .getByLabel("Title", { exact: true })
    .fill("Review upgraded workspace");
  await form.getByLabel("Review description").fill("Independent human review");
  await form.getByRole("checkbox").check();
  await form
    .getByRole("button", { name: "Confirm workspace action", exact: true })
    .click();
  await expect(page.locator("[data-workspace-studio]")).toContainText(
    "separate-workspace-review",
  );
  expect(writes[1].body).toEqual({
    title: "Review upgraded workspace",
    description: "Independent human review",
    approve: false,
  });
  expect(writes).toHaveLength(2);
  await page
    .getByRole("button", { name: "Preview and logs", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Stage preview start", exact: true })
    .click();
  await form.getByRole("checkbox").check();
  await form
    .getByRole("button", { name: "Confirm workspace action", exact: true })
    .click();
  await expect(page.locator("[data-workspace-detail]")).toContainText(
    "RUNNING",
  );
  await page
    .getByRole("button", { name: "Stage preview stop", exact: true })
    .click();
  await form.getByRole("checkbox").check();
  await form
    .getByRole("button", { name: "Confirm workspace action", exact: true })
    .click();
  await expect(page.locator("[data-workspace-detail]")).toContainText(
    "STOPPED",
  );
  expect(writes.map((write) => write.path)).toEqual([
    "/v1/dev-workspaces/workspace-b/upgrade",
    "/v1/dev-workspaces/workspace-b/submit",
    "/v1/dev-workspaces/workspace-b/runtime",
    "/v1/dev-workspaces/workspace-b/runtime/stop",
  ]);
});

test("ontology explorer scopes pages, drops stale reads and validates without execution", async ({
  page,
}) => {
  const metadata = JSON.parse(
    readFileSync(
      new URL(
        "../../../../testdata/ontology/full-metadata.json",
        import.meta.url,
      ),
      "utf8",
    ),
  );
  let releaseA!: () => void;
  const heldA = new Promise<void>((resolve) => {
    releaseA = resolve;
  });
  let startedA = false;
  const actions: any[] = [];
  await page.route("**/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/v1/session")
      return route.fulfill({
        json: {
          actor: { username: "alice" },
          effective: { username: "alice" },
        },
      });
    return route.fulfill({ status: 404, json: { error: "not configured" } });
  });
  await page.route("**/api/v2/ontologies/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/fullMetadata"))
      return route.fulfill({ json: metadata });
    const body = route.request().postDataJSON();
    if (path.endsWith("/objectSets/loadObjects")) {
      expect(body.pageSize).toBe(25);
      if (body.objectSet.objectType === "Environment") {
        startedA = true;
        await heldA;
        return route.fulfill({
          json: { data: [{ id: "foreign-environment" }] },
        });
      }
      if (body.pageToken === "second")
        return route.fulfill({
          status: 403,
          json: { error: "sample forbidden" },
        });
      return route.fulfill({
        json: {
          data: [
            {
              id: "plan-b",
              __primaryKey: "plan-b",
              secret: "hidden-value",
              title: "<img src=x onerror=alert(1)>",
            },
          ],
          nextPageToken: "second",
        },
      });
    }
    if (path.includes("/actions/")) {
      actions.push(body);
      return route.fulfill({ json: { validation: { result: "VALID" } } });
    }
    return route.fulfill({ status: 404, json: { error: "unknown" } });
  });
  await hydrate(page);
  await page.goto("/console/#s19-ontology");
  const picker = page.getByLabel("Ontology object type");
  await expect(picker.locator('option[value="Environment"]')).toHaveCount(1);
  await picker.selectOption("Environment");
  await expect.poll(() => startedA).toBe(true);
  await picker.selectOption("Plan");
  await expect(page.locator("[data-ontology-samples]")).toContainText("plan-b");
  const staleResponse = page.waitForResponse(
    (response) =>
      response.url().endsWith("/objectSets/loadObjects") &&
      response.status() === 200,
  );
  releaseA();
  await staleResponse;
  await page.evaluate(
    () =>
      new Promise((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(resolve)),
      ),
  );
  await expect(page.locator("[data-ontology-samples]")).not.toContainText(
    "foreign-environment",
  );
  await expect(page.locator("[data-ontology-samples]")).not.toContainText(
    "hidden-value",
  );
  await expect(page.locator("[data-ontology-samples] img")).toHaveCount(0);
  await page.getByLabel("Ontology action").selectOption("createChangeRequest");
  await page
    .getByLabel("Ontology validation parameters")
    .fill('{"title":"reviewed draft"}');
  await page
    .getByRole("button", { name: "Validate only", exact: true })
    .click();
  await expect(page.locator("[data-ont-validation-result]")).toContainText(
    "Validation accepted",
  );
  expect(actions).toEqual([
    {
      parameters: { title: "reviewed draft" },
      options: { mode: "VALIDATE_ONLY" },
    },
  ]);
  await page.getByRole("button", { name: "Next page", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("sample forbidden");
  await expect(page.locator("[data-ontology-samples]")).not.toContainText(
    "plan-b",
  );
});

test("ontology graph distinguishes an empty catalog from denied metadata", async ({
  page,
}) => {
  let denied = false;
  let populated = false;
  let objectReads = 0;
  await page.route("**/v1/**", async (route) => {
    if (new URL(route.request().url()).pathname === "/v1/session")
      return route.fulfill({
        json: {
          actor: { username: "alice" },
          effective: { username: "alice" },
        },
      });
    return route.fulfill({ status: 404, json: { error: "not configured" } });
  });
  await page.route("**/api/v2/ontologies/**", async (route) => {
    if (route.request().url().endsWith("/fullMetadata"))
      return denied
        ? route.fulfill({ status: 403, json: { error: "forbidden" } })
        : route.fulfill({
            json: populated
              ? JSON.parse(
                  readFileSync(
                    new URL(
                      "../../../../testdata/ontology/full-metadata.json",
                      import.meta.url,
                    ),
                    "utf8",
                  ),
                )
              : { objectTypes: {}, actionTypes: {} },
          });
    objectReads++;
    return route.fulfill({ status: 403, json: { error: "forbidden" } });
  });
  await hydrate(page);
  await page.goto("/console/#og01-ontology-graph");
  await expect(
    page.getByText("No object types returned", { exact: true }),
  ).toBeVisible();
  await expect(page.locator('svg[aria-label="Ontology graph"] g')).toHaveCount(
    0,
  );
  expect(objectReads).toBe(0);
  denied = true;
  await page.evaluate(() => Reflect.get(window, "reloadOntologyGraph")());
  await expect(
    page.getByText("Metadata unavailable; no example data substituted", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(page.locator("#main-workspace")).not.toContainText(
    "edge-west-02",
  );
  await expect(page.locator('svg[aria-label="Ontology graph"] g')).toHaveCount(
    0,
  );
  expect(objectReads).toBe(0);
  denied = false;
  populated = true;
  await page.evaluate(() => Reflect.get(window, "reloadOntologyGraph")());
  await expect(
    page.getByText("Native metadata · first 256 types", { exact: true }),
  ).toBeVisible();
  await expect(page.locator('svg[aria-label="Ontology graph"] g')).toHaveCount(
    7,
  );
  await page
    .locator('svg[aria-label="Ontology graph"] g')
    .filter({ hasText: "Plan" })
    .click();
  await expect(
    page
      .getByText("Object page unavailable; no zero-count inference.", {
        exact: true,
      })
      .first(),
  ).toBeVisible();
});

test("active Command Center uses scoped Connect overview and drops a previous actor's response", async ({
  page,
}) => {
  let actor = "alice",
    reads = 0,
    denied = false,
    releaseOld!: () => void;
  const heldOld = new Promise<void>((resolve) => {
    releaseOld = resolve;
  });
  let oldStarted = false;
  await page.route("**/v1/**", async (route) => {
    if (new URL(route.request().url()).pathname === "/v1/session")
      return route.fulfill({
        json: { actor: { username: actor }, effective: { username: actor } },
      });
    return route.fulfill({ status: 404, json: { error: "not configured" } });
  });
  await page.route(
    "**/conduit.reporting.v1.ReportingService/GetOverview",
    async (route) => {
      reads++;
      expect(route.request().method()).toBe("POST");
      expect(route.request().postDataJSON()).toEqual({});
      if (denied)
        return route.fulfill({
          status: 401,
          json: { code: "unauthenticated", message: "sign in required" },
        });
      const index = reads;
      if (index === 2) {
        oldStarted = true;
        await heldOld;
      }
      await route
        .fulfill({
          json: {
            generatedAt: "2026-11-15T12:00:00Z",
            staleAfterSeconds: "300",
            awaitingApprovals: {
              status: "CURRENT",
              count: index === 3 ? "65" : "51",
              items: [],
            },
            failedPlans: { status: "UNAVAILABLE", count: "0", items: [] },
          },
        })
        .catch(() => {});
    },
  );
  await hydrate(page);
  await page.goto("/console/#area-command");
  const total = page.locator('[data-overview-panel="awaitingApprovals"] dd');
  await expect(total).toHaveText("51");
  await expect(
    page.locator('[data-overview-panel="failedPlans"] dd'),
  ).toHaveText("Unavailable");
  expect(reads).toBe(1);
  await page.evaluate(() => Reflect.get(window, "refreshHub")());
  await expect(total).toHaveText("51");
  expect(reads).toBe(1);
  await page
    .getByRole("button", { name: "Refresh overview", exact: true })
    .click();
  await expect.poll(() => oldStarted).toBe(true);
  actor = "bob";
  await page.evaluate(() => Reflect.get(window, "refreshHub")());
  await expect(total).toHaveText("65");
  releaseOld();
  await page.evaluate(
    () =>
      new Promise((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(resolve)),
      ),
  );
  await expect(total).toHaveText("65");
  denied = true;
  await page
    .getByRole("button", { name: "Refresh overview", exact: true })
    .click();
  await expect(page.locator("[data-overview-summary]")).toContainText(
    "sign in required",
  );
  await expect(page.locator("[data-overview-summary]")).not.toContainText("65");
  const beforeIdentityLoss = reads;
  actor = "";
  await page.evaluate(() => Reflect.get(window, "refreshHub")());
  await expect(page.locator("[data-overview-rpc]")).toContainText(
    "Native identity is unavailable",
  );
  expect(reads).toBe(beforeIdentityLoss);
});

test("engineer focus preserves one live conversation, draft and pending request", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 1024 });
  await hydrate(page);
  await page.addInitScript(() =>
    localStorage.setItem("conduit.console.dockWidth", "420"),
  );
  const writes: string[] = [];
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
    if (path === "/v1/assistant/turns") {
      calls++;
      await responseReady;
      await route.fulfill({
        json: {
          label: "Fixture response",
          text: "Recorded conversation response.",
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
  expect(writes).toEqual(["/v1/assistant/turns"]);
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
  expect(writes).toEqual(["/v1/assistant/turns"]);
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
    path: "/workspace/scratch/engineer-guidance-narrow.png",
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
  expect(writes).toEqual(["/v1/assistant/turns"]);
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
  expect(writes).toEqual(["/v1/assistant/turns"]);
});

test("operator badge shows only a complete native session and clears on refusal", async ({
  page,
}) => {
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
    path: "/workspace/scratch/operator-identity-mobile.png",
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

test("proposal examples never replace missing native evidence without opt-in", async ({
  page,
}) => {
  await hydrate(page);
  let native = false;
  const writes: string[] = [];
  page.on("request", (request) => {
    const path = new URL(request.url()).pathname;
    if (request.method() !== "GET" && path.startsWith("/v1/"))
      writes.push(path);
  });
  await page.route("**/v1/**", (route) => route.fulfill({ json: {} }));
  await page.route(
    "**/conduit.packaging.v1.PackagingRunService/ListPackagingRuns",
    (route) =>
      route.fulfill({
        json: {
          configured: true,
          items: native
            ? [{ runId: "recorded-run", phase: "review", outcome: "WAITING" }]
            : [],
          total: native ? "1" : "0",
          pageSize: native ? 1 : 0,
          limit: 20,
        },
      }),
  );
  await page.goto("/console/");
  const workspace = page.locator("#main-workspace");
  const open = async (screen: string) =>
    page.evaluate((id) => Reflect.get(window, "switchScreen")(id), screen);
  await open("pt01-proposal-trace");
  await expect(
    workspace.getByText(
      "No recorded packaging proposal is available in the current Hub view.",
    ),
  ).toBeVisible();
  await expect(
    workspace.getByText("failedPlan PLN-2026-0416", { exact: false }),
  ).toHaveCount(0);
  await page.keyboard.press("Control+Enter");
  await expect(
    workspace.getByRole("button", {
      name: "Simulate trace and staged proposal",
    }),
  ).toHaveCount(0);
  await workspace
    .getByRole("button", { name: "Show proposal example data" })
    .click();
  await expect(
    workspace.getByRole("status").filter({ hasText: "Example data. Trace" }),
  ).toContainText("do not run a model");
  await workspace
    .getByRole("button", { name: "Simulate trace and staged proposal" })
    .click();
  await expect(workspace.getByText(/Staged PRP-903 for review/)).toBeVisible();
  await workspace
    .getByRole("button", { name: "Exit proposal example mode" })
    .click();
  await expect(workspace.getByText(/Staged PRP-903 for review/)).toHaveCount(0);
  await open("pt01-proposal-trace");
  await workspace
    .getByRole("button", { name: "Show proposal example data" })
    .click();
  native = true;
  await page.evaluate(() => Reflect.get(window, "refreshHub")());
  await expect(
    workspace.getByText("Recorded proposal", { exact: false }).first(),
  ).toBeVisible();
  await expect(
    workspace.getByRole("button", { name: "Exit proposal example mode" }),
  ).toHaveCount(0);
  native = false;
  await page.evaluate(() => Reflect.get(window, "refreshHub")());
  await expect(
    workspace.getByRole("button", { name: "Show proposal example data" }),
  ).toBeVisible();
  await expect(
    workspace.getByText("failedPlan PLN-2026-0416", { exact: false }),
  ).toHaveCount(0);
  expect(writes).toEqual([]);
});

test("operational prototype rows require a per-view example choice", async ({
  page,
}) => {
  await hydrate(page);
  let operator = "alice";
  const writes: string[] = [];
  page.on("request", (request) => {
    if (
      request.method() !== "GET" &&
      new URL(request.url()).pathname.startsWith("/v1/")
    )
      writes.push(request.url());
  });
  await page.route("**/v1/**", (route) => route.fulfill({ json: {} }));
  await page.route("**/v1/session", (route) =>
    route.fulfill({
      json: {
        actor: { username: operator },
        effective: { username: operator, teams: [] },
        applications: [],
      },
    }),
  );
  await page.goto("/console/");
  const root = page.locator("#main-workspace");
  const open = (id: string) =>
    page.evaluate((screen) => Reflect.get(window, "switchScreen")(screen), id);
  await open("area-command");
  await page
    .getByRole("button", { name: "Example fleet", exact: true })
    .click();
  await expect(root.getByText("Pipeline Agent", { exact: false })).toHaveCount(
    0,
  );
  await root
    .getByRole("button", { name: "Show example data", exact: true })
    .click();
  await expect(
    root.getByRole("status").filter({ hasText: "local prototype" }),
  ).toBeVisible();
  await expect(
    root.getByText("Pipeline Agent", { exact: false }).first(),
  ).toBeVisible();
  await root
    .getByRole("button", { name: "Exit example mode", exact: true })
    .click();
  await expect(root.getByText("Pipeline Agent", { exact: false })).toHaveCount(
    0,
  );
  await root
    .getByRole("button", { name: "Show example data", exact: true })
    .click();
  operator = "bob";
  await page.evaluate(() => Reflect.get(window, "refreshHub")());
  await expect(
    root.getByRole("button", { name: "Show example data", exact: true }),
  ).toBeVisible();
  await expect(root.getByText("Pipeline Agent", { exact: false })).toHaveCount(
    0,
  );
  await root
    .getByRole("button", { name: "Show example data", exact: true })
    .click();
  await open("s05-backlog");
  await expect(root.getByText("STR-216", { exact: false })).toHaveCount(0);
  await expect(
    root.getByRole("button", { name: "Show example data", exact: true }),
  ).toBeVisible();
  await open("area-command");
  await page
    .getByRole("button", { name: "Example fleet", exact: true })
    .click();
  await expect(
    root.getByRole("button", { name: "Show example data", exact: true }),
  ).toBeVisible();
  await open("missing-view");
  await expect(
    root.getByRole("region", { name: "View unavailable" }),
  ).toBeVisible();
  await expect(root.getByText("Pipeline Agent", { exact: false })).toHaveCount(
    0,
  );
  expect(writes).toEqual([]);
});

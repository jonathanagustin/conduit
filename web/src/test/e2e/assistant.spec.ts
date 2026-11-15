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
}) => {
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
    path: "/workspace/scratch/command-center-geography.png",
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
    if (url.pathname.endsWith("/events")) {
      if (url.pathname.includes("run-a")) {
        const next = url.searchParams.has("cursor");
        await route.fulfill({
          json: {
            items: [
              {
                sequence: next ? 2 : 1,
                at: "2026-11-15T09:00:00Z",
                step: next ? "review" : "assess",
                outcome: next ? "WAITING" : "PASS",
                reason: next ? "waiting-a" : "a-only-output",
              },
            ],
            total: 2,
            ...(next ? {} : { nextCursor: "cursor-a" }),
          },
        });
        return;
      }
      await route.fulfill({
        json: {
          items: [
            {
              sequence: 1,
              at: "2026-11-15T09:00:01Z",
              step: "build",
              outcome: "FAIL_REQUIRED",
              reason: "b-only-failed",
            },
          ],
          total: 1,
        },
      });
      return;
    }
    await route.fulfill({ json: { items: [], total: 0 } });
  });
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
  expect(calls.some((call) => call.path.includes("cursor=cursor-a"))).toBe(
    true,
  );
  expect(calls.every((call) => call.method === "GET")).toBe(true);
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

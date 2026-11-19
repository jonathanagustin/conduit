import { expect, test } from "@playwright/test";
import { APPLICATIONS } from "./hub-stub";
import { installPackagingReadStub, hydrate } from "./console-fixture";

installPackagingReadStub();

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
    if (path === "/v1/authorization-audit") {
      const query = new URL(route.request().url()).searchParams;
      expect(query.get("order")).toBe("desc");
      expect(query.get("view")).toBe("chain");
    }
    const bodies: Record<string, unknown> = {
      "/v1/session": {
        actor: { username: "reader" },
        effective: { username: "reader" },
        applications: APPLICATIONS,
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
        order: "desc",
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
  await page.evaluate(() => Reflect.get(window, "refreshHub")());
  await expect(labels).toBeFocused();
  await page.keyboard.down("Space");
  await page.evaluate(() => Reflect.get(window, "refreshHub")());
  // Holding Space must not toggle the replacement button a second time.
  await page.keyboard.down("Space");
  await page.keyboard.up("Space");
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
  await expect(composer).toBeFocused();
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
  // Delay the real action's focus frame while its composer is replaced.
  // A newly mounted composer must retain the user's current focus.
  await object
    .getByRole("button", { name: "Ask", exact: true })
    .evaluate((button) => {
      const frames: FrameRequestCallback[] = [];
      const originalFrame = window.requestAnimationFrame;
      window.requestAnimationFrame = (callback) => {
        frames.push(callback);
        return 0;
      };
      try {
        (button as HTMLButtonElement).click();
      } finally {
        window.requestAnimationFrame = originalFrame;
      }
      const composer = document.querySelector("#right-chat-rail textarea");
      if (!composer || frames.length !== 1)
        throw new Error("context focus frame missing");
      const replacement = composer.cloneNode(true) as HTMLTextAreaElement;
      replacement.id = "replacement-context-composer";
      composer.replaceWith(replacement);
      const current = document.createElement("button");
      current.textContent = "Current focus";
      document.body.append(current);
      current.focus();
      try {
        for (const frame of frames) frame(performance.now());
        if (document.activeElement !== current)
          throw new Error("stale context frame stole focus");
      } finally {
        replacement.replaceWith(composer);
        current.remove();
      }
    });
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

import AxeBuilder from "@axe-core/playwright";
import type { Page } from "@playwright/test";

import { expect, test, type HubStub } from "./harness.js";
import {
  ConsoleShell,
  EnvironmentDetailPom,
  EnvironmentIndexPom,
  PlanIndexPom,
  ProductDetailPom,
  ProductIndexPom,
} from "./pages/index.js";

function quietOverview(hub: HubStub) {
  hub.get("/overview", {
    generatedAt: "2026-08-09T12:00:00Z",
    awaitingApprovals: { status: "CURRENT", count: 0, items: [] },
    failedPlans: { status: "CURRENT", count: 0, items: [] },
    runningPlans: { status: "CURRENT", count: 0, items: [] },
    unhealthyEntities: { status: "CURRENT", count: 0, items: [] },
    staleReports: { status: "CURRENT", count: 0, items: [] },
    blockedPromotions: { status: "CURRENT", count: 0, items: [] },
  });
}

async function expectNoAccessibilityViolations(page: Page) {
  // @playwright/mcp intentionally carries a newer browser runtime than the
  // test runner. Axe's structural Page type resolves to that peer even though
  // both expose the same runtime protocol used here.
  const axePage = page as unknown as ConstructorParameters<typeof AxeBuilder>[0]["page"];
  const result = await new AxeBuilder({ page: axePage }).analyze();
  expect(
    result.violations.map(({ id, impact, nodes }) => ({
      id,
      impact,
      targets: nodes.map((node) => node.target),
    })),
  ).toEqual([]);
}

test("overview satisfies the automated accessibility contract", async ({ page, hub }) => {
  quietOverview(hub);
  await page.goto("overview");
  await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible();
  await expectNoAccessibilityViolations(page);
});

test("keyboard users can skip the application chrome", async ({ page, hub }) => {
  quietOverview(hub);
  await page.goto("overview");
  const shell = new ConsoleShell(page);

  await expect(shell.skipLink).toBeAttached();
  await page.keyboard.press("Tab");
  await expect(shell.skipLink).toBeFocused();
  await shell.skipLink.press("Enter");
  await expect(shell.main).toBeFocused();
});

test("mobile fleet remains readable and accessibility-valid", async ({ page, hub }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  hub.fleet([]);
  await page.goto("fleet");
  await expect(page.getByRole("heading", { name: "Fleet" })).toBeVisible();
  await expectNoAccessibilityViolations(page);
});

test("mobile shell reserves the viewport for content while retaining touch targets", async ({
  page,
  hub,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  hub.fleet([]);
  await page.goto("fleet");
  const shell = new ConsoleShell(page);

  const rail = await shell.globalNavigation.boundingBox();
  const home = await shell.globalNavigation.getByRole("link", { name: "Home" }).boundingBox();
  const canvas = await shell.workspaceCanvas.boundingBox();
  expect(rail?.width).toBe(48);
  expect(home?.width).toBeGreaterThanOrEqual(44);
  expect(home?.height).toBeGreaterThanOrEqual(44);
  expect(canvas?.x).toBe(48);
  expect(canvas?.width).toBe(342);
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth))
    .toBe(true);
});

test("Product promotion pipeline remains accessible on mobile", async ({ page, hub }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  hub.get("/products/app.example:payments/releases", {
    releases: [
      {
        product: "app.example:payments",
        version: "2.4.2",
        chart: { repository: "registry.example/payments", digest: "sha256:def" },
        images: [],
        channels: ["STAGING"],
      },
    ],
  });
  hub.get("/promotions", [
    {
      id: "prm_mobile",
      product: "app.example:payments",
      version: "2.4.2",
      fromChannel: "RELEASE",
      toChannel: "STAGING",
      status: "BLOCKED",
      blockers: [{ kind: "SOAK", message: "48 hour soak has not elapsed" }],
    },
  ]);
  await page.goto("products/app.example%3Apayments");
  const product = new ProductDetailPom(page);

  await product.promotionTransitionById("prm_mobile").click();
  await expect(product.promotionConstraintsById("prm_mobile")).toBeVisible();
  await expect
    .poll(() =>
      product.promotionPipeline.evaluate(
        (element) => element.scrollWidth <= element.clientWidth,
      ),
    )
    .toBe(true);
  await expectNoAccessibilityViolations(page);
});

test("bounded indexes use accessible mobile records without horizontal overflow", async ({
  page,
  hub,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  hub.get("/environments", {
    environments: [{
      environment: { id: "production-environment-with-a-long-coordinate", connectionSetting: "DIRECT" },
      reported: { environment: "production", observedAt: "2026-08-07T12:00:00Z", entityCount: 42 },
    }],
    total: 1, pageSize: 1, limit: 100, hasPrevious: false,
  });
  await page.goto("environments");
  const environments = new EnvironmentIndexPom(page);
  await expect(environments.root.locator("table")).toBeHidden();
  await expect(environments.root.getByTestId("environment-mobile-records")).toBeVisible();
  await expectNoAccessibilityViolations(page);

  hub.get("/products", {
    products: [{
      product: "app.example:product-with-a-deliberately-long-coordinate",
      releaseCount: 12,
      latestVersion: "2026.08.09-long-version",
    }],
    total: 1, pageSize: 1, limit: 100, hasPrevious: false,
  });
  await page.goto("products");
  const products = new ProductIndexPom(page);
  await expect(products.root.locator("table")).toBeHidden();
  await expect(products.root.getByTestId("product-mobile-records")).toBeVisible();
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth))
    .toBe(true);
  await expectNoAccessibilityViolations(page);

  hub.get("/plans", {
    plans: [{
      id: "pln_with_a_deliberately_long_coordinate_for_mobile",
      environment: "production-environment-with-a-long-coordinate",
      status: "AWAITING_APPROVAL",
      stepCount: 123,
      approvalCount: 1,
      requiredApprovals: 2,
      createdAt: "2026-08-09T12:00:00Z",
      updatedAt: "2026-08-09T12:01:00Z",
      revision: 1,
    }],
    total: 1, pageSize: 1, limit: 100, hasPrevious: false,
  });
  await page.goto("plans");
  const plans = new PlanIndexPom(page);
  await expect(plans.root.locator("table")).toBeHidden();
  await expect(plans.root.getByTestId("plan-mobile-records")).toBeVisible();
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth))
    .toBe(true);
  await expectNoAccessibilityViolations(page);
});

test("populated Environment Entities use an accessible mobile definition list", async ({
  page,
  hub,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  hub.get("/environments/prod-use1", {
    environment: { id: "prod-use1", generation: "0123456789abcdef" },
    reported: {
      environment: "prod-use1",
      agentId: "agent-prod",
      observedAt: "2026-08-07T12:00:00Z",
      entities: [
        {
          installationId: "payments",
          product: "app.example:payments-with-a-deliberately-long-mobile-coordinate",
          version: "2.4.1",
          health: "HEALTHY",
          lifecycle: "INSTALLED",
        },
      ],
    },
  });
  await page.goto("environments/prod-use1");
  const environment = new EnvironmentDetailPom(page);

  await expect(environment.entityRow("payments")).toBeHidden();
  const card = environment.entityCard("payments");
  await expect(card).toBeVisible();
  await expect(card.getByText("app.example:payments-with-a-deliberately-long-mobile-coordinate")).toBeVisible();
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth))
    .toBe(true);
  await expectNoAccessibilityViolations(page);
});

test("populated Environment Plans and Commands remain accessible on mobile", async ({
  page,
  hub,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  hub.get("/environments/prod-use1", {
    environment: { id: "prod-use1", generation: "0123456789abcdef" },
  });
  hub.get("/plans", {
    plans: [
      {
        id: "pln_mobile",
        environment: "prod-use1",
        status: "APPROVED",
        stepCount: 0,
        approvalCount: 1,
        requiredApprovals: 1,
        createdAt: "2026-08-07T12:00:00Z",
        updatedAt: "2026-08-07T12:01:00Z",
        revision: 2,
      },
    ],
    total: 1,
    pageSize: 1,
    limit: 100,
    hasPrevious: false,
  });
  hub.get("/environments/prod-use1/commands", [
    {
      id: "pln_command_mobile",
      status: "SUCCEEDED",
      createdAt: "2026-08-07T12:00:00Z",
      command: {
        type: "RESTART",
        installationId: "payments",
        issuedBy: "operator-one",
        rationale: "a-deliberately-long-rationale-without-breaks-for-mobile-overflow-verification",
        issuedAt: "2026-08-07T12:00:00Z",
      },
    },
  ]);
  await page.goto("environments/prod-use1");
  const environment = new EnvironmentDetailPom(page);

  await environment.tab("Plans").click();
  await expect(environment.planRow("pln_mobile")).toBeHidden();
  await expect(environment.planCard("pln_mobile")).toBeVisible();
  await expectNoAccessibilityViolations(page);

  await environment.tab("Commands").click();
  await expect(environment.commandRow("pln_command_mobile")).toBeHidden();
  await expect(environment.commandCard("pln_command_mobile")).toBeVisible();
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth))
    .toBe(true);
  await expectNoAccessibilityViolations(page);
});

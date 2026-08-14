import { expect, indexPage, test } from "./harness.js";
import {
  EnvironmentDetailPom,
  EnvironmentIndexPom,
  ProductDetailPom,
  ProductIndexPom,
  PromotionsPom,
} from "./pages/index.js";

/** Environments, Products and Promotions -- the remaining P0 read screens.
 *
 * Environment tabs follow captures 070 / 069 / 066; the Product catalog follows
 * 043 and 044; the promotion constraints follow 048, where selecting a
 * transition node reveals why a Release cannot advance. */

const ENVIRONMENT = {
  environment: { id: "prod-use1", generation: "0123456789abcdef", connectionSetting: "DIRECT" },
  reported: {
    environment: "prod-use1",
    agentId: "agent-prod",
    observedAt: "2026-08-07T12:00:00Z",
    entities: [
      {
        installationId: "payments",
        product: "app.example:payments",
        version: "2.4.1",
        health: "HEALTHY",
        lifecycle: "INSTALLED",
      },
    ],
  },
};

const ENVIRONMENT_SUMMARY = {
  environment: ENVIRONMENT.environment,
  reported: {
    environment: "prod-use1",
    agentId: "agent-prod",
    observedAt: "2026-08-07T12:00:00Z",
    entityCount: 1,
  },
};

const ENVIRONMENT_PLAN = {
  id: "pln_environment",
  desiredStateDigest: "sha256:desired",
  plan: { environment: "prod-use1", generation: "0123456789abcdef", steps: [] },
  status: "APPROVED",
  requiredApprovals: 1,
  approvals: [{ subject: "operator-one" }],
  createdAt: "2026-08-07T12:00:00Z",
  updatedAt: "2026-08-07T12:01:00Z",
  revision: 2,
};

test.describe("Environments", () => {
  test("lists Environments with their connection setting", async ({ page, hub }) => {
    hub.get("/environments", {
      environments: [ENVIRONMENT_SUMMARY], total: 1, pageSize: 1, limit: 100, hasPrevious: false,
    });
    await page.goto("environments");

    const row = page.getByRole("row", { name: /prod-use1/ });
    await expect(row).toContainText("DIRECT");
    await expect(row).toContainText("1");
  });

  test("marks an Environment that has never reported", async ({ page, hub }) => {
    // Never having reported and reporting nothing are different conditions: one
    // has no Agent, the other has an Agent with nothing to say.
    hub.get("/environments", {
      environments: [{ environment: { id: "dev-use2", connectionSetting: "DIRECT" } }],
      total: 1, pageSize: 1, limit: 100, hasPrevious: false,
    });
    await page.goto("environments");

    await expect(page.getByRole("row", { name: /dev-use2/ })).toContainText("never reported");
  });

  test("opens an Environment and shows its reported Entities", async ({ page, hub }) => {
    hub.get("/environments", {
      environments: [ENVIRONMENT_SUMMARY], total: 1, pageSize: 1, limit: 100, hasPrevious: false,
    });
    hub.get("/environments/prod-use1", ENVIRONMENT);
    await page.goto("environments");

    await page.getByRole("link", { name: "prod-use1" }).click();
    const environment = new EnvironmentDetailPom(page);

    await expect(environment.root).toBeVisible();
    const entity = environment.entityRow("payments");
    await expect(entity).toContainText("2.4.1");
    await expect(entity).toContainText("HEALTHY");
  });

  test("keeps Environment search and opaque paging in the URL", async ({ page, hub }) => {
    hub.get("/environments", {
      environments: [ENVIRONMENT_SUMMARY], total: 2, pageSize: 1, limit: 1,
      hasPrevious: false, nextCursor: "environment-next",
    });
    hub.get("/environments", {
      environments: [{ environment: { id: "stage-use1" } }], total: 2, pageSize: 1, limit: 1,
      hasPrevious: true, previousCursor: "",
    }, { cursor: "environment-next" });
    await page.goto("environments");
    const environments = new EnvironmentIndexPom(page);

    await environments.next.click();
    await expect(page).toHaveURL(/cursor=environment-next/);
    await expect(page.getByRole("row", { name: /stage-use1/ })).toBeVisible();
    await environments.previous.click();
    await expect(page).not.toHaveURL(/cursor=/);

    await environments.search.fill("prod");
    await environments.search.press("Enter");
    await expect(page).toHaveURL(/search=prod/);
    await expect.poll(() => hub.requests().some((request) => request.includes("search=prod"))).toBe(true);
  });

  test("leads the Overview with health, not with a table", async ({ page, hub }) => {
    // Capture 206 shows the upstream Overview as numbered sections; the plan's
    // wireframe leads with health and generation. An Entities table under the
    // name "Overview" answers a different question from the one the tab asks.
    hub.get("/environments/prod-use1", {
      ...ENVIRONMENT,
      reported: {
        ...ENVIRONMENT.reported,
        entities: [
          ...ENVIRONMENT.reported.entities,
          {
            installationId: "search",
            product: "app.example:search",
            version: "8.1.0",
            health: "UNHEALTHY",
            configStatus: "DRIFTED",
            lifecycle: "INSTALLED",
          },
        ],
      },
    });
    await page.goto("environments/prod-use1");
    const environment = new EnvironmentDetailPom(page);

    await expect(page.getByText("1 of 2")).toBeVisible();
    await expect(page.getByText("1 drifted")).toBeVisible();
    // Scoped to the Generation section: the generation also appears as a tag in
    // the header, and an unscoped match is ambiguous rather than wrong.
    const generation = environment.overviewSummary.getByText(/0123456789abcdef/);
    await expect(generation).toBeVisible();
  });

  test("explains an Environment that has never reported instead of showing an empty table", async ({
    page,
    hub,
  }) => {
    hub.get("/environments/dev-use2", { environment: { id: "dev-use2" } });
    await page.goto("environments/dev-use2");

    await expect(page.getByText("This Environment has never reported")).toBeVisible();
  });

  test("renders a command from inside the Plan that carries it", async ({ page, hub }) => {
    // /commands returns PlanRecord values whose command is nested, not a flat
    // command list. Decoding it flat produced a row of dashes with the Plan's
    // status sitting in the Type column, and the empty-state test could not see
    // it because there was nothing to render.
    hub.get("/environments/prod-use1", ENVIRONMENT);
    hub.get("/environments/prod-use1/commands", [
      {
        id: "pln_cmd",
        status: "SUCCEEDED",
        createdAt: "2026-08-07T12:00:00Z",
        command: {
          type: "RESTART",
          installationId: "payments",
          issuedBy: "operator-one",
          rationale: "wedged pod",
          issuedAt: "2026-08-07T12:00:00Z",
        },
      },
    ]);
    await page.goto("environments/prod-use1");
    const environment = new EnvironmentDetailPom(page);
    await environment.tab("Commands").click();

    const row = page.getByRole("row", { name: /RESTART/ });
    await expect(row).toContainText("payments");
    await expect(row).toContainText("operator-one");
    await expect(row).toContainText("wedged pod");
    await expect(row).toContainText("SUCCEEDED");
  });

  test("moves between the Overview, Plans and Commands tabs", async ({ page, hub }) => {
    hub.get("/environments/prod-use1", ENVIRONMENT);
    hub.get("/plans", { plans: [], total: 0, pageSize: 0, limit: 100, hasPrevious: false });
    hub.get("/environments/prod-use1/commands", []);
    await page.goto("environments/prod-use1");
    const environment = new EnvironmentDetailPom(page);

    await environment.tab("Plans").click();
    await expect(page.getByText("This Environment has no Plans.")).toBeVisible();

    await environment.tab("Commands").click();
    await expect(page.getByText(/No commands have been executed/)).toBeVisible();
  });

  test("asks the Hub only for this Environment's Plans", async ({ page, hub }) => {
    hub.get("/environments/prod-use1", ENVIRONMENT);
    hub.get("/plans", { plans: [], total: 0, pageSize: 0, limit: 100, hasPrevious: false });
    await page.goto("environments/prod-use1");
    const environment = new EnvironmentDetailPom(page);
    await environment.tab("Plans").click();

    await expect(page.getByText("This Environment has no Plans.")).toBeVisible();
    expect(hub.requests().some((request) => request.includes("environment=prod-use1"))).toBe(true);
  });

  test("opens a Plan from the Environment's scoped Plans tab", async ({ page, hub }) => {
    hub.get("/environments/prod-use1", ENVIRONMENT);
    hub.get("/plans", { plans: [{ id: ENVIRONMENT_PLAN.id, environment: ENVIRONMENT_PLAN.plan.environment,
      status: ENVIRONMENT_PLAN.status, stepCount: ENVIRONMENT_PLAN.plan.steps.length,
      approvalCount: ENVIRONMENT_PLAN.approvals.length, requiredApprovals: ENVIRONMENT_PLAN.requiredApprovals,
      createdAt: ENVIRONMENT_PLAN.createdAt, updatedAt: ENVIRONMENT_PLAN.updatedAt,
      revision: ENVIRONMENT_PLAN.revision }], total: 1, pageSize: 1, limit: 100, hasPrevious: false });
    hub.get("/plans/pln_environment", ENVIRONMENT_PLAN);
    await page.goto("environments/prod-use1");
    const environment = new EnvironmentDetailPom(page);

    await environment.tab("Plans").click();
    await expect(environment.planRow("pln_environment")).toContainText("APPROVED");
    await expect(environment.planLink("pln_environment")).toHaveAttribute(
      "href",
      "/console/plans/pln_environment",
    );
    await environment.planLink("pln_environment").click();

    await expect(page).toHaveURL(/\/console\/plans\/pln_environment$/);
    await expect(page.getByTestId("plan-detail")).toBeVisible();
  });
});

test.describe("Products", () => {
  const RELEASES = [
    {
      product: "app.example:payments",
      version: "2.4.1",
      chart: { repository: "registry.example/payments", digest: "sha256:abc" },
      images: [],
      channels: ["RELEASE"],
      vulnerabilityAssessment: { outcome: "PASS", activeVulnerabilities: 0 },
    },
    {
      product: "app.example:payments",
      version: "2.4.2",
      chart: { repository: "registry.example/payments", digest: "sha256:def" },
      images: [],
      channels: ["STAGING"],
    },
  ];
  const PRODUCT_PROMOTIONS = [
    {
      id: "prm_product",
      product: "app.example:payments",
      version: "2.4.2",
      fromChannel: "RELEASE",
      toChannel: "STAGING",
      status: "BLOCKED",
      blockers: [
        {
          kind: "HEALTHY_ENTITIES_BELOW_MINIMUM",
          message: "2 of 5 Entities are healthy",
          subjects: ["prod-use1", "prod-euw1"],
        },
      ],
    },
  ];

  test("lists Products with their release count", async ({ page, hub }) => {
    hub.get("/products", {
      products: [{ product: "app.example:payments", releaseCount: 2, latestVersion: "2.4.2" }],
      total: 1, pageSize: 1, limit: 100, hasPrevious: false,
    });
    await page.goto("products");

    const row = page.getByRole("row", { name: /app.example:payments/ });
    await expect(row).toContainText("2");
  });

  test("pages Product summaries without downloading Releases", async ({ page, hub }) => {
    hub.get("/products", {
      products: [{ product: "app.example:first", releaseCount: 12, latestVersion: "1.2.0" }],
      total: 2, pageSize: 1, limit: 1, hasPrevious: false, nextCursor: "product-next",
    });
    hub.get("/products", {
      products: [{ product: "app.example:second", releaseCount: 8, latestVersion: "2.0.0" }],
      total: 2, pageSize: 1, limit: 1, hasPrevious: true, previousCursor: "",
    }, { cursor: "product-next" });
    await page.goto("products");
    const products = new ProductIndexPom(page);

    await expect(products.pagination).toContainText("Showing 1 of 2 matching results");
    await products.next.click();
    await expect(page).toHaveURL(/cursor=product-next/);
    await expect(page.getByRole("row", { name: /app.example:second/ })).toContainText("8");
    expect(hub.requests().some((request) => request.includes("view=summary"))).toBe(true);
  });

  test("shows each Release with its channels and immutable chart digest", async ({ page, hub }) => {
    hub.get("/products/app.example:payments/releases", { releases: RELEASES });
    hub.get("/promotions", indexPage("promotions", PRODUCT_PROMOTIONS));
    await page.goto("products/app.example%3Apayments");
    const product = new ProductDetailPom(page);

    const release = product.release("2.4.1");
    await expect(release).toContainText("RELEASE");
    // The digest is what makes a Release immutable, so it is shown rather than
    // the mutable tag.
    await expect(release).toContainText("sha256:abc");
  });

  test("does not present an unassessed Release as a clean one", async ({ page, hub }) => {
    hub.get("/products/app.example:payments/releases", { releases: RELEASES });
    hub.get("/promotions", indexPage("promotions", PRODUCT_PROMOTIONS));
    await page.goto("products/app.example%3Apayments");
    const product = new ProductDetailPom(page);

    await expect(product.release("2.4.1")).toContainText("PASS");
    await expect(product.release("2.4.2")).toContainText("not assessed");
  });

  test("scopes the Release Channel pipeline to this Product and explains a blocker", async ({
    page,
    hub,
  }) => {
    hub.get("/products/app.example:payments/releases", { releases: RELEASES });
    hub.get("/promotions", indexPage("promotions", PRODUCT_PROMOTIONS));
    await page.goto("products/app.example%3Apayments");
    const product = new ProductDetailPom(page);

    await expect(product.promotionPipeline).toBeVisible();
    await product.promotionTransitionById("prm_product").click();
    const constraints = product.promotionConstraintsById("prm_product");
    await expect(constraints).toContainText("HEALTHY_ENTITIES_BELOW_MINIMUM");
    await expect(constraints).toContainText("2 of 5 Entities are healthy");
    expect(
      hub
        .requests()
        .some(
          (request) =>
            request.includes("GET /v1/promotions?") &&
            request.includes("product=app.example%3Apayments") &&
            request.includes("limit=200"),
        ),
    ).toBe(true);
  });

  test("keeps Releases usable when the Product promotion projection fails", async ({ page, hub }) => {
    hub.get("/products/app.example:payments/releases", { releases: RELEASES });
    hub.fails("/promotions", 500, "promotion store unavailable");
    await page.goto("products/app.example%3Apayments");
    const product = new ProductDetailPom(page);

    await expect(product.release("2.4.1")).toBeVisible();
    await expect(page.getByText(/promotion pipeline .* could not be read/i)).toBeVisible();
  });

  test("calls an unevaluated Product pipeline unknown rather than clear", async ({ page, hub }) => {
    hub.get("/products/app.example:payments/releases", { releases: RELEASES });
    hub.get("/promotions", indexPage("promotions", []));
    await page.goto("products/app.example%3Apayments");

    await expect(page.getByText(/channel pipeline is unknown/i)).toBeVisible();
  });
});

test.describe("Promotions", () => {
  test("keeps opaque traversal in the URL across reload and retry", async ({ page, hub }) => {
    const first = {
      id: "prm_first",
      product: "app.example:payments",
      version: "2.4.1",
      fromChannel: "RELEASE",
      toChannel: "STAGING",
      status: "BLOCKED",
    };
    const second = { ...first, id: "prm_second", version: "2.4.2" };
    hub.get(
      "/promotions",
      indexPage("promotions", [first], {
        total: 2,
        pageSize: 1,
        limit: 1,
        nextCursor: "promotion-next",
      }),
    );
    hub.get(
      "/promotions",
      indexPage("promotions", [second], {
        total: 2,
        pageSize: 1,
        limit: 1,
        hasPrevious: true,
      }),
      { cursor: "promotion-next" },
    );

    await page.goto("promotions?search=payments");
    await page.getByTestId("promotions-pagination").getByRole("button", { name: "Next" }).click();
    await expect(page).toHaveURL(/cursor=promotion-next/);
    await expect(page.getByTestId("promotion-transition-prm_second")).toBeVisible();

    const beforeReload = hub.requests().filter((request) => request.includes("cursor=promotion-next")).length;
    await page.reload();
    await expect(page.getByTestId("promotion-transition-prm_second")).toBeVisible();
    expect(hub.requests().filter((request) => request.includes("cursor=promotion-next")).length).toBeGreaterThan(beforeReload);

    hub.fails("/promotions", 500, "promotion store unavailable", { cursor: "promotion-next" });
    await page.reload();
    await expect(page.getByText(/promotions could not be read/i)).toBeVisible();
    hub.get(
      "/promotions",
      indexPage("promotions", [second], {
        total: 2,
        pageSize: 1,
        limit: 1,
        hasPrevious: true,
      }),
      { cursor: "promotion-next" },
    );
    await page.getByRole("button", { name: "Try again" }).click();
    await expect(page.getByTestId("promotion-transition-prm_second")).toBeVisible();
    await expect(page).toHaveURL(/cursor=promotion-next/);

    await page.getByTestId("promotions-pagination").getByRole("button", { name: "Previous" }).click();
    await expect(page).not.toHaveURL(/cursor=/);
    await expect(page).toHaveURL(/search=payments/);
  });

  test("says a promotion passed without listing constraints", async ({ page, hub }) => {
    hub.get("/promotions", indexPage("promotions", [
      {
        id: "prm_1",
        product: "app.example:payments",
        version: "2.4.1",
        fromChannel: "RELEASE",
        toChannel: "STAGING",
        status: "PASSED",
      },
    ]));
    await page.goto("promotions");
    const promotions = new PromotionsPom(page);
    await promotions.transition("prm_1").click();

    await expect(page.getByText("Every constraint on this transition is satisfied.")).toBeVisible();
  });

  test("shows why a blocked promotion cannot advance", async ({ page, hub }) => {
    // "Why not" is the whole screen: a status alone leaves the operator with
    // nothing to act on.
    hub.get("/promotions", indexPage("promotions", [
      {
        id: "prm_2",
        product: "app.example:payments",
        version: "2.4.2",
        fromChannel: "RELEASE",
        toChannel: "STAGING",
        status: "BLOCKED",
        blockers: [
          {
            kind: "HEALTHY_ENTITIES_BELOW_MINIMUM",
            message: "2 of 5 Entities are healthy",
            subjects: ["prod-use1", "prod-euw1"],
          },
        ],
      },
    ]));
    await page.goto("promotions");
    const promotions = new PromotionsPom(page);
    await promotions.transition("prm_2").click();

    await expect(page.getByText("HEALTHY_ENTITIES_BELOW_MINIMUM")).toBeVisible();
    await expect(page.getByText("2 of 5 Entities are healthy")).toBeVisible();
    await expect(page.getByText(/prod-use1, prod-euw1/)).toBeVisible();
  });

  test("falls back to the derived sentences when a Hub sends no structured blockers", async ({
    page,
    hub,
  }) => {
    hub.get("/promotions", indexPage("promotions", [
      {
        id: "prm_3",
        product: "app.example:payments",
        version: "2.4.3",
        fromChannel: "RELEASE",
        toChannel: "STAGING",
        status: "BLOCKED",
        reasons: ["the soak period has not elapsed"],
      },
    ]));
    await page.goto("promotions");
    const promotions = new PromotionsPom(page);
    await promotions.transition("prm_3").click();

    await expect(page.getByText("the soak period has not elapsed")).toBeVisible();
  });

  test("calls a blocked promotion with no reason a Hub defect", async ({ page, hub }) => {
    // Blocked with nothing to satisfy is not a constraint the operator can act
    // on, and saying so is more useful than an empty list.
    hub.get("/promotions", indexPage("promotions", [
      {
        id: "prm_4",
        product: "app.example:payments",
        version: "2.4.4",
        fromChannel: "RELEASE",
        toChannel: "STAGING",
        status: "BLOCKED",
      },
    ]));
    await page.goto("promotions");
    const promotions = new PromotionsPom(page);
    await promotions.transition("prm_4").click();

    await expect(page.getByText(/Hub defect/)).toBeVisible();
  });
});

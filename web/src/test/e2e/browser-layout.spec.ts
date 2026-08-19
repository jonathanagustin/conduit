import { installation } from "../fixtures.js";
import { expect, indexPage, test } from "./harness.js";
import { EnvironmentDetailPom, ProductDetailPom } from "./pages/index.js";
import { FleetPom } from "./pages/FleetPom.js";
import type { Locator } from "@playwright/test";

/** The browser archetype's geometry, asserted on the screens reconstructed to
 * it -- fleet, the Environment Overview and the Product view. The rulebook's
 * browser layout is a fixed 230-260px rail beside a fluid minmax(0,1fr) work
 * area, never an equal-width split (docs/ui/console-visual-language.md section
 * 8). These tests assert that shape from the rendered box model, because the
 * grid equation is load-bearing: a regression to a stacked or equal-width
 * layout passes every content test while breaking the thing this work exists to
 * establish. There are no screenshot baselines; the claims are DOM geometry. */

/** A fixed rail is seated to the left of a fluid work column: the rail holds a
 * width in the rulebook's band, sits entirely left of the work area, shares its
 * row, and is much narrower than it. */
async function expectFixedRailBeside(rail: Locator, work: Locator, expectedRailWidth: number) {
  await expect(rail).toBeVisible();
  await expect(work).toBeVisible();
  const railBox = await rail.boundingBox();
  const workBox = await work.boundingBox();
  expect(railBox, "rail has a box").not.toBeNull();
  expect(workBox, "work column has a box").not.toBeNull();
  if (railBox === null || workBox === null) return;

  // Fixed rail width, inside the rulebook's 230-260px category-rail band.
  expect(railBox.width).toBeGreaterThanOrEqual(expectedRailWidth - 4);
  expect(railBox.width).toBeLessThanOrEqual(expectedRailWidth + 4);
  // Beside, not stacked: the rail ends before the work column starts, and the
  // two begin on the same row.
  expect(railBox.x + railBox.width).toBeLessThanOrEqual(workBox.x + 2);
  expect(Math.abs(railBox.y - workBox.y)).toBeLessThan(80);
  // Fluid work area, much wider than the fixed rail -- not an equal-width split.
  expect(workBox.width).toBeGreaterThan(railBox.width * 1.4);
}

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

const RELEASE_WITH_FINDING = {
  product: "app.example:payments",
  version: "2.4.1",
  chart: { repository: "registry.example/payments", digest: "sha256:abc" },
  images: [],
  channels: ["RELEASE"],
  vulnerabilityAssessment: {
    provider: "anchore/grype 0.116.1",
    outcome: "FAIL",
    activeVulnerabilities: 1,
    activeFindings: [{ id: "CVE-2026-0001", cvss: 7.5, severity: "HIGH" }],
  },
};

const PRODUCT_PROMOTIONS = [
  {
    id: "prm_layout",
    product: "app.example:payments",
    version: "2.4.1",
    fromChannel: "RELEASE",
    toChannel: "STAGING",
    status: "PASSED",
  },
];

test.describe("browser archetype geometry", () => {
  test("seats the fleet facet rail as the fixed left rail beside the table", async ({
    page,
    hub,
  }) => {
    hub.fleet([
      installation({ environment: "prod-use1", installationId: "payments" }),
      installation({ environment: "prod-euw1", installationId: "ledger", health: "UNHEALTHY" }),
    ]);
    await page.goto("fleet");
    const fleet = new FleetPom(page);

    await expect(fleet.table).toBeVisible();
    // 240px rail, the value named by --console-rail in fleet-page.css.
    await expectFixedRailBeside(fleet.rail, fleet.results, 240);
  });

  test("lays the Environment Overview out as a fixed summary pane beside the Entities table", async ({
    page,
    hub,
  }) => {
    hub.get("/environments/prod-use1", ENVIRONMENT);
    await page.goto("environments/prod-use1");
    const environment = new EnvironmentDetailPom(page);

    await expect(environment.entityTable).toBeVisible();
    // 260px summary pane -- the top of the rail band; the pane holds worded
    // facts rather than a facet column.
    await expectFixedRailBeside(
      environment.overviewSummary,
      environment.overviewWork,
      260,
    );
  });

  test("heads the Product view with a fixed resource summary beside the work column", async ({
    page,
    hub,
  }) => {
    hub.get("/products/app.example:payments/releases", { releases: [RELEASE_WITH_FINDING] });
    hub.get("/promotions", indexPage("promotions", PRODUCT_PROMOTIONS));
    await page.goto("products/app.example%3Apayments");
    const product = new ProductDetailPom(page);

    await expect(product.releaseTable).toBeVisible();
    await expectFixedRailBeside(product.resourceSummary, product.work, 260);
  });

  test("draws the Product pipeline and assessment panels as Blueprint cards with 4px corners", async ({
    page,
    hub,
  }) => {
    hub.get("/products/app.example:payments/releases", { releases: [RELEASE_WITH_FINDING] });
    hub.get("/promotions", indexPage("promotions", PRODUCT_PROMOTIONS));
    await page.goto("products/app.example%3Apayments");
    const product = new ProductDetailPom(page);

    // The pipeline is a real Blueprint Section (`.bp6-section`), so its corner
    // comes from `--bp-surface-border-radius` (4px), the rulebook's normal-card
    // radius -- not from a squared styled `<section>` div.
    await expect(product.pipelineCard).toBeVisible();
    const pipelineRadius = await product.pipelineCard.evaluate(
      (element) => getComputedStyle(element).borderTopLeftRadius,
    );
    expect(pipelineRadius).toBe("4px");

    // The assessment panel is revealed under its Release row and is the same
    // primitive.
    await product.assessmentToggle("2.4.1").click();
    await expect(product.assessmentCard).toBeVisible();
    const assessmentRadius = await product.assessmentCard.evaluate(
      (element) => getComputedStyle(element).borderTopLeftRadius,
    );
    expect(assessmentRadius).toBe("4px");
  });
});

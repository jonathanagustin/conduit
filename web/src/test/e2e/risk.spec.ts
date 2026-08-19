import { expect, test } from "./harness.js";
import { RiskPom } from "./pages/index.js";

/** Risk Management -- the recalls that withdraw Release ranges from selection.
 * GET /v1/catalog-retirements returns the whole set as one named array, so the
 * index filters locally and reveals a selected recall's strategy from the row
 * already loaded. Fixtures follow the catalog.Recall Go struct: source is
 * SECURITY or MANUAL, and the exclusive ceiling and singleVersion flag decide
 * the affected range. */

const SECURITY_RECALL = {
  id: "rcl_security",
  product: "app.example:payments",
  startingVersion: "2.4.0",
  endingVersionExclusive: "2.4.3",
  strategy: { type: "ROLL_FORWARD", minimumVersion: "2.4.3" },
  reason: "CVE-2026-0001 in the bundled TLS library",
  source: "SECURITY",
  assessmentRunId: "run-77",
};

const MANUAL_RECALL = {
  id: "rcl_manual",
  product: "app.example:search",
  startingVersion: "8.1.0",
  singleVersion: true,
  strategy: { type: "FREEZE" },
  reason: "data-loss regression reported by a tenant",
  source: "MANUAL",
};

test.describe("Risk Management", () => {
  test("lists recalls with their affected range and source", async ({ page, hub }) => {
    hub.get("/catalog-retirements", { retirements: [SECURITY_RECALL, MANUAL_RECALL] });
    await page.goto("risk");
    const risk = new RiskPom(page);

    const security = risk.recall("rcl_security");
    await expect(security).toContainText("app.example:payments");
    await expect(security).toContainText("2.4.0 up to (excluding) 2.4.3");
    await expect(security).toContainText("SECURITY");
    // A single-version recall reads as one version, not an open range.
    await expect(risk.recall("rcl_manual")).toContainText("8.1.0");
    await expect(risk.recall("rcl_manual")).toContainText("MANUAL");
  });

  test("reveals a recall's strategy from the loaded row without a second fetch", async ({ page, hub }) => {
    hub.get("/catalog-retirements", { retirements: [SECURITY_RECALL] });
    await page.goto("risk");
    const risk = new RiskPom(page);

    await risk.recall("rcl_security").click();
    await expect(risk.detail).toContainText("CVE-2026-0001");
    await expect(risk.detail).toContainText("Roll forward to at least 2.4.3");
    await expect(risk.detail).toContainText("run-77");
    // No per-recall endpoint exists, so only the list is ever requested.
    expect(hub.requests().filter((request) => request.startsWith("GET /v1/catalog-retirements")).length).toBe(1);
  });

  test("frames the inline detail as a real Blueprint Section with a 4px corner", async ({ page, hub }) => {
    // The detail panel is a Blueprint Section now, not a bare <section>, so its
    // 4px corner comes from --bp-surface-border-radius rather than a radius rule
    // on raw HTML -- the same primitive the products detail panels use. It stays
    // inline and nonmodal, so the row context is preserved.
    hub.get("/catalog-retirements", { retirements: [SECURITY_RECALL] });
    await page.goto("risk");
    const risk = new RiskPom(page);

    await risk.recall("rcl_security").click();
    await expect(risk.detail).toHaveClass(/bp6-section/);
    await expect
      .poll(() => risk.detail.evaluate((element) => getComputedStyle(element).borderTopLeftRadius))
      .toBe("4px");
  });

  test("distinguishes an active recall from a reverted one", async ({ page, hub }) => {
    hub.get("/catalog-retirements", {
      retirements: [SECURITY_RECALL, { ...MANUAL_RECALL, reverted: true }],
    });
    await page.goto("risk");
    const risk = new RiskPom(page);

    await expect(risk.recall("rcl_security")).toContainText("active");
    await expect(risk.recall("rcl_manual")).toContainText("reverted");
  });

  test("filters the loaded set locally and explains a match of nothing", async ({ page, hub }) => {
    hub.get("/catalog-retirements", { retirements: [SECURITY_RECALL, MANUAL_RECALL] });
    await page.goto("risk");
    const risk = new RiskPom(page);

    await risk.search.fill("search");
    await risk.search.press("Enter");
    await expect(page).toHaveURL(/search=search/);
    await expect(risk.recall("rcl_manual")).toBeVisible();
    await expect(risk.recall("rcl_security")).toHaveCount(0);
    expect(hub.requests().filter((request) => request.startsWith("GET /v1/catalog-retirements")).length).toBe(1);

    await risk.search.fill("nothing-here");
    await risk.search.press("Enter");
    await expect(page.getByText('No recall matches "nothing-here".')).toBeVisible();
  });

  test("says the catalog has no recalls rather than showing an empty table", async ({ page, hub }) => {
    hub.get("/catalog-retirements", { retirements: [] });
    await page.goto("risk");
    await expect(page.getByText("No Releases have been recalled.")).toBeVisible();
  });

  test("surfaces a Hub failure with a retry", async ({ page, hub }) => {
    hub.fails("/catalog-retirements", 500, "recall store unavailable");
    await page.goto("risk");

    await expect(page.getByText(/could not be read/i)).toBeVisible();
    hub.get("/catalog-retirements", { retirements: [SECURITY_RECALL] });
    await page.getByRole("button", { name: "Try again" }).click();
    await expect(new RiskPom(page).recall("rcl_security")).toBeVisible();
  });
});

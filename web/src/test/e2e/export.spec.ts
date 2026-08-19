import { expect, test } from "./harness.js";
import { ExportPom } from "./pages/index.js";

/** Export -- the pipelines that build offline Bundles. GET /v1/export-pipelines
 * returns the whole set as one named array, so the index filters locally and
 * reveals a selected pipeline's selections and gates from the row already
 * loaded. Fixtures follow the controlplane.ExportPipeline Go struct: strategy
 * is SNAPSHOT or INCREMENTAL, and the require* flags are the security gates. */

// buildStrategy is SNAPSHOT or INCREMENTAL and status is ACTIVE/ARCHIVED/DELETED
// -- the Hub's own vocabulary (controlplane.bundles.go). GET /v1/export-pipelines
// returns archived and deleted pipelines too, so the status is load-bearing.
const RELEASE_PIPELINE = {
  id: "exp_release",
  displayName: "Release channel export",
  buildStrategy: "INCREMENTAL",
  status: "ACTIVE",
  checkpointFrequency: 10,
  selections: [
    { type: "PRODUCT", ids: ["app.example:payments"], releaseChannels: ["RELEASE"] },
    { type: "ENVIRONMENT", ids: ["prod-use1"] },
  ],
  resourceTypes: ["RELEASE", "ENVIRONMENT"],
  requireSecurityPass: true,
  requireMalwarePass: true,
  includeArtifacts: true,
};

const SNAPSHOT_PIPELINE = {
  id: "exp_snapshot",
  displayName: "Full snapshot",
  buildStrategy: "SNAPSHOT",
  status: "ARCHIVED",
  selections: [{ type: "TEAM", ids: ["platform"] }],
};

test.describe("Export", () => {
  test("lists export pipelines with their strategy and guarantees", async ({ page, hub }) => {
    hub.get("/export-pipelines", { pipelines: [RELEASE_PIPELINE, SNAPSHOT_PIPELINE] });
    await page.goto("export");
    const exports = new ExportPom(page);

    const release = exports.pipeline("exp_release");
    await expect(release).toContainText("Release channel export");
    await expect(release).toContainText("INCREMENTAL");
    await expect(release).toContainText("2"); // two selections
    await expect(release).toContainText("security pass");
    await expect(release).toContainText("malware pass");
    // A pipeline enforcing no gates says so rather than implying trust.
    await expect(exports.pipeline("exp_snapshot")).toContainText("—");
  });

  test("distinguishes an active pipeline from an archived or deleted one", async ({ page, hub }) => {
    // GET /v1/export-pipelines returns archived and deleted pipelines too, so a
    // retired pipeline must not read as one that still exports.
    hub.get("/export-pipelines", {
      pipelines: [
        RELEASE_PIPELINE,
        SNAPSHOT_PIPELINE,
        { id: "exp_gone", displayName: "Retired", buildStrategy: "SNAPSHOT", status: "DELETED", selections: [] },
      ],
    });
    await page.goto("export");
    const exports = new ExportPom(page);

    await expect(exports.pipeline("exp_release")).toContainText("active");
    await expect(exports.pipeline("exp_snapshot")).toContainText("archived");
    await expect(exports.pipeline("exp_gone")).toContainText("deleted");
  });

  test("reveals a pipeline's selections and checkpointing from the loaded row", async ({ page, hub }) => {
    hub.get("/export-pipelines", { pipelines: [RELEASE_PIPELINE] });
    await page.goto("export");
    const exports = new ExportPom(page);

    await exports.pipeline("exp_release").click();
    await expect(exports.detail).toContainText("app.example:payments");
    await expect(exports.detail).toContainText("channels: RELEASE");
    await expect(exports.detail).toContainText("checkpoint every 10");
    expect(hub.requests().filter((request) => request.startsWith("GET /v1/export-pipelines")).length).toBe(1);
  });

  test("frames the inline detail as a real Blueprint Section with a 4px corner", async ({ page, hub }) => {
    // The detail panel is a Blueprint Section now, not a bare <section>, so its
    // 4px corner comes from --bp-surface-border-radius rather than a radius rule
    // on raw HTML -- the same primitive the products detail panels use. It stays
    // inline and nonmodal, so the row context is preserved.
    hub.get("/export-pipelines", { pipelines: [RELEASE_PIPELINE] });
    await page.goto("export");
    const exports = new ExportPom(page);

    await exports.pipeline("exp_release").click();
    await expect(exports.detail).toHaveClass(/bp6-section/);
    await expect
      .poll(() => exports.detail.evaluate((element) => getComputedStyle(element).borderTopLeftRadius))
      .toBe("4px");
  });

  test("filters the loaded set locally and explains a match of nothing", async ({ page, hub }) => {
    hub.get("/export-pipelines", { pipelines: [RELEASE_PIPELINE, SNAPSHOT_PIPELINE] });
    await page.goto("export");
    const exports = new ExportPom(page);

    await exports.search.fill("TEAM");
    await exports.search.press("Enter");
    await expect(page).toHaveURL(/search=TEAM/);
    await expect(exports.pipeline("exp_snapshot")).toBeVisible();
    await expect(exports.pipeline("exp_release")).toHaveCount(0);

    await exports.search.fill("nothing-here");
    await exports.search.press("Enter");
    await expect(page.getByText('No pipeline matches "nothing-here".')).toBeVisible();
  });

  test("says nothing is configured rather than showing an empty table", async ({ page, hub }) => {
    hub.get("/export-pipelines", { pipelines: [] });
    await page.goto("export");
    await expect(page.getByText("No export pipelines are configured.")).toBeVisible();
  });

  test("surfaces a Hub failure with a retry", async ({ page, hub }) => {
    hub.fails("/export-pipelines", 500, "export store unavailable");
    await page.goto("export");

    await expect(page.getByText(/could not be read/i)).toBeVisible();
    hub.get("/export-pipelines", { pipelines: [SNAPSHOT_PIPELINE] });
    await page.getByRole("button", { name: "Try again" }).click();
    await expect(new ExportPom(page).pipeline("exp_snapshot")).toBeVisible();
  });
});

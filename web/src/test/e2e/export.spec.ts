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
  requiredLabels: [{ name: "tier", resourceTypes: ["RELEASE"], values: ["gold", "silver"] }],
  excludedLabels: [{ name: "deprecated", resourceTypes: [] }],
  targetHubVersions: {
    "hub-edge": {
      targetHub: "hub-edge",
      recordedAt: "2026-08-01T00:00:00Z",
      entities: [
        { environment: "prod-use1", installationId: "inst_pay", product: "app.example:payments", version: "1.4.2" },
      ],
    },
  },
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

  test("shows the label filters a pipeline requires and excludes", async ({ page, hub }) => {
    // The label filters are enforced server-side but were invisible here; a
    // pipeline that silently drops resources missing a label should say so.
    hub.get("/export-pipelines", { pipelines: [RELEASE_PIPELINE] });
    await page.goto("export");
    const exports = new ExportPom(page);

    await exports.pipeline("exp_release").click();
    const filters = exports.detail.getByTestId("pipeline-label-filters");
    await expect(filters).toContainText("Required");
    await expect(filters).toContainText("tier");
    await expect(filters).toContainText("gold | silver");
    await expect(filters).toContainText("Excluded");
    await expect(filters).toContainText("deprecated");
    // A requirement with no values gates on the label's presence.
    await expect(filters).toContainText("any value");
  });

  test("shows the Entity versions each Target Hub last reported", async ({ page, hub }) => {
    // A disconnected source Hub builds an incremental Bundle against what the
    // target last ran; the panel now shows that baseline instead of hiding it.
    hub.get("/export-pipelines", { pipelines: [RELEASE_PIPELINE] });
    await page.goto("export");
    const exports = new ExportPom(page);

    await exports.pipeline("exp_release").click();
    const targetHubs = exports.detail.getByTestId("pipeline-target-hub-versions");
    await expect(targetHubs).toContainText("hub-edge");
    await expect(targetHubs).toContainText("prod-use1");
    await expect(targetHubs).toContainText("app.example:payments");
    await expect(targetHubs).toContainText("1.4.2");
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
  test("reports what a build would omit, only when asked", async ({ page, hub }) => {
    hub.get("/export-pipelines", { pipelines: [RELEASE_PIPELINE] });
    hub.post("/export-pipelines/exp_release/preview", {
      pipelineId: "exp_release",
      pipelineRevision: 7,
      generatedAt: "2026-08-07T12:00:00Z",
      added: ["rel_payments_1.4.2", "rel_search_8.1.0"],
      updated: ["env_prod-use1"],
      removed: [],
      securityOutcome: "PASS",
      // Both closed-set kinds the Hub can emit (artifacts/domain/envelope.go).
      advisories: [
        {
          kind: "NO_PRODUCT_RELEASES",
          message: "no unrecalled Release on the requested Release Channels",
          subjects: ["app.example:ledger", "app.example:reporting"],
        },
        {
          kind: "UNIMPLEMENTED_STRATEGY",
          message: "MINIMAL falls through to the latest-per-channel path",
        },
      ],
    });
    await page.goto("export");
    await new ExportPom(page).pipeline("exp_release").click();

    // The POST is not issued because a panel opened: nothing has run yet.
    await expect(page.getByTestId("pipeline-advisories")).toBeVisible();
    await expect(page.getByTestId("pipeline-preview-result")).toHaveCount(0);

    await page.getByTestId("run-pipeline-preview").click();

    const result = page.getByTestId("pipeline-preview-result");
    await expect(result).toContainText("2 additions, 1 update, 0 removals");
    await expect(result).toContainText("PASS");
    const advisories = page.getByTestId("pipeline-advisory-list");
    await expect(advisories).toContainText("NO_PRODUCT_RELEASES");
    // The subjects are the whole point: which Products the Bundle will not hold.
    await expect(advisories).toContainText("app.example:ledger");
    await expect(advisories).toContainText("UNIMPLEMENTED_STRATEGY");
  });

  test("says a clean build reports none rather than showing nothing", async ({ page, hub }) => {
    hub.get("/export-pipelines", { pipelines: [RELEASE_PIPELINE] });
    hub.post("/export-pipelines/exp_release/preview", {
      pipelineId: "exp_release",
      pipelineRevision: 7,
      generatedAt: "2026-08-07T12:00:00Z",
      added: [],
      updated: [],
      removed: [],
      securityOutcome: "PASS",
    });
    await page.goto("export");
    await new ExportPom(page).pipeline("exp_release").click();
    await page.getByTestId("run-pipeline-preview").click();

    await expect(page.getByTestId("pipeline-no-advisories")).toContainText(
      "This build would report no advisories.",
    );
  });

  test("explains a refused preview instead of reporting a failure", async ({ page, hub }) => {
    hub.get("/export-pipelines", { pipelines: [RELEASE_PIPELINE] });
    // 409 from PreviewExportPipeline when the schedule disables the preview.
    hub.postFails(
      "/export-pipelines/exp_release/preview",
      409,
      "conflict: pre-build preview is disabled",
    );
    await page.goto("export");
    await new ExportPom(page).pipeline("exp_release").click();
    await page.getByTestId("run-pipeline-preview").click();

    await expect(page.getByTestId("pipeline-preview-refused")).toContainText(
      "disables the pre-build preview",
    );
    // A documented refusal must not read as a fault.
    await expect(page.getByTestId("pipeline-preview-error")).toHaveCount(0);
  });

  test("a preview that genuinely failed is reported as a failure", async ({ page, hub }) => {
    hub.get("/export-pipelines", { pipelines: [RELEASE_PIPELINE] });
    hub.postFails("/export-pipelines/exp_release/preview", 500, "catalog unavailable");
    await page.goto("export");
    await new ExportPom(page).pipeline("exp_release").click();
    await page.getByTestId("run-pipeline-preview").click();

    await expect(page.getByTestId("pipeline-preview-error")).toContainText("catalog unavailable");
    await expect(page.getByTestId("pipeline-preview-refused")).toHaveCount(0);
  });
});

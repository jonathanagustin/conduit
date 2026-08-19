import { expect, test } from "./harness.js";
import { ImportPom } from "./pages/index.js";

/** Import -- the record of every offline Bundle imported into an Environment.
 * GET /v1/bundle-imports returns the whole set as one named array, so the index
 * filters locally and reveals a selected import's per-stage tasks from the row
 * already loaded. Fixtures follow the controlplane.BundleImport Go struct: each
 * import carries a status and its bounded task stages. */

// Statuses and stages are the Hub's own vocabulary (controlplane.bundles.go):
// an import is PENDING_APPROVAL, then ADMITTED or ERROR; a task is PENDING,
// SUCCEEDED, FAILED, or SKIPPED; stages are VERIFY_BUNDLE, STAGE_ARTIFACTS,
// IMPORT_CATALOG, APPLY_ENVIRONMENT_CHANGES. Fixtures use these, not plausible
// invented names, so a passing test means the screen renders what the Hub sends.
const ADMITTED_IMPORT = {
  id: "imp_ok",
  bundleId: "bnd_2026",
  manifestDigest: "sha256:cafe",
  environment: "prod-use1",
  changeRequestId: "chr_42",
  requestedBy: "operator-one",
  status: "ADMITTED",
  tasks: [
    { sequence: 1, stage: "VERIFY_BUNDLE", status: "SUCCEEDED" },
    { sequence: 2, stage: "IMPORT_CATALOG", status: "SUCCEEDED", resources: ["app.example:payments"] },
  ],
  requestedAt: "2026-08-07T12:00:00Z",
  importedAt: "2026-08-07T12:05:00Z",
  revision: 3,
};

const ERRORED_IMPORT = {
  id: "imp_bad",
  bundleId: "bnd_2027",
  manifestDigest: "sha256:beef",
  environment: "stage-use1",
  requestedBy: "operator-two",
  status: "ERROR",
  error: "manifest signer is not trusted",
  tasks: [
    { sequence: 1, stage: "VERIFY_BUNDLE", status: "FAILED", failure: "unknown signer key" },
    { sequence: 2, stage: "IMPORT_CATALOG", status: "SKIPPED" },
  ],
  requestedAt: "2026-08-07T13:00:00Z",
  revision: 1,
};

test.describe("Import", () => {
  test("lists Bundle imports with their Environment, status, and requester", async ({ page, hub }) => {
    hub.get("/bundle-imports", { imports: [ADMITTED_IMPORT, ERRORED_IMPORT] });
    await page.goto("import");
    const imports = new ImportPom(page);

    const ok = imports.record("imp_ok");
    await expect(ok).toContainText("bnd_2026");
    await expect(ok).toContainText("prod-use1");
    await expect(ok).toContainText("ADMITTED");
    await expect(ok).toContainText("operator-one");
    await expect(imports.record("imp_bad")).toContainText("ERROR");
  });

  test("reveals an import's per-stage tasks from the loaded row", async ({ page, hub }) => {
    hub.get("/bundle-imports", { imports: [ADMITTED_IMPORT] });
    await page.goto("import");
    const imports = new ImportPom(page);

    await imports.record("imp_ok").click();
    await expect(imports.detail).toContainText("sha256:cafe");
    await expect(imports.detail).toContainText("VERIFY_BUNDLE");
    await expect(imports.detail).toContainText("IMPORT_CATALOG");
    await expect(imports.detail).toContainText("app.example:payments");
    // No per-import endpoint exists; only the list is ever requested.
    expect(hub.requests().filter((request) => request.startsWith("GET /v1/bundle-imports")).length).toBe(1);
  });

  test("frames the inline detail as a real Blueprint Section with a 4px corner", async ({ page, hub }) => {
    // The detail panel is a Blueprint Section now, not a bare <section>, so its
    // 4px corner comes from --bp-surface-border-radius rather than a radius rule
    // on raw HTML -- the same primitive the products detail panels use. It stays
    // inline and nonmodal, so the row context is preserved.
    hub.get("/bundle-imports", { imports: [ADMITTED_IMPORT] });
    await page.goto("import");
    const imports = new ImportPom(page);

    await imports.record("imp_ok").click();
    await expect(imports.detail).toHaveClass(/bp6-section/);
    await expect
      .poll(() => imports.detail.evaluate((element) => getComputedStyle(element).borderTopLeftRadius))
      .toBe("4px");
  });

  test("shows why a failed import failed, at the import and the task", async ({ page, hub }) => {
    hub.get("/bundle-imports", { imports: [ERRORED_IMPORT] });
    await page.goto("import");
    const imports = new ImportPom(page);

    await imports.record("imp_bad").click();
    await expect(imports.detail).toContainText("manifest signer is not trusted");
    await expect(imports.detail).toContainText("unknown signer key");
  });

  test("filters the loaded set locally and explains a match of nothing", async ({ page, hub }) => {
    hub.get("/bundle-imports", { imports: [ADMITTED_IMPORT, ERRORED_IMPORT] });
    await page.goto("import");
    const imports = new ImportPom(page);

    await imports.search.fill("stage-use1");
    await imports.search.press("Enter");
    await expect(page).toHaveURL(/search=stage-use1/);
    await expect(imports.record("imp_bad")).toBeVisible();
    await expect(imports.record("imp_ok")).toHaveCount(0);

    await imports.search.fill("nothing-here");
    await imports.search.press("Enter");
    await expect(page.getByText('No import matches "nothing-here".')).toBeVisible();
  });

  test("says nothing has been imported rather than showing an empty table", async ({ page, hub }) => {
    hub.get("/bundle-imports", { imports: [] });
    await page.goto("import");
    await expect(page.getByText("No Bundles have been imported.")).toBeVisible();
  });

  test("surfaces a Hub failure with a retry", async ({ page, hub }) => {
    hub.fails("/bundle-imports", 500, "import store unavailable");
    await page.goto("import");

    await expect(page.getByText(/could not be read/i)).toBeVisible();
    hub.get("/bundle-imports", { imports: [ADMITTED_IMPORT] });
    await page.getByRole("button", { name: "Try again" }).click();
    await expect(new ImportPom(page).record("imp_ok")).toBeVisible();
  });
});

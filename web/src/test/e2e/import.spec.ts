import { expect, test } from "./harness.js";
import { ImportPom } from "./pages/index.js";

/** Import -- the record of every offline Bundle imported into an Environment.
 * GET /v1/bundle-imports returns the whole set as one named array, so the index
 * filters locally and reveals a selected import's per-stage tasks from the row
 * already loaded. Fixtures follow the controlplane.BundleImport Go struct: each
 * import carries a status and its bounded task stages. */

const COMPLETED_IMPORT = {
  id: "imp_ok",
  bundleId: "bnd_2026",
  manifestDigest: "sha256:cafe",
  environment: "prod-use1",
  changeRequestId: "chr_42",
  requestedBy: "operator-one",
  status: "IMPORTED",
  tasks: [
    { sequence: 1, stage: "VERIFY_SIGNATURE", status: "SUCCEEDED" },
    { sequence: 2, stage: "APPLY_RESOURCES", status: "SUCCEEDED", resources: ["app.example:payments"] },
  ],
  requestedAt: "2026-08-07T12:00:00Z",
  importedAt: "2026-08-07T12:05:00Z",
  revision: 3,
};

const FAILED_IMPORT = {
  id: "imp_bad",
  bundleId: "bnd_2027",
  manifestDigest: "sha256:beef",
  environment: "stage-use1",
  requestedBy: "operator-two",
  status: "FAILED",
  error: "manifest signer is not trusted",
  tasks: [
    { sequence: 1, stage: "VERIFY_SIGNATURE", status: "FAILED", failure: "unknown signer key" },
  ],
  requestedAt: "2026-08-07T13:00:00Z",
  revision: 1,
};

test.describe("Import", () => {
  test("lists Bundle imports with their Environment, status, and requester", async ({ page, hub }) => {
    hub.get("/bundle-imports", { imports: [COMPLETED_IMPORT, FAILED_IMPORT] });
    await page.goto("import");
    const imports = new ImportPom(page);

    const ok = imports.record("imp_ok");
    await expect(ok).toContainText("bnd_2026");
    await expect(ok).toContainText("prod-use1");
    await expect(ok).toContainText("IMPORTED");
    await expect(ok).toContainText("operator-one");
    await expect(imports.record("imp_bad")).toContainText("FAILED");
  });

  test("reveals an import's per-stage tasks from the loaded row", async ({ page, hub }) => {
    hub.get("/bundle-imports", { imports: [COMPLETED_IMPORT] });
    await page.goto("import");
    const imports = new ImportPom(page);

    await imports.record("imp_ok").click();
    await expect(imports.detail).toContainText("sha256:cafe");
    await expect(imports.detail).toContainText("VERIFY_SIGNATURE");
    await expect(imports.detail).toContainText("APPLY_RESOURCES");
    await expect(imports.detail).toContainText("app.example:payments");
    // No per-import endpoint exists; only the list is ever requested.
    expect(hub.requests().filter((request) => request.startsWith("GET /v1/bundle-imports")).length).toBe(1);
  });

  test("shows why a failed import failed, at the import and the task", async ({ page, hub }) => {
    hub.get("/bundle-imports", { imports: [FAILED_IMPORT] });
    await page.goto("import");
    const imports = new ImportPom(page);

    await imports.record("imp_bad").click();
    await expect(imports.detail).toContainText("manifest signer is not trusted");
    await expect(imports.detail).toContainText("unknown signer key");
  });

  test("filters the loaded set locally and explains a match of nothing", async ({ page, hub }) => {
    hub.get("/bundle-imports", { imports: [COMPLETED_IMPORT, FAILED_IMPORT] });
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
    hub.get("/bundle-imports", { imports: [COMPLETED_IMPORT] });
    await page.getByRole("button", { name: "Try again" }).click();
    await expect(new ImportPom(page).record("imp_ok")).toBeVisible();
  });
});

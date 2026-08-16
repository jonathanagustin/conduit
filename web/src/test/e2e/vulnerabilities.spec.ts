import { expect, test } from "./harness.js";
import { ProductDetailPom } from "./pages/index.js";

/** Per-CVE vulnerability detail on a Product's Releases.
 *
 * GET /v1/products/{product}/releases serializes catalog.Release whole, so the
 * assessment's activeFindings and suppressedFindingDetails arrive with the
 * table and the detail is revealed without a second request. Fixtures follow
 * the Go json tags: a finding is {id, cvss, severity}, and a suppressed finding
 * is {findingId, vulnerabilityId, artifactDigest, suppression{...}}.
 *
 * The ordering assertions matter more than they look. The Hub's findingScore
 * prefers cvss whenever it is above zero and only falls back to the severity
 * string, so a fixture whose severity disagrees with its cvss is the case that
 * proves the console ranks the way the Hub scores. */

const RELEASE_WITH_FINDINGS = {
  product: "app.example:payments",
  version: "2.4.0",
  chart: { repository: "charts.example/payments", digest: "sha256:aaa" },
  images: [{ repository: "images.example/payments", digest: "sha256:bbb" }],
  channels: ["STAGING"],
  vulnerabilityAssessment: {
    provider: "anchore/grype 0.116.1",
    runId: "sha256:run",
    status: "COMPLETE",
    outcome: "FAIL",
    artifactDigests: ["sha256:bbb"],
    activeVulnerabilities: 3,
    gracePeriodFindings: 1,
    assessedAt: "2026-08-16T09:00:00Z",
    activeFindings: [
      // Deliberately worst-last, and with severity disagreeing with cvss.
      { id: "CVE-2026-0003", cvss: 4.2, severity: "CRITICAL" },
      { id: "CVE-2026-0001", cvss: 9.8, severity: "LOW" },
      { id: "CVE-2026-0002", severity: "HIGH" },
    ],
    suppressedFindings: 1,
    suppressedFindingDetails: [
      {
        findingId: "f-1",
        vulnerabilityId: "CVE-2025-9999",
        artifactDigest: "sha256:bbb",
        suppression: {
          providerId: "sup-1",
          scope: "IMAGE",
          scopeValue: "images.example/payments",
          category: "VENDOR_DEPENDENCY",
          rationale: "fixed upstream in the next vendor release",
          requestedBy: "alice",
          approvedBy: "bob",
          changeRequestId: "crq_abc123",
          validUntil: "2027-01-01T00:00:00Z",
        },
      },
    ],
  },
};

const RELEASE_CLEAN = {
  product: "app.example:payments",
  version: "2.3.0",
  chart: { repository: "charts.example/payments", digest: "sha256:ccc" },
  images: [{ repository: "images.example/payments", digest: "sha256:ddd" }],
  vulnerabilityAssessment: { provider: "anchore/grype 0.116.1", status: "COMPLETE", outcome: "PASS" },
};

const releases = { releases: [RELEASE_WITH_FINDINGS, RELEASE_CLEAN] };

test.describe("Release vulnerability findings", () => {
  test("ranks findings by CVSS rather than by the reported severity", async ({ page, hub }) => {
    hub.get("/products/app.example:payments/releases", releases);
    hub.get("/promotions", { promotions: [], total: 0 });
    await page.goto("products/app.example%3Apayments");
    const product = new ProductDetailPom(page);

    await product.assessmentToggle("2.4.0").click();
    await expect(product.assessmentDetail).toBeVisible();

    // CVE-0001 is cvss 9.8 but severity LOW; CVE-0003 is cvss 4.2 but severity
    // CRITICAL. Ranking by severity would invert these two.
    const rows = product.activeFindings.locator("tbody tr");
    await expect(rows.nth(0)).toContainText("CVE-2026-0001");
    await expect(rows.nth(1)).toContainText("CVE-2026-0003");
    // No CVSS sorts last -- absent is not harmless.
    await expect(rows.nth(2)).toContainText("CVE-2026-0002");
  });

  test("shows an absent CVSS as absent rather than as zero", async ({ page, hub }) => {
    hub.get("/products/app.example:payments/releases", releases);
    hub.get("/promotions", { promotions: [], total: 0 });
    await page.goto("products/app.example%3Apayments");
    const product = new ProductDetailPom(page);

    await product.assessmentToggle("2.4.0").click();
    await expect(product.finding("CVE-2026-0002")).toContainText("—");
    await expect(product.finding("CVE-2026-0002")).not.toContainText("0.0");
    // The reported severity is still shown, as the provider's own text.
    await expect(product.finding("CVE-2026-0002")).toContainText("HIGH");
  });

  test("reveals findings without a second request", async ({ page, hub }) => {
    hub.get("/products/app.example:payments/releases", releases);
    hub.get("/promotions", { promotions: [], total: 0 });
    await page.goto("products/app.example%3Apayments");
    const product = new ProductDetailPom(page);

    // Wait for the first load to land before counting. page.goto resolves on
    // the document load event, but React Query issues its fetch after that, so
    // a listener attached any earlier counts the initial request and the test
    // fails for a reason that has nothing to do with revealing a row.
    await expect(product.release("2.4.0")).toBeVisible();

    // Listen rather than route: installing a second handler for the same path
    // would shadow the harness's own mock, so the test would be measuring its
    // own interception instead of the page's behaviour.
    let refetched = 0;
    page.on("request", (request) => {
      if (request.url().includes("/releases")) {
        refetched += 1;
      }
    });
    await product.assessmentToggle("2.4.0").click();
    await expect(product.assessmentDetail).toBeVisible();
    expect(refetched).toBe(0);
  });

  test("shows the approval trail behind a suppressed finding", async ({ page, hub }) => {
    hub.get("/products/app.example:payments/releases", releases);
    hub.get("/promotions", { promotions: [], total: 0 });
    await page.goto("products/app.example%3Apayments");
    const product = new ProductDetailPom(page);

    await product.assessmentToggle("2.4.0").click();
    // A suppressed count is not evidence on its own; the approver, the Change
    // Request and the expiry are what make the exclusion auditable.
    const suppressed = product.suppressed("CVE-2025-9999");
    await expect(suppressed).toContainText("VENDOR_DEPENDENCY");
    await expect(suppressed).toContainText("bob");
    await expect(suppressed).toContainText("crq_abc123");
  });

  test("names the scanner and the scan time", async ({ page, hub }) => {
    hub.get("/products/app.example:payments/releases", releases);
    hub.get("/promotions", { promotions: [], total: 0 });
    await page.goto("products/app.example%3Apayments");
    const product = new ProductDetailPom(page);

    await product.assessmentToggle("2.4.0").click();
    await expect(product.assessmentDetail).toContainText("anchore/grype 0.116.1");
    // A finding inside a grace period is neither active nor suppressed, so the
    // count is stated rather than folded into either table.
    await expect(product.assessmentDetail).toContainText("1 within a grace period");
  });

  test("a Release with no findings offers nothing to reveal", async ({ page, hub }) => {
    hub.get("/products/app.example:payments/releases", releases);
    hub.get("/promotions", { promotions: [], total: 0 });
    await page.goto("products/app.example%3Apayments");
    const product = new ProductDetailPom(page);

    // 2.3.0 passed with no findings, so its assessment stays a plain tag: a
    // control that reveals an empty panel would imply evidence that is absent.
    await expect(product.assessmentToggle("2.3.0")).toHaveCount(0);
    await expect(product.release("2.3.0")).toContainText("PASS");
  });
});

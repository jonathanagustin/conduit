import { test, type Page, type Route } from "@playwright/test";

const BOOTSTRAP_MARKER = "<!--conduit-console-bootstrap-->";
const BOOTSTRAP = {
  schemaVersion: "conduit.console.bootstrap/v1",
  tenant: { id: "conduit", displayName: "Conduit" },
  brand: {
    title: "Conduit",
    subtitle: "Control plane",
    accent: "#2d72d2",
    icon: "dashboard",
  },
  provenance: {
    claimClass: "conduit-extension",
    evidenceRef: "conduit://console/default",
  },
};

function escapeAttribute(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&#34;")
    .replaceAll("'", "&#39;");
}

export async function hydrate(page: Page): Promise<void> {
  const meta = `<meta name="x-conduit-console-bootstrap" content="${escapeAttribute(JSON.stringify(BOOTSTRAP))}" />`;
  await page.route("**/console/**", async (route: Route) => {
    if (route.request().resourceType() !== "document") {
      await route.fallback();
      return;
    }
    const response = await route.fetch();
    await route.fulfill({
      response,
      body: (await response.text()).replace(BOOTSTRAP_MARKER, meta),
    });
  });
}

// Register the same read stub explicitly in every spec, without import side effects.
export function installPackagingReadStub() {
  test.beforeEach(async ({ page }) => {
    await page.route(
      "**/conduit.fleet.v1.FleetReadService/ListFleetInstallations",
      (route) =>
        route.fulfill({
          json: {
            generatedAt: "2026-11-20T01:00:00Z",
            maxResultAgeSeconds: "0",
            installations: [],
            summary: { total: "0" },
            facets: {},
            pageSize: 0,
            limit: route.request().postDataJSON().limit || 100,
          },
        }),
    );

    await page.route(
      "**/conduit.packaging.v1.PackagingRunService/ListPackagingRuns",
      (route) =>
        route.fulfill({
          json: {
            configured: true,
            items: [],
            total: "0",
            pageSize: 0,
            limit: 20,
          },
        }),
    );
  });
}

const fleetProcedure =
  "/conduit.fleet.v1.FleetReadService/ListFleetInstallations";
/** True only for the native read RPC, never another POST. */
export function isFleetReadFixture(route: Route): boolean {
  return (
    route.request().method() === "POST" &&
    new URL(route.request().url()).pathname === fleetProcedure
  );
}
/** Interpret this test fixture's native Fleet query without sending a REST request. */
export function fleetFixtureURL(route: Route): URL {
  const url = new URL(route.request().url());
  if (!isFleetReadFixture(route)) return url;
  url.pathname = "/v1/fleet/installations";
  for (const [key, value] of Object.entries(
    route.request().postDataJSON() as Record<string, unknown>,
  ))
    url.searchParams.set(key, String(value));
  return url;
}
/** Project explicitly declared fixture rows onto the shipped read contract. */
export async function fulfillFleetFixture(
  route: Route,
  options: Parameters<Route["fulfill"]>[0],
): Promise<void> {
  if (!isFleetReadFixture(route)) {
    await route.fulfill(options);
    return;
  }
  const status = options?.status ?? 200;
  if (status !== 200) {
    const code =
      (
        {
          400: "invalid_argument",
          401: "unauthenticated",
          403: "permission_denied",
          404: "not_found",
          422: "invalid_argument",
        } as Record<number, string>
      )[status] || "unavailable";
    await route.fulfill({
      status: status === 422 ? 400 : status,
      json: { code, message: "Fleet fixture refused" },
    });
    return;
  }
  const body = (options?.json || {}) as Record<string, unknown>;
  const installations = Array.isArray(body.installations)
    ? body.installations
    : [];
  const query = route.request().postDataJSON() as Record<string, unknown>;
  await route.fulfill({
    json: {
      generatedAt: body.generatedAt || "2026-11-20T01:00:00Z",
      maxResultAgeSeconds: body.maxResultAgeSeconds ?? "0",
      installations,
      summary: body.summary || { total: String(installations.length) },
      facets: body.facets || {},
      pageSize: body.pageSize ?? installations.length,
      limit: body.limit ?? query.limit ?? 100,
      nextCursor: body.nextCursor || "",
      previousCursor: body.previousCursor || "",
      hasPrevious: body.hasPrevious || false,
    },
  });
}

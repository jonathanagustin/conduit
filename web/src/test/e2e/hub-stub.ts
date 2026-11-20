import type { Page, Request, Route } from "@playwright/test";

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

/** The application list the Hub's session handler returns, all enabled. */
export const APPLICATIONS = [
  "change-requests",
  "fleet",
  "plans",
  "promotions",
  "teams",
  "risk",
  "export",
  "import",
].map((id) => ({ id, enabled: true }));

export function session(overrides: Record<string, unknown> = {}) {
  return {
    actor: { username: "riley", teams: [] },
    effective: { username: "riley", teams: [] },
    delegated: false,
    applications: APPLICATIONS,
    globalOperations: ["resource:read"],
    resources: [],
    ...overrides,
  };
}

type Answer = { status?: number; json: unknown };
export type Handler = (request: Request, url: URL) => Answer | undefined;

/**
 * Serves the console with a tenant bootstrap and answers every /v1 request
 * from `handler`, falling back to an empty object as an unconfigured Hub
 * route does in the other suites.
 */
export async function stubHub(page: Page, handler: Handler): Promise<void> {
  const meta = JSON.stringify(BOOTSTRAP)
    .replaceAll("&", "&amp;")
    .replaceAll('"', "&quot;");
  await page.route("**/console/**", async (route: Route) => {
    if (route.request().resourceType() !== "document") {
      await route.fallback();
      return;
    }
    const response = await route.fetch();
    await route.fulfill({
      response,
      body: (await response.text()).replace(
        "<!--conduit-console-bootstrap-->",
        `<meta name="x-conduit-console-bootstrap" content="${meta}" />`,
      ),
    });
  });
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
  // Test-only projection of this fixture's declared Fleet records onto the
  // shipped RPC. Browser requests remain RPC; no production fallback is added.
  await page.route(
    "**/conduit.fleet.v1.FleetReadService/ListFleetInstallations",
    async (route) => {
      const request = route.request();
      const query = request.postDataJSON() as Record<string, unknown>;
      const url = new URL("/v1/fleet/installations", request.url());
      for (const [key, value] of Object.entries(query))
        url.searchParams.set(key, String(value));
      const answer = handler(request, url);
      const status = answer?.status ?? 200;
      if (status !== 200) {
        const code =
          (
            {
              401: "unauthenticated",
              403: "permission_denied",
              404: "not_found",
              400: "invalid_argument",
              422: "invalid_argument",
            } as Record<number, string>
          )[status] || "unavailable";
        await route.fulfill({
          status: status === 422 ? 400 : status,
          json: { code, message: "Fleet fixture refused" },
        });
        return;
      }
      const body = (answer?.json || {}) as Record<string, unknown>;
      if (!Array.isArray(body.installations)) {
        await route.fulfill({
          status: 503,
          json: {
            code: "unavailable",
            message: "Fleet fixture did not declare installation evidence",
          },
        });
        return;
      }
      const installations = Array.isArray(body.installations)
        ? body.installations
        : [];
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
    },
  );
  await page.route("**/v1/**", async (route) => {
    const request = route.request();
    const answer = handler(request, new URL(request.url()));
    await route.fulfill({
      status: answer?.status ?? 200,
      json: answer ? answer.json : {},
    });
  });
}

/** Synthetic Connect approval receipt; never a full native Plan document. */
export function approvalReceipt(
  id: string,
  status: string,
  subject = "bob",
  rationale = "reviewed",
) {
  return {
    plan: {
      id,
      environment: "dev",
      status,
      stepCount: "1",
      approvalCount: "1",
      requiredApprovals: "1",
      createdAt: "2026-11-17T00:00:00Z",
      updatedAt: "2026-11-17T00:00:01Z",
    },
    approvals: [{ subject, rationale, createdAt: "2026-11-17T00:00:01Z" }],
    changeRequestId: "crq_new",
  };
}

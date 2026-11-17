import { expect, test, type Page } from "@playwright/test";

import { APPLICATIONS } from "./hub-stub";

type BrowserStream = {
  controller: ReadableStreamDefaultController<Uint8Array> | null;
  requests: {
    cursor: string | null;
    method: string;
    credentials?: RequestCredentials;
  }[];
};
const bootstrap = {
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
async function fixture(page: Page, status = 200) {
  let planStatus = "RUNNING";
  let completed = false;
  const writes: string[] = [],
    reads: string[] = [],
    packagingRPCs: string[] = [];
  await page.route("**/console/**", async (route) => {
    if (route.request().resourceType() !== "document") {
      await route.fallback();
      return;
    }
    const response = await route.fetch();
    const meta = JSON.stringify(bootstrap)
      .replaceAll("&", "&amp;")
      .replaceAll('"', "&quot;");
    await route.fulfill({
      response,
      headers: {
        ...response.headers(),
        "content-security-policy":
          "default-src 'none'; script-src 'self'; style-src-elem 'self'; style-src-attr 'unsafe-inline'; font-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'self'; frame-ancestors 'none'",
      },
      body: (await response.text()).replace(
        "<!--conduit-console-bootstrap-->",
        `<meta name="x-conduit-console-bootstrap" content="${meta}" />`,
      ),
    });
  });
  await page.route(
    "**/conduit.packaging.v1.PackagingRunService/ListPackagingRuns",
    async (route) => {
      expect(route.request().method()).toBe("POST");
      expect(route.request().postDataJSON()).toEqual({
        view: "summary",
        limit: 20,
      });
      packagingRPCs.push(route.request().method());
      await route.fulfill({
        json: {
          configured: true,
          authority: "packaging-run",
          planAuthority: "conduit-plan",
          approvalAuthority: "conduit-governance",
          pageSize: 1,
          limit: 20,
          total: "1",
          items: [
            { runId: "run_live", phase: completed ? "complete" : "review" },
          ],
        },
      });
    },
  );
  await page.route(
    "**/conduit.packaging.v1.PackagingRunService/ListPackagingRunEvents",
    async (route) => {
      expect(route.request().method()).toBe("POST");
      expect(route.request().postDataJSON()).toEqual({ runId: "run_live" });
      await route.fulfill({
        json: {
          configured: true,
          runId: "run_live",
          items: [],
          total: "0",
          pageSize: 0,
          limit: 20,
        },
      });
    },
  );
  await page.route("**/v1/**", async (route) => {
    const request = route.request(),
      path = new URL(request.url()).pathname;
    reads.push(path);
    if (request.method() !== "GET") writes.push(`${request.method()} ${path}`);
    const person = {
      username: "alice",
      teams: ["readers"],
      serviceAccount: false,
    };
    const value =
      path === "/v1/session"
        ? {
            actor: person,
            effective: person,
            delegated: false,
            applications: APPLICATIONS,
            globalOperations: [],
            resources: [],
          }
        : path === "/v1/environments"
          ? {
              environments: [
                {
                  environment: { id: "dev" },
                  spoke: {
                    state: completed ? "live" : "stale",
                    agentId: "agent_live",
                  },
                },
              ],
            }
          : path === "/v1/fleet/installations"
            ? {
                installations: [
                  {
                    environment: "dev",
                    desiredVersion: "1",
                    observedVersion: "1",
                    desiredReleaseDigest: "sha256:a",
                    observedReleaseDigest: "sha256:a",
                    desiredConfigDigest: "sha256:b",
                    observedConfigDigest: "sha256:b",
                    observedAt: "2026-11-15T12:00:00Z",
                    stale: false,
                    lifecycle: "INSTALLED",
                    configStatus: "APPLIED",
                    rolloutStatus: "UP_TO_DATE",
                    health: completed ? "HEALTHY" : "UNKNOWN",
                  },
                ],
              }
            : path === "/v1/products"
              ? { products: [] }
              : path === "/v1/packaging-runs"
                ? {
                    configured: true,
                    items: [
                      {
                        runId: "run_live",
                        phase: completed ? "complete" : "review",
                      },
                    ],
                  }
                : path === "/v1/change-requests"
                  ? {
                      changeRequests: completed
                        ? []
                        : [{ id: "cr_live", status: "pending" }],
                    }
                  : path === "/v1/packaging-runs/run_live/events"
                    ? { items: [], total: 0 }
                    : path === "/v1/promotions"
                      ? { promotions: [] }
                      : path === "/v1/plans"
                        ? {
                            plans: [
                              {
                                id: "pln_live",
                                status: planStatus,
                                plan: { environment: "dev", steps: [] },
                              },
                            ],
                          }
                        : path === "/v1/environments/dev/timeline"
                          ? { timeline: [] }
                          : {};
    await route.fulfill({ json: value });
  });
  // A browser-native streaming response exercises real chunk parsing/lifecycle
  // under the Hub CSP. Native HTTP/outbox integration is tested by the Go Hub
  // tests; this fixture makes no claim of a deployed Temporal/model session.
  await page.addInitScript(
    ({ status }) => {
      const target = window as unknown as {
        __conduitLiveFixture: BrowserStream;
      };
      target.__conduitLiveFixture = { controller: null, requests: [] };
      const original = window.fetch.bind(window);
      window.fetch = async (input, init) => {
        const url =
          typeof input === "string"
            ? input
            : input instanceof URL
              ? input.href
              : input.url;
        if (new URL(url, location.href).pathname !== "/v1/events")
          return original(input, init);
        target.__conduitLiveFixture.requests.push({
          cursor: new Headers(init?.headers).get("Last-Event-ID"),
          method: init?.method ?? "GET",
          credentials: init?.credentials,
        });
        if (status !== 200)
          return new Response("private source response", { status });
        const body = new ReadableStream<Uint8Array>({
          start(controller) {
            target.__conduitLiveFixture.controller = controller;
          },
        });
        return new Response(body, {
          headers: {
            "content-type": "text/event-stream",
            "cache-control": "no-store",
            "X-Conduit-Stream-ID": "s_browser",
            "X-Conduit-Event-Sequence": "0",
            "X-Conduit-Event-Cursor": "c_root",
          },
        });
      };
    },
    { status },
  );
  return {
    writes,
    reads,
    packagingRPCs,
    completeMetrics() {
      completed = true;
    },
    setStatus(value: string) {
      planStatus = value;
    },
  };
}

test("live invalidations refetch the native Plan view under the Hub CSP without approval writes", async ({
  page,
}, testInfo) => {
  const current = await fixture(page);
  await page.goto("/console/#rm03-plan-detail");
  await expect(page.locator("#live-events-status")).toHaveAttribute(
    "data-live-state",
    "live",
  );
  await expect(page.locator("[data-release-plan-inspector]")).toContainText(
    "pln_live · RUNNING",
  );
  current.setStatus("SUCCEEDED");
  await page.evaluate(() => {
    const event = {
      version: 1,
      streamID: "s_browser",
      sequence: 1,
      cursor: "c_one",
      occurredAt: "2026-11-15T12:00:00Z",
      kind: "index.invalidated",
      resource: { type: "index", id: "idx_plans", revision: 2 },
      data: { areas: ["plans", "overview", "kpi"] },
    };
    const source = (
      window as unknown as { __conduitLiveFixture: BrowserStream }
    ).__conduitLiveFixture;
    source.controller!.enqueue(
      new TextEncoder().encode(
        `id: ${event.cursor}\nevent: ${event.kind}\ndata: ${JSON.stringify(event)}\n\n`,
      ),
    );
  });
  await expect(page.locator("[data-release-plan-inspector]")).toContainText(
    "pln_live · SUCCEEDED",
  );
  await expect(page.locator("#live-events-status")).toHaveAttribute(
    "data-live-state",
    "live",
  );
  expect(current.writes).toEqual([]);
  expect(current.packagingRPCs).toEqual([]);
  await page.screenshot({
    path: testInfo.outputPath("live-plan-invalidation.png"),
  });
});

test("live permission failure is visible and leaves polling active without reconnecting", async ({
  page,
}) => {
  await page.clock.install();
  const current = await fixture(page, 403);
  await page.goto("/console/#rm03-plan-detail");
  await expect(page.locator("#live-events-status")).toHaveText(
    "No live access",
  );
  await expect(page.locator("#live-events-retry")).toBeVisible();
  const before = current.reads.filter((path) => path === "/v1/plans").length;
  await page.clock.runFor(30000);
  await expect
    .poll(() => current.reads.filter((path) => path === "/v1/plans").length)
    .toBeGreaterThan(before);
  const requests = await page.evaluate(
    () =>
      (window as unknown as { __conduitLiveFixture: BrowserStream })
        .__conduitLiveFixture.requests,
  );
  expect(requests).toHaveLength(1);
  expect(requests[0]).toMatchObject({
    method: "GET",
    credentials: "same-origin",
    cursor: null,
  });
  expect(current.writes).toEqual([]);
  expect(current.packagingRPCs).toEqual([]);
  expect(current.reads.some((path) => path === "/v1/packaging-runs")).toBe(
    false,
  );
});

test("Audit session feed and native KPI counts update from authorized invalidations without writes", async ({
  page,
}, testInfo) => {
  await page.clock.install();
  const current = await fixture(page);
  await page.goto("/console/#area-packaging");
  await expect(page.locator("#live-events-status")).toHaveAttribute(
    "data-live-state",
    "live",
  );
  const kpi = (id: string) =>
    page.locator(`[data-kpi="${id}"] .kpi-value .tnum`);
  await expect(kpi("convergence")).toHaveText("—");
  await expect(kpi("packaging")).toHaveText("1");
  await expect(kpi("changes")).toHaveText("—");
  await expect(kpi("spokes")).toHaveText("—");
  current.completeMetrics();
  await page.evaluate(() => {
    const source = (
      window as unknown as { __conduitLiveFixture: BrowserStream }
    ).__conduitLiveFixture;
    for (const sequence of [1, 1, 2]) {
      const event = {
        version: 1,
        streamID: "s_browser",
        sequence,
        cursor: `c_${sequence}`,
        occurredAt: "2026-11-15T12:00:00Z",
        kind: "index.invalidated",
        resource: { type: "index", id: "idx_operations", revision: sequence },
        data: { areas: ["plans", "approvals", "packaging", "fleet", "kpi"] },
      };
      source.controller!.enqueue(
        new TextEncoder().encode(
          `id: ${event.cursor}\nevent: ${event.kind}\ndata: ${JSON.stringify(event)}\n\n`,
        ),
      );
    }
  });
  await expect(kpi("convergence")).toHaveText("—");
  await expect(kpi("packaging")).toHaveText("0");
  await expect(kpi("changes")).toHaveText("—");
  await expect(kpi("spokes")).toHaveText("—");
  await page.evaluate(() =>
    (window as unknown as { switchScreen: (id: string) => void }).switchScreen(
      "au01-audit",
    ),
  );
  const feed = page.locator("[data-live-operations]");
  await expect(feed).toBeVisible();
  await expect(feed.locator("[data-operation-sequence]")).toHaveCount(2);
  await expect(
    feed.locator("[data-operation-sequence]").first(),
  ).toHaveAttribute("data-operation-sequence", "2");
  await expect(feed).toContainText("Authorized index refreshed");
  await expect(feed).toContainText("not a complete event history");
  await expect(feed).not.toContainText("c_1");
  expect(current.writes).toEqual([]);
  expect(current.packagingRPCs.length).toBeGreaterThan(0);
  expect(current.packagingRPCs.every((method) => method === "POST")).toBe(true);
  await page.screenshot({
    path: testInfo.outputPath("live-operations-audit.png"),
  });
  await page.clock.pauseAt(new Date(Date.now() + 1000));
  await page.evaluate(() =>
    (
      window as unknown as { __conduitLiveFixture: BrowserStream }
    ).__conduitLiveFixture.controller!.close(),
  );
  await expect(feed.locator("[data-operations-state]")).toHaveAttribute(
    "data-operations-state",
    "offline",
  );
});

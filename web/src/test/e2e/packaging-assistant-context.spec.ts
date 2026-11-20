import { expect, test, type Page, type Request } from "@playwright/test";
import { session, stubHub } from "./hub-stub";

const fleetReads = new WeakMap<Page, Request[]>();
test.afterEach(async ({ page }) => {
  const requests = fleetReads.get(page) || [];
  expect(
    requests.filter(
      (request) =>
        new URL(request.url()).pathname === "/v1/fleet/installations",
    ),
  ).toEqual([]);
  for (const request of requests) {
    expect(new URL(request.url()).pathname).toBe(
      "/conduit.fleet.v1.FleetReadService/ListFleetInstallations",
    );
    expect(request.method()).toBe("POST");
  }
});
const runID = "run-selected-offpage";
const run = (id = runID) => ({
  configured: true,
  runId: id,
  phase: "review",
  outcome: "WAITING",
  targetPlanRef: "target-env/api@namespace/rid",
  sourceDigest: `sha256:${"a".repeat(64)}`,
  proposalRevision: "reviewed-revision",
  templateVersion: "fixture-v1",
  toolVersions: "tools-v1",
});
const target = () => ({
  generatedAt: "2026-11-20T01:00:00Z",
  facets: {},
  installations: [
    {
      environment: "target-env",
      installationId: "api@namespace/rid",
      product: "acme:correct-api",
      desiredVersion: "1",
    },
  ],
  summary: { total: 1 },
  pageSize: 1,
  limit: 1,
  hasPrevious: false,
});

async function setup(page: Page, forbidden = false, spaceId = "") {
  const reads: Request[] = [];
  fleetReads.set(page, reads);
  page.on("request", (request) => {
    const path = new URL(request.url()).pathname;
    if (
      path === "/v1/fleet/installations" ||
      path === "/conduit.fleet.v1.FleetReadService/ListFleetInstallations"
    )
      reads.push(request);
  });
  const tools: Record<string, unknown>[] = [];
  const lookups: URL[] = [];
  await stubHub(page, (request, url) => {
    if (url.pathname === "/v1/session") return { json: session() };
    if (url.pathname === "/v1/spaces")
      return {
        json: { spaces: spaceId ? [{ id: spaceId, name: "Selected" }] : [] },
      };
    if (url.pathname === `/v1/packaging-runs/${runID}`) return { json: run() };
    if (url.pathname.endsWith("/feedback")) return { json: { items: [] } };
    if (url.pathname === "/v1/fleet/installations") {
      lookups.push(url);
      return forbidden
        ? { status: 403, json: { error: "forbidden" } }
        : { json: target() };
    }
    if (url.pathname === "/v1/assistant/tools") {
      tools.push(request.postDataJSON());
      // This proves browser request binding, not a real worker/CR success.
      return {
        json: {
          ok: true,
          allowed: true,
          tool: "propose_governed_action",
          changeRequestId: "cr-request-binding-fixture",
          changeRequestStatus: "PENDING_APPROVALS",
          writeOps: 1,
        },
      };
    }
    return { json: {} };
  });
  await page.route(
    "**/conduit.packaging.v1.PackagingRunService/ListPackagingRuns",
    (route) =>
      route.fulfill({
        json: {
          configured: true,
          items: Array.from({ length: 20 }, (_, index) =>
            run(`visible-${index}`),
          ),
          total: "100",
          pageSize: 20,
          limit: 20,
          nextCursor: "fixture-next-page",
        },
      }),
  );
  await page.route(
    "**/conduit.packaging.v1.PackagingRunService/GetPackagingRun",
    (route) => route.fulfill({ json: run() }),
  );
  await page.route(
    "**/conduit.packaging.v1.PackagingRunService/ListPackagingRunEvents",
    (route) =>
      route.fulfill({
        json: {
          configured: true,
          runId: route.request().postDataJSON().runId,
          items: [],
          total: "0",
          pageSize: 0,
          limit: 20,
        },
      }),
  );
  await page.goto(
    `/console/#area-packaging?tab=timeline&run=${runID}${spaceId ? `&space=${spaceId}` : ""}`,
  );
  await expect(page.locator("#operator-name")).toHaveText("riley");
  await expect(
    page.getByText("Showing 20 of 100 packaging runs in this page."),
  ).toBeVisible();
  await page.getByRole("button", { name: "Toggle right chat panel" }).click();
  await page.locator('[data-set-mode="release_plan"]').click();
  return {
    tools,
    lookups,
    action: (kind: string) =>
      page.locator(
        `[data-assistant-action="${kind}"][data-chip-key="PackagingRun:${runID}"]`,
      ),
  };
}

test("the exact target lookup retains the selected Space", async ({ page }) => {
  const spaceId = `space_${"a".repeat(32)}`;
  const { tools, lookups, action } = await setup(page, false, spaceId);
  await action("propose-cancel").click();
  await expect(page.locator("[data-assistant-results]")).toContainText(
    "cr-request-binding-fixture",
  );
  expect(tools).toHaveLength(1);
  expect(lookups).toHaveLength(1);
  expect(lookups[0].searchParams.get("spaceId")).toBe(spaceId);
});

for (const [kind, actionID] of [
  ["propose-rerun", "rerun_packaging_run"],
  ["propose-cancel", "cancel_packaging_run"],
]) {
  test(`a fresh off-page run ${kind} sends the exact target without widening the visible index`, async ({
    page,
  }) => {
    const { tools, lookups, action } = await setup(page);
    await action(kind).click();
    await expect(page.locator("[data-assistant-results]")).toContainText(
      "cr-request-binding-fixture",
    );
    expect(tools).toHaveLength(1);
    expect(tools[0]).toMatchObject({
      mode: "release_plan",
      tool: "propose_governed_action",
      arguments: {
        actionId: actionID,
        targetEntityId: runID,
        targetObjectType: "PackagingRun",
        environment: "target-env",
        installationId: "api@namespace/rid",
        productId: "acme:correct-api",
      },
    });
    expect(lookups).toHaveLength(1);
    expect(Object.fromEntries(lookups[0].searchParams)).toEqual({
      environment: "target-env",
      installationId: "api@namespace/rid",
      limit: "1",
    });
    await expect(
      page.getByText("Showing 20 of 100 packaging runs in this page."),
    ).toBeVisible();
  });
}

test("a forbidden exact lookup refuses before the assistant tool request", async ({
  page,
}) => {
  const { tools, action } = await setup(page, true);
  await action("propose-cancel").click();
  await expect(page.locator("[data-assistant-results]")).toContainText(
    "Fleet fixture refused",
  );
  expect(tools).toEqual([]);
});

test("changing screens while the exact target read is pending never posts the old action", async ({
  page,
}) => {
  const { tools, action } = await setup(page);
  let release!: () => void, entered!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  const reading = new Promise<void>((resolve) => {
    entered = resolve;
  });
  await page.route(
    "**/conduit.fleet.v1.FleetReadService/ListFleetInstallations",
    async (route) => {
      if (!route.request().postDataJSON().installationId) {
        await route.fallback();
        return;
      }
      entered();
      await gate;
      await route.fulfill({ json: target() });
    },
  );
  await action("propose-rerun").click();
  await reading;
  await page
    .locator("#left-nav")
    .getByRole("button", { name: "Fleet", exact: true })
    .click();
  release();
  await expect(page.locator("[data-assistant-results]")).toContainText(
    "selected packaging context changed",
  );
  expect(tools).toEqual([]);
});

test("removing the selected context chip during its target read never posts the removed action", async ({
  page,
}) => {
  const { tools, action } = await setup(page);
  let release!: () => void, entered!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  const reading = new Promise<void>((resolve) => {
    entered = resolve;
  });
  await page.route(
    "**/conduit.fleet.v1.FleetReadService/ListFleetInstallations",
    async (route) => {
      entered();
      await gate;
      await route.fulfill({ json: target() });
    },
  );
  await action("propose-cancel").click();
  await reading;
  await page.locator(`[data-remove-chip="PackagingRun:${runID}"]`).click();
  await expect(action("propose-cancel")).toHaveCount(0);
  release();
  await expect(page.locator("[data-assistant-results] article")).toHaveCount(1);
  expect(tools).toEqual([]);
  await expect(page.locator("[data-assistant-results]")).toContainText(
    "selected packaging context changed",
  );
});

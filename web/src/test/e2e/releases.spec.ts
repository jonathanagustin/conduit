import { expect, test } from "@playwright/test";
import { APPLICATIONS } from "./hub-stub";
import { installPackagingReadStub, hydrate } from "./console-fixture";

installPackagingReadStub();

test("release convergence uses reported Hub evidence in the releases area", async ({
  page,
}) => {
  await page.route("**/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/v1/fleet/installations") {
      await route.fulfill({
        json: {
          installations: [
            {
              environment: "prod-native",
              installationId: "service-a",
              product: "native-product",
              desiredVersion: "2.0.0",
              observedVersion: "2.0.0",
              desiredReleaseDigest: "sha256:desired",
              observedReleaseDigest: "sha256:other",
              desiredConfigDigest: "sha256:config",
              observedConfigDigest: "sha256:config",
              observedAt: "2026-11-15T10:00:00Z",
              stale: false,
              lifecycle: "INSTALLED",
              health: "HEALTHY",
              configStatus: "APPLIED",
              rolloutStatus: "UP_TO_DATE",
            },
          ],
        },
      });
      return;
    }
    await route.fulfill({ json: {} });
  });
  await hydrate(page);
  await page.goto("/console/");
  await page
    .getByRole("button", { name: "Releases", exact: true })
    .first()
    .click();
  await page.locator('[data-area-tab="convergence"]').click();
  const matrix = page.locator("[data-release-convergence]");
  await expect(matrix).toContainText("native-product");
  await expect(matrix).toContainText("diverged");
  await expect(matrix).not.toContainText("edge-east-01");
});

test("release waves show recorded gates and reasons without synthetic calendar data", async ({
  page,
}) => {
  await page.route("**/v1/**", async (route) => {
    if (new URL(route.request().url()).pathname === "/v1/session")
      return route.fulfill({ json: { applications: APPLICATIONS } });
    const path = new URL(route.request().url()).pathname;
    await route.fulfill({
      json:
        path === "/v1/rollouts"
          ? {
              rollouts: [
                {
                  id: "native-wave-run",
                  product: "native-service",
                  version: "3",
                  status: "paused",
                  pausedBy: "reviewer",
                  waves: [
                    {
                      name: "canary-native",
                      environments: ["dev"],
                      gate: "health",
                      status: "passed",
                    },
                    {
                      name: "fleet-native",
                      environments: ["prod"],
                      gate: "approval",
                      status: "blocked",
                      reason: "Waiting for separate approver",
                    },
                  ],
                },
              ],
            }
          : {},
    });
  });
  await hydrate(page);
  await page.goto("/console/");
  await page
    .getByRole("button", { name: "Releases", exact: true })
    .first()
    .click();
  await page.locator('[data-area-tab="waves"]').click();
  const waves = page.locator("[data-release-waves]");
  await expect(waves).toContainText("native-wave-run");
  await expect(waves).toContainText("Gate: approval");
  await expect(waves).toContainText("Waiting for separate approver");
  await expect(waves).toContainText("scheduling is unavailable");
});

test("plan inspector loads the selected environment timeline and preserves audit evidence limits", async ({
  page,
}) => {
  const requests: string[] = [];
  await page.route("**/v1/**", async (route) => {
    if (new URL(route.request().url()).pathname === "/v1/session")
      return route.fulfill({ json: { applications: APPLICATIONS } });
    const path = new URL(route.request().url()).pathname;
    requests.push(path);
    if (path === "/v1/plans")
      return route.fulfill({
        json: {
          plans: [
            {
              id: "native-plan-a",
              status: "FAILED",
              failure: "Recorded denial",
              plan: { environment: "env-a", steps: [] },
            },
            {
              id: "native-plan-b",
              status: "SUCCEEDED",
              plan: { environment: "env-b", steps: [] },
            },
          ],
        },
      });
    if (path === "/v1/environments/env-a/timeline")
      return route.fulfill({
        json: {
          timeline: [
            {
              at: "2026-11-15",
              kind: "audit",
              summary: "native-audit-a",
              verified: true,
              digest: "sha256:link",
              sequence: 7,
            },
          ],
        },
      });
    if (path === "/v1/environments/env-b/timeline")
      return route.fulfill({
        json: {
          timeline: [
            {
              at: "2026-11-15",
              kind: "plan",
              summary: "native-plan-event-b",
              verified: true,
            },
          ],
        },
      });
    return route.fulfill({ json: {} });
  });
  await hydrate(page);
  await page.goto("/console/");
  await page
    .getByRole("button", { name: "Releases", exact: true })
    .first()
    .click();
  await page.locator('[data-area-tab="plans"]').click();
  const inspector = page.locator("[data-release-plan-inspector]");
  await expect(inspector).toContainText("Recorded denial");
  await expect(inspector).toContainText("Hub-verified chain link 7");
  // Plans detail is read from the Hub and must say so (#822).
  await expect(page.locator("#example-data-badge")).toHaveText("LIVE");
  await expect(page.locator("#example-data-badge")).toHaveAttribute(
    "data-mode",
    "live",
  );
  await inspector.locator("[data-release-plan]").selectOption("native-plan-b");
  await expect(inspector).toContainText("native-plan-event-b");
  await expect(inspector).not.toContainText("native-audit-a");
  await expect(inspector).not.toContainText("Hub-verified chain link");
  expect(requests).toContain("/v1/environments/env-b/timeline");
});

test("an index past the read bound lists what was read and says more exist", async ({
  page,
}) => {
  const planPages: string[] = [];
  const scoped: string[] = [];
  await page.route("**/v1/**", async (route) => {
    if (new URL(route.request().url()).pathname === "/v1/session")
      return route.fulfill({ json: { applications: APPLICATIONS } });
    const url = new URL(route.request().url());
    if (url.pathname === "/v1/plans") {
      const cursor = Number(url.searchParams.get("cursor") || 0);
      planPages.push(url.search);
      return route.fulfill({
        json: {
          plans: Array.from({ length: 20 }, (_, row) => ({
            id: `bulk-plan-${cursor}-${row}`,
            status: "AWAITING_APPROVAL",
            plan: { environment: "edge-a", steps: [] },
          })),
          total: 240,
          pageSize: 20,
          limit: 20,
          nextCursor: String(cursor + 1),
        },
      });
    }
    if (url.pathname === "/v1/environments")
      return route.fulfill({
        json: { environments: [{ environment: { id: "edge-a" } }], total: 1 },
      });
    if (url.pathname.endsWith("/timeline") || url.pathname === "/v1/blackouts")
      scoped.push(url.pathname + url.search);
    return route.fulfill({ json: {} });
  });
  await hydrate(page);
  await page.goto("/console/");
  await page.evaluate(() =>
    (window as unknown as { switchScreen: (id: string) => void }).switchScreen(
      "s16-approvals-board",
    ),
  );
  const board = page.locator("[data-approvals-board]");
  await expect(board).toContainText("Needs review · 20");
  await expect(board).toContainText("bulk-plan-0-19");
  await expect(board).not.toContainText("Hub did not return plans.");
  await expect(page.locator("[data-index-truncation]")).toHaveText(
    "Showing 20 of 240 plans in this page. Use Next or Previous to change page.",
  );
  expect(
    planPages.every(
      (query) => new URLSearchParams(query).get("view") === "summary",
    ),
  ).toBe(true);
  expect(
    planPages.every((query) => !new URLSearchParams(query).has("cursor")),
  ).toBe(true);
  // Environment-scoped refresh reads name the environment the Hub returned.
  // The approval board does not request unrelated environment evidence.
  expect(scoped).toEqual([]);
  expect(scoped.some((path) => /\/dev\/|=dev$/.test(path))).toBe(false);
});

test("environment hub scopes header, installations, blackout and timeline to selection", async ({
  page,
}) => {
  const proposals: unknown[] = [];
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/v1/fleet/propose-fix") {
      proposals.push(route.request().postDataJSON());
      return route.fulfill({ json: { changeRequestId: "native-fix-request" } });
    }
    if (url.pathname === "/v1/environments")
      return route.fulfill({
        json: {
          environments: [
            { environment: { id: "env-a" } },
            {
              environment: {
                id: "env-b",
                maintenance: {
                  downtime: {
                    timeZone: "UTC",
                    intervals: ["Monday 02:00-04:00"],
                  },
                },
              },
            },
          ],
        },
      });
    if (url.pathname === "/v1/fleet/installations")
      return route.fulfill({
        json: {
          installations: [
            { environment: "env-a", product: "other-service" },
            {
              environment: "env-b",
              product: "selected-service",
              installationId: "service-b",
              desiredVersion: "2",
              observedVersion: "1",
              desiredReleaseDigest: "sha256:wanted",
              observedReleaseDigest: "sha256:old",
              desiredConfigDigest: "sha256:config",
              observedConfigDigest: "sha256:config",
              observedAt: "2026-11-15T10:00:00Z",
              stale: false,
            },
          ],
        },
      });
    if (url.pathname === "/v1/blackouts")
      return route.fulfill({
        json: {
          environment: url.searchParams.get("environment"),
          blocked: true,
          reason: `hold-${url.searchParams.get("environment")}`,
        },
      });
    if (url.pathname.endsWith("/timeline"))
      return route.fulfill({
        json: {
          timeline: [
            {
              kind: "plan",
              at: "2026-11-15",
              summary: `event-${url.pathname.split("/")[3]}`,
              verified: false,
            },
          ],
        },
      });
    return route.fulfill({ json: {} });
  });
  await hydrate(page);
  await page.goto("/console/");
  await page
    .getByRole("button", { name: "Releases", exact: true })
    .first()
    .click();
  await page.locator("[data-release-environment]").selectOption("env-b");
  const hub = page.locator("[data-release-environment-hub]");
  await expect(hub).toContainText("selected-service");
  await expect(hub).not.toContainText("other-service");
  await expect(hub).toContainText("hold-env-b");
  await expect(hub).toContainText("event-env-b");
  await expect(hub).not.toContainText("hold-env-a");
  await expect(hub).not.toContainText("event-env-a");
  await expect(page.locator("[data-object-header]")).toContainText("env-b");
  await expect(hub).toContainText("Monday 02:00-04:00");
  await hub.getByRole("button", { name: "Propose fix for service-b" }).click();
  await expect(hub).toContainText("native-fix-request");
  expect(proposals).toEqual([
    { environment: "env-b", installationId: "service-b" },
  ]);
});

test("release dependencies display bounded native constraints and recorded upgrade lag", async ({
  page,
}) => {
  const writes: string[] = [];
  await page.route("**/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (route.request().method() !== "GET") writes.push(path);
    if (path === "/v1/products")
      return route.fulfill({
        json: {
          products: [
            { product: "native-service", releases: [{ version: "2" }] },
            { product: "native-library", releases: [{ version: "1" }] },
          ],
        },
      });
    if (path === "/v1/products/native-service/dependencies/2")
      return route.fulfill({
        json: {
          dependencies: [
            {
              product: "native-library",
              minimumVersion: "1",
              maximumVersion: "2",
              optional: true,
            },
          ],
        },
      });
    if (path === "/v1/products/native-library/dependencies/1")
      return route.fulfill({ json: { dependencies: [] } });
    if (path === "/v1/fleet/upgrades")
      return route.fulfill({
        json: {
          upgrades: [
            {
              environment: "prod-native",
              installationId: "service-a",
              product: "native-service",
              desiredVersion: "2",
              observedVersion: "1",
              reason: "observed version differs",
            },
          ],
        },
      });
    return route.fulfill({ json: {} });
  });
  await hydrate(page);
  await page.goto("/console/");
  await page
    .getByRole("button", { name: "Releases", exact: true })
    .first()
    .click();
  await page.locator('[data-area-tab="dependencies"]').click();
  const graph = page.locator("[data-release-dependencies]");
  await expect(graph.locator("svg")).toBeVisible();
  await expect(graph).toContainText("native-library · optional");
  await graph
    .locator("[data-release-dependency]")
    .selectOption(JSON.stringify(["native-library", "1"]));
  await expect(graph).toContainText("native-service");
  await expect(graph).toContainText(
    "does not distinguish an empty dependency set",
  );
  await expect(page.locator("[data-release-upgrade-lag]")).toContainText(
    "observed version differs",
  );
  expect(
    writes.filter(
      (path) =>
        path.startsWith("/v1/products/") || path === "/v1/fleet/upgrades",
    ),
  ).toEqual([]);
});

test("release planning, ramp and windows use recorded data with explicit calendar limits", async ({
  page,
}) => {
  await page.route("**/v1/**", async (route) => {
    if (new URL(route.request().url()).pathname === "/v1/session")
      return route.fulfill({ json: { applications: APPLICATIONS } });
    const url = new URL(route.request().url());
    if (url.pathname === "/v1/environments")
      return route.fulfill({
        json: {
          environments: [
            {
              environment: {
                id: "native-env",
                maintenance: {
                  downtime: {
                    timeZone: "UTC",
                    intervals: ["Tuesday 03:00-05:00"],
                  },
                  overrides: [
                    {
                      type: "DOWNTIME",
                      startsAt: "2026-11-15T03:00:00Z",
                      endsAt: "2026-11-15T04:00:00Z",
                      reason: "Recorded maintenance approval",
                      approvedBy: "reviewer",
                    },
                  ],
                },
              },
            },
          ],
        },
      });
    if (url.pathname === "/v1/plans")
      return route.fulfill({
        json: {
          plans: [
            {
              id: "native-planning-record",
              status: "PENDING_APPROVAL",
              plan: { environment: "native-env", steps: [] },
            },
          ],
        },
      });
    if (url.pathname === "/v1/rollouts")
      return route.fulfill({
        json: {
          rollouts: [
            {
              id: "native-ramp-record",
              product: "service",
              version: "1",
              status: "paused",
              waves: [
                {
                  name: "native-canary",
                  environments: ["native-env"],
                  gate: "approval",
                  status: "blocked",
                  reason: "Waiting for review",
                },
              ],
            },
          ],
        },
      });
    if (url.pathname === "/v1/blackouts")
      return route.fulfill({
        json: {
          environment: url.searchParams.get("environment"),
          blocked: false,
        },
      });
    if (url.pathname.endsWith("/timeline"))
      return route.fulfill({ json: { timeline: [] } });
    return route.fulfill({ json: {} });
  });
  await hydrate(page);
  await page.goto("/console/");
  await page
    .getByRole("button", { name: "Releases", exact: true })
    .first()
    .click();
  await page.locator('[data-area-tab="planning"]').click();
  await expect(page.locator("[data-release-plan-inspector]")).toContainText(
    "native-planning-record",
  );
  await expect(page.locator("[data-release-calendar]")).toContainText(
    "Resolved scheduling data is unavailable",
  );
  await expect(page.locator("#area-body")).not.toContainText("prod-root-2026");
  await page.locator('[data-area-tab="ramp"]').click();
  await expect(page.locator("[data-release-waves]")).toContainText(
    "native-ramp-record",
  );
  await expect(page.locator("[data-release-waves]")).toContainText(
    "Waiting for review",
  );
  await expect(page.locator("[data-release-calendar]")).toContainText(
    "No Gantt bars",
  );
  await page.locator('[data-area-tab="windows"]').click();
  await expect(page.locator("[data-release-environment-hub]")).toContainText(
    "Tuesday 03:00-05:00",
  );
  await expect(page.locator("[data-release-calendar]")).toContainText(
    "A governed scheduling proposal",
  );
  await expect(
    page.getByRole("img", { name: "Recorded maintenance override interval" }),
  ).toBeVisible();
  await expect(page.locator("[data-release-calendar]")).toContainText(
    "Recorded maintenance approval",
  );
});

test("window scheduler creates only a confirmed preview-bound change request", async ({
  page,
}) => {
  const proposals: Record<string, any>[] = [];
  const forbidden: string[] = [];
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url());
    if (
      route.request().method() !== "GET" &&
      /\/(approve|leases|start)$/.test(url.pathname)
    )
      forbidden.push(url.pathname);
    if (url.pathname === "/v1/environments")
      return route.fulfill({
        json: { environments: [{ environment: { id: "scheduled-prod" } }] },
      });
    if (url.pathname === "/v1/plans")
      return route.fulfill({
        json: {
          plans: [
            {
              id: "scheduled-plan",
              status: "AWAITING_APPROVAL",
              plan: {
                environment: "scheduled-prod",
                steps: [{ entity: "api", installationId: "api-1" }],
              },
            },
          ],
        },
      });
    if (url.pathname.endsWith("/timeline"))
      return route.fulfill({ json: { timeline: [] } });
    if (url.pathname === "/v1/blackouts")
      return route.fulfill({
        json: {
          environment: url.searchParams.get("environment"),
          blocked: false,
        },
      });
    if (url.pathname === "/v1/temporary-maintenance-overrides") {
      const body = route.request().postDataJSON();
      proposals.push(body);
      return route.fulfill({
        json: {
          valueTier: "CONDUIT_NATIVE",
          dryRun: body.dryRun,
          requests: [
            {
              changeRequestId: "scheduled-request",
              ...body.entities[0],
              startsAt: body.startsAt,
              endsAt: new Date(
                Date.parse(body.startsAt) +
                  Number(body.duration.slice(0, -1)) * 60000,
              ).toISOString(),
              type: body.downtime ? "DOWNTIME" : "NO_DOWNTIME",
              status: "PENDING_APPROVALS",
            },
          ],
        },
      });
    }
    return route.fulfill({ json: {} });
  });
  await hydrate(page);
  await page.goto("/console/");
  await page
    .getByRole("button", { name: "Releases", exact: true })
    .first()
    .click();
  await page.locator('[data-area-tab="windows"]').click();
  const form = page.locator("[data-window-scheduler]");
  const save = form.getByRole("button", { name: "Create change request" });
  await expect(save).toBeDisabled();
  await form
    .locator('[name="start"]')
    .fill(new Date(Date.now() + 2 * 3600000).toISOString().slice(0, 16));
  await form.locator('[name="reason"]').fill("Reviewed maintenance");
  await form.getByRole("button", { name: "Preview change request" }).click();
  await expect(form.locator("[data-window-message]")).toContainText(
    "Preview scheduled-request",
  );
  await form.locator('[name="minutes"]').fill("90");
  await expect(save).toBeDisabled();
  const bar = form.locator("[data-draft-bar]");
  await bar.focus();
  await bar.press("ArrowRight");
  await bar.press("Shift+ArrowRight");
  await expect(form.locator('[name="minutes"]')).toHaveValue("105");
  const before = await form.locator('[name="start"]').inputValue();
  const box = await bar.boundingBox();
  expect(box).not.toBeNull();
  await page.mouse.move(box!.x + 2, box!.y + 20);
  await page.mouse.down();
  await page.mouse.move(box!.x + 45, box!.y + 20);
  await page.mouse.up();
  await expect(form.locator('[name="start"]')).not.toHaveValue(before);
  await form.getByRole("button", { name: "Preview change request" }).click();
  await expect(form.locator("[data-window-message]")).toContainText(
    "Preview scheduled-request",
  );
  await form.locator("[data-confirm-window]").check();
  await save.click();
  await expect(form.locator("[data-window-message]")).toContainText(
    "Change request scheduled-request",
  );
  expect(proposals).toHaveLength(3);
  expect(proposals.map((row) => row.dryRun)).toEqual([true, true, false]);
  expect({
    ...proposals[1],
    dryRun: false,
    expectedChangeRequestId: "scheduled-request",
  }).toEqual(proposals[2]);
  expect(proposals[2]!.entities).toEqual([
    { environment: "scheduled-prod", entity: "api" },
  ]);
  expect(forbidden).toEqual([]);
  await form.getByRole("button", { name: "Preview change request" }).click();
  await expect(form.locator("[data-window-message]")).toContainText(
    "Preview scheduled-request",
  );
  await form.locator("[data-confirm-window]").check();
  await save.click();
  await expect(form.locator("[data-window-message]")).toContainText(
    "Submission already attempted",
  );
  expect(proposals.filter((row) => !row.dryRun)).toHaveLength(1);
});

test("inspector keeps the newest descending audit events within its 20-row bound", async ({
  page,
}) => {
  const entries = Array.from({ length: 25 }, (_, index) => ({
    sequence: index + 1,
    id: `inspector-event-${index + 1}`,
    actor: "reader",
    action: `inspector-action-${String(index + 1).padStart(2, "0")}`,
    resourceRid: "environment/dev",
  }));
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/v1/session")
      return route.fulfill({
        json: { actor: { username: "root" }, effective: { username: "root" } },
      });
    if (url.pathname === "/v1/authorization-audit") {
      expect(url.searchParams.get("order")).toBe("desc");
      return route.fulfill({ json: { order: "desc", entries } });
    }
    return route.fulfill({ status: 404, json: { error: "not configured" } });
  });
  await page.route("**/api/v2/ontologies/**", async (route) => {
    if (!new URL(route.request().url()).pathname.endsWith("/loadObjects"))
      return route.fulfill({ status: 404, json: { error: "not configured" } });
    const body = route.request().postDataJSON();
    return route.fulfill({
      json: {
        data:
          body.objectSet.type === "searchAround"
            ? []
            : [{ id: "dev", name: "dev", __primaryKey: "dev" }],
      },
    });
  });
  await hydrate(page);
  await page.goto("/console/#au01-audit");
  await expect(page.locator(".audit-ledger")).toContainText(
    "inspector-action-01",
  );
  await page.evaluate(() =>
    Reflect.get(window, "inspectObject")("Environment", "dev"),
  );
  const inspector = page.locator(".kit-inspector");
  await inspector.getByRole("tab", { name: "Timeline", exact: true }).click();
  await expect(inspector.locator("li")).toHaveCount(20);
  await expect(inspector).toContainText("inspector-action-01");
  await expect(inspector).toContainText("inspector-action-20");
  for (const omitted of [21, 22, 23, 24, 25])
    await expect(inspector).not.toContainText(`inspector-action-${omitted}`);
});

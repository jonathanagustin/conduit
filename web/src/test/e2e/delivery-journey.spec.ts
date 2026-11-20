import { create, toBinary } from "@bufbuild/protobuf";
import { PlanApprovalOutcomeSchema } from "../../api/gen/conduit/plans/v1/plan_approval_pb.js";
import {
  expect,
  test,
  type Page,
  type Route,
  type Locator,
} from "@playwright/test";
import { APPLICATIONS, approvalReceipt } from "./hub-stub";

// The delivery journey's writes and the planning deep link, against stubbed
// Hub routes. These establish what the console sends and paints GIVEN an
// answer; src/test/live/release-write-journey.spec.ts walks the same journey
// against a real Hub.

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

async function hydrate(page: Page): Promise<void> {
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

const digest = `sha256:${"a".repeat(64)}`;
const release = (version: string) => ({
  product: "app.example:api",
  version,
  chart: { repository: "registry.example/api", tag: version, digest },
  images: [{ repository: "registry.example/api", tag: version, digest }],
});
const basePlan = {
  id: "pln_base",
  status: "SUCCEEDED",
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
  plan: { environment: "dev", steps: [] },
  desiredState: {
    apiVersion: "platform.delivery/v1alpha1",
    kind: "EnvironmentDeployment",
    environment: { id: "dev", generation: "gen-1" },
    releases: [release("0.1.0")],
    entities: [
      {
        name: "api",
        product: "app.example:api",
        version: "0.1.0",
        namespace: "example",
      },
    ],
  },
};
const awaitingPlan = {
  id: "pln_new",
  status: "AWAITING_APPROVAL",
  changeRequestId: "crq_new",
  createdAt: "2026-02-01T00:00:00Z",
  updatedAt: "2026-02-01T00:00:00Z",
  requiredApprovals: 1,
  approvals: [],
  plan: { environment: "dev", steps: [] },
};
const changeRequest = (author: string) => ({
  id: "crq_new",
  planId: "pln_new",
  environment: "dev",
  title: "Set api to 0.2.0 in dev",
  status: "PENDING_APPROVALS",
  author: { username: author },
  changes: [],
  evaluations: [],
  requirements: [],
});

type Hub = {
  operator: string;
  /** What /v1/session lists at platform/root; read and operate when absent. */
  globalOperations?: string[];
  resourceOperations?: string[];
  installations?: Record<string, unknown>[];
  plans: Record<string, unknown>[];
  changes: Record<string, unknown>[];
  promotions: Record<string, unknown>[];
  writes: { path: string; body: any; headers: Record<string, string> }[];
  answer: (path: string, body: any) => { status: number; json: unknown } | null;
};

/** One dispatcher for every Hub route the delivery views read or write. */
async function stubHub(page: Page, hub: Hub) {
  await page.route(
    "**/conduit.fleet.v1.FleetReadService/ListFleetInstallations",
    (route) => {
      const body = route.request().postDataJSON();
      const installations = hub.installations ?? [];
      return route.fulfill({
        json: {
          generatedAt: "2026-11-20T01:00:00Z",
          maxResultAgeSeconds: "0",
          installations,
          summary: { total: String(installations.length) },
          facets: {},
          pageSize: installations.length,
          limit: body.limit || 100,
        },
      });
    },
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
  await page.route(
    /\/(?:v1\/|conduit\.plans\.v1\.PlanApprovalService\/ApprovePlan$)/,
    (route) => {
      const request = route.request();
      const url = new URL(request.url());
      const path = url.pathname;
      if (request.method() === "POST") {
        const body = request.postDataJSON();
        const answer = hub.answer(path, body);
        if (answer) {
          hub.writes.push({ path, body, headers: request.headers() });
          return route.fulfill({
            status: answer.status,
            json: answer.json,
            headers:
              path === "/v1/plans" && answer.status === 201
                ? { "X-Change-Request-ID": "crq_new" }
                : {},
          });
        }
        return route.fulfill({ status: 404, json: { error: "not stubbed" } });
      }
      const json = (body: unknown) => route.fulfill({ json: body });
      if (path === "/v1/session")
        return json({
          actor: { username: hub.operator },
          effective: { username: hub.operator },
          applications: APPLICATIONS,
          resources:
            hub.resourceOperations === undefined
              ? []
              : [
                  {
                    resourceRid: "environment/dev",
                    operations: hub.resourceOperations,
                  },
                ],
          globalOperations: hub.globalOperations ?? [
            "resource:read",
            "resource:operate",
          ],
        });
      if (path === "/v1/spaces") return json({ spaces: [] });
      if (path === "/v1/environments")
        return json({
          environments: [
            { environment: { id: "dev", generation: "gen-1" }, spoke: {} },
          ],
          total: 1,
        });
      if (path === "/v1/plans") return json({ plans: hub.plans });
      if (path === "/v1/products")
        return json({
          products: [
            {
              product: "app.example:api",
              releases: [release("0.1.0"), release("0.2.0")],
            },
          ],
        });
      if (path === "/v1/change-requests")
        return json({ changeRequests: hub.changes });
      if (path === "/v1/change-requests/crq_new")
        return json(hub.changes[0] ?? changeRequest(hub.operator));
      if (path === "/v1/promotions")
        return json({ promotions: hub.promotions });
      if (path === "/v1/fleet/installations")
        return json({ installations: hub.installations ?? [] });
      return route.fulfill({ status: 404, json: { error: "not stubbed" } });
    },
  );
}

async function openArea(page: Page, area: string, tab: string) {
  await page
    .locator("#left-nav")
    .getByText(area, { exact: true })
    .first()
    .click();
  await page
    .locator("#main-workspace")
    .getByRole("button", { name: tab, exact: true })
    .first()
    .click();
}

test("proposes a plan with a confirmation, then shows the author cannot approve it", async ({
  page,
}) => {
  const hub: Hub = {
    operator: "alice",
    plans: [basePlan],
    changes: [],
    promotions: [],
    writes: [],
    answer: (path, body) => {
      if (path !== "/v1/plans") return null;
      const created = { ...awaitingPlan, desiredState: body };
      hub.plans = [created, basePlan];
      hub.changes = [changeRequest("alice")];
      return { status: 201, json: created };
    },
  };
  await hydrate(page);
  await stubHub(page, hub);
  await page.goto("/console/");
  await openArea(page, "Releases", "Environments");
  const panel = page.locator('#main-workspace [data-dj-panel="propose"]');
  await expect(panel).toBeVisible();
  // The picker is the Hub's catalog, nothing typed.
  await expect(panel.locator('[data-dj-field="version"] option')).toHaveCount(
    2,
  );
  await expect(panel.locator('[data-dj-field="version"]')).toHaveValue("0.2.0");
  const review = panel.locator("[data-dj-review]");
  await expect(review).toBeDisabled();
  await panel.locator('[data-dj-field="generation"]').fill("gen-2");
  await panel.locator('[data-dj-field="rationale"]').fill("roll api forward");
  await review.click();

  // Confirmation: the exact request, and nothing sent yet.
  await expect(panel.locator("[data-dj-request]")).toContainText(
    "POST /v1/plans",
  );
  await expect(panel.locator("[data-dj-request]")).toContainText(
    "X-Change-Title: Set api to 0.2.0 in dev",
  );
  expect(hub.writes).toHaveLength(0);
  await panel.locator("[data-dj-confirm]").click();

  const outcome = panel.locator("[data-dj-outcome]");
  await expect(outcome).toHaveAttribute("data-dj-outcome", "ok");
  await expect(outcome).toContainText("pln_new");
  await expect(outcome).toContainText("PENDING_APPROVALS");
  expect(hub.writes).toHaveLength(1);
  expect(hub.writes[0].headers["x-change-title"]).toBe(
    "Set api to 0.2.0 in dev",
  );
  expect(hub.writes[0].headers["x-change-description"]).toBe(
    "roll api forward",
  );
  expect(hub.writes[0].headers["authorization"]).toBeUndefined();
  expect(hub.writes[0].body).toEqual({
    ...basePlan.desiredState,
    environment: { id: "dev", generation: "gen-2" },
    releases: [release("0.1.0"), release("0.2.0")],
    entities: [{ ...basePlan.desiredState.entities[0], version: "0.2.0" }],
  });

  // The author sees the rule and no live Approve.
  await openArea(page, "Releases", "Plans");
  await page
    .locator("#main-workspace [data-release-plan]")
    .selectOption("pln_new");
  const decide = page.locator('#main-workspace [data-dj-panel="decide"]');
  await expect(decide.locator("[data-dj-reason]")).toContainText(
    "Another person must approve or reject it.",
  );
  await expect(decide.getByRole("button", { name: "Approve" })).toBeDisabled();
  await expect(decide.getByRole("button", { name: "Reject" })).toBeDisabled();
});

test("a second person approves a plan from the approvals board", async ({
  page,
}) => {
  let unhold: () => void = () => {};
  const held = new Promise<void>((resolve) => {
    unhold = resolve;
  });
  const hub: Hub = {
    operator: "bob",
    plans: [awaitingPlan, basePlan],
    changes: [changeRequest("alice")],
    promotions: [],
    writes: [],
    answer: (path, body) => {
      if (path !== "/conduit.plans.v1.PlanApprovalService/ApprovePlan")
        return null;
      hub.plans = [{ ...awaitingPlan, status: "APPROVED" }, basePlan];
      return { status: 200, json: approvalReceipt("pln_new", "APPROVED") };
    },
  };
  await hydrate(page);
  await stubHub(page, hub);
  // Hold the approval so the in-flight state can be observed.
  await page.route(
    "**/conduit.plans.v1.PlanApprovalService/ApprovePlan",
    async (route) => {
      await held;
      await route.fallback();
    },
  );
  await page.goto("/console/");
  await openArea(page, "Change requests", "Approvals");
  const decide = page.locator('#main-workspace [data-dj-panel="decide"]');
  await expect(decide).toContainText("pln_new");
  const approve = decide.locator('[data-dj-review="approve"]');
  await expect(approve).toBeDisabled();
  await decide.locator('[data-dj-field="rationale"]').fill("reviewed the diff");
  await approve.click();
  await expect(decide.locator("[data-dj-request]")).toContainText(
    "POST /conduit.plans.v1.PlanApprovalService/ApprovePlan",
  );
  await decide.locator("[data-dj-confirm]").click();

  // In flight: stated as not done, and nothing left to press.
  await expect(decide.locator("[data-dj-inflight]")).toContainText(
    "nothing is recorded as done yet",
  );
  await expect(decide.locator("[data-dj-review]")).toHaveCount(0);
  await expect(decide.locator("[data-dj-confirm]")).toHaveCount(0);
  await expect(decide.locator('[data-dj-field="rationale"]')).toBeDisabled();
  await expect(decide.locator("[data-dj-outcome]")).toHaveCount(0);
  // Both entry points must share the pending command, including its reply.
  await openArea(page, "Releases", "Plans");
  await page
    .locator("#main-workspace [data-release-plan]")
    .selectOption("pln_new");
  await expect(decide.locator("[data-dj-inflight]")).toBeVisible();
  await expect(decide.locator("[data-dj-review]")).toHaveCount(0);
  await expect(decide.locator('[data-dj-field="rationale"]')).toBeDisabled();
  unhold();

  await expect(decide.locator("[data-dj-outcome]")).toContainText(
    "pln_new is now APPROVED.",
  );
  expect(hub.writes).toEqual([
    expect.objectContaining({
      path: "/conduit.plans.v1.PlanApprovalService/ApprovePlan",
      body: { planId: "pln_new", rationale: "reviewed the diff" },
    }),
  ]);
  // The board was read again after the Hub answered.
  await openArea(page, "Change requests", "Approvals");
  await expect(
    page.locator("#main-workspace [data-approvals-board]"),
  ).toContainText("APPROVED");
  await decide.getByRole("button", { name: "Review another Plan" }).click();
  await expect(decide.locator("[data-dj-outcome]")).toHaveCount(0);
  expect(hub.writes).toHaveLength(1);
});

test("shows the Hub's refusal of a proposal inline, escaped", async ({
  page,
}) => {
  const hub: Hub = {
    operator: "alice",
    plans: [basePlan],
    changes: [],
    promotions: [],
    writes: [],
    answer: (path) =>
      path === "/v1/plans"
        ? {
            status: 403,
            json: {
              error: 'forbidden: <img src=x onerror="window.pwned=1">',
              status: "403",
            },
          }
        : null,
  };
  await hydrate(page);
  await stubHub(page, hub);
  await page.goto("/console/");
  await openArea(page, "Releases", "Environments");
  const panel = page.locator('#main-workspace [data-dj-panel="propose"]');
  await panel.locator('[data-dj-field="generation"]').fill("gen-2");
  await panel.locator('[data-dj-field="rationale"]').fill("roll api forward");
  await panel.locator("[data-dj-review]").click();
  await panel.locator("[data-dj-confirm]").click();
  await expect(panel.locator("[data-dj-error]")).toContainText(
    'forbidden: <img src=x onerror="window.pwned=1">',
  );
  await expect(panel.locator("[data-dj-error] img")).toHaveCount(0);
  await expect(panel.locator("[data-dj-outcome]")).toHaveCount(0);
  expect(await page.evaluate(() => Reflect.get(window, "pwned"))).toBe(
    undefined,
  );
  // The form is still there to correct and send again.
  await expect(panel.locator("[data-dj-review]")).toBeEnabled();
});

test("a blocked promotion is painted as blocked, with the Hub's reasons", async ({
  page,
}) => {
  const policy = {
    minimumHealthyEntities: 1,
    maximumUnhealthyEntities: 0,
    minimumHealthyDurationSeconds: 0,
  };
  const recorded = {
    id: "pro_0",
    product: "app.example:api",
    version: "0.1.0",
    fromChannel: "DEV",
    toChannel: "STAGING",
    status: "BLOCKED",
    policy,
  };
  const hub: Hub = {
    operator: "alice",
    plans: [basePlan],
    changes: [],
    promotions: [recorded],
    writes: [],
    answer: (path) =>
      path === "/v1/promotions/evaluations"
        ? {
            status: 201,
            json: {
              ...recorded,
              id: "pro_1",
              version: "0.2.0",
              lifecycle: "RUNNING",
              blockers: [
                {
                  kind: "HEALTHY_ENTITIES_BELOW_MINIMUM",
                  message: "healthy entity count 0 is below required 1",
                },
              ],
            },
          }
        : null,
  };
  await hydrate(page);
  await stubHub(page, hub);
  await page.goto("/console/");
  await openArea(page, "Releases", "Promotion");
  const panel = page.locator('#main-workspace [data-dj-panel="promote"]');
  await panel.locator('[data-dj-field="version"]').selectOption("0.2.0");
  await panel.locator("[data-dj-review]").click();
  expect(hub.writes).toHaveLength(0);
  await panel.locator("[data-dj-confirm]").click();
  const outcome = panel.locator("[data-dj-outcome]");
  await expect(outcome).toHaveAttribute("data-dj-outcome", "err");
  await expect(outcome).toContainText("BLOCKED");
  await expect(outcome).toContainText("was not promoted");
  await expect(panel.locator("[data-dj-blockers]")).toContainText(
    "healthy entity count 0 is below required 1",
  );
  expect(hub.writes[0].body).toEqual({
    release: release("0.2.0"),
    fromChannel: "DEV",
    toChannel: "STAGING",
    policy,
    evidence: [],
  });
});

test("editing a proposal cannot re-enable a capability-denied write", async ({
  page,
}) => {
  const hub: Hub = {
    operator: "alice",
    resourceOperations: ["resource:read"],
    plans: [basePlan],
    changes: [],
    promotions: [],
    writes: [],
    answer: () => null,
  };
  await hydrate(page);
  await stubHub(page, hub);
  await page.goto("/console/");
  await openArea(page, "Releases", "Environments");
  const panel = page.locator('#main-workspace [data-dj-panel="propose"]');
  const review = panel.locator("[data-dj-review]");
  const reason =
    "Your session does not hold change:propose on environment/dev.";
  await expect(panel.locator("[data-dj-capability]")).toHaveText(reason);
  await expect(review).toBeDisabled();
  await panel.locator('[data-dj-field="generation"]').fill("gen-2");
  await panel.locator('[data-dj-field="rationale"]').fill("reviewed change");
  await expect(review).toBeDisabled();
  await expect(review).toHaveAttribute("title", reason);
  await expect(panel.locator("[data-dj-confirm]")).toHaveCount(0);
  expect(hub.writes).toHaveLength(0);
});

test("a session without resource:operate is not offered a promotion, and is told why", async ({
  page,
}) => {
  const hub: Hub = {
    operator: "alice",
    globalOperations: ["resource:read"],
    plans: [basePlan],
    changes: [],
    promotions: [
      {
        id: "pro_0",
        product: "app.example:api",
        version: "0.1.0",
        fromChannel: "DEV",
        toChannel: "STAGING",
        status: "BLOCKED",
        policy: {
          minimumHealthyEntities: 1,
          maximumUnhealthyEntities: 0,
          minimumHealthyDurationSeconds: 0,
        },
      },
    ],
    writes: [],
    answer: () => null,
  };
  await hydrate(page);
  await stubHub(page, hub);
  await page.goto("/console/");
  await openArea(page, "Releases", "Promotion");
  const panel = page.locator('#main-workspace [data-dj-panel="promote"]');
  const reason =
    "Your session does not hold resource:operate on platform/root.";
  await expect(panel.locator("[data-dj-capability]")).toHaveText(reason);
  const review = panel.locator("[data-dj-review]");
  await expect(review).toBeDisabled();
  await expect(review).toHaveAttribute("title", reason);
  expect(hub.writes).toHaveLength(0);
});

for (const permission of ["denied", "allowed", "unknown"]) {
  test(`drift repair controls respect ${permission} environment capabilities`, async ({
    page,
  }) => {
    const hub: Hub = {
      operator: "alice",
      resourceOperations:
        permission === "unknown"
          ? undefined
          : permission === "allowed"
            ? ["resource:read", "change:propose"]
            : ["resource:read"],
      plans: [basePlan],
      changes: [],
      promotions: [],
      writes: [],
      installations: [
        {
          environment: "dev",
          installationId: "api-1",
          product: "app.example:api",
          desiredVersion: "2",
          observedVersion: "1",
          desiredReleaseDigest: "sha256:desired",
          observedReleaseDigest: "sha256:observed",
          desiredConfigDigest: "sha256:config",
          observedConfigDigest: "sha256:config",
          observedAt: "2026-11-17T12:00:00Z",
          stale: false,
        },
      ],
      answer: (path) =>
        path === "/v1/fleet/propose-fix"
          ? { status: 201, json: { changeRequestId: "reviewed-repair" } }
          : null,
    };
    await hydrate(page);
    await stubHub(page, hub);
    await page.goto("/console/");
    await openArea(page, "Releases", "Environments");
    const selectedRepair = page.locator("[data-release-propose-fix]");
    const reason =
      "Your session does not hold change:propose on environment/dev.";
    if (permission === "denied") {
      await expect(selectedRepair).toBeDisabled();
      await expect(selectedRepair).toHaveAttribute("title", reason);
      await expect(page.locator("[data-release-repair-capability]")).toHaveText(
        reason,
      );
    } else {
      await selectedRepair.click();
      await expect(selectedRepair).toHaveText("reviewed-repair");
    }
    // The retained fleet renderer is not currently mounted by the rail.
    // Exercise its real method as a browser component, not a navigation claim.
    await page.evaluate(() => {
      const host = document.createElement("div");
      host.id = "legacy-fleet-fixture";
      document.body.append(host);
      (
        Reflect.get(window, "state") as {
          liveHub: { renderEnvironments(node: HTMLElement): void };
        }
      ).liveHub.renderEnvironments(host);
    });
    const fleetRepair = page.locator(
      "#legacy-fleet-fixture [data-propose-fix]",
    );
    if (permission === "denied") {
      await expect(fleetRepair).toBeDisabled();
      await expect(fleetRepair).toHaveAttribute("title", reason);
      await expect(page.locator("[data-fleet-repair-capability]")).toHaveText(
        reason,
      );
      expect(hub.writes).toHaveLength(0);
    } else {
      await fleetRepair.click();
      await expect(fleetRepair).toHaveText("reviewed-repair");
      expect(hub.writes.map(({ path, body }) => ({ path, body }))).toEqual([
        {
          path: "/v1/fleet/propose-fix",
          body: { environment: "dev", installationId: "api-1" },
        },
        {
          path: "/v1/fleet/propose-fix",
          body: { environment: "dev", installationId: "api-1" },
        },
      ]);
    }
  });
}

for (const permitted of [false, true]) {
  test(`maintenance proposal ${permitted ? "uses a reviewed preview" : "states the missing environment operation"}`, async ({
    page,
  }) => {
    const hub: Hub = {
      operator: "alice",
      resourceOperations: permitted
        ? ["resource:read", "change:propose"]
        : ["resource:read"],
      plans: [
        {
          ...basePlan,
          plan: {
            environment: "dev",
            steps: [{ entity: "api", installationId: "api-1" }],
          },
        },
      ],
      changes: [],
      promotions: [],
      writes: [],
      answer: (path, body) =>
        path !== "/v1/temporary-maintenance-overrides"
          ? null
          : {
              status: 200,
              json: {
                valueTier: "CONDUIT_NATIVE",
                dryRun: body.dryRun,
                requests: [
                  {
                    changeRequestId: "maintenance-preview",
                    environment: "dev",
                    entity: "api",
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
            },
    };
    await hydrate(page);
    await stubHub(page, hub);
    await page.goto("/console/");
    await openArea(page, "Releases", "Windows");
    const panel = page.locator("[data-window-scheduler]");
    const preview = panel.locator("[data-preview-window]");
    const save = panel.locator("[data-save-window]");
    if (!permitted) {
      const reason =
        "Your session does not hold change:propose on environment/dev.";
      await expect(panel.locator("[data-window-capability]")).toHaveText(
        reason,
      );
      await expect(preview).toBeDisabled();
      await expect(preview).toHaveAttribute("title", reason);
      await expect(save).toBeDisabled();
      expect(hub.writes).toHaveLength(0);
      return;
    }
    await expect(preview).toBeEnabled();
    await panel.locator('[name="reason"]').fill("reviewed maintenance");
    await preview.click();
    await expect(panel.locator("[data-window-message]")).toContainText(
      "Preview maintenance-preview",
    );
    await expect(save).toBeDisabled();
    expect(hub.writes).toHaveLength(1);
    expect(hub.writes[0].body.dryRun).toBe(true);
    await panel.locator("[data-confirm-window]").check();
    await save.click();
    await expect(panel.locator("[data-window-message]")).toContainText(
      "Change request maintenance-preview",
    );
    expect(hub.writes).toHaveLength(2);
    expect(hub.writes[1].body).toMatchObject({
      dryRun: false,
      expectedChangeRequestId: "maintenance-preview",
    });
  });
}

const planningSession = {
  id: "ps_linked",
  status: "ready",
  profile: { name: "onboard", version: "v1", hash: "h" },
  intake: { repoUrl: "https://example.test/org/app", ref: "main" },
  needsInput: [],
  answers: [],
  messages: [],
  events: [],
  plan: null,
  binding: null,
};

async function stubPlanning(page: Page, found: boolean) {
  await page.route("**/v1/planning-sessions?*", (route) =>
    route.fulfill({
      json: {
        items: found
          ? [
              {
                id: "ps_linked",
                status: "ready",
                profile: { name: "onboard" },
                repoUrl: "https://example.test/org/app",
                createdAt: "2026-01-01T00:00:00Z",
                updatedAt: "2026-01-01T00:00:00Z",
              },
            ]
          : [],
        total: found ? 1 : 0,
        pageSize: found ? 1 : 0,
        limit: 100,
        valueTier: "CONDUIT_NATIVE",
        hasPrevious: false,
      },
    }),
  );
  await page.route("**/v1/app-generations", (route) =>
    route.fulfill({
      json: {
        items: [
          {
            id: "gen_1",
            name: "orders",
            gates: [{ name: "build", outcome: "pass", reason: "" }],
          },
        ],
      },
    }),
  );
  await page.route("**/v1/planning-sessions/**", (route) => {
    const path = new URL(route.request().url()).pathname;
    if (!found)
      return route.fulfill({
        status: 404,
        json: {
          code: "SESSION_NOT_FOUND",
          error: "planning session was not found",
        },
      });
    if (path.endsWith("/events"))
      return route.fulfill({
        json: {
          items: [
            {
              id: "e1",
              type: "message",
              timestamp: "2026-01-01T00:00:00Z",
              message: { role: "model", text: "Inspected the repository." },
            },
          ],
        },
      });
    return route.fulfill({ json: planningSession });
  });
}

test("a planning deep link rehydrates the session, and Stop aborts the stream", async ({
  page,
}) => {
  await hydrate(page);
  await stubHub(page, {
    operator: "alice",
    plans: [],
    changes: [],
    promotions: [],
    writes: [],
    answer: () => null,
  });
  await stubPlanning(page, true);
  // A run that never finishes, so Stop has a stream to abort.
  let runs = 0;
  let activeRun = "";
  let cancellations = 0;
  await page.route("**/v1/agent-runs", (route) => {
    runs += 1;
    activeRun = route.request().postDataJSON().runId;
  });
  await page.route(
    "**/v1/planning-sessions/ps_linked/turns/*/cancel",
    async (route) => {
      expect(
        decodeURIComponent(
          new URL(route.request().url()).pathname.split("/").at(-2)!,
        ),
      ).toBe(activeRun);
      expect(route.request().postDataJSON()).toEqual({});
      cancellations += 1;
      await route.fulfill({
        json: {
          runId: activeRun,
          state: cancellations === 1 ? "cancellation_requested" : "cancelled",
          valueTier: "CONDUIT_NATIVE",
        },
      });
    },
  );
  await page.goto("/console/#s01-intake/ps_linked");
  const flow = page.locator("#main-workspace [data-plan-flow]");
  await expect(flow.locator("[data-plan-composer]")).toContainText("ps_linked");
  await expect(flow).toContainText("Inspected the repository.");
  await expect(page.getByLabel("Repository URL")).toBeDisabled();
  await expect(page).toHaveURL(/#s01-intake\/ps_linked$/);
  // Both inventories come from owner-scoped native reads.
  await expect(flow.locator('[data-plan-generation="gen_1"]')).toContainText(
    "gates passed",
  );
  await expect(flow.locator('[data-plan-session="ps_linked"]')).toContainText(
    "ready",
  );
  await expect(flow.locator("[data-plan-sessions-note]")).toContainText(
    "history retention follows Hub configuration",
  );

  const aborted = page.waitForEvent("requestfailed", (request) =>
    request.url().endsWith("/v1/agent-runs"),
  );
  await page.getByLabel("Message", { exact: true }).fill("Continue");
  await page.locator("[data-plan-send]").click();
  await expect(page.locator("[data-plan-stop]")).toBeVisible();
  await expect.poll(() => runs).toBe(1);
  await page.locator("[data-plan-stop]").click();
  await aborted;
  await expect(page.locator("[data-plan-stopped]")).toContainText(
    "not yet confirmed",
  );
  await expect(page.locator("[data-plan-stop]")).toHaveCount(0);
  await expect(page.locator("[data-plan-error]")).toHaveCount(0);
  await expect(page.locator("[data-plan-send]")).toBeEnabled();
  expect(runs).toBe(1);
  await page.locator("[data-plan-cancel-check]").click();
  await expect(page.locator("[data-plan-stopped]")).toContainText(
    "confirmed this planning turn is cancelled",
  );
  await expect(page.locator("[data-plan-cancel-check]")).toHaveCount(0);
  expect(cancellations).toBe(2);

  // The link survives leaving the screen and coming back.
  await page.evaluate(() =>
    (Reflect.get(window, "switchScreen") as (id: string) => void)("area-fleet"),
  );
  await expect(page).not.toHaveURL(/ps_linked/);
  await page.evaluate(() =>
    (Reflect.get(window, "switchScreen") as (id: string) => void)("s01-intake"),
  );
  await expect(page).toHaveURL(/#s01-intake\/ps_linked$/);
});

test("planning history pages native summaries and opens the selected session", async ({
  page,
}) => {
  await hydrate(page);
  await stubHub(page, {
    operator: "alice",
    plans: [],
    changes: [],
    promotions: [],
    writes: [],
    answer: () => null,
  });
  await stubPlanning(page, true);
  const rows = Array.from({ length: 101 }, (_, index) => ({
    id: `ps_${String(index).padStart(3, "0")}`,
    status: "ready",
    profile: { name: "onboard" },
    repoUrl: "https://example.test/org/app",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
  }));
  const writes: string[] = [];
  page.on("request", (request) => {
    if (
      request.method() !== "GET" &&
      new URL(request.url()).pathname.startsWith("/v1/planning-sessions")
    )
      writes.push(request.method());
  });
  await page.route("**/v1/planning-sessions?*", (route) => {
    const query = new URL(route.request().url()).searchParams;
    expect(query.get("limit")).toBe("100");
    expect(query.has("owner")).toBe(false);
    const second = query.get("cursor") === "cHNfMDk5";
    return route.fulfill({
      json: {
        items: second ? rows.slice(100) : rows.slice(0, 100),
        total: 101,
        pageSize: second ? 1 : 100,
        limit: 100,
        valueTier: "CONDUIT_NATIVE",
        hasPrevious: second,
        ...(second ? {} : { nextCursor: "cHNfMDk5" }),
      },
    });
  });
  await page.route("**/v1/planning-sessions/ps_100", (route) =>
    route.fulfill({ json: { ...planningSession, id: "ps_100" } }),
  );
  await page.goto("/console/#s01-intake");
  const flow = page.locator("#main-workspace [data-plan-flow]");
  const list = flow.locator("[data-plan-sessions]");
  await expect(list.locator("[data-plan-session]")).toHaveCount(100);
  await expect(list).toContainText("100 shown · 101 available");
  await expect(
    list.getByRole("button", { name: "Previous", exact: true }),
  ).toBeDisabled();
  await list.getByRole("button", { name: "Next", exact: true }).click();
  await expect(list.locator("[data-plan-session]")).toHaveCount(1);
  await expect(list.locator('[data-plan-session="ps_100"]')).toBeVisible();
  await expect(
    list.getByRole("button", { name: "Next", exact: true }),
  ).toBeDisabled();
  await list.getByRole("button", { name: "Previous", exact: true }).click();
  await expect(list.locator("[data-plan-session]")).toHaveCount(100);
  await list.getByRole("button", { name: "Next", exact: true }).click();
  await list.locator('[data-plan-session="ps_100"] a').click();
  await expect(page).toHaveURL(/#s01-intake\/ps_100$/);
  await expect(flow.locator("[data-plan-composer]")).toContainText("ps_100");
  expect(writes).toEqual([]);
});

test("a link to a session the Hub no longer has says so", async ({ page }) => {
  await hydrate(page);
  await stubHub(page, {
    operator: "alice",
    plans: [],
    changes: [],
    promotions: [],
    writes: [],
    answer: () => null,
  });
  await stubPlanning(page, false);
  await page.goto("/console/#s01-intake/ps_gone");
  const lost = page.locator('#main-workspace [data-plan-link="lost"]');
  await expect(lost).toContainText(
    "was not found on this Hub (it may have restarted)",
  );
  await expect(page.locator("[data-plan-send]")).toHaveCount(0);
  await lost.locator("[data-plan-link-clear]").click();
  await expect(page.locator("[data-plan-send]")).toBeVisible();
  await expect(page).toHaveURL(/#s01-intake$/);
});

// Reach controls through the browser tab order; never focus/click them directly.
async function keyboardReach(page: Page, target: Locator) {
  await expect(target).toBeVisible();
  for (let attempt = 0; attempt < 100; attempt++) {
    // Settle dialog restoration and inspect focus in the same browser call.
    // Keep every movement a real key, without round trips for each DOM read.
    const key = await target.evaluate(
      (element) =>
        new Promise<"Tab" | "Shift+Tab" | null>((resolve) => {
          requestAnimationFrame(() => {
            if (element === document.activeElement) {
              resolve(null);
              return;
            }
            // Stay inside the document instead of browser-chrome Tab wrap.
            resolve(
              (document.activeElement?.compareDocumentPosition(element) ?? 0) &
                Node.DOCUMENT_POSITION_PRECEDING
                ? "Shift+Tab"
                : "Tab",
            );
          });
        }),
    );
    if (key === null) return;
    await page.keyboard.press(key);
  }
  await expect(target).toBeFocused();
}
async function keyboardActivate(page: Page, target: Locator) {
  await keyboardReach(page, target);
  await page.keyboard.press("Enter");
}
async function keyboardArea(page: Page, area: string, tab: string) {
  await keyboardActivate(
    page,
    page
      .locator("#left-nav")
      .getByRole("button", { name: area, exact: true })
      .first(),
  );
  await keyboardActivate(
    page,
    page
      .locator("#main-workspace")
      .getByRole("button", { name: tab, exact: true })
      .first(),
  );
}

test("keyboard-only proposal and independent approval retain both confirmations", async ({
  page,
}) => {
  test.setTimeout(90_000);
  const hub: Hub = {
    operator: "alice",
    plans: [basePlan],
    changes: [],
    promotions: [],
    writes: [],
    answer: (path, body) => {
      if (path === "/v1/plans") {
        const created = { ...awaitingPlan, desiredState: body };
        hub.plans = [created, basePlan];
        hub.changes = [changeRequest("alice")];
        return { status: 201, json: created };
      }
      if (path === "/conduit.plans.v1.PlanApprovalService/ApprovePlan") {
        hub.plans = [{ ...awaitingPlan, status: "APPROVED" }, basePlan];
        return {
          status: 200,
          json: approvalReceipt("pln_new", "APPROVED", "bob", body.rationale),
        };
      }
      return null;
    },
  };
  await hydrate(page);
  await stubHub(page, hub);
  await page.goto("/console/");
  const search = page.getByRole("button", {
    name: "Search views",
    exact: true,
  });
  await keyboardReach(page, search);
  await expect(
    page.getByRole("dialog", { name: "Command palette" }),
  ).toHaveCount(0);
  await page.keyboard.press("Enter");
  await expect(
    page.getByRole("dialog", { name: "Command palette" }),
  ).toBeVisible();
  await page.keyboard.press("Escape");
  await keyboardArea(page, "Releases", "Environments");
  const propose = page.locator('[data-dj-panel="propose"]');
  await keyboardReach(page, propose.locator('[data-dj-field="generation"]'));
  await page.keyboard.press("ControlOrMeta+A");
  await page.keyboard.insertText("gen-2");
  await keyboardReach(page, propose.locator('[data-dj-field="rationale"]'));
  await page.keyboard.insertText("keyboard proposal");
  await keyboardActivate(page, propose.locator("[data-dj-review]"));
  await expect(propose.locator("[data-dj-request]")).toContainText(
    "POST /v1/plans",
  );
  expect(hub.writes).toHaveLength(0);
  await keyboardActivate(page, propose.locator("[data-dj-confirm]"));
  await expect(propose.locator("[data-dj-outcome]")).toContainText("pln_new");
  expect(hub.writes).toHaveLength(1);
  expect(hub.writes[0].body.environment.generation).toBe("gen-2");
  const done = propose.getByRole("button", { name: "Done", exact: true });
  await keyboardReach(page, done);
  // A completed Hub refresh must not strand the keyboard's tab starting point.
  await page.evaluate(() => Reflect.get(window, "renderMainWorkspace")());
  await expect(done).toBeFocused();

  await keyboardArea(page, "Change requests", "Approvals");
  const decide = page.locator('[data-dj-panel="decide"]');
  await expect(
    decide.getByRole("button", { name: "Approve", exact: true }),
  ).toBeDisabled();
  await expect(decide).toContainText(
    "Another person must approve or reject it.",
  );

  // A fresh fixture session for the independent reviewer, not UI impersonation.
  hub.operator = "bob";
  await page.reload();
  await expect(decide).toContainText("pln_new");
  await keyboardReach(page, decide.locator('[data-dj-field="rationale"]'));
  await page.keyboard.insertText("keyboard independent review");
  await keyboardActivate(page, decide.locator('[data-dj-review="approve"]'));
  await expect(decide.locator("[data-dj-request]")).toContainText(
    "POST /conduit.plans.v1.PlanApprovalService/ApprovePlan",
  );
  expect(hub.writes).toHaveLength(1);
  await keyboardActivate(page, decide.locator("[data-dj-confirm]"));
  await expect(decide.locator("[data-dj-outcome]")).toContainText(
    "pln_new is now APPROVED.",
  );
  expect(hub.writes).toHaveLength(2);
  expect(hub.writes[1]).toEqual(
    expect.objectContaining({
      path: "/conduit.plans.v1.PlanApprovalService/ApprovePlan",
      body: { planId: "pln_new", rationale: "keyboard independent review" },
    }),
  );
});

for (const failure of ["unknown", "recorded", "handoff-failed"]) {
  test(`approval ${failure} outcome blocks replay until an explicit reload`, async ({
    page,
  }) => {
    const known = failure !== "unknown";
    const outcome = create(PlanApprovalOutcomeSchema, {
      planId: "pln_new",
      approvalRecorded: known,
      handoffFailed: failure === "handoff-failed",
    });
    const hub: Hub = {
      operator: "bob",
      plans: [awaitingPlan, basePlan],
      changes: [changeRequest("alice")],
      promotions: [],
      writes: [],
      answer: (path) =>
        path === "/conduit.plans.v1.PlanApprovalService/ApprovePlan"
          ? {
              status: 503,
              json: {
                code: "unavailable",
                message: "sanitized failure",
                details: known
                  ? [
                      {
                        type: PlanApprovalOutcomeSchema.typeName,
                        value: Buffer.from(
                          toBinary(PlanApprovalOutcomeSchema, outcome),
                        ).toString("base64"),
                      },
                    ]
                  : [],
              },
            }
          : null,
    };
    const restApprovals: string[] = [];
    page.on("request", (request) => {
      if (
        request.method() === "POST" &&
        /\/v1\/plans\/[^/]+\/approvals$/.test(new URL(request.url()).pathname)
      )
        restApprovals.push(request.url());
    });
    await hydrate(page);
    await stubHub(page, hub);
    await page.goto("/console/");
    await openArea(page, "Change requests", "Approvals");
    const decide = page.locator('[data-dj-panel="decide"]');
    await decide
      .locator('[data-dj-field="rationale"]')
      .fill("reviewed exact target");
    await decide.locator('[data-dj-review="approve"]').click();
    expect(hub.writes).toHaveLength(0);
    await decide.locator("[data-dj-confirm]").click();
    const message = decide.locator("[data-dj-uncertain]");
    await expect(message).toHaveAttribute(
      "data-dj-approval-recorded",
      String(known),
    );
    await expect(message).toContainText(
      known ? "Approval was recorded" : "outcome is unknown",
    );
    if (failure === "handoff-failed")
      await expect(message).toContainText("execution handoff failed");
    await expect(message).not.toContainText("did not accept");
    await expect(decide.locator('[data-dj-field="rationale"]')).toBeDisabled();
    await expect(decide.locator("[data-dj-review]")).toHaveCount(0);
    await page.evaluate(() => Reflect.get(window, "renderMainWorkspace")());
    await expect(message).toBeVisible();
    await openArea(page, "Releases", "Plans");
    await page
      .locator("#main-workspace [data-release-plan]")
      .selectOption("pln_new");
    await expect(message).toBeVisible();
    await expect(message).toContainText("pln_new");
    await expect(decide.locator("[data-dj-review]")).toHaveCount(0);
    expect(hub.writes).toHaveLength(1);
    expect(hub.writes[0]?.body).toEqual({
      planId: "pln_new",
      rationale: "reviewed exact target",
    });
    expect(hub.writes[0]?.headers["x-conduit-csrf"]).toBe("1");
    await Promise.all([
      page.waitForEvent("load"),
      message.getByRole("button", { name: "Reload Plan state" }).click(),
    ]);
    await openArea(page, "Change requests", "Approvals");
    await expect(decide.locator("[data-dj-uncertain]")).toHaveCount(0);
    expect(hub.writes).toHaveLength(1);
    expect(restApprovals).toEqual([]);
  });
}

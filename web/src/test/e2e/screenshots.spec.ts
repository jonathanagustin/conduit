import { installation } from "../fixtures.js";
import { expect, indexPage, test } from "./harness.js";

/** Full-page captures of every console screen, for design review.
 *
 * Not assertions -- this spec exists to produce images. It is skipped unless
 * CONSOLE_SCREENSHOTS is set, so the ordinary suite stays fast and a review
 * capture is always deliberate.
 *
 * The fixtures here are richer than the behavioural specs' on purpose: a review
 * of spacing, density and hierarchy needs a screen with enough on it to have
 * hierarchy. They remain synthetic and Hub-shaped -- no upstream data. */

const CAPTURE = process.env["CONSOLE_SCREENSHOTS"] !== undefined;

test.skip(!CAPTURE, "set CONSOLE_SCREENSHOTS=1 to capture");

// 1440x1024 is the viewport the plan names for its wireframes, so a capture can
// be compared against them without rescaling.
test.use({ viewport: { width: 1440, height: 1024 } });

const FLEET = [
  installation({ environment: "prod-use1", installationId: "payments", product: "app.example:payments" }),
  installation({
    environment: "prod-use1",
    installationId: "search",
    product: "app.example:search",
    desiredVersion: "8.1.0",
    observedVersion: "8.1.0",
    configStatus: "DRIFTED",
  }),
  installation({
    environment: "prod-euw1",
    installationId: "ledger",
    product: "app.example:ledger",
    desiredVersion: "1.9.0",
    observedVersion: "1.8.2",
    health: "UNHEALTHY",
    rolloutStatus: "OUT_OF_DATE",
  }),
  installation({
    environment: "dev-use2",
    installationId: "reporting",
    product: "app.example:reporting",
    desiredVersion: "0.4.0",
    observedVersion: "0.3.0",
    health: "UNKNOWN",
    rolloutStatus: "OUT_OF_DATE",
    observationAgeSeconds: 7200,
    stale: true,
  }),
];

const CHANGE_REQUEST = {
  id: "crq_upgrade",
  planId: "pln_deploy",
  environment: "prod-use1",
  title: "Upgrade payments to 2.4.1",
  description: "Rolls the payments Entity forward one patch release.",
  author: { username: "a.okonkwo" },
  status: "PENDING_APPROVALS",
  requirementsSatisfied: false,
  changes: [
    {
      id: "chg-1",
      type: "UPDATE_ENTITY",
      installationId: "payments",
      diff: {
        version: { before: "2.4.0", after: "2.4.1" },
        releaseDigest: { before: "sha256:9f1c", after: "sha256:be27" },
      },
    },
  ],
  evaluations: [
    { changeId: "chg-1", policyId: "production-review", outcome: "REQUIRE_TEAM", reason: "production Environment" },
    { changeId: "chg-1", policyId: "security-scan", outcome: "APPROVE", reason: "assessment passed" },
  ],
  requirements: [
    { id: "req-1", team: "sre", reason: "production change", satisfiedBy: [] },
    { id: "req-2", team: "security", reason: "image assessment", satisfiedBy: ["s.mehta"] },
  ],
  reviews: [{ subject: "s.mehta", decision: "APPROVE", rationale: "assessment is clean" }],
  createdAt: "2026-08-07T12:00:00Z",
  updatedAt: "2026-08-07T12:00:00Z",
  revision: 1,
};

const PLAN = {
  id: "pln_deploy",
  desiredStateDigest: "sha256:aaa",
  plan: {
    environment: "prod-use1",
    generation: "0123456789abcdef",
    steps: [
      { position: 1, action: "RECONCILE", installationId: "payments", entity: "payments", product: "app.example:payments", version: "2.4.1", namespace: "payments" },
      { position: 2, action: "RECONCILE", installationId: "search", entity: "search", product: "app.example:search", version: "8.1.0", namespace: "search" },
    ],
  },
  status: "FAILED",
  requiredApprovals: 2,
  approvals: [{ subject: "s.mehta", rationale: "assessment is clean" }],
  failure: "helm upgrade failed",
  failedInstallationId: "payments",
  execution: {
    startedAt: "2026-08-07T12:00:00Z",
    finishedAt: "2026-08-07T12:04:00Z",
    tasks: [
      { sequence: 1, phase: "VALIDATE", status: "SUCCEEDED" },
      {
        sequence: 2,
        phase: "APPLY",
        status: "FAILED",
        installationId: "payments",
        failure: "0/3 nodes are available: 3 Insufficient memory.",
        agentOutput: "Error: UPGRADE FAILED",
        resources: [
          {
            apiVersion: "apps/v1",
            kind: "Deployment",
            namespace: "payments",
            name: "payments-api",
            reason: "ProgressDeadlineExceeded",
            message: "ReplicaSet did not become ready before the deadline.",
          },
          {
            apiVersion: "v1",
            kind: "Pod",
            namespace: "payments",
            name: "payments-api-7f6d9",
            reason: "Unschedulable",
            message: "0/3 nodes are available: 3 Insufficient memory.",
          },
        ],
      },
      { sequence: 3, phase: "OBSERVE", status: "SUCCEEDED" },
    ],
  },
  createdAt: "2026-08-07T12:00:00Z",
  updatedAt: "2026-08-07T12:05:00Z",
  revision: 3,
};

const ENVIRONMENT = {
  environment: { id: "prod-use1", generation: "0123456789abcdef", connectionSetting: "DIRECT", defaultReleaseChannel: "RELEASE", declaredStateSource: "COPY_OF_ANOTHER_HUB" },
  reported: {
    environment: "prod-use1",
    agentId: "agent-prod",
    observedAt: "2026-08-07T12:00:00Z",
    entities: [
      { installationId: "payments", product: "app.example:payments", version: "2.4.1", health: "HEALTHY", lifecycle: "INSTALLED" },
      { installationId: "search", product: "app.example:search", version: "8.1.0", health: "HEALTHY", lifecycle: "INSTALLED" },
    ],
  },
};

const PROMOTIONS = [
  { id: "prm_1", product: "app.example:payments", version: "2.4.1", fromChannel: "RELEASE", toChannel: "STAGING", status: "PASSED" },
  {
    id: "prm_2", product: "app.example:ledger", version: "1.9.0", fromChannel: "RELEASE", toChannel: "STAGING", status: "BLOCKED",
    blockers: [
      { kind: "HEALTHY_ENTITIES_BELOW_MINIMUM", message: "2 of 5 Entities are healthy", subjects: ["prod-use1", "prod-euw1"] },
      { kind: "MINIMUM_HEALTHY_DURATION_NOT_MET", message: "healthy for 4h of the required 48h" },
    ],
  },
];

const RELEASES = [
  { product: "app.example:payments", version: "2.4.1", chart: { repository: "registry.example/payments", digest: "sha256:be27f1a0c3" }, images: [], channels: ["RELEASE"], vulnerabilityAssessment: { outcome: "PASS", activeVulnerabilities: 0 } },
  { product: "app.example:payments", version: "2.4.2", chart: { repository: "registry.example/payments", digest: "sha256:0a91cc72d4" }, images: [], channels: ["STAGING"] },
];

function seed(hub: Parameters<Parameters<typeof test>[2]>[0]["hub"]) {
  hub.fleet(FLEET);
  hub.get("/overview", {
    generatedAt: "2026-08-09T12:00:00Z",
    awaitingApprovals: {
      status: "CURRENT",
      count: 1,
      items: [{ id: CHANGE_REQUEST.id, title: CHANGE_REQUEST.title, environment: CHANGE_REQUEST.environment }],
    },
    failedPlans: { status: "CURRENT", count: 0, items: [] },
    runningPlans: { status: "CURRENT", count: 0, items: [] },
    unhealthyEntities: {
      status: "CURRENT",
      count: 1,
      items: [{ environment: "prod-use1", installationId: "ledger", health: "UNHEALTHY" }],
    },
    staleReports: { status: "CURRENT", count: 1, items: [] },
    blockedPromotions: {
      status: "CURRENT",
      count: 1,
      items: [{ id: "prm_2", product: "app.example:ledger", version: "1.9.0", fromChannel: "RELEASE", toChannel: "STAGING" }],
    },
  });
  hub.get("/change-requests", indexPage("changeRequests", [CHANGE_REQUEST]));
  hub.get("/change-requests/crq_upgrade", CHANGE_REQUEST);
  hub.get("/plans", { plans: [{ id: PLAN.id, environment: PLAN.plan.environment, status: PLAN.status,
    stepCount: PLAN.plan.steps.length, approvalCount: PLAN.approvals.length,
    requiredApprovals: PLAN.requiredApprovals, createdAt: PLAN.createdAt,
    updatedAt: PLAN.updatedAt, revision: PLAN.revision }], total: 1, pageSize: 1, limit: 100, hasPrevious: false });
  hub.get("/plans/pln_deploy", PLAN);
  hub.get("/environments", {
    environments: [{ environment: ENVIRONMENT.environment, reported: {
      environment: "prod-use1", observedAt: ENVIRONMENT.reported.observedAt, entityCount: 1,
    } }], total: 1, pageSize: 1, limit: 100, hasPrevious: false,
  });
  hub.get("/environments/prod-use1", ENVIRONMENT);
  hub.get("/environments/prod-use1/commands", []);
  hub.get("/products", {
    products: [{ product: "app.example:payments", releaseCount: RELEASES.length, latestVersion: "2.4.2" }],
    total: 1, pageSize: 1, limit: 100, hasPrevious: false,
  });
  hub.get("/products/app.example:payments/releases", { releases: RELEASES });
  hub.get("/promotions", indexPage("promotions", PROMOTIONS));
  hub.get("/notifications/inbox", {
    owner: { kind: "USER", id: "a.okonkwo" },
    channel: "IN_APP",
    limit: 100,
    entries: NOTIFICATIONS,
  });
  hub.get("/notifications/preferences", PREFERENCE);
  hub.get("/teams", { teams: [PAYMENTS_TEAM] });
  hub.get("/teams/payments", PAYMENTS_TEAM);
  hub.get("/notifications/contacts", {
    owner: { kind: "TEAM", id: "payments" },
    contacts: TEAM_CONTACTS,
  });
  hub.get("/environments/prod-use1/profile", ENVIRONMENT_PROFILE);
}

const PAYMENTS_TEAM = {
  name: "payments",
  displayName: "Payments",
  description: "Owns the payments Product line.",
  adminUsers: ["a.okonkwo"],
  adminGroups: ["payments-leads"],
  memberUsers: ["s.mehta", "j.ruiz"],
  memberGroups: ["payments-engineers"],
  labels: { costCenter: "cc-42" },
  createdAt: "2026-08-01T09:00:00Z",
  updatedAt: "2026-08-07T12:00:00Z",
  revision: 3,
};

const TEAM_CONTACTS = [
  { id: "c-1", owner: { kind: "TEAM", id: "payments" }, kind: "EMAIL", address: "payments-oncall@example.test", optIntoFutureResources: true, createdAt: "2026-08-01T09:00:00Z" },
  { id: "c-2", owner: { kind: "TEAM", id: "payments" }, kind: "SLACK", address: "#payments-alerts", createdAt: "2026-08-01T09:00:00Z" },
  { id: "c-3", owner: { kind: "TEAM", id: "payments" }, kind: "PAGERDUTY", address: "PDABC123", createdAt: "2026-08-01T09:00:00Z" },
];

const ENVIRONMENT_PROFILE = {
  environmentId: "prod-use1",
  displayName: "Production — US East",
  description: "Primary revenue-serving Environment for the payments product.",
  ownerTeam: "payments",
  readme:
    "# Runbook\n\nPage the **on-call** before any change. See the [dashboards](https://example.test/dashboards).\n\n- Health checks run every 30s\n- Rollbacks are automatic on a failed canary\n\n```\nkubectl -n payments get pods\n```",
};

const NOTIFICATIONS = [
  {
    recipient: { owner: { kind: "USER", id: "a.okonkwo" }, channel: "IN_APP" },
    event: {
      id: "nev-1",
      type: "PLAN_FAILURES",
      resourceRid: "ri.apollo.main.environment.d41d8cd98f00",
      environment: "prod-use1",
      summary: "Reconciliation of installation payments in Environment prod-use1 gave up",
      actor: "a.okonkwo",
      occurredAt: "2026-08-09T11:42:00Z",
    },
    preferenceId: "npf-1",
  },
  {
    recipient: { owner: { kind: "USER", id: "a.okonkwo" }, channel: "IN_APP" },
    event: {
      id: "nev-2",
      type: "PRODUCT_RELEASE_PROMOTION_FAILED",
      resourceRid: "ri.apollo.main.product.7c9a2be40155",
      summary: "app.example:ledger 1.9.0 could not be promoted to STAGING",
      occurredAt: "2026-08-09T09:15:00Z",
    },
    preferenceId: "npf-2",
  },
];

const PREFERENCE = {
  id: "npf-1",
  owner: { kind: "USER", id: "a.okonkwo" },
  resourceRid: "ri.apollo.main.environment.d41d8cd98f00",
  channel: "IN_APP",
  types: ["PLAN_FAILURES"],
  createdAt: "2026-08-01T08:00:00Z",
  updatedAt: "2026-08-08T08:00:00Z",
  revision: 2,
};

const SCREENS: Array<{ name: string; path: string; ready: string }> = [
  { name: "01-overview", path: "overview", ready: "overview-page" },
  { name: "02-fleet", path: "fleet", ready: "fleet-page" },
  { name: "03-environments", path: "environments", ready: "environments-page" },
  { name: "04-environment-detail", path: "environments/prod-use1", ready: "environment-detail" },
  { name: "05-plans", path: "plans", ready: "plans-page" },
  { name: "06-plan-detail", path: "plans/pln_deploy", ready: "plan-detail" },
  { name: "07-change-requests", path: "change-requests", ready: "change-requests-page" },
  { name: "08-change-request-detail", path: "change-requests/crq_upgrade", ready: "change-request-detail" },
  { name: "09-products", path: "products", ready: "products-page" },
  { name: "10-product-detail", path: "products/app.example%3Apayments", ready: "product-detail" },
  { name: "11-promotions", path: "promotions", ready: "promotions-page" },
  {
    name: "12-notifications",
    path: "notifications?resourceRid=ri.apollo.main.environment.d41d8cd98f00",
    ready: "notifications-page",
  },
  { name: "13-team-detail", path: "teams/payments", ready: "team-detail" },
];

for (const screen of SCREENS) {
  test(`capture ${screen.name}`, async ({ page, hub }) => {
    seed(hub);
    await page.goto(screen.path);
    await expect(page.getByTestId(screen.ready)).toBeVisible();
    await page.screenshot({ path: `screenshots/${screen.name}.png`, fullPage: true });
  });
}

// The team's Contacts tab is a click past the detail landing, so it gets its
// own capture with the tab opened.
test("capture 14-team-contacts", async ({ page, hub }) => {
  seed(hub);
  await page.goto("teams/payments");
  await expect(page.getByTestId("team-detail")).toBeVisible();
  await page.getByRole("tab", { name: "Contacts" }).click();
  await expect(page.getByTestId("team-contacts")).toBeVisible();
  await page.screenshot({ path: "screenshots/14-team-contacts.png", fullPage: true });
});

test("capture 15-team-notifications", async ({ page, hub }) => {
  seed(hub);
  // The team inbox is owner-scoped; override the personal inbox the seed set
  // with a team-owned one so the capture shows a team's notifications.
  hub.get("/notifications/inbox", {
    owner: { kind: "TEAM", id: "payments" },
    channel: "IN_APP",
    limit: 100,
    entries: [
      {
        recipient: { owner: { kind: "TEAM", id: "payments" }, channel: "IN_APP" },
        event: { id: "tev-1", type: "PLAN_FAILURES", resourceRid: "ri.apollo.main.environment.d41d8cd98f00", environment: "prod-use1", summary: "Reconciliation of installation payments in prod-use1 gave up", occurredAt: "2026-08-09T11:42:00Z" },
        preferenceId: "tpf-1",
      },
      {
        recipient: { owner: { kind: "TEAM", id: "payments" }, channel: "IN_APP" },
        event: { id: "tev-2", type: "PRODUCT_RELEASE_PROMOTION_FAILED", resourceRid: "ri.apollo.main.product.7c9a2be40155", summary: "app.example:ledger 1.9.0 could not be promoted to STAGING", occurredAt: "2026-08-09T09:15:00Z" },
        preferenceId: "tpf-2",
      },
    ],
  });
  await page.goto("teams/payments");
  await expect(page.getByTestId("team-detail")).toBeVisible();
  await page.getByRole("tab", { name: "Notifications" }).click();
  await expect(page.getByTestId("team-notifications")).toBeVisible();
  await page.screenshot({ path: "screenshots/15-team-notifications.png", fullPage: true });
});

test("capture 16-empty-state", async ({ page, hub }) => {
  // An empty browse list shows the Foundry-UI-derived EmptyState rather than a
  // bare paragraph.
  hub.get("/environments", { environments: [], total: 0, pageSize: 0, limit: 100, hasPrevious: false });
  await page.goto("environments");
  await expect(page.getByTestId("environments-page")).toBeVisible();
  await page.screenshot({ path: "screenshots/16-empty-state.png", fullPage: true });
});

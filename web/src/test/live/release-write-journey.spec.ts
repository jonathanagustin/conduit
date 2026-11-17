import {
  expect,
  type APIRequestContext,
  type Page,
  test,
} from "@playwright/test";

// The delivery journey's writes, walked against a real Hub: one person
// proposes a Plan in the browser, is refused their own approval, and a second
// person approves it; then a promotion is evaluated and the Hub blocks it.
// Nothing here is stubbed. Every assertion about a write is made twice: on
// what the console painted, and on what the Hub holds afterwards.
//
// scripts/simulate-console.sh seeds the Hub as "proposer" and the browser acts
// as "operator" (playwright.live.config.ts). Here "operator" proposes, so the
// approver is "proposer", in a second browser context with its own identity.

test.describe.configure({ mode: "serial" });

type PlanRecord = {
  id: string;
  status: string;
  changeRequestId?: string;
  createdAt: string;
  approvals?: { subject: string }[];
  plan: { environment: string };
  desiredState: {
    environment: { id: string; generation: string };
    releases: Record<string, any>[];
    entities: { name: string; product: string; version: string }[];
  };
};

const ENVIRONMENT = "dev-use2";
// example-workspace depends on example-api below 0.2.0, so moving the
// workspace is a change the Hub accepts and moving the API is one it refuses.
const ENTITY = "example-workspace";
const PRODUCT = "app.example:example-workspace";
const REFUSED_ENTITY = "example-api";
const API_PRODUCT = "app.example:example-api";
const TARGET = "0.2.0";
const stamp = Date.now().toString(16);

function hubURL(page: Page, path: string) {
  return new URL(path, page.url()).toString();
}

async function read<T>(page: Page, path: string): Promise<T> {
  const response = await page.request.get(hubURL(page, path));
  expect(response.ok(), `GET ${path}`).toBe(true);
  return (await response.json()) as T;
}

async function send(
  request: APIRequestContext,
  url: string,
  method: "POST" | "PUT",
  subject: string,
  data: unknown,
) {
  const response = await request.fetch(url, {
    method,
    data,
    headers: {
      "content-type": "application/json",
      "X-Platform-Subject": subject,
    },
  });
  expect(
    response.ok(),
    `${method} ${url} as ${subject}: ${response.status()} ${await response.text()}`,
  ).toBe(true);
  return response.json();
}

async function openWorkbench(page: Page) {
  await page.goto("./");
  await expect(page.locator("#left-nav")).toBeVisible();
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

const plans = async (page: Page) =>
  (await read<{ plans: PlanRecord[] }>(page, "/v1/plans")).plans;

let proposedPlanId = "";

test("a plan is proposed in the console and the author cannot approve it", async ({
  page,
}) => {
  await openWorkbench(page);

  // Arrange through the Hub's own API. The approver must be able to satisfy
  // the seeded policy, and the catalog must list a second release to move to.
  // The team is revisioned: the update is the stored record with one more
  // member, not a fresh document.
  const team = await read<Record<string, unknown>>(page, "/v1/teams/reviewers");
  await send(
    page.request,
    hubURL(page, "/v1/teams/reviewers"),
    "PUT",
    "operator",
    { ...team, memberUsers: ["operator", "proposer"] },
  );
  const seeded = (await plans(page)).find(
    (plan) => plan.desiredState?.environment?.id === ENVIRONMENT,
  );
  expect(seeded, "the seeded plan").toBeTruthy();
  const retag = (artifact: Record<string, any>) => ({
    ...artifact,
    tag: TARGET,
  });
  const next = seeded!.desiredState.releases.map((current) => ({
    ...current,
    version: TARGET,
    chart: retag(current["chart"]),
    images: (current["images"] as Record<string, any>[]).map(retag),
  }));
  await send(page.request, hubURL(page, "/v1/plans"), "POST", "proposer", {
    ...seeded!.desiredState,
    environment: {
      ...seeded!.desiredState.environment,
      generation: `live-catalog-${stamp}`,
    },
    releases: [...seeded!.desiredState.releases, ...next],
  });
  const catalog = await read<{
    products: { product: string; releases: { version: string }[] }[];
  }>(page, "/v1/products?limit=20");
  expect(
    catalog.products
      .find((row) => row.product === PRODUCT)
      ?.releases.map((release) => release.version),
    "the Hub's catalog lists the target release",
  ).toContain(TARGET);

  // Act in the browser, as "operator".
  await page.reload();
  await expect(page.locator("#left-nav")).toBeVisible();
  await openArea(page, "Releases", "Environments");
  const panel = page.locator('#main-workspace [data-dj-panel="propose"]');
  await expect(panel).toBeVisible();
  const generation = `live-console-${stamp}`;

  // First, a change the Hub refuses: its reason is shown, and nothing is
  // recorded.
  const before = (await plans(page)).length;
  await panel
    .locator('[data-dj-field="entityName"]')
    .selectOption(REFUSED_ENTITY);
  await panel.locator('[data-dj-field="version"]').selectOption(TARGET);
  await panel.locator('[data-dj-field="generation"]').fill(generation);
  await panel
    .locator('[data-dj-field="rationale"]')
    .fill("Move example-api past what its dependent allows.");
  await panel.locator("[data-dj-review]").click();
  await panel.locator("[data-dj-confirm]").click();
  const refusal = panel.locator("[data-dj-error]");
  await expect(refusal).toContainText("validation error");
  // The validator's detail names the rule, which the proposal route omits.
  await expect(refusal).toContainText(API_PRODUCT);
  await expect(panel.locator("[data-dj-outcome]")).toHaveCount(0);
  expect((await plans(page)).length).toBe(before);

  await panel.locator('[data-dj-field="entityName"]').selectOption(ENTITY);
  // The picker offers what the catalog lists and nothing else.
  const offered = await panel
    .locator('[data-dj-field="version"] option')
    .evaluateAll((options) =>
      options.map((option) => (option as HTMLOptionElement).value),
    );
  expect(offered.sort()).toEqual(["0.1.0", TARGET]);
  await panel.locator('[data-dj-field="version"]').selectOption(TARGET);
  const review = panel.locator("[data-dj-review]");
  await panel
    .locator('[data-dj-field="rationale"]')
    .fill("Move example-workspace to 0.2.0 from the console.");
  await expect(review).toBeEnabled();

  // The confirmation states the request; nothing is sent before Confirm.
  await review.click();
  await expect(panel.locator("[data-dj-confirm-step]")).toContainText(
    "Nothing has been sent",
  );
  await expect(panel.locator("[data-dj-request]")).toContainText(
    "POST /v1/plans",
  );
  await expect(panel.locator("[data-dj-request]")).toContainText(generation);
  expect((await plans(page)).length).toBe(before);

  await panel.locator("[data-dj-confirm]").click();
  const outcome = panel.locator("[data-dj-outcome]");
  await expect(outcome).toBeVisible();
  // Proposed, pending review: not approved, not deployed.
  await expect(outcome).toHaveAttribute("data-dj-outcome", "ok");
  await expect(outcome).toContainText("PENDING_APPROVALS");

  // The Hub holds exactly what the console said it sent.
  const after = await plans(page);
  expect(after.length).toBe(before + 1);
  const proposed = after.find(
    (plan) => plan.desiredState?.environment?.generation === generation,
  );
  expect(proposed, "the plan proposed from the console").toBeTruthy();
  proposedPlanId = proposed!.id;
  await expect(outcome).toContainText(proposedPlanId);
  expect(proposed!.status).toBe("AWAITING_APPROVAL");
  expect(
    proposed!.desiredState.entities.find((entity) => entity.name === ENTITY)
      ?.version,
  ).toBe(TARGET);
  const request = await read<{
    author: { username: string };
    status: string;
    title: string;
  }>(page, `/v1/change-requests/${proposed!.changeRequestId}`);
  expect(request.author.username).toBe("operator");
  expect(request.title).toBe(`Set ${ENTITY} to ${TARGET} in ${ENVIRONMENT}`);

  // The author is shown why they cannot approve, and offered no live button.
  await openArea(page, "Releases", "Plans");
  await page
    .locator("#main-workspace [data-release-plan]")
    .selectOption(proposedPlanId);
  const decide = page.locator('#main-workspace [data-dj-panel="decide"]');
  await expect(decide).toContainText(proposedPlanId);
  await expect(decide.locator("[data-dj-reason]")).toContainText(
    "Another person must approve or reject it.",
  );
  await expect(decide.locator("[data-dj-review]")).toHaveCount(0);
  await expect(decide.getByRole("button", { name: "Approve" })).toBeDisabled();
});

test("a second identity approves the plan in the console", async ({
  browser,
  baseURL,
}) => {
  expect(proposedPlanId, "the plan proposed by the previous test").not.toBe("");
  const context = await browser.newContext({
    baseURL,
    extraHTTPHeaders: { "X-Platform-Subject": "proposer" },
  });
  const page = await context.newPage();
  try {
    await openWorkbench(page);
    await expect(page.locator("#operator-name")).toContainText("proposer");
    await openArea(page, "Releases", "Plans");
    await page
      .locator("#main-workspace [data-release-plan]")
      .selectOption(proposedPlanId);
    const decide = page.locator('#main-workspace [data-dj-panel="decide"]');
    await expect(decide).toContainText(proposedPlanId);
    const approve = decide.locator('[data-dj-review="approve"]');
    await expect(approve).toBeDisabled();
    await decide
      .locator('[data-dj-field="rationale"]')
      .fill("Reviewed the proposed version as a second person.");
    await expect(approve).toBeEnabled();
    await approve.click();
    await expect(decide.locator("[data-dj-request]")).toContainText(
      "POST /conduit.plans.v1.PlanApprovalService/ApprovePlan",
    );
    // Still awaiting: the confirmation alone approved nothing.
    expect(
      (await plans(page)).find((plan) => plan.id === proposedPlanId)?.status,
    ).toBe("AWAITING_APPROVAL");
    await decide.locator("[data-dj-confirm]").click();
    await expect(decide.locator("[data-dj-outcome]")).toContainText(
      "The Hub recorded the approve.",
    );
    await expect(decide.locator("[data-dj-outcome]")).toContainText(
      new RegExp(
        `${proposedPlanId} is now (APPROVED|LEASED|RUNNING|SUCCEEDED)`,
      ),
    );

    // On the Hub: approved, by the second person.
    const approved = await read<PlanRecord>(
      page,
      `/v1/plans/${proposedPlanId}`,
    );
    expect(approved.status).not.toBe("AWAITING_APPROVAL");
    expect(approved.approvals?.map((approval) => approval.subject)).toContain(
      "proposer",
    );
  } finally {
    await context.close();
  }
});

test("a promotion the Hub blocks is shown as blocked, with its reasons", async ({
  page,
}) => {
  await openWorkbench(page);
  // A fresh Hub holds no promotion policy, and the console does not author
  // one. Record one evaluation through the API so a stage exists to evaluate.
  const catalog = await read<{
    products: { product: string; releases: Record<string, any>[] }[];
  }>(page, "/v1/products?limit=20");
  const release = catalog.products
    .find((row) => row.product === PRODUCT)!
    .releases.find((row) => row["version"] === "0.1.0")!;
  await send(
    page.request,
    hubURL(page, "/v1/promotions/evaluations"),
    "POST",
    "operator",
    {
      release,
      fromChannel: "DEV",
      toChannel: "STAGING",
      policy: {
        minimumHealthyEntities: 99,
        maximumUnhealthyEntities: 0,
        minimumHealthyDurationSeconds: 0,
      },
      evidence: [],
    },
  );
  const recorded = async () =>
    (
      await read<{ promotions: { id: string; status: string }[] }>(
        page,
        "/v1/promotions",
      )
    ).promotions;
  const before = await recorded();

  await page.reload();
  await expect(page.locator("#left-nav")).toBeVisible();
  await openArea(page, "Releases", "Promotion");
  const panel = page.locator('#main-workspace [data-dj-panel="promote"]');
  await expect(panel.locator('[data-dj-field="stage"]')).toContainText(
    "DEV -> STAGING",
  );
  await panel.locator('[data-dj-field="version"]').selectOption("0.1.0");
  await panel.locator("[data-dj-review]").click();
  await expect(panel.locator("[data-dj-request]")).toContainText(
    "POST /v1/promotions/evaluations",
  );
  await panel.locator("[data-dj-confirm]").click();

  const outcome = panel.locator("[data-dj-outcome]");
  await expect(outcome).toHaveAttribute("data-dj-outcome", "err");
  await expect(outcome).toContainText("BLOCKED");
  await expect(outcome).toContainText("was not promoted");
  await expect(panel.locator("[data-dj-blockers]")).toContainText(
    "HEALTHY_ENTITIES_BELOW_MINIMUM",
  );

  // The Hub recorded the evaluation the console sent, and it is not PASSED.
  const after = await recorded();
  expect(after.length).toBeGreaterThanOrEqual(before.length);
  expect(after.every((row) => row.status === "BLOCKED")).toBe(true);
});

test("the Space selector scopes environments at the Hub", async ({ page }) => {
  await openWorkbench(page);
  const space = (await send(
    page.request,
    hubURL(page, "/v1/spaces"),
    "POST",
    "operator",
    { name: `live-${stamp}` },
  )) as { id: string };
  await page.reload();
  const selector = page.locator("#space-selector");
  await expect(selector.locator(`option[value="${space.id}"]`)).toHaveCount(1);

  // An empty Space: the Hub returns no environment for it.
  const scoped = page.waitForResponse((response) =>
    response.url().includes(`/v1/environments?spaceId=${space.id}`),
  );
  await selector.selectOption(space.id);
  expect((await (await scoped).json()).environments).toEqual([]);
  await expect(page.locator("[data-space-scope]")).toBeVisible();
  await openArea(page, "Fleet", "Environments");
  await expect(
    page.locator(`#main-workspace [data-env-open="${ENVIRONMENT}"]`),
  ).toHaveCount(0);

  // Bind the environment to the Space on the Hub; it comes back into view.
  await send(
    page.request,
    hubURL(page, `/v1/spaces/${space.id}/bindings`),
    "PUT",
    "operator",
    { kind: "ENVIRONMENT", id: ENVIRONMENT },
  );
  await page.reload();
  await expect(page.locator("#space-selector")).toHaveValue(space.id);
  await openArea(page, "Fleet", "Environments");
  await expect(
    page.locator(`#main-workspace [data-env-open="${ENVIRONMENT}"]`),
  ).toBeVisible();

  // Back to all Spaces for whatever runs after this.
  await page.locator("#space-selector").selectOption("");
  await expect(page.locator("[data-space-scope]")).toHaveCount(0);
});

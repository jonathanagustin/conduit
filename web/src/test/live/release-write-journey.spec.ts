import { fromJson, toJson } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
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
  await expect(decide.getByRole("button", { name: "Reject" })).toBeDisabled();
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

test("a reviewed promotion passes on actual reported healthy installations", async ({
  page,
}) => {
  await openWorkbench(page);
  const catalog = await read<{
    products: { product: string; releases: any[] }[];
  }>(page, "/v1/products?limit=20");
  const releases = catalog.products.find(
    (row) => row.product === API_PRODUCT,
  )!.releases;
  let healthy: any;
  await expect
    .poll(
      async () => {
        const fleet = await read<{ installations: any[] }>(
          page,
          "/v1/fleet/installations?limit=200",
        );
        healthy = fleet.installations.find(
          (row) =>
            row.product === API_PRODUCT &&
            row.health === "HEALTHY" &&
            releases.some((release) => release.version === row.observedVersion),
        );
        return healthy?.observedVersion || "";
      },
      {
        message: "the simulated Agent must report a real healthy installation",
      },
    )
    .not.toBe("");
  const release = releases.find(
    (row) => row.version === healthy.observedVersion,
  )!;
  const toChannel = `LIVE_${stamp.toUpperCase()}`;
  const policy = {
    minimumHealthyEntities: 1,
    maximumUnhealthyEntities: 0,
    minimumHealthyDurationSeconds: 0,
  };
  // Policy setup is deliberately BLOCKED: no successful promotion is seeded.
  const seeded = await send(
    page.request,
    hubURL(page, "/v1/promotions/evaluations"),
    "POST",
    "operator",
    { release, fromChannel: "DEV", toChannel, policy, evidence: [] },
  );
  expect(seeded.status).toBe("BLOCKED");
  const recorded = async () =>
    (
      await read<{ promotions: any[] }>(page, "/v1/promotions")
    ).promotions.filter(
      (row) => row.product === API_PRODUCT && row.toChannel === toChannel,
    );
  const before = await recorded();
  expect(before).toHaveLength(1);
  expect(before[0].status).toBe("BLOCKED");
  await page.reload();
  await expect(page.locator("#left-nav")).toBeVisible();
  await openArea(page, "Releases", "Promotion");
  const panel = page.locator('#main-workspace [data-dj-panel="promote"]');
  await panel
    .locator('[data-dj-field="stage"]')
    .selectOption(`${API_PRODUCT}\nDEV\n${toChannel}`);
  await panel
    .locator('[data-dj-field="version"]')
    .selectOption(release.version);
  await expect(panel.locator("[data-dj-evidence]")).toContainText(
    "reported installation(s)",
  );
  expect((await recorded()).map((row) => row.id)).toEqual(
    before.map((row) => row.id),
  );
  await panel.locator("[data-dj-review]").click();
  await expect(panel.locator("[data-dj-request]")).toContainText(
    "POST /v1/promotions/evaluations",
  );
  expect((await recorded()).map((row) => row.id)).toEqual(
    before.map((row) => row.id),
  );
  const submitted = page.waitForRequest(
    (request) =>
      request.method() === "POST" &&
      new URL(request.url()).pathname === "/v1/promotions/evaluations",
  );
  await panel.locator("[data-dj-confirm]").click();
  const body = (await submitted).postDataJSON();
  expect(body.release.product).toBe(API_PRODUCT);
  expect(body.release.version).toBe(release.version);
  expect(body.fromChannel).toBe("DEV");
  expect(body.toChannel).toBe(toChannel);
  expect(body.policy).toEqual(policy);
  expect(body.evidence).toContainEqual(
    expect.objectContaining({
      environment: healthy.environment,
      installationId: healthy.installationId,
      health: "HEALTHY",
    }),
  );
  const outcome = panel.locator("[data-dj-outcome]");
  await expect(outcome).toHaveAttribute("data-dj-outcome", "ok");
  await expect(outcome).toContainText("PASSED");
  const after = await recorded();
  const passed = after.find((row) => row.status === "PASSED");
  expect(passed).toBeDefined();
  expect(passed.version).toBe(release.version);
  expect(passed.fromChannel).toBe("DEV");
  expect(passed.toChannel).toBe(toChannel);
  expect(passed.policy).toEqual(policy);
  expect(passed.evidence).toHaveLength(body.evidence.length);
  // Protobuf JSON keeps 3/6/9 fractional digits; Go RFC3339Nano trims
  // trailing zeros. Compare exact instants, preserving nanoseconds and every
  // other evidence field rather than rounding through JavaScript Date.
  const preciseEvidence = (rows: Record<string, any>[]) =>
    rows.map((entry) => ({
      ...entry,
      observedAt:
        entry.observedAt === undefined
          ? undefined
          : fromJson(TimestampSchema, entry.observedAt),
      healthySince:
        entry.healthySince === undefined
          ? undefined
          : fromJson(TimestampSchema, entry.healthySince),
    }));
  const actualEvidence = preciseEvidence(passed.evidence);
  const expectedEvidence = preciseEvidence(body.evidence);
  expect(actualEvidence).toEqual(
    expect.arrayContaining(
      expectedEvidence.map((entry) => expect.objectContaining(entry)),
    ),
  );
  // This assertion must still reject even a one-nanosecond evidence change.
  const observed = fromJson(TimestampSchema, body.evidence[0].observedAt);
  const alteredEvidence = preciseEvidence([
    {
      ...body.evidence[0],
      observedAt: toJson(TimestampSchema, {
        ...observed,
        nanos:
          observed.nanos === 999999999
            ? observed.nanos - 1
            : observed.nanos + 1,
      }),
    },
  ]);
  expect(actualEvidence).not.toEqual(
    expect.arrayContaining(
      alteredEvidence.map((entry) => expect.objectContaining(entry)),
    ),
  );
  expect(passed.id).not.toBe(seeded.id);
  await expect(outcome).toContainText(passed.id);
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
  const scoped = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return (
      response.request().method() === "GET" &&
      url.pathname === "/v1/environments" &&
      url.searchParams.get("spaceId") === space.id
    );
  });
  await selector.selectOption(space.id);
  const scopedResponse = await scoped;
  expect(scopedResponse.ok()).toBe(true);
  expect((await scopedResponse.json()).environments).toEqual([]);
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

test("maintenance review previews without persistence and submits only the confirmed interval", async ({
  page,
}) => {
  await openWorkbench(page);
  await openArea(page, "Releases", "Windows");
  const form = page.locator("[data-window-scheduler]");
  await expect(form.locator('[name="entity"]')).toBeVisible();
  const entity = await form.locator('[name="entity"]').inputValue();
  expect(entity).not.toBe("");
  const start = new Date(Date.now() + 2 * 3600000).toISOString().slice(0, 16);
  await form.locator('[name="start"]').fill(start);
  await form.locator('[name="minutes"]').fill("45");
  await form
    .locator('[name="reason"]')
    .fill("Reviewed live harness maintenance");
  const save = form.locator("[data-save-window]");
  await expect(save).toBeDisabled();
  const responseFor = (dryRun: boolean) =>
    page.waitForResponse((response) => {
      const request = response.request();
      return (
        request.method() === "POST" &&
        new URL(response.url()).pathname ===
          "/v1/temporary-maintenance-overrides" &&
        request.postDataJSON()?.dryRun === dryRun
      );
    });
  const previewResponse = responseFor(true);
  await form.locator("[data-preview-window]").click();
  const previewed = await previewResponse;
  expect(previewed.status()).toBe(200);
  const preview = await previewed.json();
  expect(preview).toMatchObject({ valueTier: "CONDUIT_NATIVE", dryRun: true });
  expect(preview.requests).toHaveLength(1);
  const receipt = preview.requests[0];
  expect(receipt).toMatchObject({
    environment: ENVIRONMENT,
    entity,
    type: "NO_DOWNTIME",
  });
  expect(Date.parse(receipt.startsAt)).toBe(Date.parse(start + ":00Z"));
  expect(Date.parse(receipt.endsAt) - Date.parse(receipt.startsAt)).toBe(
    45 * 60000,
  );
  const persistedBefore = await page.request.get(
    hubURL(page, `/v1/change-requests/${receipt.changeRequestId}`),
  );
  expect(persistedBefore.status()).toBe(404);
  await expect(save).toBeDisabled();
  await form.locator("[data-confirm-window]").check();
  await expect(save).toBeEnabled();
  const submittedResponse = responseFor(false);
  await save.click();
  const submitted = await submittedResponse;
  expect(submitted.status()).toBe(201);
  expect(submitted.request().postDataJSON()).toMatchObject({
    expectedChangeRequestId: receipt.changeRequestId,
    dryRun: false,
    entities: [{ environment: ENVIRONMENT, entity }],
    duration: "45m",
    startsAt: new Date(start + ":00Z").toISOString(),
  });
  const storedReceipt = await submitted.json();
  expect(storedReceipt.requests).toHaveLength(1);
  expect(storedReceipt.requests[0]).toMatchObject(receipt);
  const stored = await read<any>(
    page,
    `/v1/change-requests/${receipt.changeRequestId}`,
  );
  expect(stored.author.username).toBe("operator");
  expect(stored.environment).toBe(ENVIRONMENT);
  expect(stored.changes).toHaveLength(1);
  expect(stored.changes[0]).toMatchObject({
    type: "CREATE_ENTITY_MAINTENANCE_WINDOW_OVERRIDE",
    request: { entity, type: "NO_DOWNTIME" },
  });
  expect(Date.parse(stored.changes[0].request.startTime)).toBe(
    Date.parse(receipt.startsAt),
  );
  expect(Date.parse(stored.changes[0].request.endTime)).toBe(
    Date.parse(receipt.endsAt),
  );
  await expect(form.locator("[data-window-message]")).toContainText(
    `Change request ${receipt.changeRequestId}`,
  );
  await expect(save).toBeDisabled();
});

test("a reviewed Change Request rejection is confirmed once and retained on the real Hub", async ({
  page,
}) => {
  await openWorkbench(page);
  const team = await read<Record<string, any>>(page, "/v1/teams/reviewers");
  await send(
    page.request,
    hubURL(page, "/v1/teams/reviewers"),
    "PUT",
    "operator",
    {
      ...team,
      memberUsers: [
        ...new Set([...(team.memberUsers ?? []), "operator", "proposer"]),
      ],
    },
  );
  const seeded = (await plans(page)).find(
    (plan) => plan.desiredState?.environment?.id === ENVIRONMENT,
  );
  expect(seeded, "a real seeded desired state").toBeTruthy();
  const fleet = await read<{
    installations: { product: string; observedVersion: string }[];
  }>(page, "/v1/fleet/installations?limit=200");
  const observed = fleet.installations.find(
    (row) => row.product === PRODUCT,
  )?.observedVersion;
  const version = observed === TARGET ? "0.1.0" : TARGET;
  const catalog = await read<{
    products: { product: string; releases: Record<string, any>[] }[];
  }>(page, "/v1/products?limit=20");
  const targetRelease = catalog.products
    .find((row) => row.product === PRODUCT)
    ?.releases.find((release) => release["version"] === version);
  expect(
    targetRelease,
    "the selected version has actual catalog coordinates",
  ).toBeTruthy();
  const state = structuredClone(seeded!.desiredState);
  state.releases = [
    ...state.releases.filter(
      (release) =>
        !(release["product"] === PRODUCT && release["version"] === version),
    ),
    targetRelease!,
  ];
  state.environment.generation = `live-rejection-${Date.now().toString(16)}`;
  state.entities.find((entity) => entity.name === ENTITY)!.version = version;
  const proposed = (await send(
    page.request,
    hubURL(page, "/v1/plans"),
    "POST",
    "proposer",
    state,
  )) as PlanRecord;
  expect(proposed.status).toBe("AWAITING_APPROVAL");
  expect(proposed.changeRequestId).toBeTruthy();
  const path = `/v1/change-requests/${proposed.changeRequestId}`;
  type Change = {
    id: string;
    planId: string;
    status: string;
    author: { username: string };
    reviews?: { subject: string; decision: string; rationale: string }[];
  };
  const pending = await read<Change>(page, path);
  expect(pending).toMatchObject({
    id: proposed.changeRequestId,
    planId: proposed.id,
    status: "PENDING_APPROVALS",
    author: { username: "proposer" },
  });
  const writes: unknown[] = [];
  page.on("request", (request) => {
    if (
      request.method() === "POST" &&
      new URL(request.url()).pathname === `${path}/reject`
    ) {
      writes.push(request.postDataJSON());
    }
  });
  await page.reload();
  await openArea(page, "Releases", "Plans");
  await page
    .locator("#main-workspace [data-release-plan]")
    .selectOption(proposed.id);
  const panel = page.locator('#main-workspace [data-dj-panel="decide"]');
  await expect(panel).toContainText(proposed.id);
  const reject = panel.locator('[data-dj-review="reject"]');
  await expect(reject).toBeDisabled();
  const reason = `Reject unapproved configuration — ${proposed.id}`;
  await panel
    .locator('[data-dj-field="rationale"]')
    .fill("Discard this draft rejection");
  await reject.click();
  await expect(panel.locator("[data-dj-request]")).toContainText(
    `POST ${path}/reject`,
  );
  expect(writes).toHaveLength(0);
  expect((await read<Change>(page, path)).status).toBe("PENDING_APPROVALS");
  await panel.locator("[data-dj-cancel]").click();
  await expect(panel.locator("[data-dj-confirm]")).toHaveCount(0);
  expect(writes).toHaveLength(0);
  await panel.locator('[data-dj-field="rationale"]').fill(reason);
  await panel.locator('[data-dj-review="reject"]').click();
  await expect(panel.locator("[data-dj-confirm-step]")).toContainText(
    proposed.id,
  );
  await expect(panel.locator("[data-dj-confirm-step]")).toContainText(
    ENVIRONMENT,
  );
  await expect(panel.locator("[data-dj-request]")).toContainText(reason);
  expect(writes).toHaveLength(0);
  expect((await read<Change>(page, path)).reviews ?? []).toEqual([]);
  await panel.locator("[data-dj-confirm]").click();
  await expect(panel.locator("[data-dj-outcome]")).toHaveAttribute(
    "data-dj-outcome",
    "ok",
  );
  await expect(panel.locator("[data-dj-outcome]")).toContainText(
    "The Hub recorded the reject.",
  );
  await expect(panel.locator("[data-dj-outcome]")).toContainText("REJECTED");
  expect(writes).toEqual([{ rationale: reason }]);
  const rejected = await read<Change>(page, path);
  expect(rejected).toMatchObject({
    id: pending.id,
    planId: proposed.id,
    status: "REJECTED",
    author: pending.author,
  });
  expect(rejected.reviews).toEqual([
    expect.objectContaining({
      subject: "operator",
      decision: "REJECT",
      rationale: reason,
    }),
  ]);
  const plan = await read<PlanRecord & { cancellation?: unknown }>(
    page,
    `/v1/plans/${proposed.id}`,
  );
  expect(plan.status).toBe("AWAITING_APPROVAL");
  expect(plan.cancellation).toBeFalsy();
  await page.reload();
  expect((await read<Change>(page, path)).reviews).toEqual(rejected.reviews);
});

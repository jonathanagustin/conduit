import { expect, test, type Page } from "@playwright/test";

// This suite needs the real recorded-fixture packaging workflow started by the
// opt-in harness. An empty run catalog is a failed setup, never qualification.
const runId = process.env["CONDUIT_SIMULATED_PACKAGING_RUN"];
if (!runId)
  throw new Error(
    "Run simulate-console.sh --with-packaging for packaging feedback qualification.",
  );
const path = `/v1/packaging-runs/${encodeURIComponent(runId)}`;
type ReviewNote = {
  id: string;
  runId: string;
  target: string;
  selector: string;
  note: string;
  author: string;
  approval: boolean;
  createdAt: string;
};
async function read<T>(page: Page, suffix = ""): Promise<T> {
  const response = await page.request.get(
    new URL(path + suffix, page.url()).toString(),
  );
  expect(response.ok(), await response.text()).toBe(true);
  return response.json() as Promise<T>;
}

test("packaging review notes retain exact input without approving the real waiting workflow", async ({
  page,
}, testInfo) => {
  await page.goto("./#s08-staged-run");
  const form = page.locator(`form[data-run="${runId}"]`);
  await expect(form).toBeVisible();
  const parent = await read<Record<string, unknown>>(page);
  expect(parent).toMatchObject({
    runId,
    phase: "review",
    targetPlanRef: "env-dev/sample-app",
  });
  expect(parent.sourceDigest).toMatch(/^sha256:[0-9a-f]{64}$/);
  expect(parent.artifactDigest).toMatch(/^sha256:[0-9a-f]{64}$/);
  expect(parent.proposalRevision).toBeTruthy();
  expect(parent.planId || "").toBe("");
  const history = await read<{ items: unknown[] }>(page, "/events");
  const initial = await read<{ items: ReviewNote[] }>(page, "/feedback");
  expect(initial.items).toEqual([]);
  const writes: unknown[] = [];
  page.on("request", (request) => {
    if (
      request.method() === "POST" &&
      new URL(request.url()).pathname === path + "/feedback"
    )
      writes.push(request.postDataJSON());
  });
  const notes: ReviewNote[] = [];
  const stamp = Date.now().toString(16);
  for (const target of ["file", "manifest", "step"]) {
    const body = {
      target,
      selector: `${target}.reviewed`,
      note: `Reviewed ${target} <script>literal</script> ${stamp}`,
    };
    await form.locator('[name="target"]').selectOption(target);
    await form.getByLabel("Selector", { exact: true }).fill(body.selector);
    await form.getByLabel("Note", { exact: true }).fill(body.note);
    // Force an ordinary read refresh after entry. It must not replace a
    // reviewed draft with the default empty form or record any feedback.
    await page.evaluate(async () => {
      await Reflect.get(window, "state").liveHub.refresh();
    });
    await expect(form.locator('[name="target"]')).toHaveValue(body.target);
    await expect(form.getByLabel("Selector", { exact: true })).toHaveValue(
      body.selector,
    );
    await expect(form.getByLabel("Note", { exact: true })).toHaveValue(
      body.note,
    );
    // Draft edits do not write; the labeled Record action is the commit point.
    expect(
      (await read<{ items: ReviewNote[] }>(page, "/feedback")).items,
    ).toEqual(notes);
    expect(writes).toHaveLength(notes.length);
    const response = page.waitForResponse(
      (value) =>
        value.request().method() === "POST" &&
        new URL(value.url()).pathname === path + "/feedback",
    );
    await form
      .getByRole("button", { name: "Record review note", exact: true })
      .click();
    const recorded = await response;
    expect(recorded.status()).toBe(201);
    const note = (await recorded.json()) as ReviewNote;
    expect(note).toMatchObject({
      ...body,
      runId,
      author: "operator",
      approval: false,
    });
    expect(note.id).toMatch(/^fb_[0-9a-f]+$/);
    expect(Number.isFinite(Date.parse(note.createdAt))).toBe(true);
    notes.push(note);
    expect(writes).toEqual(
      notes.map(({ target, selector, note }) => ({ target, selector, note })),
    );
    expect(
      (await read<{ items: ReviewNote[] }>(page, "/feedback")).items,
    ).toEqual(notes);
    const displayed = page.locator("li").filter({ hasText: body.note });
    await expect(displayed).toContainText(`${target} ${body.selector}`);
    await expect(displayed).toContainText("operator, not an approval");
    await expect(displayed.locator("script,img")).toHaveCount(0);
  }
  await page.reload();
  await expect(form).toBeVisible();
  for (const note of notes)
    await expect(
      page.locator("li").filter({ hasText: note.note }),
    ).toContainText(note.selector);

  await form.getByLabel("Selector", { exact: true }).fill("");
  await form
    .getByLabel("Note", { exact: true })
    .fill("Invalid selector must not be stored");
  const refused = page.waitForResponse(
    (value) =>
      value.request().method() === "POST" &&
      new URL(value.url()).pathname === path + "/feedback",
  );
  await form
    .getByRole("button", { name: "Record review note", exact: true })
    .click();
  expect((await refused).status()).toBe(422);
  await expect(form.locator("[data-error]")).toHaveText(
    "The note was not recorded.",
  );
  expect(
    (await read<{ items: ReviewNote[] }>(page, "/feedback")).items,
  ).toEqual(notes);
  const missing = await page.request.post(
    new URL(
      "/v1/packaging-runs/missing-feedback-parent/feedback",
      page.url(),
    ).toString(),
    {
      data: { target: "step", selector: "review", note: "must not be stored" },
    },
  );
  expect(missing.status()).toBe(404);

  // This existing route is operator-any, not owner-private or environment-grant
  // scoped. A second authenticated reader sees the same run's review input.
  const peer = await page.request.get(
    new URL(path + "/feedback", page.url()).toString(),
    { headers: { "X-Platform-Subject": "feedback-peer" } },
  );
  expect(peer.status()).toBe(200);
  expect((await peer.json()).items).toEqual(notes);
  expect(await read(page)).toEqual(parent);
  expect(await read(page, "/events")).toEqual(history);
  await testInfo.attach("packaging-feedback-receipts", {
    contentType: "application/json",
    body: Buffer.from(
      JSON.stringify({ parent, history, notes, writes }, null, 2),
    ),
  });
});

test("assistant packaging cancel proposal binds the real waiting run without dispatch", async ({
  page,
}) => {
  await page.goto("./#s08-staged-run");
  const origin = new URL(page.url()).origin;
  const url = (suffix: string) => origin + suffix;
  const get = async (suffix: string) => {
    const response = await page.request.get(url(suffix));
    expect(response.ok(), await response.text()).toBe(true);
    return response.json();
  };
  const parent = await get(path);
  expect(parent).toMatchObject({
    runId,
    phase: "review",
    targetPlanRef: "env-dev/sample-app",
  });
  expect(parent.sourceDigest).toMatch(/^sha256:[0-9a-f]{64}$/);
  expect(parent.proposalRevision).toBeTruthy();
  expect(parent.planId || "").toBe("");
  // Explicit API setup supplies native desired state at the fixture run's exact
  // target. This is not a browser planning or deployment qualification.
  const plans = await get("/v1/plans?environment=dev-use2&limit=20");
  const seed = plans.plans.find(
    (row: any) => row.desiredState?.environment?.id === "dev-use2",
  );
  expect(seed).toBeTruthy();
  const release = seed.desiredState.releases.find(
    (row: any) => row.product === "app.example:example-api",
  );
  expect(release).toBeTruthy();
  const setup = await page.request.post(url("/v1/plans"), {
    data: {
      ...seed.desiredState,
      environment: {
        ...seed.desiredState.environment,
        id: "env-dev",
        generation: "native-assistant-proposal-fixture",
      },
      releases: [release],
      entities: [
        {
          name: "sample-app",
          product: release.product,
          version: release.version,
          namespace: "example",
        },
      ],
    },
  });
  expect(setup.status(), await setup.text()).toBe(201);
  const team = await page.request.put(url("/v1/teams/Operator"), {
    data: {
      name: "Operator",
      displayName: "Native assistant fixture operators",
      adminUsers: ["operator"],
      memberUsers: ["operator"],
    },
  });
  expect(team.ok(), await team.text()).toBe(true);
  await page.reload();
  await expect(page.locator(`form[data-run="${runId}"]`)).toBeVisible();
  await page.getByRole("button", { name: "Toggle right chat panel" }).click();
  const rail = page.locator("#right-chat-rail");
  await rail.locator('[data-set-mode="release_plan"]').click();
  const action = rail.locator(
    `[data-assistant-action="propose-cancel"][data-chip-key="PackagingRun:${runId}"]`,
  );
  await expect(action).toBeVisible();
  const writes: string[] = [];
  const restFleetReads: string[] = [];
  // These exact generated read procedures use POST; every other POST and all
  // PUT/PATCH/DELETE requests remain visible to the no-dispatch assertion.
  const readProcedures = new Set([
    "/conduit.packaging.v1.PackagingRunService/ListPackagingRuns",
    "/conduit.packaging.v1.PackagingRunService/ListPackagingRunEvents",
    "/conduit.packaging.v1.PackagingRunService/GetPackagingRun",
    "/conduit.fleet.v1.FleetReadService/ListFleetInstallations",
  ]);
  page.on("request", (request) => {
    const requestPath = new URL(request.url()).pathname;
    if (requestPath === "/v1/fleet/installations")
      restFleetReads.push(request.url());
    if (
      ["POST", "PUT", "PATCH", "DELETE"].includes(request.method()) &&
      !(request.method() === "POST" && readProcedures.has(requestPath))
    )
      writes.push(requestPath);
  });
  const sent = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname === "/v1/assistant/tools",
  );
  const fleetLookup = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname ===
      "/conduit.fleet.v1.FleetReadService/ListFleetInstallations",
  );
  await action.click();
  const lookup = await fleetLookup;
  expect(lookup.method()).toBe("POST");
  expect(lookup.postDataJSON()).toEqual({
    environment: "env-dev",
    installationId: "sample-app",
    limit: 1,
  });
  const response = await sent;
  expect(response.status(), await response.text()).toBe(200);
  expect(response.request().postDataJSON()).toEqual({
    mode: "release_plan",
    tool: "propose_governed_action",
    arguments: {
      actionId: "cancel_packaging_run",
      targetEntityId: runId,
      targetObjectType: "PackagingRun",
      environment: "env-dev",
      installationId: "sample-app",
      productId: release.product,
    },
  });
  const receipt = await response.json();
  expect(receipt).toMatchObject({
    ok: true,
    allowed: true,
    tool: "propose_governed_action",
    changeRequestStatus: "PENDING_APPROVALS",
    writeOps: 1,
  });
  expect(receipt.changeRequestId).toBeTruthy();
  await expect(rail.locator("[data-assistant-results]")).toContainText(
    receipt.changeRequestId,
  );
  const changePath = `/v1/change-requests/${receipt.changeRequestId}`;
  const pending = await get(changePath);
  expect(pending).toMatchObject({
    id: receipt.changeRequestId,
    status: "PENDING_APPROVALS",
    environment: "env-dev",
    author: { username: "operator" },
    requirementsSatisfied: false,
  });
  expect(pending.execution).toBeFalsy();
  expect(pending.changes).toHaveLength(1);
  expect(pending.changes[0].request.assistantAction).toMatchObject({
    version: "conduit.assistant-action/v1",
    tier: "CONDUIT_NATIVE",
    action: "cancel_packaging_run",
    runId,
    environment: "env-dev",
    installation: "sample-app",
    product: release.product,
    binding: {
      sourceDigest: parent.sourceDigest,
      proposalRevision: parent.proposalRevision,
      targetPlanRef: parent.targetPlanRef,
      templateVersion: parent.templateVersion,
      toolVersions: parent.toolVersions,
    },
  });
  expect(pending.changes[0].request.assistantAction.binding.decisionId).toMatch(
    /^assistant-[0-9a-f]{32}$/,
  );
  const refused = await page.request.post(url(changePath + "/approve"), {
    data: { rationale: "Author must not approve this proposal." },
  });
  expect(refused.status(), await refused.text()).toBe(403);
  expect(await get(changePath)).toEqual(pending);
  expect(await get(path)).toEqual(parent);
  await page.reload();
  await expect(page.locator(`form[data-run="${runId}"]`)).toBeVisible();
  expect(await get(changePath)).toEqual(pending);
  expect(await get(path)).toEqual(parent);
  expect(writes).toEqual(["/v1/assistant/tools"]);
  expect(restFleetReads).toEqual([]);
});

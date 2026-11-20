import { expect, test } from "@playwright/test";

const repository = process.env["CONDUIT_SIMULATED_PLANNING_REPO"];
const modelOrigin = process.env["CONDUIT_SIMULATED_MODEL_ORIGIN"];
if (!repository || !modelOrigin)
  throw new Error(
    "Run simulate-console.sh --with-planning for cancellation qualification.",
  );

test("planning Stop cancels the exact native owner turn and another actor cannot cancel it", async ({
  page,
}) => {
  await page.goto("./#s01-intake");
  expect(repository).toMatch(
    /^http:\/\/127\.0\.0\.1:\d+\/planning-cancel\.git$/,
  );
  expect(modelOrigin).toMatch(/^http:\/\/127\.0\.0\.1:\d+$/);
  const origin = new URL(page.url()).origin;
  const fixture = async () => {
    const response = await page.request.get(`${modelOrigin}/planning-fixture`);
    expect(response.ok(), await response.text()).toBe(true);
    return response.json() as Promise<{
      started: number;
      active: number;
      disconnected: number;
      expired: number;
    }>;
  };
  expect(await fixture()).toEqual({
    started: 0,
    active: 0,
    disconnected: 0,
    expired: 0,
  });
  // Session creation is API setup. Cancellation itself must be the browser's
  // real native POST, with no intercepted Hub response.
  const created = await page.request.post(`${origin}/v1/planning-sessions`, {
    data: { repoUrl: repository, ref: "reviewed-ref" },
  });
  expect(created.status(), await created.text()).toBe(201);
  const initial = await created.json();
  expect(initial.id).toBeTruthy();
  const sessionPath = `${origin}/v1/planning-sessions/${encodeURIComponent(initial.id)}`;
  await page.goto(`./#s01-intake/${encodeURIComponent(initial.id)}`);
  const flow = page.locator("#main-workspace [data-plan-flow]");
  await expect(flow.getByLabel("Repository URL")).toHaveValue(repository);
  await expect(flow.getByLabel("Ref", { exact: true })).toHaveValue(
    "reviewed-ref",
  );
  const running = page.waitForRequest(
    (request) =>
      request.method() === "POST" &&
      new URL(request.url()).pathname === "/v1/agent-runs",
  );
  await flow
    .getByLabel("Message", { exact: true })
    .fill("Inspect this local fixture before cancellation.");
  await flow.locator("[data-plan-send]").click();
  const input = (await running).postDataJSON();
  expect(input.threadId).toBe(initial.id);
  expect(input.runId).toMatch(/^[0-9a-f-]{36}$/);
  await expect
    .poll(async () => (await fixture()).active, { timeout: 20_000 })
    .toBe(1);
  const cancelPath = `${sessionPath}/turns/${encodeURIComponent(input.runId)}/cancel`;
  const refused = await page.request.post(cancelPath, {
    headers: { "X-Platform-Subject": "other-operator" },
    data: {},
  });
  expect(refused.status()).toBe(404);
  expect(await fixture()).toEqual({
    started: 1,
    active: 1,
    disconnected: 0,
    expired: 0,
  });

  const writes: unknown[] = [];
  page.on("request", (request) => {
    if (request.method() === "POST" && request.url() === cancelPath)
      writes.push(request.postDataJSON());
  });
  const acknowledged = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" && response.url() === cancelPath,
  );
  await flow.locator("[data-plan-stop]").click();
  const response = await acknowledged;
  expect(response.ok(), await response.text()).toBe(true);
  const receipt = await response.json();
  expect(receipt.runId).toBe(input.runId);
  expect(receipt.valueTier).toBe("CONDUIT_NATIVE");
  expect(["cancellation_requested", "cancelled"]).toContain(receipt.state);
  await expect.poll(async () => (await fixture()).disconnected).toBe(1);
  expect(await fixture()).toEqual({
    started: 1,
    active: 0,
    disconnected: 1,
    expired: 0,
  });
  // Repeating the owner's exact cancellation is idempotent and confirms the
  // native terminal state rather than equating a closed browser stream with it.
  await expect
    .poll(async () => {
      const replay = await page.request.post(cancelPath, { data: {} });
      expect(replay.ok()).toBe(true);
      const body = await replay.json();
      expect(body.runId).toBe(input.runId);
      expect(body.valueTier).toBe("CONDUIT_NATIVE");
      return body.state;
    })
    .toBe("cancelled");
  // Wait for the browser to consume its first receipt before deciding whether
  // it needs the explicit Check action for an acknowledged but pending stop.
  await expect(flow.locator("[data-plan-stopped]")).toContainText(
    /recorded the cancellation request|confirmed this planning turn is cancelled/,
  );
  const check = flow.locator("[data-plan-cancel-check]");
  if (await check.isVisible()) await check.click();
  await expect(flow.locator("[data-plan-stopped]")).toContainText(
    "confirmed this planning turn is cancelled",
  );
  await expect(flow.locator("[data-plan-error]")).toHaveCount(0);
  await expect(flow.locator("[data-plan-stop]")).toHaveCount(0);
  expect(writes.length).toBeGreaterThan(0);
  for (const body of writes) expect(body).toEqual({});
  const readback = await page.request.get(sessionPath);
  expect(readback.ok()).toBe(true);
  const recorded = await readback.json();
  expect(recorded.id).toBe(initial.id);
  expect(recorded.intake).toEqual(initial.intake);
  expect(recorded.answers).toEqual(initial.answers);
  expect(recorded.plan).toEqual(initial.plan);
  expect(recorded.inspection).toEqual(initial.inspection);
});

test("planning intake creates the exact native session through the browser composer", async ({
  page,
}, testInfo) => {
  await page.goto("./#s01-intake");
  const origin = new URL(page.url()).origin;
  const flow = page.locator("#main-workspace [data-plan-flow]");
  const fixture = async () => {
    const response = await page.request.get(`${modelOrigin}/planning-fixture`);
    expect(response.ok()).toBe(true);
    return response.json();
  };
  const before = await fixture();
  expect(before.active).toBe(0);
  expect(before.expired).toBe(0);
  const creates: unknown[] = [];
  page.on("request", (request) => {
    if (
      request.method() === "POST" &&
      new URL(request.url()).pathname === "/v1/planning-sessions"
    )
      creates.push(request.postDataJSON());
  });
  await expect(flow.getByLabel("Repository URL")).toBeEnabled();
  await flow
    .getByLabel("Message", { exact: true })
    .fill("Inspect the reviewed fixture source.");
  await flow.locator("[data-plan-send]").click();
  await expect(flow.locator("[data-plan-error]")).toContainText(
    "Enter a repository URL and a ref.",
  );
  expect(creates).toEqual([]);
  expect(await fixture()).toEqual(before);
  await flow.getByLabel("Repository URL").fill(repository);
  await flow.getByLabel("Ref", { exact: true }).fill("reviewed-ref");
  const createdResponse = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname === "/v1/planning-sessions",
  );
  const running = page.waitForRequest(
    (request) =>
      request.method() === "POST" &&
      new URL(request.url()).pathname === "/v1/agent-runs",
  );
  await flow.locator("[data-plan-send]").click();
  const created = await createdResponse;
  expect(created.status()).toBe(201);
  const initial = await created.json();
  expect(initial.id).toBeTruthy();
  expect(initial.intake).toMatchObject({
    repoUrl: repository,
    ref: "reviewed-ref",
  });
  expect(creates).toEqual([{ repoUrl: repository, ref: "reviewed-ref" }]);
  const input = (await running).postDataJSON();
  expect(input.threadId).toBe(initial.id);
  expect(input.runId).toMatch(/^[0-9a-f-]{36}$/);
  const sessionPath = `${origin}/v1/planning-sessions/${encodeURIComponent(initial.id)}`;
  const peer = await page.request.get(sessionPath, {
    headers: { "X-Platform-Subject": "proposer" },
  });
  expect(peer.status()).toBe(404);
  await expect(flow.getByLabel("Repository URL")).toHaveValue(repository);
  await expect(flow.getByLabel("Ref", { exact: true })).toHaveValue(
    "reviewed-ref",
  );
  await expect(flow.getByLabel("Repository URL")).toBeDisabled();
  await expect
    .poll(async () => (await fixture()).active, { timeout: 20_000 })
    .toBe(1);
  const cancelPath = `${sessionPath}/turns/${encodeURIComponent(input.runId)}/cancel`;
  const acknowledged = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" && response.url() === cancelPath,
  );
  await flow.locator("[data-plan-stop]").click();
  const stopped = await acknowledged;
  expect(stopped.ok(), await stopped.text()).toBe(true);
  const receipt = await stopped.json();
  expect(receipt).toMatchObject({
    runId: input.runId,
    valueTier: "CONDUIT_NATIVE",
  });
  expect(["cancellation_requested", "cancelled"]).toContain(receipt.state);
  await expect
    .poll(async () => (await fixture()).disconnected)
    .toBe(before.disconnected + 1);
  expect(await fixture()).toEqual({
    started: before.started + 1,
    active: 0,
    disconnected: before.disconnected + 1,
    expired: before.expired,
  });
  const readback = await page.request.get(sessionPath);
  expect(readback.ok()).toBe(true);
  const recorded = await readback.json();
  expect(recorded.id).toBe(initial.id);
  expect(recorded.intake).toEqual(initial.intake);
  expect(recorded.plan).toEqual(initial.plan);
  expect(recorded.binding).toEqual(initial.binding);
  expect(recorded.computeReceipt).toEqual(initial.computeReceipt);
  await page.goto(`./#s01-intake/${encodeURIComponent(initial.id)}`);
  await expect(flow.getByLabel("Repository URL")).toHaveValue(repository);
  await expect(flow.getByLabel("Ref", { exact: true })).toHaveValue(
    "reviewed-ref",
  );
  await testInfo.attach("native-planning-intake-receipts", {
    contentType: "application/json",
    body: Buffer.from(
      JSON.stringify({ initial, input, receipt, recorded, creates }, null, 2),
    ),
  });
});

test("planning recovery reads the native owner snapshot without repeating a failed provider turn", async ({
  page,
}) => {
  await page.goto("./#s01-intake");
  const origin = new URL(page.url()).origin;
  expect(repository).toMatch(
    /^http:\/\/127\.0\.0\.1:\d+\/planning-cancel\.git$/,
  );
  expect(modelOrigin).toMatch(/^http:\/\/127\.0\.0\.1:\d+$/);
  const calls = async () => {
    const r = await page.request.get(`${modelOrigin}/planning-failure-fixture`);
    expect(r.ok(), await r.text()).toBe(true);
    return (await r.json()).calls as number;
  };
  const baseline = await calls();
  // Session creation is setup; failure, recovery and the stream are native UI actions.
  const created = await page.request.post(`${origin}/v1/planning-sessions`, {
    data: { repoUrl: repository, ref: "reviewed-ref" },
  });
  expect(created.status(), await created.text()).toBe(201);
  const initial = await created.json();
  const sessionPath = `${origin}/v1/planning-sessions/${encodeURIComponent(initial.id)}`;
  await page.goto(`./#s01-intake/${encodeURIComponent(initial.id)}`);
  const flow = page.locator("#main-workspace [data-plan-flow]");
  await expect(flow.getByLabel("Repository URL")).toHaveValue(repository);
  await expect(flow.getByLabel("Ref", { exact: true })).toHaveValue(
    "reviewed-ref",
  );
  const firstRun = page.waitForRequest(
    (r) =>
      r.method() === "POST" && new URL(r.url()).pathname === "/v1/agent-runs",
  );
  await flow
    .getByLabel("Message", { exact: true })
    .fill("CONDUIT_NATIVE_RECOVERY_FAILURE: inspect the local fixture.");
  await flow.locator("[data-plan-send]").click();
  const failed = (await firstRun).postDataJSON();
  expect(failed.threadId).toBe(initial.id);
  await expect(flow.locator("[data-plan-error]")).toBeVisible();
  await expect(flow.locator("[data-plan-recover]")).toBeVisible();
  const failures = await calls();
  expect(failures).toBeGreaterThan(baseline);
  const beforeResponse = await page.request.get(sessionPath);
  expect(beforeResponse.ok(), await beforeResponse.text()).toBe(true);
  const before = await beforeResponse.json();
  expect(before.binding).toBeFalsy();
  const writes: string[] = [];
  page.on("request", (r) => {
    if (r.method() === "POST") writes.push(new URL(r.url()).pathname);
  });
  const recovering = page.waitForRequest(
    (r) =>
      r.method() === "POST" && new URL(r.url()).pathname === "/v1/agent-runs",
  );
  const recovered = page.waitForResponse(
    (r) =>
      r.request().method() === "POST" &&
      new URL(r.url()).pathname === "/v1/agent-runs",
  );
  await flow.locator("[data-plan-recover]").click();
  const input = (await recovering).postDataJSON();
  expect(input.threadId).toBe(initial.id);
  expect(input.runId).toMatch(/^[0-9a-f-]{36}$/);
  expect(input.runId).not.toBe(failed.runId);
  expect(input.forwardedProps).toEqual({ conduit: { mode: "snapshot" } });
  expect(input.resume).toBeUndefined();
  const receipt = await recovered;
  expect(receipt.status()).toBe(200);
  expect(receipt.headers()["content-type"]).toContain("text/event-stream");
  const stream = await receipt.text();
  for (const event of ["RUN_STARTED", "STATE_SNAPSHOT", "RUN_FINISHED"])
    expect(stream).toContain(event);
  expect(stream).not.toContain("RUN_ERROR");
  await expect(flow.locator("[data-plan-recover]")).toHaveCount(0);
  await expect(flow.locator("[data-plan-error]")).toHaveCount(0);
  const peer = await page.request.post(`${origin}/v1/agent-runs`, {
    headers: { "X-Platform-Subject": "other-operator" },
    data: input,
  });
  expect(peer.status()).toBe(404);
  const afterResponse = await page.request.get(sessionPath);
  expect(afterResponse.ok(), await afterResponse.text()).toBe(true);
  const after = await afterResponse.json();
  for (const key of [
    "id",
    "intake",
    "answers",
    "plan",
    "inspection",
    "binding",
    "resolvedCommit",
    "compute",
  ]) {
    expect(after[key]).toEqual(before[key]);
  }
  expect(await calls()).toBe(failures);
  expect(writes).toEqual(["/v1/agent-runs"]);
});

test("planning Accept binds the exact native reviewed source and plan without compute", async ({
  page,
}) => {
  await page.goto("./#s01-intake");
  const origin = new URL(page.url()).origin;
  expect(repository).toMatch(
    /^http:\/\/127\.0\.0\.1:\d+\/planning-cancel\.git$/,
  );
  expect(modelOrigin).toMatch(/^http:\/\/127\.0\.0\.1:\d+$/);
  const calls = async () => {
    const r = await page.request.get(`${modelOrigin}/planning-accept-fixture`);
    expect(r.ok(), await r.text()).toBe(true);
    return r.json() as Promise<{
      inspect: number;
      questions: number;
      plan: number;
    }>;
  };
  const baseline = await calls();
  const created = await page.request.post(`${origin}/v1/planning-sessions`, {
    data: { repoUrl: repository, ref: "reviewed-ref" },
  });
  expect(created.status(), await created.text()).toBe(201);
  const initial = await created.json();
  const path = `${origin}/v1/planning-sessions/${encodeURIComponent(initial.id)}`;
  await page.goto(`./#s01-intake/${encodeURIComponent(initial.id)}`);
  const flow = page.locator("#main-workspace [data-plan-flow]");
  await expect(flow.getByLabel("Repository URL")).toHaveValue(repository);
  const message =
    "CONDUIT_NATIVE_ACCEPT_FIXTURE: prepare a review-only plan for this local source.";
  await flow.getByLabel("Message", { exact: true }).fill(message);
  await flow.locator("[data-plan-send]").click();
  await expect(flow.locator("[data-plan-accept]")).toBeEnabled({
    timeout: 30_000,
  });
  await expect(flow.locator("[data-plan-error]")).toHaveCount(0);
  const reviewedResponse = await page.request.get(path);
  expect(reviewedResponse.ok(), await reviewedResponse.text()).toBe(true);
  const reviewed = await reviewedResponse.json();
  expect(reviewed.resolvedCommit).toMatch(/^[0-9a-f]{40}$/);
  expect(reviewed.plan.sha256).toMatch(/^[0-9a-f]{64}$/);
  expect(JSON.parse(reviewed.plan.text).summary).toContain(
    "CONDUIT_NATIVE_ACCEPT_FIXTURE",
  );
  expect(reviewed.inspection.toolCalls).toEqual(
    expect.arrayContaining(["inspect_repo", "read_file"]),
  );
  expect(reviewed.binding).toBeFalsy();
  const before = await calls();
  expect(before).toEqual({
    inspect: baseline.inspect + 2,
    questions: baseline.questions + 2,
    plan: baseline.plan + 2,
  });
  const refused = await page.request.post(`${path}/accept`, {
    headers: { "X-Platform-Subject": "other-operator" },
    data: {},
  });
  expect(refused.status()).toBe(404);
  const writes: string[] = [];
  page.on("request", (r) => {
    if (r.method() === "POST") writes.push(new URL(r.url()).pathname);
  });
  const accepted = page.waitForResponse(
    (r) => r.request().method() === "POST" && r.url() === `${path}/accept`,
  );
  await flow.locator("[data-plan-accept]").click();
  const receipt = await accepted;
  expect(receipt.status(), await receipt.text()).toBe(200);
  expect(receipt.request().postDataJSON()).toEqual({});
  const recorded = await receipt.json();
  expect(recorded.binding.profile).toEqual(reviewed.profile);
  expect(recorded.binding.intake).toEqual({ ...reviewed.intake, message });
  expect(recorded.binding.resolvedCommit).toBe(reviewed.resolvedCommit);
  expect(recorded.binding.model).toBe(reviewed.model);
  expect(recorded.binding.plan).toEqual(reviewed.plan);
  expect(Number.isFinite(Date.parse(recorded.binding.acceptedAt))).toBe(true);
  expect(recorded.compute).toBeFalsy();
  await expect(flow.locator("[data-plan-accept]")).toHaveCount(0);
  const readback = await page.request.get(path);
  expect(readback.ok()).toBe(true);
  expect((await readback.json()).binding).toEqual(recorded.binding);
  await page.reload();
  await expect(page.locator("[data-plan-accept]")).toHaveCount(0);
  await expect(page.locator("[data-plan-compute]")).toBeVisible();
  expect(await calls()).toEqual(before);
  expect(writes).toEqual([`${new URL(path).pathname}/accept`]);
});

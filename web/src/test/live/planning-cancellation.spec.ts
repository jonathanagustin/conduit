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

import { expect, test } from "@playwright/test";

// Actual Hub, recorded Temporal run and governance store; only the model is a
// scripted HTTP fixture. This does not qualify model quality or execution.
const runId = process.env["CONDUIT_SIMULATED_PACKAGING_RUN"];
const modelOrigin = process.env["CONDUIT_SIMULATED_MODEL_ORIGIN"];
if (!runId || !modelOrigin)
  throw new Error(
    "Run simulate-console.sh --with-packaging for model proposals.",
  );

test("packaging model proposals enforce visibility and deterministic gates without approving the waiting run", async ({
  page,
}, testInfo) => {
  await page.goto("./#pt01-proposal-trace");
  const root = page.locator("#main-workspace");
  const form = root.locator(`form[data-model-run="${runId}"]`);
  await expect(form).toBeVisible();
  const path = `/v1/packaging-runs/${encodeURIComponent(runId)}`;
  const url = (path: string) => new URL(path, page.url()).toString();
  const read = async (path: string) => {
    const response = await page.request.get(url(path));
    expect(response.ok(), await response.text()).toBe(true);
    return response.json();
  };
  const modelCalls = async () => {
    const response = await page.request.get(`${modelOrigin}/packaging-fixture`);
    expect(response.status()).toBe(200);
    return (await response.json()).calls;
  };
  const changes = async () =>
    (
      (await read("/v1/change-requests?limit=200")).changeRequests as {
        id: string;
        targetId: string;
      }[]
    )
      .filter((row) => row.targetId === `${runId}:memory`)
      .map((row) => row.id)
      .sort();
  const parent = await read(path);
  expect(parent).toMatchObject({
    runId,
    phase: "review",
    targetPlanRef: "env-dev/sample-app",
  });
  expect(parent.sourceDigest).toMatch(/^sha256:[0-9a-f]{64}$/);
  expect(parent.artifactDigest).toMatch(/^sha256:[0-9a-f]{64}$/);
  expect(parent.proposalRevision).toBeTruthy();
  expect(parent.planId || "").toBe("");
  const beforeChanges = await changes();
  expect(await modelCalls()).toBe(0);
  const denied = await page.request.post(url(path + "/model-proposal"), {
    headers: { "X-Platform-Subject": "packaging-ungranted-peer" },
    data: {},
  });
  expect(denied.status()).toBe(404);
  expect(await denied.json()).toEqual({
    error: "packaging run not found",
    status: "404",
  });
  expect(await modelCalls()).toBe(0);
  expect(await changes()).toEqual(beforeChanges);

  const submit = async () => {
    const response = page.waitForResponse(
      (value) =>
        value.request().method() === "POST" &&
        new URL(value.url()).pathname === path + "/model-proposal",
    );
    await form
      .getByRole("button", { name: "Propose with model", exact: true })
      .click();
    return response;
  };
  const accepted = await submit();
  expect(accepted.status()).toBe(200);
  const receipt = await accepted.json();
  expect(receipt).toMatchObject({
    runId,
    outcome: "PASS",
    proposal: { field: "memory", value: "512Mi" },
    provenance: {
      adapter: "openai-http",
      configuredModel: "simulated-model",
      providerModel: "simulated-model",
    },
  });
  for (const field of ["promptSha256", "responseSha256", "skillSha256"])
    expect(receipt.provenance[field]).toMatch(/^[0-9a-f]{64}$/);
  expect(receipt.changeRequestId).toBeTruthy();
  await expect(root).toContainText(receipt.changeRequestId);
  await expect(root).toContainText("512Mi");
  const pending = await read(`/v1/change-requests/${receipt.changeRequestId}`);
  expect(pending).toMatchObject({
    id: receipt.changeRequestId,
    targetKind: "PLAN",
    targetId: `${runId}:memory`,
    environment: "env-dev",
    status: "PENDING_APPROVALS",
    author: { username: "operator" },
    requirementsSatisfied: false,
  });
  expect(pending.changes[0].installationId).toBe(runId);
  expect(pending.requirements.length).toBeGreaterThan(0);
  expect(pending.reviews ?? []).toEqual([]);
  expect(pending.planId || "").toBe("");
  expect(await changes()).toEqual(
    [...beforeChanges, receipt.changeRequestId].sort(),
  );
  expect(await modelCalls()).toBe(1);

  const blocked = await submit();
  expect(blocked.status()).toBe(200);
  const blockedReceipt = await blocked.json();
  expect(blockedReceipt).toMatchObject({
    runId,
    outcome: "FAIL",
    proposal: { field: "memory", value: "2Gi" },
  });
  expect(blockedReceipt.changeRequestId).toBeUndefined();
  await expect(root).toContainText("No change request.");
  expect(await modelCalls()).toBe(2);
  const unavailable = await submit();
  expect(unavailable.status()).toBe(422);
  const error = await unavailable.json();
  expect(error.error).toContain("model call failed");
  expect(error.error).not.toMatch(/CLI/);
  await expect(root).toContainText(error.error);
  expect(await changes()).toEqual(
    [...beforeChanges, receipt.changeRequestId].sort(),
  );
  const after = await read(path);
  for (const field of [
    "runId",
    "phase",
    "planId",
    "targetPlanRef",
    "sourceDigest",
    "artifactDigest",
    "proposalRevision",
  ])
    expect(after[field]).toEqual(parent[field]);
  expect(
    (await read(`/v1/change-requests/${receipt.changeRequestId}`)).status,
  ).toBe("PENDING_APPROVALS");
  await testInfo.attach("packaging-model-proposal-receipts", {
    contentType: "application/json",
    body: Buffer.from(
      JSON.stringify(
        { parent, after, receipt, pending, blockedReceipt, error },
        null,
        2,
      ),
    ),
  });
});

import { expect, test, type Page } from "@playwright/test";

type Case = {
  id: string;
  scenario: string;
  boundObject: string;
  value: string;
  expectedOutcome: string;
  source: string;
};
async function catalog(page: Page): Promise<Case[]> {
  const response = await page.request.get(
    new URL("/v1/intelligence/evals", page.url()).toString(),
  );
  expect(response.ok()).toBe(true);
  const body = await response.json();
  expect(Array.isArray(body.cases)).toBe(true);
  return body.cases;
}
async function openEvals(page: Page) {
  await page.goto("./#ai02-evals");
  await expect(page.locator("[data-intel-add-case]")).toBeVisible();
}

test("Intelligence records only confirmed feedback and retains exact deduplicated native cases", async ({
  page,
}) => {
  await openEvals(page);
  const before = await catalog(page);
  const scenario = `Reviewed live feedback <script>literal</script> ${Date.now().toString(16)}`;
  const expected = {
    scenario,
    boundObject: "live-native-case",
    value: "512Mi",
    expectedOutcome: "PASS",
    source: "operator feedback",
  };
  const writes: unknown[] = [];
  page.on("request", (request) => {
    if (
      request.method() === "POST" &&
      new URL(request.url()).pathname === "/v1/intelligence/evals/cases"
    )
      writes.push(request.postDataJSON());
  });
  const form = page.locator("[data-intel-add-case]");
  const fill = async () => {
    await form.locator('[name="scenario"]').fill(scenario);
    await form.locator('[name="boundObject"]').fill(expected.boundObject);
    await form.locator('[name="value"]').fill(expected.value);
    await form
      .locator('[name="expectedOutcome"]')
      .selectOption(expected.expectedOutcome);
  };
  await fill();
  await expect(form).toContainText("not durable across Hub restart");
  await form
    .getByRole("button", { name: "Add feedback case", exact: true })
    .click();
  // Exercise the handler's own confirmation check as well as native HTML validity.
  await form.evaluate((node) =>
    node.dispatchEvent(
      new Event("submit", { bubbles: true, cancelable: true }),
    ),
  );
  expect(await catalog(page)).toEqual(before);
  expect(writes).toHaveLength(0);
  const submit = async () => {
    await form.locator("[data-case-confirm]").check();
    const response = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        new URL(response.url()).pathname === "/v1/intelligence/evals/cases",
    );
    await form
      .getByRole("button", { name: "Add feedback case", exact: true })
      .click();
    const result = await response;
    expect(result.status()).toBe(201);
    return (await result.json()) as Case;
  };
  const added = await submit();
  expect(added).toMatchObject(expected);
  expect(added.id).toMatch(/^case-[0-9a-f]{16}$/);
  expect(writes).toEqual([
    {
      Scenario: scenario,
      BoundObject: expected.boundObject,
      Value: expected.value,
      ExpectedOutcome: expected.expectedOutcome,
    },
  ]);
  let stored = await catalog(page);
  expect(stored).toHaveLength(before.length + 1);
  expect(stored.filter((item) => item.id === added.id)).toEqual([added]);
  const row = page
    .locator("[data-intel-evals] tbody tr")
    .filter({ hasText: added.id });
  await expect(row).toContainText(scenario);
  await expect(row.locator("script,img")).toHaveCount(0);
  await expect(row).toContainText("not run");
  await openEvals(page);
  await expect(row).toContainText(scenario);
  await fill();
  expect(await submit()).toEqual(added);
  stored = await catalog(page);
  expect(stored).toHaveLength(before.length + 1);
  expect(stored.filter((item) => item.id === added.id)).toEqual([added]);
});

test("evaluation runs retain audit identity and separate recorded gate results from provider errors", async ({
  page,
}) => {
  await openEvals(page);
  const cases = await catalog(page);
  const completed = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname === "/v1/intelligence/evals/run",
  );
  await page
    .getByRole("button", { name: "Run supplied cases", exact: true })
    .click();
  const response = await completed;
  expect(response.status()).toBe(200);
  const body = await response.json();
  expect(body.label).toBe("Live model call");
  expect(body.auditEventId).toMatch(/^aud_[0-9a-f]{32}$/);
  expect(body.results.map((row: { case: Case }) => row.case)).toEqual(cases);
  for (const row of body.results) {
    const recorded = row.columns.filter(
      (column: { adapter: string }) => column.adapter === "recorded",
    );
    expect(recorded).toHaveLength(1);
    expect(recorded[0]).toMatchObject({
      label: "No model call",
      outcome: row.case.expectedOutcome,
      passed: true,
    });
  }
  // This harness supplies the assistant's fixed reply, not a packaging proposal.
  // The real HTTP adapter must retain that invalid response as an error; a
  // recorded gate match must never turn it into a provider success.
  const liveCase = body.results.find(
    (row: { case: Case }) => row.case.id === "mem-512",
  );
  expect(liveCase).toBeTruthy();
  const provider = liveCase.columns.filter(
    (column: { adapter: string }) => column.adapter === "openai-http",
  );
  expect(provider).toHaveLength(1);
  expect(provider[0]).toMatchObject({
    label: "Live model call",
    outcome: "error",
    passed: false,
  });
  expect(provider[0].reason).not.toBe("");
  expect(
    body.results
      .filter((row: { case: Case }) => row.case.id !== "mem-512")
      .every((row: { columns: unknown[] }) => row.columns.length === 1),
  ).toBe(true);
  const panel = page.locator("[data-intel-evals]");
  const liveRow = panel.locator("tbody tr").filter({ hasText: "mem-512" });
  await expect(liveRow).toContainText("recorded · PASS · expectation matched");
  await expect(liveRow).toContainText(
    "openai-http · error · expectation differed",
  );
  await expect(
    panel.locator("tbody tr").filter({ hasText: "mem-8gi" }),
  ).toContainText("FAIL_REQUIRED · expectation matched");
  await expect(panel).toContainText("not a deployment gate pass");
  await expect(panel).toContainText(
    "No environment deployment or live-provider validation is inferred.",
  );
  const auditResponse = await page.request.get(
    new URL(
      "/v1/authorization-audit?resourceRid=intelligence%2Fevals&order=desc&limit=100",
      page.url(),
    ).toString(),
  );
  expect(auditResponse.ok()).toBe(true);
  const audit = await auditResponse.json();
  expect(
    audit.events.find(
      (event: { id: string }) => event.id === body.auditEventId,
    ),
  ).toMatchObject({
    subject: "operator",
    action: "intelligence.eval",
    resourceRid: "intelligence/evals",
    decision: "ALLOW",
  });
});

for (const scenario of [
  {
    name: "course",
    route: "ai04-courses",
    actor: "operator",
    heading: "Courses of action",
  },
  {
    name: "logic workbench",
    route: "ai01-logic",
    actor: "proposer",
    heading: "Logic workbench / staged writes",
  },
]) {
  test(`${scenario.name} selection binds a reviewed visible target and refuses ungranted callers`, async ({
    page,
    browser,
  }) => {
    await page
      .context()
      .setExtraHTTPHeaders({ "X-Platform-Subject": scenario.actor });
    await page.goto(`./#${scenario.route}`);
    await expect(
      page.getByRole("heading", { name: scenario.heading, exact: true }),
    ).toBeVisible();
    const form = page.locator("[data-intel-stage]");
    await expect(form).toBeVisible();
    const target = form.locator('[name="target"]');
    await expect(target.locator("option")).not.toHaveCount(0);
    const [environment, installationId, productId] = JSON.parse(
      await target.inputValue(),
    ) as string[];
    expect(environment).not.toBe("");
    expect(installationId).not.toBe("");
    expect(productId).not.toBe("");
    const value = form.locator('[name="value"]');
    await expect(value.locator('option[value="coa-8gi"]')).toBeDisabled();
    await value.selectOption("coa-1gi");
    const confirm = form.locator("[data-intel-confirm]");
    const create = form.locator("[data-intel-create]");
    await expect(create).toBeDisabled();
    const readRequests = async () => {
      const response = await page.request.get(
        new URL("/v1/change-requests?limit=200", page.url()).toString(),
      );
      expect(response.ok()).toBe(true);
      const body = await response.json();
      expect(Array.isArray(body.changeRequests)).toBe(true);
      return body.changeRequests.map((row: { id: string }) => row.id).sort();
    };
    const before = await readRequests();
    const writes: unknown[] = [];
    page.on("request", (request) => {
      if (
        request.method() === "POST" &&
        new URL(request.url()).pathname === "/v1/intelligence/courses/select"
      )
        writes.push(request.postDataJSON());
    });
    await form.evaluate((node) =>
      node.dispatchEvent(
        new Event("submit", { bubbles: true, cancelable: true }),
      ),
    );
    expect(await readRequests()).toEqual(before);
    expect(writes).toHaveLength(0);
    const endpoint = new URL(
      "/v1/intelligence/courses/select",
      page.url(),
    ).toString();
    const blocked = await page.request.post(endpoint, {
      headers: { "X-Conduit-CSRF": "1" },
      data: {
        Value: "8Gi",
        Environment: environment,
        InstallationID: installationId,
        ProductID: productId,
      },
    });
    expect(blocked.status()).toBe(422);
    expect(await readRequests()).toEqual(before);
    await confirm.check();
    await expect(create).toBeEnabled();
    await value.selectOption("coa-512");
    await expect(confirm).not.toBeChecked();
    await expect(create).toBeDisabled();
    await value.selectOption("coa-1gi");
    await confirm.check();
    const completed = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        new URL(response.url()).pathname === "/v1/intelligence/courses/select",
    );
    await create.click();
    const response = await completed;
    expect(response.status()).toBe(200);
    const receipt = await response.json();
    expect(receipt).toMatchObject({
      valueTier: "CONDUIT_NATIVE",
      label: "No model call",
      environment,
      installationId,
      productId,
      value: "1Gi",
      status: "PENDING_APPROVALS",
    });
    expect(receipt.changeRequestId).toMatch(/^crq_[0-9a-f]+$/);
    expect(writes).toEqual([
      {
        Value: "1Gi",
        Environment: environment,
        InstallationID: installationId,
        ProductID: productId,
      },
    ]);
    const storedResponse = await page.request.get(
      new URL(
        `/v1/change-requests/${receipt.changeRequestId}`,
        page.url(),
      ).toString(),
    );
    expect(storedResponse.ok()).toBe(true);
    const stored = await storedResponse.json();
    expect(stored).toMatchObject({
      author: { username: scenario.actor },
      environment,
      targetKind: "CONDUIT_NATIVE_INTELLIGENCE_COURSE",
      status: "PENDING_APPROVALS",
    });
    expect(stored.changes).toHaveLength(1);
    expect(stored.changes[0]).toMatchObject({
      installationId,
      type: "UPDATE_ENTITY",
      request: { memory: "1Gi" },
      diff: { "limits.memory": { after: "1Gi" } },
    });
    expect(await readRequests()).toEqual(
      [...before, receipt.changeRequestId].sort(),
    );
    await expect(form.locator("[data-intel-note]")).toContainText(
      receipt.changeRequestId,
    );
    await expect(form.locator("[data-intel-note]")).toContainText(
      "applied no workload change",
    );
    await expect(confirm).not.toBeChecked();
    await expect(create).toBeDisabled();
    const outsider = await browser.newContext({
      extraHTTPHeaders: { "X-Platform-Subject": "ungranted-course-caller" },
    });
    try {
      const refused = await outsider.request.post(endpoint, {
        headers: { "X-Conduit-CSRF": "1" },
        data: {
          Value: "1Gi",
          Environment: environment,
          InstallationID: installationId,
          ProductID: productId,
        },
      });
      expect(refused.status()).toBe(403);
      expect(await readRequests()).toEqual(
        [...before, receipt.changeRequestId].sort(),
      );
    } finally {
      await outsider.close();
    }
  });
}

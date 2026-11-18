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

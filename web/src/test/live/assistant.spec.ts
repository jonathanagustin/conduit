import { expect, test } from "@playwright/test";

// The Hub serves the embedded bundle with the tenant bootstrap already rendered.
test("the console opens straight to the operator workbench", async ({
  page,
}) => {
  await page.goto("./");
  await expect(page).toHaveTitle("Conduit Console");
  await expect(page.locator("#operator-root")).toBeVisible();
  await expect(page.getByText("Starter Template")).toHaveCount(0);
});

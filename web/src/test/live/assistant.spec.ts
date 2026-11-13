import { expect, test } from "@playwright/test";

// The Hub serves the embedded bundle with the tenant bootstrap already rendered.
test("embedded assistant shell is the default starter", async ({ page }) => {
  await page.goto("./");
  await expect(page.getByText("How can I help you today?")).toBeVisible();
  await expect(page.getByText("Starter Template")).toBeVisible();
  await expect(page.getByRole("link", { name: "assistant-ui" })).toBeVisible();
});

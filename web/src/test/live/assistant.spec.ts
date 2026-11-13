import { expect, test } from "@playwright/test";

// The Hub serves the embedded bundle with the tenant bootstrap already
// rendered. There is no Codex bridge beside that process, so the shell must
// come up and say so rather than invent a session.
test("embedded assistant shell names the tenant and the missing bridge", async ({ page }) => {
  await page.goto("./");
  await expect(page.getByTestId("tenant-title")).toBeVisible();
  await expect(page.getByTestId("bridge-status")).toContainText("not running");
});

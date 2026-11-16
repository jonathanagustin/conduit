import { caption, expect, start, still, test } from "./browser";

const hub = process.env.DEMO_HUB || "http://127.0.0.1:8081";
const repo = process.env.DEMO_REPO_URL || "https://github.com/excalidraw/excalidraw";
const ref = process.env.DEMO_REF || "a2ec2889babf7d2295469c6d90ebe77fae57df84";

test("governed packaging walkthrough", async ({ page }) => {
  start();
  caption("Open the operator console");
  await page.goto(`${hub}/console/#s01-intake`, { waitUntil: "domcontentloaded" });
  await page.locator("[data-plan-field=repoUrl]").fill(repo);
  await page.locator("[data-plan-field=ref]").fill(ref);
  await still(page, "01-intake");
  caption("Inspect the repository");
  await page.locator("[data-plan-send]").click();
  const question = page.locator(
    '[data-plan-answer="node_version"], [data-a2ui-component="question:node_version"] select, [data-a2ui-component="question:node_version"]',
  );
  const accept = page.locator("[data-plan-accept]:not([disabled])");
  await expect(question.or(accept).first()).toBeVisible({ timeout: 180_000 });
  if (await question.count()) {
    caption("Answer Needs Input");
    const select = question.locator("select").or(question);
    await select.first().selectOption("20");
    await page.locator("[data-plan-submit-answers]").click();
    await expect(accept.or(question).first()).toBeVisible({ timeout: 180_000 });
    if (!(await accept.count())) {
      await page.locator("[data-plan-send]").click();
    }
  }
  await expect(accept).toBeVisible({ timeout: 180_000 });
  await still(page, "02-plan");
  caption("Accept the plan");
  await accept.click();
  await expect(page.locator("[data-plan-binding]")).toBeVisible();
  await still(page, "03-accept");
  caption("Build from source and pin the image");
  await page.locator("[data-plan-compute]").click();
  const digest = page.locator("[data-plan-image-digest]");
  await expect(digest).toBeVisible({ timeout: 45 * 60_000 });
  await expect(digest).toContainText("sha256:");
  await still(page, "04-digest");
  const review = page.locator("[data-plan-review]");
  if (await review.count()) {
    caption("Open the change request");
    await review.click();
    await still(page, "05-review");
  }
});

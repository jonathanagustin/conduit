import { expect, test as base, type Page } from "@playwright/test";

/** Live-Hub helpers.
 *
 * The stubbed suite's harness exists to serve responses. This one exists to
 * make sure nothing serves them: every assertion below reads what a real Hub
 * put on the screen.
 *
 * The one thing it does add is a standing objection to console errors. A screen
 * that fails to parse a Hub response frequently still renders -- an empty table
 * and an error in the console -- and an assertion written against the empty
 * table passes. Failing the test on any console error is what turns that into a
 * red run. */

/** The Plan scripts/simulate-console.sh seeded, so a test can look for the
 * identifier the Hub actually minted rather than hard-coding a digest that
 * changes whenever the seed does. */
export const seededPlan = process.env["CONDUIT_SIMULATED_PLAN"] ?? "";

interface LiveFixtures {
  /** Navigate to a console route and fail if the page reports an error. */
  visit(path: string): Promise<void>;
}

export const test = base.extend<LiveFixtures>({
  visit: async ({ page }, use) => {
    const problems: string[] = [];
    page.on("console", (message) => {
      if (message.type() === "error") problems.push(`console: ${message.text()}`);
    });
    page.on("pageerror", (error) => problems.push(`pageerror: ${String(error)}`));
    page.on("requestfailed", (request) => {
      // Only /v1 matters. A favicon the simulation does not serve is noise.
      if (request.url().includes("/v1/")) {
        problems.push(`requestfailed: ${request.url()} ${request.failure()?.errorText ?? ""}`);
      }
    });
    await use(async (path: string) => {
      await page.goto(path);
      await expect(page.getByTestId("app-frame")).toBeVisible();
    });
    expect(problems, "the console reported errors against a real Hub").toEqual([]);
  },
});

export { expect };

/** Assert a screen reached its data rather than its empty state. Written as a
 * helper because "the table has a row" is the assertion every screen here
 * needs, and writing it per screen invites one of them to settle for
 * "the heading rendered", which a Hub serving nothing also satisfies. */
export async function expectRows(page: Page, atLeast = 1): Promise<void> {
  await expect(page.getByRole("row")).not.toHaveCount(0, { timeout: 15_000 });
  // Subtract the header row, which exists whether or not the Hub answered.
  const rows = await page.getByRole("row").count();
  expect(rows - 1, "the screen rendered its table but the Hub filled no rows").toBeGreaterThanOrEqual(atLeast);
}

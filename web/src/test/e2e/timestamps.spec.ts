import { expect, test } from "./harness.js";

/** The relative-time ladder, proved in a browser rather than in a function.
 *
 * `relative()` has unit tests, and they pin every boundary. What they cannot
 * establish is that the value reaches the screen: the unit suite calls the
 * function directly, so a Timestamp that stopped rendering its own output --
 * or rendered it without the exact UTC value beside it -- would leave all 113
 * of them green. Nothing in the e2e suite asserted a rendered timestamp at all
 * before this file, so the whole component was browser-untested.
 *
 * The expectations are Foundry's, recovered from the captured
 * `apollo-control-panel` 6.533.15 bundle -- see
 * `docs/apollo/FRONTEND-CAS-CAPTURE.md` for the strings and the algorithm they
 * come from. Each row below is one rung of that ladder.
 *
 * The clock is fixed rather than mocked: `setFixedTime` pins `Date.now()`
 * without replacing the timers React Query schedules on, so the page still
 * behaves like a page. Fixtures are written as absolute instants against that
 * fixed now, so the expected strings are literals and a reader can check the
 * arithmetic by hand. */

const NOW = "2026-08-07T12:00:00Z";

function planSummary(id: string, createdAt: string) {
  return {
    id,
    environment: "prod-use1",
    status: "SUCCEEDED",
    stepCount: 1,
    approvalCount: 2,
    requiredApprovals: 2,
    createdAt,
    updatedAt: createdAt,
    revision: 1,
  };
}

/** id -> [instant, what the captured ladder renders for it] */
const RUNGS: Array<[string, string, string]> = [
  // Under a second either way is "now", not a zero.
  ["pln_now", "2026-08-07T12:00:00Z", "now"],
  ["pln_seconds", "2026-08-07T11:59:15Z", "45 seconds ago"],
  // Truncated, not rounded: 59m59s is not an hour.
  ["pln_minutes", "2026-08-07T11:00:01Z", "59 minutes ago"],
  ["pln_hours", "2026-08-07T02:00:00Z", "10 hours ago"],
  // The named day is tested before the hour count, so this is not "12 hours ago".
  ["pln_yesterday", "2026-08-06T23:50:00Z", "Yesterday at 23:50 UTC"],
  ["pln_days", "2026-08-05T12:00:00Z", "2 days ago"],
  // 21 days is where days give way to weeks; 60 days to months; 730 to years.
  ["pln_weeks", "2026-07-17T12:00:00Z", "3 weeks ago"],
  ["pln_months", "2026-05-02T12:00:00Z", "3 months ago"],
  // Both sides of the last rung: 729 days is still months, 730 is years.
  ["pln_max_months", "2024-08-08T12:00:00Z", "24 months ago"],
  ["pln_years", "2024-08-07T12:00:00Z", "2 years ago"],
  // A Hub clock ahead of the browser's reads forward, not as a negative past.
  ["pln_ahead", "2026-08-07T12:30:00Z", "in 30 minutes"],
  ["pln_tomorrow", "2026-08-08T01:00:00Z", "Tomorrow at 01:00 UTC"],
];

test.describe("rendered timestamps", () => {
  test("render the captured Foundry ladder, with the exact UTC value beside it", async ({
    page,
    hub,
  }) => {
    await page.clock.setFixedTime(new Date(NOW));
    const plans = RUNGS.map(([id, createdAt]) => planSummary(id, createdAt));
    hub.get("/plans", { plans, total: plans.length, pageSize: plans.length, limit: 100, hasPrevious: false });

    await page.goto("plans");

    for (const [id, createdAt, expected] of RUNGS) {
      const row = page.getByRole("row", { name: new RegExp(id) });
      const rendered = row.locator("time");
      await expect(rendered, `${id} should read "${expected}"`).toHaveText(expected);
      // Relative is what an operator reads; the exact UTC value is what they
      // compare across screens and paste into a ticket. Both, always.
      await expect(rendered).toHaveAttribute("datetime", createdAt);
      await expect(rendered).toHaveAttribute("title", createdAt);
    }
  });

  test("show an unparseable timestamp as sent rather than as Invalid Date", async ({ page, hub }) => {
    await page.clock.setFixedTime(new Date(NOW));
    hub.get("/plans", {
      plans: [planSummary("pln_broken", "not-a-timestamp")],
      total: 1,
      pageSize: 1,
      limit: 100,
      hasPrevious: false,
    });

    await page.goto("plans");

    // The raw value is the only thing that can be reported to whoever sent it.
    const row = page.getByRole("row", { name: /pln_broken/ });
    await expect(row).toContainText("not-a-timestamp");
    await expect(row.locator("time")).toHaveCount(0);
  });
});

import { installation } from "../fixtures.js";
import { expect, test } from "./harness.js";
import { FleetPom } from "./pages/FleetPom.js";

/** Fleet overview -- P0 use case 1 of docs/ui/contracts/overview.md.
 *
 * "See what the fleet is doing, and what is wrong with it." Each test below is
 * one thing an operator has to be able to do on that screen. */

// Paths here are relative on purpose. baseURL ends in /console/, and a leading
// slash resolves against the origin instead -- which lands outside the console
// entirely and is precisely the mistake the base path exists to prevent.
test.describe("fleet overview", () => {
  test("shows every installation with its four status axes", async ({ page, hub }) => {
    hub.fleet([installation()]);
    await page.goto("fleet");

    const row = page.getByRole("row", { name: /payments/ });
    await expect(row).toBeVisible();
    await expect(row).toContainText("prod-use1");
    await expect(row).toContainText("app.example:payments");
    // Desired and observed are separate columns because they disagreeing is the
    // whole point of the screen.
    await expect(row).toContainText("2.4.1");
    await expect(row).toContainText("HEALTHY");
    await expect(row).toContainText("UP_TO_DATE");
    await expect(row).toContainText("APPLIED");
    await expect(row).toContainText("INSTALLED");
  });

  test("summarises the result above the table", async ({ page, hub }) => {
    hub.fleet([
      installation(),
      installation({ installationId: "ledger", health: "UNHEALTHY" }),
    ]);
    await page.goto("fleet");

    // The headline describes the result and nothing else. Per-health counts
    // belong to the rail, where they mean something different -- see
    // docs/ui/contracts/fleet-facets.md.
    const summary = page.getByRole("status", { name: "Fleet summary" });
    await expect(summary).toContainText("2 installations");
    await expect(summary).toContainText("STALE 0");
  });

  test("says when a report is stale rather than presenting it as current", async ({ page, hub }) => {
    // An operator reading an observed version has to know whether it describes
    // the Entity now or the last time it spoke. This is the difference between
    // a stale row and a healthy one.
    hub.fleet([installation({ stale: true, observationAgeSeconds: 7200 })]);
    await page.goto("fleet");

    await expect(page.getByText(/have not reported recently/)).toBeVisible();
    await expect(page.getByRole("row", { name: /payments/ })).toContainText("stale");
  });

  test("does not warn about staleness when nothing is stale", async ({ page, hub }) => {
    hub.fleet([installation()]);
    await page.goto("fleet");

    await expect(page.getByTestId("fleet-page")).toBeVisible();
    await expect(page.getByText(/have not reported recently/)).toHaveCount(0);
  });

  test("filters by status at the Hub, not in the page", async ({ page, hub }) => {
    hub.fleet([installation(), installation({ installationId: "ledger", health: "UNHEALTHY" })]);
    await page.goto("fleet");
    const fleet = new FleetPom(page);
    await expect(fleet.root).toBeVisible();

    await fleet.facetValue("health", "UNHEALTHY").click();

    // The filter has to reach the Hub as a query parameter. Filtering the rows
    // already fetched would filter a page rather than the fleet, and would be
    // wrong the moment the fleet is larger than one response.
    await expect
      .poll(() => hub.requests().some((request) => request.includes("health=UNHEALTHY")))
      .toBe(true);
    await expect(page).toHaveURL(/health=UNHEALTHY/);
  });

  test("searches across Entity, Environment and Product at the Hub", async ({
    page,
    hub,
  }) => {
    hub.fleet([
      installation(),
      installation({ installationId: "ledger", product: "app.example:ledger" }),
    ]);
    await page.goto("fleet");
    const fleet = new FleetPom(page);

    await fleet.search.fill("ledger");
    await fleet.search.press("Enter");

    await expect
      .poll(() => hub.requests().some((request) => request.includes("search=ledger")))
      .toBe(true);
    await expect(page).toHaveURL(/search=ledger/);
  });

  test("says so when nothing matches instead of showing an empty table", async ({ page, hub }) => {
    hub.fleet([]);
    await page.goto("fleet");

    const fleet = new FleetPom(page);
    await fleet.search.fill("nothing-matches-this");
    await fleet.search.press("Enter");

    await expect(page.getByText("No installations match")).toBeVisible();
    await expect(page.getByText('No installation matches "nothing-matches-this".')).toBeVisible();
  });

  test("owns opaque pagination in the URL and preserves the global total", async ({ page, hub }) => {
    hub.fleet([installation({ installationId: "first" })], {
      nextCursor: "opaque-next",
      total: 2,
      limit: 1,
    });
    hub.fleet([installation({ installationId: "second" })], {
      cursor: "opaque-next",
      hasPrevious: true,
      previousCursor: "",
      total: 2,
      limit: 1,
    });
    await page.goto("fleet");
    const fleet = new FleetPom(page);

    await expect(page.getByRole("row", { name: /first/ })).toBeVisible();
    await expect(fleet.pagination).toContainText("Showing 1 of 2 matching installations");
    await expect(fleet.previous).toBeDisabled();
    await fleet.next.click();

    await expect(page).toHaveURL(/cursor=opaque-next/);
    await expect(page.getByRole("row", { name: /second/ })).toBeVisible();
    await expect(fleet.previous).toBeEnabled();
    await expect(fleet.next).toBeDisabled();
    await fleet.previous.click();
    await expect(page).not.toHaveURL(/cursor=/);
    await expect(page.getByRole("row", { name: /first/ })).toBeVisible();
  });

  test("shows an unrecognised status rather than hiding it", async ({ page, hub }) => {
    // The Hub's vocabularies grow. A status the console has no colour for must
    // still reach the operator.
    hub.fleet([installation({ health: "QUARANTINED" })]);
    await page.goto("fleet");

    await expect(page.getByRole("row", { name: /payments/ })).toContainText("QUARANTINED");
  });

  test("distinguishes not being signed in from the fleet being unreadable", async ({
    page,
    hub,
  }) => {
    hub.fails("/fleet/installations", 401, "unauthorized");
    await page.goto("fleet");

    await expect(page.getByText("Not signed in")).toBeVisible();
  });

  test("reports a server failure with the Hub's own message and offers a retry", async ({
    page,
    hub,
  }) => {
    hub.fails("/fleet/installations", 500, "the catalog is unavailable");
    await page.goto("fleet");

    await expect(page.getByText("The fleet could not be read")).toBeVisible();
    await expect(page.getByText("the catalog is unavailable")).toBeVisible();
    await expect(page.getByRole("button", { name: "Try again" })).toBeVisible();
  });
});

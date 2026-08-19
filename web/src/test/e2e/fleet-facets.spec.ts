import { installation } from "../fixtures.js";
import { expect, test } from "./harness.js";
import { FleetPom } from "./pages/FleetPom.js";

/** The fleet's faceted filter rail -- docs/ui/contracts/fleet-facets.md.
 *
 * The rail's whole claim is that a count beside a filter says how many rows
 * that filter would give. Every test here is that claim in one of the states it
 * has to survive: nothing selected, something selected, a selection that matches
 * nothing, a dimension too long to show, a request that failed, and a fleet that
 * is empty. */

// Distinct Products as well as distinct Entities, because the default fixture
// gives every installation the same Product -- which makes a row locator for
// one Entity match every row through the Product column.
const FOUR = [
  installation({
    environment: "prod-use1",
    installationId: "payments",
    product: "app.example:payments",
  }),
  installation({
    environment: "prod-use1",
    installationId: "search",
    product: "app.example:search",
    configStatus: "DRIFTED",
  }),
  installation({
    environment: "prod-euw1",
    installationId: "ledger",
    product: "app.example:ledger",
    health: "UNHEALTHY",
    rolloutStatus: "OUT_OF_DATE",
  }),
  installation({
    environment: "dev-use2",
    installationId: "reporting",
    product: "app.example:reporting",
    health: "UNKNOWN",
  }),
];

test.describe("fleet facet rail", () => {
  test("shows each dimension with its counts", async ({ page, hub }) => {
    hub.fleet(FOUR);
    await page.goto("fleet");
    const fleet = new FleetPom(page);

    await expect(fleet.rail).toBeVisible();
    for (const [key, heading] of [
      ["environment", "Environment"],
      ["product", "Product"],
      ["health", "Health"],
      ["rolloutStatus", "Rollout"],
      ["configStatus", "Config"],
      ["lifecycle", "Lifecycle"],
    ] as const) {
      await expect(fleet.facet(key).getByRole("heading", { name: heading })).toBeVisible();
    }

    // The count is asserted, not just the value: a rail that rendered the
    // vocabulary and ignored the numbers would look identical.
    await expect(fleet.facetValue("health", "HEALTHY", 2)).toBeVisible();
    await expect(fleet.facetValue("health", "UNHEALTHY", 1)).toBeVisible();
    await expect(fleet.facetValue("health", "UNKNOWN", 1)).toBeVisible();
    await expect(fleet.facetValue("environment", "prod-use1", 2)).toBeVisible();
    await expect(fleet.facetValue("configStatus", "DRIFTED", 1)).toBeVisible();

    // Largest first, so a reader finds where the fleet is before reading it.
    await expect(fleet.facet("environment").getByRole("button").first()).toHaveAccessibleName(
      /^prod-use1\s+2$/,
    );
  });

  test("says what the counts are over rather than leaving it assumed", async ({ page, hub }) => {
    hub.fleet(FOUR);
    await page.goto("fleet");

    // A count with no stated scope can be read as the wrong thing, and this one
    // deliberately is not the number of rows in the table.
    await expect(new FleetPom(page).rail).toContainText(
      "Each dimension ignores its own selection",
    );
  });

  test("selecting a value narrows the table and the URL", async ({ page, hub }) => {
    hub.fleet(FOUR);
    hub.fleet([FOUR[2]!], {
      query: "health=UNHEALTHY",
      // The Hub's facets exclude the selected dimension, so HEALTHY is still
      // counted while the result holds one unhealthy row. That divergence is
      // the point of the shape and cannot be derived from the rows.
      facets: { health: { HEALTHY: 2, UNHEALTHY: 1, UNKNOWN: 1 }, environment: { "prod-euw1": 1 } },
    });
    await page.goto("fleet");
    const fleet = new FleetPom(page);
    await expect(page.getByRole("row", { name: /payments/ })).toBeVisible();

    await fleet.facetValue("health", "UNHEALTHY").click();

    await expect(page).toHaveURL(/health=UNHEALTHY/);
    await expect
      .poll(() => hub.requests().some((request) => request.includes("health=UNHEALTHY")))
      .toBe(true);
    await expect(page.getByRole("row", { name: /ledger/ })).toBeVisible();
    await expect(page.getByRole("row", { name: /payments/ })).toHaveCount(0);
    await expect(fleet.facetValue("health", "UNHEALTHY")).toHaveAttribute("aria-pressed", "true");
    // The sibling still reports the whole fleet's healthy installations, which
    // is what makes the rail navigable rather than a dead end.
    await expect(fleet.facetValue("health", "HEALTHY", 2)).toBeVisible();
    // A dimension nobody selected narrows with the result, as it should.
    await expect(fleet.facetValue("environment", "prod-use1")).toHaveCount(0);
  });

  test("deselecting restores the unfiltered result", async ({ page, hub }) => {
    hub.fleet(FOUR);
    hub.fleet([FOUR[2]!], {
      query: "health=UNHEALTHY",
      facets: { health: { HEALTHY: 2, UNHEALTHY: 1, UNKNOWN: 1 } },
    });
    await page.goto("fleet");
    const fleet = new FleetPom(page);

    await fleet.facetValue("health", "UNHEALTHY").click();
    await expect(page).toHaveURL(/health=UNHEALTHY/);
    await fleet.facetValue("health", "UNHEALTHY").click();

    await expect(page).not.toHaveURL(/health=/);
    await expect(page.getByRole("row", { name: /payments/ })).toBeVisible();
  });

  test("keeps a selection that matches nothing liftable", async ({ page, hub }) => {
    // Facets exclude their own dimension, so a selected value the Hub counts at
    // zero is a real state. If the rail dropped it, the only control that could
    // undo the filter would be the one it had just removed.
    hub.fleet([], { query: "health=TERMINAL", facets: { health: { HEALTHY: 2 } } });
    await page.goto("fleet?health=TERMINAL");
    const fleet = new FleetPom(page);

    await expect(fleet.facetValue("health", "TERMINAL", 0)).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await fleet.facetValue("health", "TERMINAL").click();
    await expect(page).not.toHaveURL(/health=/);
  });

  test("bounds a long dimension and names what it withheld", async ({ page, hub }) => {
    const many = Array.from({ length: 12 }, (_, index) =>
      installation({ environment: `env-${index}`, installationId: `entity-${index}` }),
    );
    hub.fleet(many);
    await page.goto("fleet");
    const fleet = new FleetPom(page);

    // Eight values plus the control that names the rest. The number in that
    // label is the assertion: a bound that folds values away without saying how
    // many reads exactly like the end of the list.
    await expect(fleet.facet("environment").getByRole("button")).toHaveCount(9);
    const more = fleet.facet("environment").getByRole("button", { name: "4 more" });
    await expect(more).toBeVisible();
    await expect(fleet.facetValue("environment", "env-9")).toHaveCount(0);

    await more.click();
    await expect(fleet.facet("environment").getByRole("button")).toHaveCount(13);
    await expect(fleet.facetValue("environment", "env-9", 1)).toBeVisible();
  });

  test("an empty fleet reads as empty, not as a rail of zeroes", async ({ page, hub }) => {
    hub.fleet([]);
    await page.goto("fleet");
    const fleet = new FleetPom(page);

    await expect(fleet.rail).toContainText("No installations to summarise.");
    await expect(fleet.facet("health")).toHaveCount(0);
  });

  test("a failed read reads as a failure, not as a rail with no facets", async ({ page, hub }) => {
    hub.fails("/fleet/installations", 500, "the catalog is unavailable");
    await page.goto("fleet");

    await expect(page.getByText("The fleet could not be read")).toBeVisible();
    await expect(page.getByText("the catalog is unavailable")).toBeVisible();
    // The distinction that matters: a rail showing nothing would be read as a
    // fleet with nothing in it.
    await expect(page.getByTestId("facet-rail")).toHaveCount(0);
    await expect(page.getByText("No installations to summarise.")).toHaveCount(0);
  });
});

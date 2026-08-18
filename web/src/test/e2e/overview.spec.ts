import { expect, test } from "./harness.js";
import { ConsoleShell } from "./pages/index.js";

/** Overview -- P0: "What needs attention now?"
 *
 * This is the screen an operator checks first, so its failure modes matter more
 * than its happy path: a panel that could not be read must never look like a
 * panel with nothing in it. */

const QUIET = {
  generatedAt: "2026-08-09T12:00:00Z",
  awaitingApprovals: { status: "CURRENT", count: 0, items: [] },
  failedPlans: { status: "CURRENT", count: 0, items: [] },
  runningPlans: { status: "CURRENT", count: 0, items: [] },
  unhealthyEntities: { status: "CURRENT", count: 0, items: [] },
  staleReports: { status: "CURRENT", count: 0, items: [] },
  blockedPromotions: { status: "CURRENT", count: 0, items: [] },
};

function quiet(hub: { get: (suffix: string, body: unknown) => void }) {
  hub.get("/overview", QUIET);
}

test.describe("Overview", () => {
  test("says outright when nothing needs attention", async ({ page, hub }) => {
    quiet(hub);
    await page.goto("overview");

    await expect(page.getByText("Nothing needs attention")).toBeVisible();
    const reads = hub.requests().filter((request) => request !== "GET /v1/session");
    expect(reads).toEqual(["GET /v1/overview"]);
  });

  test("counts what is waiting on a person", async ({ page, hub }) => {
    hub.get("/overview", {
      ...QUIET,
      awaitingApprovals: {
        status: "CURRENT",
        count: 1,
        items: [{ id: "crq_1", environment: "prod-use1", title: "Upgrade payments" }],
      },
      failedPlans: {
        status: "CURRENT",
        count: 1,
        items: [{ id: "pln_failed", environment: "prod-use1" }],
      },
      unhealthyEntities: {
        status: "CURRENT",
        count: 1,
        items: [{ environment: "prod-use1", installationId: "payments", health: "UNHEALTHY" }],
      },
      blockedPromotions: {
        status: "CURRENT",
        count: 1,
        items: [
          {
            id: "prm_1",
            product: "app.example:payments",
            version: "2.4.2",
            fromChannel: "RELEASE",
            toChannel: "STAGING",
          },
        ],
      },
    });
    await page.goto("overview");

    await expect(page.getByText("Nothing needs attention")).toHaveCount(0);
    await expect(page.getByText("Upgrade payments")).toBeVisible();
    await expect(page.getByText("pln_failed")).toBeVisible();
    await expect(page.getByText(/2\.4\.2/)).toBeVisible();
  });

  test("does not present an unreadable panel as an empty one", async ({ page, hub }) => {
    // "No pending approvals" and "the approval queue could not be read" must
    // never look the same here. This is the screen someone checks to decide
    // whether anything is wrong.
    hub.get("/overview", {
      ...QUIET,
      awaitingApprovals: { status: "UNAVAILABLE", count: 0, items: [] },
    });
    await page.goto("overview");

    await expect(page.getByText("This page is incomplete")).toBeVisible();
    await expect(page.getByText(/Change Requests could not be read/)).toBeVisible();
    // And it must not claim everything is fine.
    await expect(page.getByText("Nothing needs attention")).toHaveCount(0);
    await expect(page.getByText("Unknown", { exact: true })).toHaveCount(1);
    await expect(page.getByText("Could not be read", { exact: true })).toHaveCount(1);
    await expect(page.getByText("Clear", { exact: true })).toHaveCount(5);
  });

  test("does not claim a clear state when the native snapshot cannot be read", async ({
    page,
    hub,
  }) => {
    // A failed request is not six unavailable panels. Rendering it as one hid
    // the Hub's message and left the operator without a retry, under a callout
    // that describes a partial answer this page never received.
    hub.fails("/overview", 500, "overview projection unavailable");
    await page.goto("overview");

    await expect(page.getByText("the control plane could not be read")).toBeVisible();
    await expect(page.getByText("overview projection unavailable")).toBeVisible();
    await expect(page.getByRole("button", { name: "Try again" })).toBeVisible();
    await expect(page.getByText("Nothing needs attention")).toHaveCount(0);
    await expect(page.getByText("This page is incomplete")).toHaveCount(0);
    await expect(page.getByText("Could not be read", { exact: true })).toHaveCount(0);
  });

  test("distinguishes not being signed in from the control plane being unreadable", async ({
    page,
    hub,
  }) => {
    // /console/overview is the default landing route, so this is the first
    // screen a signed-out operator sees. Telling them six sources are
    // unreadable describes a degraded platform and offers nothing to act on;
    // the 30-second poll cannot recover a 401 either.
    hub.fails("/overview", 401, "unauthorized");
    await page.goto("overview");

    await expect(page.getByText("Not signed in")).toBeVisible();
    await expect(page.getByText("This page is incomplete")).toHaveCount(0);
    await expect(page.getByText("Unknown", { exact: true })).toHaveCount(0);
    await expect(page.getByText("Nothing needs attention")).toHaveCount(0);
  });

  test("links each panel to the screen that can act on it", async ({ page, hub }) => {
    quiet(hub);
    await page.goto("overview");

    await page.getByRole("link", { name: "Awaiting approval" }).click();
    await expect(page).toHaveURL(/\/console\/change-requests$/);
  });
});

test.describe("navigation rail", () => {
  test("carries the documented architecture, not a most-used list", async ({ page, hub }) => {
    quiet(hub);
    await page.goto("overview");

    // Home, Environments, Software Catalog, Notifications. Fleet is an
    // Application, matching the plan's IA where it takes the upstream Entity Map
    // role. Notifications is a rail peer rather than an Application because an
    // inbox is per-principal: it is not in the session projection's application
    // vocabulary, and gating it on one would hide an operator's own inbox behind
    // a platform-scoped grant they may not hold.
    const rail = page.getByRole("navigation", { name: "Global navigation" });
    await expect(rail.getByRole("link", { name: "Home" })).toBeVisible();
    await expect(rail.getByRole("link", { name: "Environments" })).toBeVisible();
    await expect(rail.getByRole("link", { name: "Software Catalog" })).toBeVisible();
    await expect(rail.getByRole("link", { name: "Notifications" })).toBeVisible();
    await expect(rail.getByRole("link", { name: "Fleet" })).toHaveCount(0);
  });

  test("reaches Fleet and Change Requests through Applications", async ({ page, hub }) => {
    quiet(hub);
    await page.goto("overview");
    const shell = new ConsoleShell(page);

    await shell.applicationsButton.click();
    await expect(shell.application("Fleet")).toHaveAttribute("href", "/console/fleet");
    await shell.application("Fleet").click();

    await expect(page).toHaveURL(/\/console\/fleet$/);
  });

  test("offers every application the Hub's session projection serves", async ({ page, hub }) => {
    // Eight, from internal/controlplane/session.go. The harness stub carried
    // four for a while, so Export, Import, Risk Management and Teams were in
    // APPLICATION_DESTINATIONS and reachable by URL but appeared in no browser
    // test: the menu under test was a strict subset of the menu operators see,
    // and a destination dropped from the rail would have changed nothing here.
    quiet(hub);
    await page.goto("overview");
    const shell = new ConsoleShell(page);

    await shell.applicationsButton.click();
    for (const [name, href] of [
      ["Change Requests", "/console/change-requests"],
      ["Fleet", "/console/fleet"],
      ["Plans", "/console/plans"],
      ["Promotions", "/console/promotions"],
      ["Export", "/console/export"],
      ["Import", "/console/import"],
      ["Risk Management", "/console/risk"],
      ["Teams", "/console/teams"],
    ] as const) {
      await expect(shell.application(name)).toHaveAttribute("href", href);
    }
  });

  test("returns keyboard focus to Applications when its menu closes", async ({ page, hub }) => {
    quiet(hub);
    await page.goto("overview");
    const shell = new ConsoleShell(page);

    await shell.applicationsButton.focus();
    await shell.applicationsButton.press("Enter");
    await expect(shell.application("Change Requests")).toBeFocused();
    await page.keyboard.press("Escape");

    await expect(shell.applicationsButton).toBeFocused();
  });

  test("shows only applications enabled by the native session projection", async ({ page, hub }) => {
    quiet(hub);
    hub.get("/session", {
      actor: { username: "viewer", teams: [], teamsTruncated: false, serviceAccount: false },
      effective: { username: "viewer", teams: [], teamsTruncated: false, serviceAccount: false },
      delegated: false,
      applications: [
        { id: "change-requests", enabled: true },
        { id: "fleet", enabled: false },
        { id: "graphql-explorer", enabled: true },
      ],
      globalOperations: [],
      resources: [],
    });
    await page.goto("overview");
    const shell = new ConsoleShell(page);

    await shell.applicationsButton.click();
    await expect(shell.application("Change Requests")).toBeVisible();
    await expect(shell.application("Fleet")).toHaveCount(0);
    await expect(shell.application("Export")).toHaveCount(0);
    await expect(page.getByRole("menuitem", { name: /GraphQL/i })).toHaveCount(0);
  });

  test("reports an unreadable session instead of falling back to hardcoded applications", async ({
    page,
    hub,
  }) => {
    quiet(hub);
    hub.fails("/session", 500, "identity projection unavailable");
    await page.goto("overview");
    const shell = new ConsoleShell(page);

    await expect(shell.capabilitiesUnavailable).toBeVisible();
    await expect(shell.applicationsButton).toHaveCount(0);
  });

  test("offers no rail entry for a capability the Hub does not have", async ({ page, hub }) => {
    // The plan puts Search on the rail, but there is no /v1/search contract. A
    // button opening an empty omnibar would promise what the Hub cannot answer.
    quiet(hub);
    await page.goto("overview");

    const rail = page.getByRole("navigation", { name: "Global navigation" });
    await expect(rail.getByRole("link", { name: "Search" })).toHaveCount(0);
  });
});

test.describe("navigation truthfulness", () => {
  test("does not leave Home active on another screen", async ({ page, hub }) => {
    // NavLink matches on prefix, and "/" prefixes everything, so Home stayed lit
    // on every route. A rail that says you are somewhere you are not is worse
    // than one that says nothing.
    quiet(hub);
    await page.goto("environments");

    // aria-current is what NavLink sets, and what a screen reader announces --
    // a better thing to assert than a class name, which is styling.
    const rail = page.getByRole("navigation", { name: "Global navigation" });
    await expect(rail.getByRole("link", { name: "Environments" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    await expect(rail.getByRole("link", { name: "Home" })).not.toHaveAttribute("aria-current");
  });
});

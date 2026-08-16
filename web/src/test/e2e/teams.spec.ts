import { expect, test } from "./harness.js";
import { TeamDetailPom, TeamIndexPom } from "./pages/index.js";

/** Teams -- the governance read screen. GET /v1/teams returns the whole set as
 * one named array, so the index filters locally rather than paging, and the
 * detail shows who administers and who belongs to a team. Fixtures follow the
 * governance.Team Go struct: adminGroups and memberGroups are always present,
 * the user lists and labels are omitempty. */

const PAYMENTS_TEAM = {
  name: "payments",
  displayName: "Payments",
  description: "Owns the payments Product line.",
  adminUsers: ["operator-one"],
  adminGroups: ["payments-leads"],
  memberUsers: ["dev-a", "dev-b"],
  memberGroups: ["payments-engineers"],
  labels: { costCenter: "cc-42" },
  createdAt: "2026-08-01T09:00:00Z",
  updatedAt: "2026-08-07T12:00:00Z",
  revision: 3,
};

const PLATFORM_TEAM = {
  name: "platform",
  displayName: "Platform",
  adminGroups: ["platform-admins"],
  memberGroups: [],
  createdAt: "2026-07-01T09:00:00Z",
  updatedAt: "2026-07-01T09:00:00Z",
  revision: 1,
};

test.describe("Teams", () => {
  test("lists teams with their admin and member reach", async ({ page, hub }) => {
    hub.get("/teams", { teams: [PAYMENTS_TEAM, PLATFORM_TEAM] });
    await page.goto("teams");
    const teams = new TeamIndexPom(page);

    const row = teams.row("Payments");
    // Two admins (one user, one group) and three members (two users, one group).
    await expect(row).toContainText("2");
    await expect(row).toContainText("3");
    await expect(teams.row("Platform")).toBeVisible();
  });

  test("filters the loaded set locally by team, display name, or principal", async ({ page, hub }) => {
    hub.get("/teams", { teams: [PAYMENTS_TEAM, PLATFORM_TEAM] });
    await page.goto("teams");
    const teams = new TeamIndexPom(page);

    await teams.search.fill("platform-admins");
    await teams.search.press("Enter");
    await expect(page).toHaveURL(/search=platform-admins/);
    await expect(teams.row("Platform")).toBeVisible();
    await expect(teams.row("Payments")).toHaveCount(0);
    // Local filtering must not re-query the Hub: one GET for the whole set.
    expect(hub.requests().filter((request) => request.startsWith("GET /v1/teams")).length).toBe(1);
  });

  test("opens a team and shows who administers and who belongs", async ({ page, hub }) => {
    hub.get("/teams", { teams: [PAYMENTS_TEAM] });
    hub.get("/teams/payments", PAYMENTS_TEAM);
    await page.goto("teams");

    await page.getByRole("link", { name: "Payments" }).click();
    const team = new TeamDetailPom(page);
    await expect(team.root).toBeVisible();

    await expect(team.admins).toContainText("operator-one");
    await expect(team.admins).toContainText("payments-leads");
    await expect(team.members).toContainText("dev-a");
    await expect(team.members).toContainText("payments-engineers");
    await expect(team.root).toContainText("costCenter: cc-42");
  });

  test("says a role names no one rather than showing an empty list", async ({ page, hub }) => {
    hub.get("/teams/platform", PLATFORM_TEAM);
    await page.goto("teams/platform");
    const team = new TeamDetailPom(page);

    await expect(team.admins).toContainText("platform-admins");
    await expect(team.members).toContainText("No members are named.");
  });

  test("explains an empty catalog and a filter that matches nothing", async ({ page, hub }) => {
    hub.get("/teams", { teams: [] });
    await page.goto("teams");
    await expect(page.getByText("No teams are defined.")).toBeVisible();

    hub.get("/teams", { teams: [PAYMENTS_TEAM] });
    await page.goto("teams");
    const teams = new TeamIndexPom(page);
    await teams.search.fill("nothing-here");
    await teams.search.press("Enter");
    await expect(page.getByText('No team matches "nothing-here".')).toBeVisible();
  });

  test("surfaces a Hub failure with a retry rather than an empty table", async ({ page, hub }) => {
    hub.fails("/teams", 500, "team store unavailable");
    await page.goto("teams");

    await expect(page.getByText(/could not be read/i)).toBeVisible();
    hub.get("/teams", { teams: [PAYMENTS_TEAM] });
    await page.getByRole("button", { name: "Try again" }).click();
    await expect(new TeamIndexPom(page).row("Payments")).toBeVisible();
  });
});

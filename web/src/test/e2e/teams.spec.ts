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

  test("lists a team's contact methods and marks the undeliverable kind", async ({ page, hub }) => {
    hub.get("/teams/payments", PAYMENTS_TEAM);
    hub.get("/notifications/contacts", {
      owner: { kind: "TEAM", id: "payments" },
      contacts: [
        { id: "c-1", owner: { kind: "TEAM", id: "payments" }, kind: "EMAIL", address: "payments-oncall@example.test", optIntoFutureResources: true, createdAt: "2026-08-01T09:00:00Z" },
        { id: "c-2", owner: { kind: "TEAM", id: "payments" }, kind: "PAGERDUTY", address: "PDABC123", createdAt: "2026-08-01T09:00:00Z" },
      ],
    });
    await page.goto("teams/payments");
    const team = new TeamDetailPom(page);
    await team.openContacts();

    await expect(team.contacts).toContainText("payments-oncall@example.test");
    // PAGERDUTY is created but not delivered to, mirroring the Hub.
    await expect(page.getByTestId("team-contact-PAGERDUTY")).toContainText("not delivered");
    await expect(page.getByTestId("team-contact-EMAIL")).toContainText("Auto-subscribed");
  });

  test("explains that only members may see a team's contacts", async ({ page, hub }) => {
    hub.get("/teams/payments", PAYMENTS_TEAM);
    // The address book refuses an owner the caller does not belong to; that is a
    // membership fact, not a broken console, so the panel says so.
    hub.fails("/notifications/contacts", 403, "notifications for TEAM payments belong to that owner");
    await page.goto("teams/payments");
    const team = new TeamDetailPom(page);
    await team.openContacts();

    await expect(page.getByTestId("team-contacts-error")).toContainText("Only members of this team");
  });

  test("adds a contact method and re-reads the stored list", async ({ page, hub }) => {
    hub.get("/teams/payments", PAYMENTS_TEAM);
    hub.get("/notifications/contacts", { owner: { kind: "TEAM", id: "payments" }, contacts: [] });
    await page.goto("teams/payments");
    const team = new TeamDetailPom(page);
    await team.openContacts();
    await expect(team.contacts).toContainText("This team has no contact methods.");

    hub.put("/notifications/contacts", {
      id: "c-9", owner: { kind: "TEAM", id: "payments" }, kind: "SLACK", address: "#payments-alerts", createdAt: "2026-08-09T12:00:00Z",
    });
    // The re-read after the write returns the stored contact (last stub wins).
    hub.get("/notifications/contacts", {
      owner: { kind: "TEAM", id: "payments" },
      contacts: [{ id: "c-9", owner: { kind: "TEAM", id: "payments" }, kind: "SLACK", address: "#payments-alerts", createdAt: "2026-08-09T12:00:00Z" }],
    });

    await page.getByLabel("Contact kind").selectOption("SLACK");
    await page.getByLabel("Contact address").fill("#payments-alerts");
    await page.getByRole("button", { name: "Save contact" }).click();

    await expect(page.getByTestId("team-contact-SLACK")).toContainText("#payments-alerts");
    // The console sent the team owner and the chosen kind, not a bare address.
    const sent = hub.bodies().at(-1) as { owner: { kind: string; id: string }; kind: string; address: string };
    expect(sent.owner).toEqual({ kind: "TEAM", id: "payments" });
    expect(sent.kind).toBe("SLACK");
  });

  test("removes a contact method", async ({ page, hub }) => {
    hub.get("/teams/payments", PAYMENTS_TEAM);
    hub.get("/notifications/contacts", {
      owner: { kind: "TEAM", id: "payments" },
      contacts: [{ id: "c-1", owner: { kind: "TEAM", id: "payments" }, kind: "EMAIL", address: "payments-oncall@example.test", createdAt: "2026-08-01T09:00:00Z" }],
    });
    await page.goto("teams/payments");
    const team = new TeamDetailPom(page);
    await team.openContacts();
    await expect(page.getByTestId("team-contact-EMAIL")).toBeVisible();

    hub.del("/notifications/contacts", { owner: { kind: "TEAM", id: "payments" }, kind: "EMAIL", deleted: true });
    hub.get("/notifications/contacts", { owner: { kind: "TEAM", id: "payments" }, contacts: [] });
    await page.getByRole("button", { name: "Remove EMAIL contact" }).click();

    await expect(team.contacts).toContainText("This team has no contact methods.");
    // The delete named the kind in the query string, the way the route reads it.
    expect(hub.requests().some((request) => request.startsWith("DELETE /v1/notifications/contacts") && request.includes("kind=EMAIL"))).toBe(true);
  });

  test("shows what has reached the team inbox", async ({ page, hub }) => {
    hub.get("/teams/payments", PAYMENTS_TEAM);
    hub.get("/notifications/inbox", {
      owner: { kind: "TEAM", id: "payments" },
      channel: "IN_APP",
      limit: 100,
      entries: [
        {
          recipient: { owner: { kind: "TEAM", id: "payments" }, channel: "IN_APP" },
          event: {
            id: "tev-1",
            type: "PLAN_FAILURES",
            resourceRid: "ri.apollo.main.environment.d41d8cd98f00",
            environment: "prod-use1",
            summary: "Reconciliation of installation payments in prod-use1 gave up",
            occurredAt: "2026-08-09T11:42:00Z",
          },
          preferenceId: "tpf-1",
        },
      ],
    });
    await page.goto("teams/payments");
    const team = new TeamDetailPom(page);
    await team.openNotifications();

    await expect(page.getByTestId("team-inbox-entry-tev-1")).toContainText("PLAN_FAILURES");
    await expect(team.notifications).toContainText("Reconciliation of installation payments");
    // The read named the team owner, which is what makes a team inbox reachable.
    expect(hub.requests().some((request) => request.startsWith("GET /v1/notifications/inbox") && request.includes("ownerId=payments"))).toBe(true);
  });

  test("explains that only members may read a team's notifications", async ({ page, hub }) => {
    hub.get("/teams/payments", PAYMENTS_TEAM);
    hub.fails("/notifications/inbox", 403, "notifications for TEAM payments belong to that owner");
    await page.goto("teams/payments");
    const team = new TeamDetailPom(page);
    await team.openNotifications();

    await expect(page.getByTestId("team-notifications-error")).toContainText("Only members of this team");
  });
});

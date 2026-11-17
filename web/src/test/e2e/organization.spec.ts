import { expect, test } from "@playwright/test";
import { installPackagingReadStub, hydrate } from "./console-fixture";

installPackagingReadStub();

test("organization keeps empty and denied snapshots native and read only", async ({
  page,
}) => {
  let mode = "empty";
  const writes: string[] = [];
  await page.route("**/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (route.request().method() !== "GET") writes.push(path);
    if (path === "/v1/session")
      return route.fulfill({
        json: {
          actor: { username: "alice" },
          effective: { username: "alice", teams: [] },
          applications: [],
        },
      });
    if (path === "/v1/operator-identity")
      return route.fulfill({
        status: mode === "denied" ? 503 : 200,
        json:
          mode === "denied"
            ? { error: "unavailable" }
            : { enabled: false, providers: [] },
      });
    if (
      ["/v1/teams", "/v1/resource-grants", "/v1/approval-policies"].includes(
        path,
      )
    ) {
      if (mode === "denied")
        return route.fulfill({ status: 403, json: { error: "forbidden" } });
      if (path === "/v1/teams")
        return route.fulfill({
          json: {
            teams:
              mode === "empty"
                ? []
                : [
                    {
                      name: "live-team",
                      displayName: "Live Team",
                      memberUsers: ["one", "two"],
                      memberGroups: ["group"],
                    },
                  ],
          },
        });
      if (path === "/v1/resource-grants")
        return route.fulfill({ json: { resourceGrants: [] } });
      return route.fulfill({ json: { approvalPolicies: [] } });
    }
    if (path === "/v1/environments")
      return route.fulfill({ json: { environments: [] } });
    return route.fulfill({ status: 404, json: { error: "not configured" } });
  });
  await page.route("**/health/ready", (route) =>
    route.fulfill({ json: { status: "ok" } }),
  );
  await hydrate(page);
  await page.goto("/console/#adm02-org-control-panel");
  await expect(page.locator("[data-org-snapshot]")).toContainText("read only");
  await expect(
    page.locator(
      "#main-workspace input, #main-workspace select, #main-workspace textarea",
    ),
  ).toHaveCount(0);
  await page
    .getByRole("radio", { name: "Teams and roles", exact: true })
    .click();
  await expect(page.locator("[data-org-directory-status]")).toContainText(
    "0 stored teams returned",
  );
  await expect(page.locator("#main-workspace")).not.toContainText(
    "release-managers",
  );
  mode = "populated";
  await page
    .getByRole("button", { name: "Refresh snapshot", exact: true })
    .click();
  const row = page.getByRole("row").filter({ hasText: "Live Team" });
  await expect(row.getByRole("cell").nth(1)).toHaveText("2");
  await expect(row.getByRole("cell").nth(2)).toHaveText("1");
  mode = "denied";
  await page
    .getByRole("button", { name: "Refresh snapshot", exact: true })
    .click();
  await expect(page.locator("[data-org-directory-status]")).toContainText(
    "unavailable",
  );
  await expect(page.locator("#main-workspace")).not.toContainText("Live Team");
  await expect(page.locator("#main-workspace")).not.toContainText(
    "Example data",
  );
  await page
    .getByRole("radio", { name: "Identity provider", exact: true })
    .click();
  await expect(page.locator("#main-workspace")).toContainText(
    "metadata is unavailable",
  );
  expect(writes).toEqual([]);
});

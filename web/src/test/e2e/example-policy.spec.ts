import { expect, test } from "@playwright/test";
import { installPackagingReadStub, hydrate } from "./console-fixture";

installPackagingReadStub();

test("proposal examples never replace missing native evidence without opt-in", async ({
  page,
}) => {
  await hydrate(page);
  let native = false;
  const writes: string[] = [];
  page.on("request", (request) => {
    const path = new URL(request.url()).pathname;
    if (request.method() !== "GET" && path.startsWith("/v1/"))
      writes.push(path);
  });
  await page.route("**/v1/**", (route) => route.fulfill({ json: {} }));
  await page.route(
    "**/conduit.packaging.v1.PackagingRunService/ListPackagingRuns",
    (route) =>
      route.fulfill({
        json: {
          configured: true,
          items: native
            ? [{ runId: "recorded-run", phase: "review", outcome: "WAITING" }]
            : [],
          total: native ? "1" : "0",
          pageSize: native ? 1 : 0,
          limit: 20,
        },
      }),
  );
  await page.goto("/console/");
  const workspace = page.locator("#main-workspace");
  const open = async (screen: string) =>
    page.evaluate((id) => Reflect.get(window, "switchScreen")(id), screen);
  await open("pt01-proposal-trace");
  await expect(
    workspace.getByText(
      "No recorded packaging proposal is available in the current Hub view.",
    ),
  ).toBeVisible();
  await expect(
    workspace.getByText("failedPlan PLN-2026-0416", { exact: false }),
  ).toHaveCount(0);
  await page.keyboard.press("Control+Enter");
  await expect(
    workspace.getByRole("button", {
      name: "Simulate trace and staged proposal",
    }),
  ).toHaveCount(0);
  await workspace
    .getByRole("button", { name: "Show proposal example data" })
    .click();
  await expect(
    workspace.getByRole("status").filter({ hasText: "Example data. Trace" }),
  ).toContainText("do not run a model");
  await workspace
    .getByRole("button", { name: "Simulate trace and staged proposal" })
    .click();
  await expect(workspace.getByText(/Staged PRP-903 for review/)).toBeVisible();
  await workspace
    .getByRole("button", { name: "Exit proposal example mode" })
    .click();
  await expect(workspace.getByText(/Staged PRP-903 for review/)).toHaveCount(0);
  await open("pt01-proposal-trace");
  await workspace
    .getByRole("button", { name: "Show proposal example data" })
    .click();
  native = true;
  await page.evaluate(() => Reflect.get(window, "refreshHub")());
  await expect(
    workspace.getByText("Recorded proposal", { exact: false }).first(),
  ).toBeVisible();
  await expect(
    workspace.getByRole("button", { name: "Exit proposal example mode" }),
  ).toHaveCount(0);
  native = false;
  await page.evaluate(() => Reflect.get(window, "refreshHub")());
  await expect(
    workspace.getByRole("button", { name: "Show proposal example data" }),
  ).toBeVisible();
  await expect(
    workspace.getByText("failedPlan PLN-2026-0416", { exact: false }),
  ).toHaveCount(0);
  expect(writes).toEqual([]);
});

test("operational prototype rows require a per-view example choice", async ({
  page,
}) => {
  await hydrate(page);
  let operator = "alice";
  const writes: string[] = [];
  page.on("request", (request) => {
    if (
      request.method() !== "GET" &&
      new URL(request.url()).pathname.startsWith("/v1/")
    )
      writes.push(request.url());
  });
  await page.route("**/v1/**", (route) => route.fulfill({ json: {} }));
  await page.route("**/v1/session**", (route) =>
    route.fulfill({
      json: {
        actor: { username: operator },
        effective: { username: operator, teams: [] },
        applications: [],
      },
    }),
  );
  await page.goto("/console/");
  const root = page.locator("#main-workspace");
  const open = (id: string) =>
    page.evaluate((screen) => Reflect.get(window, "switchScreen")(screen), id);
  await open("area-command");
  await page
    .getByRole("button", { name: "Example fleet", exact: true })
    .click();
  await expect(root.getByText("Pipeline Agent", { exact: false })).toHaveCount(
    0,
  );
  await root
    .getByRole("button", { name: "Show example data", exact: true })
    .click();
  await expect(
    root.getByRole("status").filter({ hasText: "local prototype" }),
  ).toBeVisible();
  await expect(
    root.getByText("Pipeline Agent", { exact: false }).first(),
  ).toBeVisible();
  await root
    .getByRole("button", { name: "Exit example mode", exact: true })
    .click();
  await expect(root.getByText("Pipeline Agent", { exact: false })).toHaveCount(
    0,
  );
  await root
    .getByRole("button", { name: "Show example data", exact: true })
    .click();
  operator = "bob";
  await page.evaluate(() => Reflect.get(window, "refreshHub")());
  await expect(
    root.getByRole("button", { name: "Show example data", exact: true }),
  ).toBeVisible();
  await expect(root.getByText("Pipeline Agent", { exact: false })).toHaveCount(
    0,
  );
  await root
    .getByRole("button", { name: "Show example data", exact: true })
    .click();
  await open("s05-backlog");
  await expect(root.getByText("STR-216", { exact: false })).toHaveCount(0);
  await expect(
    root.getByRole("button", { name: "Show example data", exact: true }),
  ).toBeVisible();
  await open("area-command");
  await page
    .getByRole("button", { name: "Example fleet", exact: true })
    .click();
  await expect(
    root.getByRole("button", { name: "Show example data", exact: true }),
  ).toBeVisible();
  await open("missing-view");
  await expect(
    root.getByRole("region", { name: "View unavailable" }),
  ).toBeVisible();
  await expect(root.getByText("Pipeline Agent", { exact: false })).toHaveCount(
    0,
  );
  expect(writes).toEqual([]);
});

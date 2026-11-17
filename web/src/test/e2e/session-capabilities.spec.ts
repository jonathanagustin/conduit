import { expect, test, type Page } from "@playwright/test";

import { APPLICATIONS, session, stubHub } from "./hub-stub";

const rail = (page: Page, name: string) =>
  page.locator("#left-nav").getByRole("button", { name, exact: true });

/** What the Hub returns for an operator without resource:read at the root. */
const restricted = session({
  applications: APPLICATIONS.map((app) => ({
    ...app,
    enabled: ["change-requests", "plans", "teams"].includes(app.id),
  })),
  globalOperations: [],
});

test("navigation shows only the applications the session enables", async ({
  page,
}) => {
  await stubHub(page, (_request, url) => {
    if (url.pathname === "/v1/session") return { json: restricted };
    if (url.pathname === "/v1/environments")
      return { json: { environments: [] } };
    return undefined;
  });
  await page.goto("/console/");
  await expect(rail(page, "Change requests")).toBeVisible();
  await expect(rail(page, "Releases")).toBeVisible();
  // No application is published for Intelligence, so it stays.
  await expect(rail(page, "Intelligence")).toBeVisible();
  await expect(rail(page, "Fleet")).toHaveCount(0);
  await expect(page.locator('[data-capabilities="restricted"]')).toContainText(
    "not enabled for this session",
  );

  await rail(page, "Releases").click();
  await expect(page.locator('[data-area-tab="plans"]')).toBeVisible();
  await expect(page.locator('[data-area-tab="promotion"]')).toHaveCount(0);
  await expect(page.locator('[data-area-tab="waves"]')).toHaveCount(0);
  // An empty list is a filtered answer for this session, and says so.
  await expect(page.locator("[data-no-grants]")).toContainText(
    "You have no grants here",
  );

  // A direct link to a withheld area states the missing grant.
  await page.goto("/console/#area-fleet");
  await expect(
    page.locator('[data-capability-blocked="disabled"]'),
  ).toContainText("Fleet is not enabled for this session");
});

test("a fully granted session sees every area and no grant notice", async ({
  page,
}) => {
  await stubHub(page, (_request, url) => {
    if (url.pathname === "/v1/session") return { json: session() };
    if (url.pathname === "/v1/environments")
      return { json: { environments: [] } };
    return undefined;
  });
  await page.goto("/console/");
  await expect(rail(page, "Fleet")).toBeVisible();
  await expect(rail(page, "Change requests")).toBeVisible();
  await expect(page.locator("[data-capabilities]")).toHaveCount(0);
  await rail(page, "Releases").click();
  await expect(page.locator('[data-area-tab="promotion"]')).toBeVisible();
  await expect(page.locator("[data-no-grants]")).toHaveCount(0);
});

test("a failed session read is stated, and Retry restores navigation", async ({
  page,
}) => {
  let broken = true;
  await stubHub(page, (_request, url) => {
    if (url.pathname !== "/v1/session") return undefined;
    return broken
      ? { status: 503, json: { error: "session unavailable" } }
      : { json: session() };
  });
  await page.goto("/console/");
  const notice = page.locator('[data-capabilities="unavailable"]');
  await expect(notice).toContainText("Capabilities unavailable");
  await expect(notice).toContainText("navigation is incomplete");
  // Neither everything nor nothing: mapped areas are withheld, others stay.
  await expect(rail(page, "Fleet")).toHaveCount(0);
  await expect(rail(page, "Change requests")).toHaveCount(0);
  await expect(rail(page, "Packaging")).toBeVisible();
  await expect(rail(page, "Intelligence")).toBeVisible();
  broken = false;
  await notice.getByRole("button", { name: "Retry" }).click();
  await expect(rail(page, "Fleet")).toBeVisible();
  await expect(rail(page, "Change requests")).toBeVisible();
  await expect(notice).toHaveCount(0);
});

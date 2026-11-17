import { expect, test, type Route } from "@playwright/test";

import { session, stubHub } from "./hub-stub";

const coverage = {
  typeAvailabilityScope: "ALL_KNOWN",
  typesNotYetEmitted: ["RBAC_CHANGE"],
};

for (const change of ["resource", "channel", "draft"] as const) {
  test(`a ${change} change cancels the reviewed save and requires another explicit Save`, async ({
    page,
  }) => {
    const writes: Record<string, unknown>[] = [];
    await stubHub(page, (request, url) => {
      if (url.pathname === "/v1/session") return { json: session() };
      if (url.pathname === "/v1/notifications/inbox")
        return {
          json: {
            owner: { kind: "USER", id: "riley" },
            channel: "IN_APP",
            entries: [],
            limit: 100,
          },
        };
      if (url.pathname === "/v1/notifications/contacts")
        return { json: { contacts: [] } };
      if (url.pathname !== "/v1/notifications/preferences") return undefined;
      if (request.method() === "GET")
        return { status: 404, json: { error: "absent", ...coverage } };
      const body = request.postDataJSON();
      writes.push(body);
      return {
        json: {
          ...body,
          owner: { kind: "USER", id: "riley" },
          revision: 1,
          ...coverage,
        },
      };
    });
    await page.goto("/console/notifications");
    await page.locator('.kit-subnav [data-ntf-view="preferences"]').click();
    await page.getByLabel("Resource").fill("product/A");
    await page.getByRole("button", { name: "Load preference" }).click();
    await page.locator('[data-ntf-type="PLAN_FAILURES"]').check();

    let release!: () => void;
    let captured!: (route: Route) => void;
    let completed!: () => void;
    const held = new Promise<Route>((resolve) => {
      captured = resolve;
    });
    const released = new Promise<void>((resolve) => {
      release = resolve;
    });
    const done = new Promise<void>((resolve) => {
      completed = resolve;
    });
    let holding = true;
    await page.route("**/v1/notifications/preferences?**", async (route) => {
      if (!holding) return route.fallback();
      holding = false;
      captured(route);
      await released;
      // The browser has aborted the request. Delivering a late response must
      // never become a mutation or a receipt for the replacement draft.
      await route
        .fulfill({ status: 404, json: { error: "absent", ...coverage } })
        .catch(() => {});
      completed();
    });
    const save = page.getByRole("button", { name: "Save preference" });
    await save.click();
    const oldRead = (await held).request();
    const aborted = page.waitForEvent("requestfailed", {
      predicate: (request) => request === oldRead,
    });
    if (change === "resource") {
      await page.getByLabel("Resource").fill("product/B");
      await page.getByRole("button", { name: "Load preference" }).click();
      await expect(page.locator("[data-preference-editor]")).toContainText(
        "product/B",
      );
    } else if (change === "channel") {
      await page.locator("[data-ntf-channel]").selectOption("EMAIL");
      await expect(page.locator("[data-preference-editor]")).toContainText(
        "EMAIL",
      );
    } else await page.locator('[data-ntf-type="PRODUCT_RECALLS"]').check();
    await aborted;
    release();
    await done;
    await page.evaluate(() => new Promise(requestAnimationFrame));
    expect(writes).toEqual([]);
    await expect(page.locator("[data-preference-saved]")).toHaveCount(0);

    await page.locator('[data-ntf-type="PRODUCT_RECALLS"]').check();
    await save.click();
    await expect(page.locator("[data-preference-saved]")).toBeVisible();
    expect(writes).toHaveLength(1);
    expect(writes[0]).toMatchObject({
      resourceRid: change === "resource" ? "product/B" : "product/A",
      channel: change === "channel" ? "EMAIL" : "IN_APP",
      types:
        change === "draft"
          ? ["PLAN_FAILURES", "PRODUCT_RECALLS"]
          : ["PRODUCT_RECALLS"],
    });
  });
}

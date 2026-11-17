import { expect, test } from "@playwright/test";
import { session, stubHub } from "./hub-stub";

test("compact toolbar keeps a pointer target through initial identity and live-status reads", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 720 });
  await stubHub(page, () => ({
    json: { items: [], plans: [], environments: [], bundles: [], events: [] },
  }));
  let finishSession!: () => void;
  const sessionReady = new Promise<void>((resolve) => {
    finishSession = resolve;
  });
  await page.route("**/v1/session", async (route) => {
    await sessionReady;
    await route.fulfill({ json: session() });
  });
  try {
    await page.goto("/console/");
    await page.evaluate(() => document.fonts.ready);
    const chat = page.getByRole("button", { name: "Toggle right chat panel" });
    await chat.scrollIntoViewIfNeeded();
    const before = await chat.boundingBox();
    expect(before).not.toBeNull();
    await page.mouse.move(
      before!.x + before!.width / 2,
      before!.y + before!.height / 2,
    );
    await page.mouse.down();
    finishSession();
    await expect(page.locator("#operator-name")).toHaveText("riley");
    await expect(page.locator("#live-events-status")).toBeVisible();
    const after = await chat.boundingBox();
    expect(after).not.toBeNull();
    const x = before!.x + before!.width / 2;
    const y = before!.y + before!.height / 2;
    expect(x >= after!.x && x < after!.x + after!.width).toBe(true);
    expect(y >= after!.y && y < after!.y + after!.height).toBe(true);
    expect(
      await chat.evaluate(
        (element, point) =>
          element.contains(document.elementFromPoint(point.x, point.y)),
        { x, y },
      ),
    ).toBe(true);
    expect(await page.evaluate(() => window.scrollX)).toBe(0);
    await page.mouse.up();
    await expect(page.getByLabel("Message input")).toBeVisible();
  } finally {
    finishSession();
  }
});

test("navigation and area tabs retain focus when their view is repainted", async ({
  page,
}) => {
  await stubHub(page, (_request, url) => {
    if (url.pathname === "/v1/session") return { json: session() };
    return {
      json: { items: [], plans: [], environments: [], bundles: [], events: [] },
    };
  });
  await page.goto("/console/");
  await expect(page.locator("#operator-name")).toHaveText("riley");
  const releases = page
    .locator("#left-nav")
    .getByRole("button", { name: /^Releases\b/ });
  await releases.focus();
  await page.keyboard.press("Enter");
  await expect(releases).toBeFocused();
  const tab = page.locator('[data-area-tab="environments"]');
  await tab.click();
  await expect(tab).toBeFocused();
  await page.evaluate(() => Reflect.get(window, "renderMainWorkspace")());
  await expect(tab).toBeFocused();
});

import { expect, test } from "@playwright/test";
import { AREAS } from "../../console/operator/navigation.js";
import { session, stubHub } from "./hub-stub";

// Pixel baselines are qualified per renderer; each remains zero tolerance.
const renderer =
  process.env["CONDUIT_LOCAL_CI_BROWSER_PROFILE"] === "qemu-x86_64-no-zygote"
    ? "qemu-x86-64-no-zygote"
    : "native";

for (const [size, viewport] of Object.entries({
  desktop: { width: 1440, height: 900 },
  phone: { width: 390, height: 844 },
})) {
  for (const theme of ["light", "dark"]) {
    for (const [area, spec] of Object.entries(AREAS)) {
      test(`${size} ${theme} ${spec.label}`, async ({ page }) => {
        await page.setViewportSize(viewport);
        await page.clock.setFixedTime(new Date("2026-11-17T00:00:00Z"));
        await page.addInitScript(
          (theme) => localStorage.setItem("conduit.console.theme", theme),
          theme,
        );
        await stubHub(page, (_request, url) => {
          if (url.pathname === "/v1/session") return { json: session() };
          return {
            json: {
              items: [],
              plans: [],
              environments: [],
              bundles: [],
              events: [],
            },
          };
        });
        await page.goto(`/console/#${area}`);
        await expect(page.locator("#main-workspace")).toContainText(spec.label);
        await expect
          .poll(() =>
            page.evaluate(() => Reflect.get(window, "state")?.hub?.loading),
          )
          .toBe(false);
        await page.evaluate(() => document.fonts.ready);
        await expect(page).toHaveScreenshot(
          `${size}-${theme}-${area}-${renderer}.png`,
          {
            animations: "disabled",
            caret: "hide",
            fullPage: true,
            maxDiffPixels: 0,
            threshold: 0,
          },
        );
      });
    }
  }
}

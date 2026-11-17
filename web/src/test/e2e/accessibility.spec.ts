import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";
import { AREAS } from "../../console/operator/navigation.js";
import { session, stubHub } from "./hub-stub";

for (const theme of ["light", "dark"]) {
  for (const [area, spec] of Object.entries(AREAS)) {
    test(`${theme} ${spec.label} has no serious accessibility violations`, async ({
      page,
    }, testInfo) => {
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
      const result = await new AxeBuilder({ page }).analyze();
      const violations = result.violations.filter(
        (finding) =>
          finding.impact === "serious" || finding.impact === "critical",
      );
      await testInfo.attach("axe-results", {
        body: JSON.stringify(result, null, 2),
        contentType: "application/json",
      });
      expect(
        violations.map((finding) => ({
          id: finding.id,
          impact: finding.impact,
          nodes: finding.nodes.map((node) => ({
            target: node.target,
            summary: node.failureSummary,
          })),
        })),
      ).toEqual([]);
    });
  }
}

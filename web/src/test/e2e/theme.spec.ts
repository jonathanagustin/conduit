import { createServer } from "vite-plus";
import { expect, test } from "@playwright/test";
import { AREAS } from "../../console/operator/navigation.js";
import { stubHub, session } from "./hub-stub";

test("native light tokens reach workbench, assistant and rendered tool cards", async ({
  page,
}, testInfo) => {
  await page.setViewportSize({ width: 1920, height: 1080 });
  await page.addInitScript(() => {
    if (localStorage.getItem("conduit.console.theme") === null)
      localStorage.setItem("conduit.console.theme", "light");
    localStorage.setItem("conduit.console.dockWidth", "520");
  });
  await stubHub(page, (request, url) => {
    if (url.pathname === "/v1/session") return { json: session() };
    if (url.pathname === "/v1/assistant/threads")
      return request.method() === "POST"
        ? {
            status: 201,
            json: {
              id: "ath_theme",
              title: "Theme fixture",
              createdAt: "2026-11-17T09:00:00Z",
              updatedAt: "2026-11-17T09:00:00Z",
              messageCount: 0,
              messages: [],
            },
          }
        : { json: { threads: [] } };

    if (url.pathname === "/v1/assistant/turns")
      return {
        json: {
          label: "Theme fixture",
          text: "Recorded response with native tool results.",
          results: [
            {
              tool: "query_ontology_objects",
              label: "Recorded read",
              readOps: 1,
              writeOps: 0,
              matchedCount: 1,
              entities: [{ id: "env-theme", label: "Theme environment" }],
            },
            { tool: "plan", label: "Recorded plan" },
          ],
        },
      };
    return {
      json: { items: [], plans: [], environments: [], bundles: [], events: [] },
    };
  });
  await page.goto("/console/#area-packaging");
  const root = page.locator("#operator-root");
  await expect(root).toHaveAttribute("data-theme", "light");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await expect(root).not.toHaveClass(/bp6-dark/);
  await expect(root).toHaveCSS("color", "rgb(15, 23, 42)");
  await expect(page.locator("#main-workspace .kit-card").first()).toHaveCSS(
    "background-color",
    "rgb(246, 248, 251)",
  );
  for (const [area, spec] of Object.entries(AREAS)) {
    await page.evaluate((hash) => {
      window.location.hash = hash;
    }, `#${area}`);
    await expect(page.locator("#main-workspace")).toContainText(spec.label);
    await expect(page.locator("#main-workspace")).toHaveCSS(
      "color",
      "rgb(15, 23, 42)",
    );
  }
  await page.evaluate(() => {
    window.location.hash = "#area-packaging";
  });
  await page.getByRole("button", { name: "Toggle right chat panel" }).click();
  const thread = page.locator(".aui-thread-root");
  await expect(thread).toBeVisible();
  await expect(thread).toHaveCSS("background-color", "rgb(241, 244, 248)");
  const composer = page.getByLabel("Message input");
  await expect(composer).toHaveCSS("color", "rgb(15, 23, 42)");
  expect(
    await composer.evaluate(
      (element) => element.closest(".dark, .bp6-dark") === null,
    ),
  ).toBe(true);
  await composer.fill("Show recorded tool results");
  await composer.press("Enter");
  const nativeCard = thread.locator("[data-console-tool-card]").first();
  await expect(thread.locator("[data-console-tool-card]")).toHaveCount(1);
  await expect(
    thread.getByText("Theme environment", { exact: true }),
  ).toBeVisible();
  const blueprintCard = thread.locator(".bp6-card").first();
  await expect(blueprintCard).toBeVisible();
  await expect(blueprintCard).toHaveCSS(
    "background-color",
    "rgb(255, 255, 255)",
  );
  await expect(blueprintCard).toHaveCSS("color", "rgb(15, 23, 42)");
  const warning = blueprintCard.locator(".bp6-tag.bp6-intent-warning");
  await expect(warning).toHaveCSS("color", "rgb(115, 68, 6)");
  await expect(warning).toHaveCSS("background-color", "rgb(254, 247, 230)");
  await expect(nativeCard).toHaveCSS("color", "rgb(15, 23, 42)");
  await expect(nativeCard).toHaveCSS("background-color", "rgb(255, 255, 255)");
  await page.screenshot({
    path: testInfo.outputPath("light-workbench-assistant.png"),
    fullPage: true,
    animations: "disabled",
  });
  await page.locator("#theme-toggle").click();
  await expect(root).toHaveAttribute("data-theme", "dark");
  await expect(root).toHaveClass(/bp6-dark/);
  await expect(composer).toHaveCSS("color", "rgb(226, 232, 240)");
  await expect(blueprintCard).toHaveCSS("background-color", "rgb(16, 22, 26)");
  await expect(warning).toHaveCSS("color", "rgb(242, 190, 104)");
  await expect(warning).toHaveCSS("background-color", "rgb(40, 31, 17)");
  await expect(nativeCard).toHaveCSS("color", "rgb(226, 232, 240)");
  await expect(nativeCard).toHaveCSS("background-color", "rgb(16, 22, 26)");
  await page.screenshot({
    path: testInfo.outputPath("dark-workbench-assistant.png"),
    fullPage: true,
    animations: "disabled",
  });
  await page.reload();
  await expect(root).toHaveAttribute("data-theme", "dark");
});

test("gallery primitive aliases follow a local theme opposite the document", async ({
  page,
}) => {
  const server = await createServer({
    server: { host: "127.0.0.1", port: 0 },
    logLevel: "error",
  });
  try {
    await server.listen();
    const address = server.httpServer?.address();
    if (!address || typeof address === "string")
      throw new Error("gallery server has no TCP address");
    await page.goto(`http://127.0.0.1:${address.port}/console/dev/components`);
    await page.evaluate(() =>
      document.documentElement.setAttribute("data-theme", "light"),
    );
    const gallery = page.locator(".conduit-shell");
    const card = gallery.locator(".cnd-switch-card");
    const label = gallery.locator(".cnd-switch-card-label");
    const warning = gallery.locator(".bp6-tag.bp6-intent-warning").first();
    await expect(warning).toHaveCSS("color", "rgb(242, 190, 104)");
    await expect(warning).toHaveCSS("background-color", "rgb(40, 31, 17)");
    await expect(gallery).toHaveAttribute("data-theme", "dark");
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
    await expect(card).toHaveCSS("background-color", "rgb(16, 22, 26)");
    await expect(label).toHaveCSS("color", "rgb(226, 232, 240)");
    await page.evaluate(() =>
      document.documentElement.setAttribute("data-theme", "dark"),
    );
    await gallery.getByRole("button", { name: "Light", exact: true }).click();
    await expect(gallery).toHaveAttribute("data-theme", "light");
    await expect(card).toHaveCSS("background-color", "rgb(255, 255, 255)");
    await expect(label).toHaveCSS("color", "rgb(15, 23, 42)");
    await expect(warning).toHaveCSS("color", "rgb(115, 68, 6)");
    await expect(warning).toHaveCSS("background-color", "rgb(254, 247, 230)");
  } finally {
    await server.close();
  }
});

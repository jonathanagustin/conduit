import { expect, test, type Page, type Route } from "@playwright/test";

const BOOTSTRAP_MARKER = "<!--conduit-console-bootstrap-->";
const BOOTSTRAP = {
  schemaVersion: "conduit.console.bootstrap/v1",
  tenant: { id: "conduit", displayName: "Conduit" },
  brand: { title: "Conduit", subtitle: "Control plane", accent: "#2d72d2", icon: "dashboard" },
  provenance: { claimClass: "conduit-extension", evidenceRef: "conduit://console/default" },
};

function escapeAttribute(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&#34;")
    .replaceAll("'", "&#39;");
}

async function hydrate(page: Page): Promise<void> {
  const meta = `<meta name="x-conduit-console-bootstrap" content="${escapeAttribute(JSON.stringify(BOOTSTRAP))}" />`;
  await page.route("**/console/**", async (route: Route) => {
    if (route.request().resourceType() !== "document") {
      await route.fallback();
      return;
    }
    const response = await route.fetch();
    await route.fulfill({
      response,
      body: (await response.text()).replace(BOOTSTRAP_MARKER, meta),
    });
  });
}

test("refuses to start when the Hub did not render a tenant bootstrap", async ({ page }) => {
  await page.goto("/console/");
  await expect(page.getByTestId("console-configuration-unavailable")).toBeVisible();
});

test("shows the default assistant-ui starter", async ({ page }) => {
  await hydrate(page);
  await page.goto("/console/");
  await expect(page.getByText("How can I help you today?")).toBeVisible();
  await expect(page.getByText("Starter Template")).toBeVisible();
  await expect(page.getByRole("link", { name: "assistant-ui" })).toBeVisible();
  await expect(page.getByLabel("Message input")).toBeVisible();
});

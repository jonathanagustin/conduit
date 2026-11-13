import { expect, test, type Page, type Route } from "@playwright/test";
import { fileURLToPath } from "node:url";

import { createBridge, type BridgeServer } from "../../server/http.ts";

const BOOTSTRAP_MARKER = "<!--conduit-console-bootstrap-->";
const BOOTSTRAP = {
  schemaVersion: "conduit.console.bootstrap/v1",
  tenant: { id: "conduit", displayName: "Conduit" },
  brand: {
    title: "Conduit",
    subtitle: "Control plane",
    accent: "#2d72d2",
    icon: "dashboard",
  },
  provenance: {
    claimClass: "conduit-extension",
    evidenceRef: "conduit://console/default",
  },
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

test("refuses to start when the Hub did not render a tenant bootstrap", async ({
  page,
}) => {
  await page.goto("/console/");
  await expect(
    page.getByTestId("console-configuration-unavailable"),
  ).toBeVisible();
});

test("shows the default assistant-ui starter", async ({ page }) => {
  await hydrate(page);
  await page.goto("/console/");
  await expect(page.getByText("How can I help you today?")).toBeVisible();
  await expect(page.getByText("Starter Template")).toBeVisible();
  await expect(page.getByRole("link", { name: "assistant-ui" })).toBeVisible();
  await expect(page.getByLabel("Message input")).toBeVisible();
});

test("shows a clear error when the Codex bridge is unreachable", async ({
  page,
}) => {
  await hydrate(page);
  await page.route("**/api/assistant/**", (route) => route.abort());
  await page.goto("/console/");
  await expect(page.getByRole("alert")).toContainText(
    "Codex bridge is unreachable",
  );
});

test.describe("fake ACP bridge", () => {
  test.describe.configure({ mode: "serial" });

  test("runs a fake ACP thread with usage, model, and theme controls", async ({
    page,
  }) => {
    process.env["ASSISTANT_ACP_COMMAND"] = process.execPath;
    process.env["ASSISTANT_ACP_ARGS"] = JSON.stringify([
      fileURLToPath(new URL("../../server/fake-agent.mjs", import.meta.url)),
    ]);
    process.env["ASSISTANT_ACP_FIXTURE"] = "1";
    process.env["INITIAL_AGENT_MODE"] = "workspace-write";
    const bridge: BridgeServer = await createBridge({ port: 8787 });
    try {
      await hydrate(page);
      await page.goto("/console/");
      await expect(
        page.getByRole("button", { name: "Assess this workspace" }),
      ).toBeVisible();
      await expect(page.getByText("Codex demo")).toBeVisible();
      await page.getByLabel("Message input").fill("inspect the workspace");
      await page.getByRole("button", { name: "Send message" }).click();
      await page.getByRole("button", { name: "Allow once" }).click();
      await expect(page.getByText("12 (12%)")).toBeVisible();
      await expect(page.getByText("Ask", { exact: true })).toBeInViewport();
      const copy = page
        .getByRole("button", { name: "Copy", exact: true })
        .last();
      const chip = page.getByRole("button", { name: "What is still unknown?" });
      await expect(chip).toBeVisible();
      await expect
        .poll(
          async () => {
            const copyBox = await copy.boundingBox();
            const chipBox = await chip.boundingBox();
            if (!copyBox || !chipBox) return false;
            return copyBox.y + copyBox.height <= chipBox.y + 2;
          },
          { timeout: 10_000 },
        )
        .toBe(true);
      await page.getByLabel("Message input").fill("second turn");
      await page.getByRole("button", { name: "Send message" }).click();
      await page.getByRole("button", { name: "Allow once" }).click();
      await expect(page.getByText("24 (24%)")).toBeVisible();
      await page.getByRole("button", { name: "Dark" }).click();
      await expect(page.locator("html")).toHaveClass(/dark/);
      await page.getByRole("button", { name: "Light" }).click();
      await expect(page.locator("html")).not.toHaveClass(/dark/);
    } finally {
      await bridge.close();
    }
  });

  test("opens settings, the command palette, and thread search", async ({
    page,
  }) => {
    process.env["ASSISTANT_ACP_COMMAND"] = process.execPath;
    process.env["ASSISTANT_ACP_ARGS"] = JSON.stringify([
      fileURLToPath(new URL("../../server/fake-agent.mjs", import.meta.url)),
    ]);
    process.env["ASSISTANT_ACP_FIXTURE"] = "1";
    process.env["INITIAL_AGENT_MODE"] = "workspace-write";
    const bridge: BridgeServer = await createBridge({ port: 8787 });
    try {
      await hydrate(page);
      await page.goto("/console/");
      await page.getByRole("button", { name: "Settings" }).click();
      await expect(
        page.getByRole("heading", { name: "Settings" }),
      ).toBeVisible();
      await expect(page.getByText("/api/assistant")).toBeVisible();
      await expect(page.getByText("Command palette")).toBeVisible();
      await expect(
        page.getByRole("button", { name: "Codex demo" }),
      ).toBeVisible();
      await page.keyboard.press("Escape");
      await page.keyboard.press("Control+K");
      await expect(
        page.getByRole("combobox", { name: "Type a command" }),
      ).toBeVisible();
      await expect(
        page.getByRole("option", { name: "Open settings" }),
      ).toBeVisible();
      await page.keyboard.press("Escape");
      await page.getByLabel("Message input").fill("inspect the workspace");
      await page.getByRole("button", { name: "Send message" }).click();
      await page.getByRole("button", { name: "Allow once" }).click();
      await expect(
        page.locator("[data-slot=aui_thread-list-item]"),
      ).toContainText(/inspect/i);
      await page.getByRole("button", { name: "Search", exact: true }).click();
      await expect(
        page.getByRole("combobox", { name: "Search threads" }),
      ).toBeVisible();
      await page
        .getByRole("combobox", { name: "Search threads" })
        .fill("inspect");
      await expect(
        page.getByRole("option", { name: /inspect the workspace/i }),
      ).toBeVisible();
      await page.keyboard.press("Escape");
      await page.getByRole("button", { name: "Prompts" }).click();
      await page.getByRole("option", { name: /Packaging blockers/ }).dblclick();
      await expect(page.getByLabel("Message input")).toHaveValue(
        /packaging blockers/,
      );
    } finally {
      await bridge.close();
    }
  });
});

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

const thread = {
  id: "thread-1",
  title: "New chat",
  archived: false,
  updatedAt: "2026-11-13T00:00:00.000Z",
};

async function stubBridge(page: Page, runs: "complete" | "hang"): Promise<{ cancels: string[] }> {
  const cancels: string[] = [];
  await page.route("**/api/assistant/health", async (route) => {
    await route.fulfill({
      json: {
        ok: true,
        runtime: "codex-acp",
        fixture: true,
        workspace: "/tmp/conduit-assistant",
        mode: "workspace-write",
        adapter: "fixture",
      },
    });
  });
  await page.route("**/api/assistant/threads", async (route) => {
    if (route.request().method() === "GET") {
      await route.fulfill({ json: { threads: [thread] } });
      return;
    }
    await route.fulfill({ json: thread });
  });
  await page.route("**/api/assistant/threads/thread-1", async (route) => {
    await route.fulfill({ json: { ...thread, title: "Assess this workspace" } });
  });
  await page.route("**/api/assistant/threads/thread-1/cancel", async (route) => {
    cancels.push(route.request().postData() ?? "");
    await route.fulfill({ json: { ok: true } });
  });
  await page.route("**/api/assistant/permissions/**", async (route) => {
    await route.fulfill({ json: { ok: true } });
  });
  await page.route("**/api/assistant/threads/thread-1/runs", async (route) => {
    if (runs === "hang") {
      await new Promise((resolve) => setTimeout(resolve, 30_000));
      await route.fulfill({ status: 204, body: "" });
      return;
    }
    const snapshot = {
      text: "Working on it. Read complete.",
      reasoning: "Considering [redacted]",
      tools: [
        {
          toolCallId: "call-1",
          toolName: "read",
          title: "read README",
          status: "pending",
          argsText: '{"path":"README.md"}',
          resultText: "",
          isError: false,
          approval: {
            requestId: "thread-1::run-1::perm-1",
            prompt: "read README",
            options: [
              { optionId: "allow", name: "Allow once", kind: "allow_once" },
              { optionId: "reject", name: "Reject", kind: "reject_once" },
            ],
          },
        },
      ],
      plan: [{ content: "Inspect the workspace", status: "pending", priority: "high" }],
      sources: [{ id: "https://example.com/docs", url: "https://example.com/docs", title: "Docs" }],
      usage: { used: 12, size: 100 },
    };
    const body = `data: ${JSON.stringify({ type: "snapshot", snapshot })}\n\ndata: ${JSON.stringify({ type: "done", stopReason: "end_turn" })}\n\n`;
    await route.fulfill({
      status: 200,
      contentType: "text/event-stream",
      body,
    });
  });
  return { cancels };
}

test("refuses to start when the Hub did not render a tenant bootstrap", async ({ page }) => {
  await page.goto("/console/");
  await expect(page.getByTestId("console-configuration-unavailable")).toBeVisible();
});

test("shows the assistant, a tool approval, and context usage", async ({ page }) => {
  await hydrate(page);
  await stubBridge(page, "complete");
  await page.goto("/console/");
  await expect(page.getByTestId("tenant-title")).toHaveText("Conduit");
  await expect(page.getByTestId("assistant-welcome")).toBeVisible();
  await expect(page.getByRole("button", { name: "Assess this workspace" })).toBeVisible();
  await page.getByRole("button", { name: "Assess this workspace" }).click();
  await expect(page.getByText("Read complete.")).toBeVisible();
  await expect(page.getByTestId("permission-request")).toBeVisible();
  await expect(page.getByText("does not approve a Conduit deployment")).toBeVisible();
  await page.getByRole("button", { name: "Allow once" }).click();
  await expect(page.getByText("Allowed by the operator.")).toBeVisible();
  await expect(page.getByTestId("context-usage")).toHaveText("Context 12 / 100");
  await expect(page.getByRole("link", { name: "Docs" })).toHaveAttribute("href", "https://example.com/docs");
});

test("stop cancels the active run", async ({ page }) => {
  await hydrate(page);
  const bridge = await stubBridge(page, "hang");
  await page.goto("/console/");
  await page.getByLabel("Message input").fill("please cancel");
  await page.getByTestId("send-message").click();
  await page.getByTestId("stop-message").click();
  await expect.poll(() => bridge.cancels.length).toBeGreaterThan(0);
});

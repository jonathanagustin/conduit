import { expect, test, type Page, type Route } from "@playwright/test";
import { mkdir } from "node:fs/promises";
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

  test("shows the workbench from hub routes and the fake agent", async ({
    page,
  }) => {
    test.setTimeout(60_000);
    process.env["ASSISTANT_ACP_COMMAND"] = process.execPath;
    process.env["ASSISTANT_ACP_ARGS"] = JSON.stringify([
      fileURLToPath(new URL("../../server/fake-agent.mjs", import.meta.url)),
    ]);
    process.env["ASSISTANT_ACP_FIXTURE"] = "1";
    process.env["INITIAL_AGENT_MODE"] = "workspace-write";
    let approved = false;
    await page.route("**/health/live", (route) =>
      route.fulfill({ json: { ok: true } }),
    );
    await page.route("**/v1/environments**", (route) => {
      const url = route.request().url();
      if (url.includes("reported-state")) {
        return route.fulfill({
          json: { agentId: "example-spoke", observedAt: "2026-11-14T00:00:00Z", entities: [{ name: "sample" }] },
        });
      }
      if (/\/v1\/environments\/[^/?]+/.test(url)) {
        return route.fulfill({
          json: {
            environment: {
              id: "lab",
              generation: "4",
              connectionSetting: "spoke",
              defaultReleaseChannel: "stable",
              labels: { tier: "example" },
              maintenance: { noDowntime: { timeZone: "UTC", intervals: ["Mon 00:00-04:00"] } },
              config: { replicas: "2" },
            },
          },
        });
      }
      return route.fulfill({
        json: {
          environments: [
            {
              environment: { id: "lab", connectionSetting: "spoke" },
              reported: { environment: "lab" },
            },
          ],
          total: 1,
        },
      });
    });
    await page.route("**/v1/products**", (route) => {
      if (route.request().url().includes("view=full")) {
        return route.fulfill({
          json: {
            products: [
              { product: "sample-app", releases: [{ version: "1.2.0", channels: ["candidate", "stable"] }] },
            ],
            total: 1,
          },
        });
      }
      return route.fulfill({ json: { products: [{ product: "sample-app" }], total: 1 } });
    });
    await page.route("**/v1/fleet/installations**", (route) =>
      route.fulfill({
        json: {
          installations: [
            { installationId: "sample-a", environment: "lab", product: "sample-app", health: "HEALTHY" },
            { installationId: "sample-b", environment: "lab", product: "sample-app", health: "ERROR" },
          ],
        },
      }),
    );
    await page.route("**/v1/change-requests**", (route) =>
      route.fulfill({
        json: {
          changeRequests: [
            {
              id: "change-1",
              title: "Example change",
              status: "IN_REVIEW",
              environment: "lab",
              requirementsSatisfied: false,
              changes: [{ diff: { replicas: { before: "1", after: "2" } } }],
              reviews: [{ decision: "comment", rationale: "Example review" }],
            },
          ],
        },
      }),
    );
    await page.route("**/v1/promotions**", (route) =>
      route.fulfill({
        json: {
          promotions: [
            {
              id: "promo-1",
              product: "sample-app",
              version: "1.2.0",
              fromChannel: "candidate",
              toChannel: "stable",
              status: "BLOCKED",
              reasons: ["Example: maintenance window is closed"],
            },
          ],
        },
      }),
    );
    await page.route("**/v1/plans**", async (route) => {
      const url = route.request().url();
      if (route.request().method() === "POST" && url.includes("/approvals")) {
        approved = true;
        await route.fulfill({ json: { id: "plan-1", status: "APPROVED" } });
        return;
      }
      await route.fulfill({
        json: {
          plans: [
            approved
              ? {
                  id: "plan-1",
                  status: "APPROVED",
                  requiredApprovals: 1,
                  approvals: [{ subject: "operator" }],
                  plan: { environment: "lab", steps: [{}] },
                }
              : {
                  id: "plan-1",
                  status: "AWAITING_APPROVAL",
                  requiredApprovals: 1,
                  plan: { environment: "lab", steps: [{}] },
                },
            {
              id: "plan-failed",
              status: "FAILED",
              failure: "Example rollout stopped",
              requiredApprovals: 1,
              plan: { environment: "lab", steps: [{}] },
              execution: { tasks: [{ phase: "apply", status: "FAILED", agentOutput: "example output" }] },
            },
          ],
          total: 1,
        },
      });
    });
    await page.route("**/v1/packaging-runs**", async (route) => {
      const url = route.request().url();
      if (url.includes("/events")) {
        await route.fulfill({
          json: {
            configured: true,
            items: [
              {
                sequence: 1,
                at: "2026-11-14T00:00:00Z",
                step: "review",
                outcome: "waiting",
                reason: "example gate",
              },
            ],
            total: 1,
          },
        });
        return;
      }
      if (route.request().method() === "GET" && !url.includes("sample-app-1") && !url.includes("missing")) {
        await route.fulfill({
          json: {
            configured: true,
            items: [
              {
                configured: true,
                phase: "review",
                runId: "sample-app-1",
                planId: "plan-1",
                tier: "CONDUIT_NATIVE",
              },
            ],
            total: 1,
          },
        });
        return;
      }
      await route.fallback();
    });
    await page.route("**/*PackagingRunService/GetPackagingRun", async (route) => {
      const body = route.request().postDataJSON() as { runId?: string };
      const missing = body.runId === "missing";
      await route.fulfill({
        json: missing
          ? { configured: false, tier: "CONDUIT_NATIVE", runId: "missing" }
          : {
              configured: true,
              phase: "review",
              runId: "sample-app-1",
              planId: "plan-1",
              tier: "CONDUIT_NATIVE",
              sourceDigest: "sha256:example",
              artifactDigest: "sha256:artifact-example",
              proposalRevision: "rev-1",
              targetPlanRef: "lab/plan-1",
            },
      });
    });
    const bridge: BridgeServer = await createBridge({ port: 8787 });
    try {
      await hydrate(page);
      await page.goto("/console/");
      await page.getByRole("button", { name: "Workbench" }).click();
      await expect(page.getByText("LIVE", { exact: true })).toBeVisible();
      await expect(page.getByRole("button", { name: "sample-app" })).toBeVisible();
      await expect(page.getByText("1/1")).toBeVisible();
      await page.getByRole("menuitem", { name: "Packaging run" }).click();
      await expect(page.getByText("Review wait").first()).toBeVisible();
      await expect(page.getByText("Phase review").first()).toBeVisible();
      await page.getByLabel("Packaging run id").fill("missing");
      await page.getByRole("button", { name: "Load", exact: true }).click();
      await expect(page.getByText("configured: false", { exact: true })).toBeVisible();
      await page.getByRole("menuitem", { name: "Plans & approvals" }).click();
      await expect(page.getByText("Needs approval", { exact: true })).toBeVisible();
      await page.getByRole("button", { name: "Review", exact: true }).click();
      await page.getByRole("button", { name: "Approve", exact: true }).click();
      await expect(page.getByText("APPROVED", { exact: true })).toBeVisible();
      await expect(page.getByLabel("Message input")).toBeVisible();
      if (process.env["CONSOLE_SHOTS"] === "1") {
        await mkdir("/opt/cursor/artifacts", { recursive: true });
        const shot = async (name: string) => {
          await page.screenshot({ path: `/opt/cursor/artifacts/${name}.png` });
        };
        await page.setViewportSize({ width: 1440, height: 900 });
        await page.getByRole("menuitem", { name: "Overview" }).click();
        await shot("overview-1440");
        await page.getByRole("menuitem", { name: "Environments" }).click();
        await page.getByRole("button", { name: /lab/ }).click();
        await expect(page.getByText("Release channel")).toBeVisible();
        await shot("environment-1440");
        await page.getByRole("menuitem", { name: "Software catalog" }).click();
        await expect(page.getByText("1.2.0", { exact: true })).toBeVisible();
        await shot("releases-1440");
        await page.getByRole("menuitem", { name: "Packaging run" }).click();
        await page.getByLabel("Packaging run id").fill("sample-app-1");
        await shot("packaging-1440");
        await page.getByRole("menuitem", { name: "Plans & approvals" }).click();
        await page.getByRole("button", { name: "plan-failed" }).click();
        const failed = page.getByText("Example rollout stopped").first();
        await failed.scrollIntoViewIfNeeded();
        await expect(failed).toBeVisible();
        await shot("plans-1440");
        await page.getByRole("menuitem", { name: "Change requests" }).click();
        await page.getByRole("button", { name: /Example change/ }).click();
        await expect(page.getByText("Before", { exact: true })).toBeVisible();
        await shot("changes-1440");
        await page.getByRole("menuitem", { name: "Audit" }).click();
        await shot("audit-1440");
        await page.setViewportSize({ width: 1024, height: 768 });
        await page.getByRole("menuitem", { name: "Overview" }).click();
        await shot("overview-1024");
      }
      await page.setViewportSize({ width: 1000, height: 768 });
      await expect(page.getByRole("button", { name: "Toggle assistant" })).toBeVisible();
      await page.getByRole("button", { name: "Toggle assistant" }).click();
      await expect(page.getByRole("complementary", { name: "Assistant" })).toBeVisible();
      if (process.env["CONSOLE_SHOTS"] === "1") {
        await page.screenshot({ path: "/opt/cursor/artifacts/overview-1000-drawer.png" });
      }
    } finally {
      await bridge.close();
    }
  });
});

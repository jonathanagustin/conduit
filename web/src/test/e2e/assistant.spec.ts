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
      await expect(page.getByText("LIVE", { exact: true }).first()).toBeVisible();
      await expect(page.getByText("EXAMPLE DATA", { exact: true })).toBeVisible();
      await expect(page.getByRole("button", { name: "Command Center" })).toBeVisible();
      await expect(page.getByText("87.5%").first()).toBeVisible();
      await page.evaluate(() => {
        const view = window as unknown as {
          switchScreen: (id: string) => void;
          toggleChatRail: () => void;
        };
        view.switchScreen("s08-staged-run");
      });
      await expect(page.getByText("RUN-8842").first()).toBeVisible();
      await page.evaluate(() => {
        (window as unknown as { switchScreen: (id: string) => void }).switchScreen("s16-approvals-board");
      });
      await expect(page.getByText("PLN-2026-0418").first()).toBeVisible();
      await page.evaluate(() => {
        (window as unknown as { switchScreen: (id: string) => void }).switchScreen("rm09-recalls");
      });
      await expect(page.getByText("RCL-2026-019").first()).toBeVisible();
      await page.evaluate(() => {
        (window as unknown as { switchScreen: (id: string) => void }).switchScreen("rm10-ramped-rollout");
      });
      await expect(page.getByText("Canary pacing").first()).toBeVisible();
      await page.evaluate(() => {
        (window as unknown as { switchScreen: (id: string) => void }).switchScreen("rm11-debug-terminal");
      });
      await expect(page.getByText("JITA-204").first()).toBeVisible();
      await page.evaluate(() => {
        (window as unknown as { switchScreen: (id: string) => void }).switchScreen("pt01-proposal-trace");
      });
      await expect(page.getByText("Guardrail check").first()).toBeVisible();
      await page.evaluate(() => {
        (window as unknown as { switchScreen: (id: string) => void }).switchScreen("adm01-user-prefs");
      });
      await expect(page.getByText("Personal API tokens").first()).toBeVisible();
      await page.evaluate(() => {
        (window as unknown as { switchScreen: (id: string) => void }).switchScreen("adm02-org-control-panel");
      });
      await expect(page.getByRole("radio", { name: "Organizations" }).first()).toBeVisible();
      await page.evaluate(() => {
        (window as unknown as { switchScreen: (id: string) => void }).switchScreen("adm03-auth-signin");
      });
      await expect(page.getByText("Sign-in is not enabled on this Hub").first()).toBeVisible();
      await page.evaluate(() => {
        (window as unknown as { toggleChatRail: () => void }).toggleChatRail();
      });
      await expect(page.getByLabel("Message input")).toBeVisible();
    } finally {
      await bridge.close();
    }
  });
});

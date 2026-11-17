import { expect, test } from "@playwright/test";
import { APPLICATIONS } from "./hub-stub";
import { installPackagingReadStub, hydrate } from "./console-fixture";

installPackagingReadStub();

for (const denied of [false, true]) {
  test(`offline bundles bind verified envelope and operate permission (${denied})`, async ({
    page,
  }) => {
    const writes: Record<string, any>[] = [];
    const envelope = (id: string) => ({
      manifest: { id },
      manifestDigest: `sha256:${id}`,
    });
    await page.route("**/v1/**", async (route) => {
      const url = new URL(route.request().url());
      if (url.pathname === "/v1/session")
        return route.fulfill({
          json: {
            actor: { username: "alice" },
            effective: { username: "alice" },
            applications: APPLICATIONS,
            globalOperations: denied
              ? ["resource:read"]
              : ["resource:read", "resource:operate"],
          },
        });
      if (url.pathname === "/v1/environments")
        return route.fulfill({
          json: {
            environments: [
              { environment: { id: "east" } },
              { environment: { id: "west" } },
            ],
          },
        });
      if (url.pathname === "/v1/bundles")
        return route.fulfill({
          json: {
            bundles: [
              { id: "bundle-a", manifestDigest: "sha256:bundle-a" },
              { id: "bundle-b", manifestDigest: "sha256:bundle-b" },
            ],
          },
        });
      if (
        url.pathname.startsWith("/v1/bundles/") &&
        route.request().method() === "GET"
      )
        return route.fulfill({
          json: envelope(url.pathname.split("/").pop()!),
        });
      if (url.pathname === "/v1/bundles/verification") {
        const input = route.request().postDataJSON();
        return route.fulfill({
          json: {
            bundleId: input.manifest.id,
            manifestDigest: input.manifestDigest,
            signature: { status: "verified", keyId: "key-1" },
            sbom: [],
          },
        });
      }
      if (url.pathname === "/v1/offline-bundles") {
        const input = route.request().postDataJSON();
        writes.push(input);
        const handoff = {
          id: "handoff-a",
          bundleId: "bundle-a",
          digest: "sha256:bundle-a",
          site: "west",
          takenBy: "alice",
          status:
            input.action === "handoff" ? "handed_off" : "pending_check_in",
        };
        return route.fulfill({
          status: input.action === "handoff" ? 201 : 202,
          json: {
            handoff,
            reason:
              input.action === "report"
                ? "report-back is pending until the site checks in"
                : "offline bundle handed off",
          },
        });
      }
      return route.fulfill({ status: 404, json: { error: "not configured" } });
    });
    await hydrate(page);
    await page.goto("/console/#rm08-bundle-preview");
    await expect(page.locator(".cnd-topbar")).toContainText("alice");
    await page
      .getByLabel("Offline bundle", { exact: true })
      .selectOption("bundle-a");
    await expect(page.locator("[data-offline-signature]")).toContainText(
      "Verified by Hub",
    );
    await expect(page.locator("[data-offline-preview]")).toContainText(
      "bundle-a",
    );
    await expect(page.locator("[data-offline-preview]")).not.toContainText(
      "bundle-b",
    );
    await page.evaluate(() => Reflect.get(window, "refreshHub")());
    await expect(
      page.getByLabel("Offline bundle", { exact: true }),
    ).toHaveValue("bundle-a");
    await expect(page.locator("[data-offline-signature]")).toContainText(
      "Verified by Hub",
    );
    const download = page.waitForEvent("download");
    await page
      .getByRole("button", { name: "Download signed envelope" })
      .click();
    expect((await download).suggestedFilename()).toBe("bundle-a.json");
    const form = page.locator("[data-offline-handoff]");
    await page.getByLabel("Offline bundle site").selectOption("east");
    await form.getByRole("checkbox").check();
    if (denied) {
      await expect(form.getByRole("button")).toBeDisabled();
      await expect(page.locator("[data-offline-capability]")).toHaveText(
        "Your session does not hold resource:operate on platform/root.",
      );
      expect(writes).toHaveLength(0);
      return;
    }
    await expect(form.getByRole("button")).toBeEnabled();
    await page.getByLabel("Offline bundle site").selectOption("west");
    await expect(form.getByRole("button")).toBeDisabled();
    await form.getByRole("checkbox").check();
    await form.getByRole("button").click();
    await expect(page.locator("[data-offline-note]")).toContainText(
      "Operator handoff recorded",
    );
    expect(writes).toEqual([
      { action: "handoff", bundleId: "bundle-a", site: "west", confirm: true },
    ]);
    const report = page.locator("[data-offline-report]");
    await report.getByRole("combobox").selectOption("installed");
    await report.getByRole("checkbox").check();
    await report.getByRole("button").click();
    await expect(page.locator("[data-offline-note]")).toContainText(
      "pending_check_in",
    );
    expect(writes[1]).toEqual({
      action: "report",
      id: "handoff-a",
      outcome: "installed",
    });
    await report.getByRole("combobox").selectOption("failed");
    await report.getByRole("checkbox").check();
    await expect(report.getByRole("button")).toBeDisabled();
    expect(writes).toHaveLength(2);
  });
}

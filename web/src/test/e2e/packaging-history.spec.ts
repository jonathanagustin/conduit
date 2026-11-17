import { expect, test } from "@playwright/test";
import { installPackagingReadStub, hydrate } from "./console-fixture";

installPackagingReadStub();

test("packaging graph and staged chunks keep the selected run and paged history", async ({
  page,
}) => {
  await hydrate(page);
  const calls: { path: string; method: string }[] = [];
  await page.route("**/v1/packaging-runs**", async (route) => {
    const url = new URL(route.request().url());
    calls.push({
      path: url.pathname + url.search,
      method: route.request().method(),
    });
    if (url.pathname === "/v1/packaging-runs") {
      await route.fulfill({
        json: {
          items: [
            {
              runId: "run-a",
              configured: true,
              phase: "review",
              outcome: "WAITING",
              sourceDigest: "sha256:source-a",
            },
            {
              runId: "run-b",
              configured: true,
              phase: "blocked",
              outcome: "FAIL_REQUIRED",
              artifactDigest: "sha256:artifact-b",
            },
          ],
          total: 2,
        },
      });
      return;
    }
    await route.fulfill({ json: { items: [], total: 0 } });
  });
  await page.route(
    "**/conduit.packaging.v1.PackagingRunService/ListPackagingRuns",
    async (route) => {
      calls.push({
        path: new URL(route.request().url()).pathname,
        method: route.request().method(),
      });
      expect(route.request().postDataJSON()).toEqual({
        view: "summary",
        limit: 20,
      });
      await route.fulfill({
        json: {
          configured: true,
          items: [
            {
              runId: "run-a",
              phase: "review",
              outcome: "WAITING",
              sourceDigest: "sha256:source-a",
            },
            {
              runId: "run-b",
              phase: "blocked",
              outcome: "FAIL_REQUIRED",
              artifactDigest: "sha256:artifact-b",
            },
          ],
          total: "2",
          pageSize: 2,
          limit: 20,
        },
      });
    },
  );
  const historyQueries: { runId: string; cursor?: string }[] = [];
  await page.route(
    "**/conduit.packaging.v1.PackagingRunService/ListPackagingRunEvents",
    async (route) => {
      expect(route.request().method()).toBe("POST");
      const query = route.request().postDataJSON();
      historyQueries.push(query);
      const first = query.runId === "run-a";
      const next = Boolean(query.cursor);
      await route.fulfill({
        json: {
          configured: true,
          runId: query.runId,
          pageSize: 1,
          limit: 20,
          total: first ? "2" : "1",
          nextCursor: first && !next ? "cursor-a" : "",
          items: [
            {
              sequence: first && next ? "2" : "1",
              at: "2026-11-15T09:00:00.123456789Z",
              step: first ? (next ? "review" : "assess") : "build",
              outcome: first ? (next ? "WAITING" : "PASS") : "FAIL_REQUIRED",
              reason: first
                ? next
                  ? "waiting-a"
                  : "a-only-output"
                : "b-only-failed",
            },
          ],
        },
      });
    },
  );
  await page.goto("/console/#s07-pipeline");
  await expect(page.locator("[data-packaging-pipeline]")).toBeVisible();
  await expect(
    page.locator("[data-stage-graph] [data-packaging-stage]"),
  ).toHaveCount(2);
  await expect(page.locator("#example-data-badge")).toHaveText("LIVE");
  await expect(page.locator("[data-stage-preview]")).toContainText("waiting-a");
  await page.locator("[data-packaging-stage='0']").click();
  await expect(page.locator("[data-stage-preview]")).toContainText(
    "a-only-output",
  );
  await page.locator("[data-packaging-run]").selectOption("run-b");
  await expect(page.locator("[data-object-header]")).toContainText("run-b");
  await expect(page.locator("[data-object-header]")).not.toContainText("run-a");
  await expect(
    page.locator("[data-stage-graph] [data-packaging-stage]"),
  ).toHaveCount(1);
  await expect(page.locator("[data-stage-preview]")).toContainText(
    "b-only-failed",
  );
  await expect(page.locator("[data-stage-preview]")).not.toContainText(
    "a-only-output",
  );
  await page
    .getByRole("button", { name: "Open staged run", exact: true })
    .click();
  await expect(page.locator("[data-stage-chunk]")).toHaveCount(1);
  await expect(page.locator("[data-stage-chunk]")).toContainText(
    "b-only-failed",
  );
  await page
    .getByRole("button", { name: "Artifacts & evidence", exact: true })
    .click();
  await expect(page.locator("[data-staged-run]")).toContainText(
    "No downloadable artifact reference",
  );
  await expect(page.locator("[data-staged-run]")).not.toContainText("RUN-8844");
  await page
    .getByRole("button", { name: "Open pipeline graph", exact: true })
    .click();
  await expect(page.locator("[data-packaging-run]")).toHaveValue("run-b");
  const beforeManual = calls.filter((call) =>
    call.path.endsWith("/ListPackagingRuns"),
  ).length;
  await page.evaluate(() =>
    (window as unknown as { refreshHub: () => Promise<void> }).refreshHub(),
  );
  expect(
    calls.filter((call) => call.path.endsWith("/ListPackagingRuns")).length,
  ).toBe(beforeManual + 1);
  await expect(page.locator("[data-packaging-run]")).toHaveValue("run-b");
  expect(historyQueries).toContainEqual({ runId: "run-a", cursor: "cursor-a" });
  expect(calls.some((call) => call.path.includes("/events"))).toBe(false);
  expect(calls.some((call) => call.path === "/v1/packaging-runs")).toBe(false);
  expect(
    calls.filter((call) => call.path.endsWith("/ListPackagingRuns")).length,
  ).toBeGreaterThan(0);
  expect(
    calls.every(
      (call) =>
        call.method ===
        (call.path.endsWith("/ListPackagingRuns") ? "POST" : "GET"),
    ),
  ).toBe(true);
});

import { expect, test } from "@playwright/test";
import { session, stubHub } from "./hub-stub";

test("1,000-row indexes keep server filters, cursors and linked selections across navigation", async ({
  page,
}, testInfo) => {
  const plans = Array.from({ length: 1000 }, (_, index) => ({
    id: `plan-${index}`,
    environment: "edge-a",
    status: "SUCCEEDED",
    stepCount: 1,
    approvalCount: 0,
    requiredApprovals: 1,
    createdAt: "2026-11-16T12:00:00Z",
    updatedAt: "2026-11-16T12:00:00Z",
  }));
  const installations = Array.from({ length: 1000 }, (_, index) => ({
    installationId: `installation-${index}`,
    environment: "edge-a",
    product: "example/app",
  }));
  const reads: URL[] = [];
  const details: string[] = [];
  await stubHub(page, (_request, url) => {
    if (url.pathname === "/v1/session") return { json: session() };
    if (url.pathname === "/v1/spaces") return { json: { spaces: [] } };
    if (url.pathname === "/v1/environments")
      return {
        json: { environments: [{ environment: { id: "edge-a" } }], total: 1 },
      };
    if (url.pathname === "/v1/change-requests")
      return { json: { changeRequests: [], total: 0 } };
    if (url.pathname.endsWith("/timeline")) return { json: { timeline: [] } };
    if (url.pathname.startsWith("/v1/plans/")) {
      const id = decodeURIComponent(url.pathname.slice("/v1/plans/".length));
      details.push(id);
      return {
        json: {
          ...plans.find((row) => row.id === id),
          plan: {
            environment: "edge-a",
            steps: [
              {
                position: 1,
                action: "APPLY",
                installationId: "installation-999",
                product: "example/app",
                version: "1.0.0",
              },
            ],
          },
          approvals: [],
        },
      };
    }
    if (
      url.pathname === "/v1/plans" ||
      url.pathname === "/v1/fleet/installations"
    ) {
      reads.push(url);
      const isPlan = url.pathname === "/v1/plans";
      let rows: Array<(typeof plans)[number] | (typeof installations)[number]> =
        isPlan ? plans : installations;
      const search = url.searchParams.get("search");
      if (search)
        rows = rows.filter((row) =>
          (isPlan
            ? (row as (typeof plans)[number]).id
            : (row as (typeof installations)[number]).installationId
          ).includes(search),
        );
      const start = Number(
        /^seek:(\d+)\/\+=$/.exec(
          url.searchParams.get("cursor") || "seek:0/+=",
        )?.[1],
      );
      return {
        json: {
          [isPlan ? "plans" : "installations"]: rows.slice(start, start + 20),
          total: rows.length,
          pageSize: Math.min(20, rows.length),
          limit: 20,
          hasPrevious: start > 0,
          ...(start > 20 ? { previousCursor: `seek:${start - 20}/+=` } : {}),
          ...(start + 20 < rows.length
            ? { nextCursor: `seek:${start + 20}/+=` }
            : {}),
        },
      };
    }
    return { json: {} };
  });

  await page.goto(
    "/console/#area-releases?tab=plans&plans.status=SUCCEEDED&plan=plan-999",
  );
  const planControls = page.locator('[data-index-controls="plans"]');
  await expect(planControls.locator("[data-index-count]")).toHaveText(
    "Showing 20 of 1000 plans in this page.",
  );
  await expect(page.locator("[data-release-plan-inspector]")).toContainText(
    "plan-999 · SUCCEEDED",
  );
  expect(details).toContain("plan-999");
  expect(reads.some((url) => url.pathname === "/v1/fleet/installations")).toBe(
    false,
  );
  expect(
    reads
      .filter((url) => url.pathname === "/v1/plans")
      .every(
        (url) =>
          url.searchParams.get("view") === "summary" &&
          url.searchParams.get("limit") === "20" &&
          !url.searchParams.has("cursor"),
      ),
  ).toBe(true);

  await planControls
    .getByRole("button", { name: "Next plans", exact: true })
    .click();
  await expect(page.locator("[data-release-plan]")).toContainText("plan-20");
  await expect(page).toHaveURL(/plans.cursor=seek%3A20%2F%2B%3D/);
  await page.goBack();
  await expect(page.locator("[data-release-plan-inspector]")).toContainText(
    "plan-999 · SUCCEEDED",
  );
  await expect(page).toHaveURL(/plan=plan-999/);
  await page.goForward();
  await expect(page.locator("[data-release-plan]")).toContainText("plan-20");
  await expect(
    planControls.getByRole("button", { name: "Previous plans", exact: true }),
  ).toBeEnabled();
  await planControls
    .getByRole("button", { name: "Previous plans", exact: true })
    .click();
  await expect(page.locator("[data-release-plan]")).toContainText("plan-0");
  expect(new URL(page.url()).hash).not.toContain("plans.cursor");
  await planControls
    .getByRole("button", { name: "Next plans", exact: true })
    .click();
  await expect(page).toHaveURL(/plans.cursor=seek%3A20%2F%2B%3D/);

  await expect(page.locator("[data-release-plan]")).toContainText("plan-20");
  await planControls.getByLabel("Search", { exact: true }).fill("plan-999");
  await planControls
    .getByRole("button", { name: "Apply plans filters", exact: true })
    .click();
  await expect(planControls.locator("[data-index-count]")).toHaveText(
    "Showing 1 of 1 plans in this page.",
  );
  expect(new URL(page.url()).hash).not.toContain("plans.cursor");
  await page.reload();
  await expect(planControls.locator("[data-index-count]")).toHaveText(
    "Showing 1 of 1 plans in this page.",
  );
  await expect(planControls.getByLabel("Search", { exact: true })).toHaveValue(
    "plan-999",
  );
  await page.screenshot({
    path: testInfo.outputPath("pagination-desktop.png"),
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.evaluate(() =>
    (window as unknown as { closeMobileNav(): void }).closeMobileNav(),
  );
  await expect(page.locator("#left-nav")).not.toBeInViewport();
  await page.screenshot({ path: testInfo.outputPath("pagination-mobile.png") });
  await page.setViewportSize({ width: 1280, height: 720 });

  await page.evaluate(() =>
    (window as unknown as { switchScreen(id: string): void }).switchScreen(
      "flt-environments",
    ),
  );
  const fleetControls = page.locator('[data-index-controls="installations"]');
  await expect(fleetControls.locator("[data-index-count]")).toHaveText(
    "Showing 20 of 1000 installations in this page.",
  );
  await fleetControls
    .getByRole("button", { name: "Next installations", exact: true })
    .click();
  await expect(page).toHaveURL(/installations.cursor=seek%3A20%2F%2B%3D/);
  await expect
    .poll(() =>
      reads
        .filter((url) => url.pathname === "/v1/fleet/installations")
        .at(-1)
        ?.searchParams.get("cursor"),
    )
    .toBe("seek:20/+=");
});

import { expect, test } from "@playwright/test";
import { session, stubHub } from "./hub-stub";

const FIRST = `space_${"a".repeat(32)}`;
const SECOND = `space_${"b".repeat(32)}`;

test("Space selection binds Plan pages and linked reads, then resets affected cursors", async ({
  page,
}) => {
  const reads: URL[] = [];
  await stubHub(page, (_request, url) => {
    if (url.pathname === "/v1/session") return { json: session() };
    if (url.pathname === "/v1/spaces")
      return {
        json: {
          spaces: [
            { id: FIRST, name: "First" },
            { id: SECOND, name: "Second" },
          ],
        },
      };
    if (url.pathname === "/v1/change-requests")
      return { json: { changeRequests: [], total: 0 } };
    if (url.pathname === "/v1/plans") {
      reads.push(url);
      const space = url.searchParams.get("spaceId");
      const prefix = space === FIRST ? "a" : space === SECOND ? "b" : "global";
      const cursor = url.searchParams.get("cursor") || `${prefix}:0`;
      if (!cursor.startsWith(`${prefix}:`))
        return { status: 422, json: { error: "cursor changed Space" } };
      const start = Number(cursor.split(":")[1]);
      return {
        json: {
          plans: Array.from({ length: 20 }, (_, offset) => ({
            id: `${prefix}-plan-${start + offset}`,
            environment: `${prefix}-environment`,
            status: "SUCCEEDED",
            stepCount: 1,
            approvalCount: 1,
            requiredApprovals: 1,
          })),
          total: 100,
          limit: 20,
          pageSize: 20,
          hasPrevious: start > 0,
          nextCursor: `${prefix}:${start + 20}`,
        },
      };
    }
    if (url.pathname.startsWith("/v1/plans/")) {
      reads.push(url);
      const id = decodeURIComponent(url.pathname.split("/").pop()!);
      const prefix = url.searchParams.get("spaceId") === FIRST ? "a" : "b";
      if (!id.startsWith(`${prefix}-`))
        return { status: 404, json: { error: "outside selected Space" } };
      return {
        json: {
          id,
          plan: { environment: `${prefix}-environment`, steps: [] },
          status: "SUCCEEDED",
          approvals: [],
        },
      };
    }
    return { json: {} };
  });
  await page.goto(
    `/console/#area-releases?tab=plans&space=${FIRST}&plans.cursor=a%3A20&plans.status=SUCCEEDED&plan=b-plan-linked`,
  );
  const controls = page.locator('[data-index-controls="plans"]');
  await expect(controls.locator("[data-index-count]")).toHaveText(
    "Showing 20 of 100 plans in this page.",
  );
  await expect(controls).toContainText("linked record is unavailable");
  expect(
    reads
      .find((url) => url.pathname === "/v1/plans/b-plan-linked")
      ?.searchParams.get("spaceId"),
  ).toBe(FIRST);
  await expect(page.locator("[data-release-plan]")).toContainText("a-plan-20");
  await controls
    .getByRole("button", { name: "Next plans", exact: true })
    .click();
  await expect(page.locator("[data-release-plan]")).toContainText("a-plan-40");
  await page.locator("#space-selector").selectOption(SECOND);
  await expect(page.locator("[data-release-plan]")).toContainText("b-plan-0");
  const query = new URLSearchParams(new URL(page.url()).hash.split("?")[1]);
  expect(query.get("space")).toBe(SECOND);
  expect(query.get("plans.status")).toBe("SUCCEEDED");
  expect(query.has("plans.cursor")).toBe(false);
  expect(query.has("plan")).toBe(false);
  const switched = reads.find(
    (url) =>
      url.pathname === "/v1/plans" &&
      url.searchParams.get("spaceId") === SECOND,
  );
  expect(switched?.searchParams.has("cursor")).toBe(false);
  expect(
    reads
      .filter((url) => url.pathname === "/v1/plans")
      .every((url) => url.searchParams.has("spaceId")),
  ).toBe(true);
});

test("an unavailable remembered Space never becomes an unscoped page", async ({
  page,
}) => {
  const remembered = `space_${"c".repeat(32)}`;
  const reads: URL[] = [];
  await stubHub(page, (_request, url) => {
    if (url.pathname === "/v1/session") return { json: session() };
    if (url.pathname === "/v1/spaces")
      return { status: 503, json: { error: "Space directory unavailable" } };
    if (url.pathname === "/v1/plans") {
      reads.push(url);
      if (url.searchParams.get("spaceId") === remembered)
        return { status: 403, json: { error: "selected Space is denied" } };
      return { json: { plans: [], total: 900, limit: 20, pageSize: 0 } };
    }
    return { json: {} };
  });
  await page.goto(`/console/#area-releases?tab=plans&space=${remembered}`);
  const controls = page.locator('[data-index-controls="plans"]');
  await expect(controls.locator("[data-index-count]")).toContainText(
    "did not return",
  );
  await expect(page.locator("#space-selector")).toHaveValue(remembered);
  expect(reads.length).toBeGreaterThan(0);
  expect(
    reads.every((url) => url.searchParams.get("spaceId") === remembered),
  ).toBe(true);
  await page.locator("#space-selector").selectOption({ label: "All Spaces" });
  await expect(controls.locator("[data-index-count]")).toHaveText(
    "Showing 0 of 900 plans in this page.",
  );
  expect(reads.some((url) => !url.searchParams.has("spaceId"))).toBe(true);
});

import { expect, test } from "@playwright/test";
import { session, stubHub } from "./hub-stub";
const SPACE = `space_${"a".repeat(32)}`;
const procedure =
  "/conduit.environments.v1.EnvironmentReadService/ListEnvironmentSummaries";

test("Environment summary pages use generated reads with Space and opaque cursors", async ({
  page,
}) => {
  const calls: Record<string, unknown>[] = [];
  const rest: string[] = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (
      url.pathname === "/v1/environments" &&
      url.searchParams.get("view") === "summary"
    )
      rest.push(request.url());
  });
  await stubHub(page, (_request, url) => {
    if (url.pathname === "/v1/session") return { json: session() };
    if (url.pathname === "/v1/spaces")
      return { json: { spaces: [{ id: SPACE, name: "Selected" }] } };
    return { json: {} };
  });
  await page.route("**" + procedure, async (route) => {
    const body = route.request().postDataJSON();
    calls.push(body);
    expect(route.request().method()).toBe("POST");
    expect(body.spaceId).toBe(SPACE);
    expect(body.limit).toBe(20);
    const start = body.cursor ? 20 : 0;
    await route.fulfill({
      json: {
        environments: Array.from({ length: 20 }, (_, i) => ({
          environment: {
            id: `environment-${start + i}`,
            generation: "opaque/+=",
          },
          spoke: { state: "UNREACHABLE" },
        })),
        total: "9007199254740993",
        pageSize: 20,
        limit: 20,
        nextCursor: start ? "" : "opaque/+==",
        hasPrevious: start > 0,
      },
    });
  });
  await page.goto(`/console/#area-fleet?tab=environments&space=${SPACE}`);
  const controls = page.locator('[data-index-controls="environments"]');
  await expect(controls.locator("[data-index-count]")).toHaveText(
    "Showing 20 of 9007199254740993 environments in this page.",
  );
  await controls
    .getByRole("button", { name: "Next environments", exact: true })
    .click();
  await expect(page).toHaveURL(/environments.cursor=opaque%2F%2B%3D%3D/);
  await expect
    .poll(() => calls.some((c) => c.cursor === "opaque/+=="))
    .toBe(true);
  expect(rest).toEqual([]);
});

for (const [status, code] of [
  [403, "permission_denied"],
  [404, "not_found"],
  [400, "invalid_argument"],
] as const) {
  test(`Environment summary refusal ${status} keeps selection without REST replay`, async ({
    page,
  }) => {
    let calls = 0;
    const rest: string[] = [];
    page.on("request", (request) => {
      const url = new URL(request.url());
      if (
        url.pathname === "/v1/environments" &&
        url.searchParams.get("view") === "summary"
      )
        rest.push(request.url());
    });
    await stubHub(page, (_request, url) =>
      url.pathname === "/v1/session" ? { json: session() } : { json: {} },
    );
    await page.route("**" + procedure, (route) => {
      calls++;
      return route.fulfill({ status, json: { code, message: "refused" } });
    });
    await page.goto(
      "/console/#area-fleet?tab=environments&environments.cursor=held%2F%2B%3D",
    );
    const controls = page.locator('[data-index-controls="environments"]');
    await expect(controls).toContainText(
      "Hub did not return this environments page. Retry keeps its filters and cursor.",
    );
    await expect.poll(() => calls).toBeGreaterThan(0);
    await expect(page).toHaveURL(/environments.cursor=held%2F%2B%3D/);
    expect(rest).toEqual([]);
  });
}

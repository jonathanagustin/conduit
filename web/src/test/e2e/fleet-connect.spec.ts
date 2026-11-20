import { expect, test } from "@playwright/test";
import { session, stubHub } from "./hub-stub";
const SPACE = `space_${"a".repeat(32)}`;
const procedure = "/conduit.fleet.v1.FleetReadService/ListFleetInstallations";

test("active Fleet pages preserve Space and opaque cursors over the generated read", async ({
  page,
}) => {
  const calls: Record<string, unknown>[] = [];
  const rest: string[] = [];
  page.on("request", (request) => {
    if (new URL(request.url()).pathname === "/v1/fleet/installations")
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
        generatedAt: "2026-11-20T01:02:03.123456789Z",
        maxResultAgeSeconds: "120",
        installations: Array.from({ length: 20 }, (_, i) => ({
          environment: "env-a",
          installationId: `item-${start + i}`,
          product: `product-${start + i}`,
          observedAt: "2026-11-20T01:01:00.123456789Z",
          health: "HEALTHY",
        })),
        summary: { total: "9007199254740993" },
        facets: { environment: { "env-a": "9007199254740993" } },
        pageSize: 20,
        limit: 20,
        nextCursor: start ? "" : "opaque/+==",
        hasPrevious: start > 0,
        previousCursor: "",
      },
    });
  });
  await page.goto(`/console/#rm05-release-rollout?space=${SPACE}`);
  const controls = page.locator('[data-index-controls="installations"]');
  await expect(controls.locator("[data-index-count]")).toHaveText(
    "Showing 20 of 9007199254740993 installations in this page.",
  );
  await controls
    .getByRole("button", { name: "Next installations", exact: true })
    .click();
  await expect(page).toHaveURL(/installations.cursor=opaque%2F%2B%3D%3D/);
  await expect
    .poll(() => calls.some((c) => c.cursor === "opaque/+=="))
    .toBe(true);
  await expect(
    controls.getByRole("button", {
      name: "Previous installations",
      exact: true,
    }),
  ).toBeEnabled();
  expect(rest).toEqual([]);
});

for (const [status, code] of [
  [403, "permission_denied"],
  [404, "not_found"],
  [400, "invalid_argument"],
] as const) {
  test(`active Fleet refusal ${status} never replays REST or displays a stale page`, async ({
    page,
  }) => {
    let calls = 0;
    const rest: string[] = [];
    page.on("request", (request) => {
      if (new URL(request.url()).pathname === "/v1/fleet/installations")
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
      "/console/#rm05-release-rollout?installations.cursor=held%2F%2B%3D",
    );
    await expect(
      page.locator('[data-index-controls="installations"]'),
    ).toContainText("Retry keeps its filters and cursor.");
    expect(new URL(page.url()).hash).toContain(
      "installations.cursor=held%2F%2B%3D",
    );
    expect(calls).toBeGreaterThan(0);
    expect(rest).toEqual([]);
  });
}

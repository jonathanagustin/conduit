import { expect, test, type Page } from "@playwright/test";
import { session, stubHub } from "./hub-stub";

function usage(reported = false) {
  return {
    owner: "riley",
    days: 1,
    budget: {
      day: "2026-11-18",
      resetsAt: "2026-11-19T00:00:00Z",
      used: { calls: 1, tokens: reported ? 3 : 0 },
      dailyCallBudget: reported ? null : 1,
      dailyTokenBudget: reported ? null : 100,
      remainingCalls: reported ? null : 0,
      remainingTokens: reported ? null : 100,
    },
    usage: [
      {
        owner: "riley",
        day: "2026-11-18",
        budgetedCalls: 1,
        attempts: 1,
        callsWithTokens: reported ? 1 : 0,
        promptTokens: reported ? 1 : null,
        completionTokens: reported ? 2 : null,
      },
    ],
  };
}

async function preferences(page: Page) {
  await page.goto("/console/");
  await expect(
    page.getByRole("img", { name: "Operator: riley" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Search views", exact: true }).click();
  const palette = page.getByRole("dialog", { name: "Command palette" });
  await palette.getByRole("searchbox").fill("Preferences");
  await page.keyboard.press("Enter");
  await expect(
    page.getByRole("region", { name: "Your AI usage" }),
  ).toBeVisible();
}

test("own usage is an explicit read and preserves exhausted and unreported counts", async ({
  page,
}) => {
  const requests: string[] = [];
  await stubHub(page, (request, url) => {
    if (url.pathname === "/v1/session") return { json: session() };
    if (url.pathname.includes("/ai/usage")) {
      requests.push(`${request.method()} ${url.pathname}${url.search}`);
      return { json: usage() };
    }
    return undefined;
  });
  await preferences(page);
  const panel = page.getByRole("region", { name: "Your AI usage" });
  expect(requests).toEqual([]);
  await panel.getByRole("button", { name: "Load AI usage" }).click();
  await expect(panel).toContainText("0 remaining of 1");
  await expect(panel).toContainText("Not reported");
  await expect(panel).toContainText("100 remaining of 100");
  await expect(panel).not.toContainText("0 reported");
  expect(requests).toEqual(["GET /v1/ai/usage?days=1"]);
  await panel.screenshot({ path: test.info().outputPath("own-usage.png") });
});

test("usage failures and refreshes do not retain a stale budget or expose server diagnostics", async ({
  page,
}) => {
  let reads = 0;
  const requests: string[] = [];
  await stubHub(page, (request, url) => {
    if (url.pathname === "/v1/session") return { json: session() };
    if (url.pathname.includes("/ai/usage")) {
      requests.push(`${request.method()} ${url.pathname}${url.search}`);
      reads++;
      return reads === 2
        ? { json: usage(true) }
        : { status: 503, json: { error: "private server diagnostic" } };
    }
    return undefined;
  });
  await preferences(page);
  const panel = page.getByRole("region", { name: "Your AI usage" });
  await panel.getByRole("button", { name: "Load AI usage" }).click();
  await expect(panel.getByRole("alert")).toHaveText(
    "AI usage could not be read. Try again.",
  );
  await expect(panel).not.toContainText("private server diagnostic");
  await panel.getByRole("button", { name: "Retry AI usage" }).click();
  await expect(panel).toContainText("3 reported");
  await expect(
    panel.locator("dd").filter({ hasText: /^Unlimited$/ }),
  ).toHaveCount(2);
  await panel.getByRole("button", { name: "Refresh AI usage" }).click();
  await expect(panel.getByRole("alert")).toBeVisible();
  await expect(panel).not.toContainText("3 reported");
  await expect(panel.locator("dd")).toHaveCount(0);
  expect(requests).toEqual(Array(3).fill("GET /v1/ai/usage?days=1"));
});

test("a delayed usage read keeps another preference's editing focus", async ({
  page,
}) => {
  await stubHub(page, (_request, url) =>
    url.pathname === "/v1/session" ? { json: session() } : undefined,
  );
  let finish!: () => void;
  const pending = new Promise<void>((resolve) => {
    finish = resolve;
  });
  let reads = 0;
  await page.route("**/v1/ai/usage?days=1", async (route) => {
    reads++;
    await pending;
    await route.fulfill({ json: usage(true) });
  });
  await preferences(page);
  const panel = page.getByRole("region", { name: "Your AI usage" });
  await panel.getByRole("button", { name: "Load AI usage" }).click();
  await expect(panel.getByRole("button")).toHaveAttribute(
    "aria-disabled",
    "true",
  );
  await page
    .getByRole("button", { name: "Show token example data", exact: true })
    .click();
  const name = page.locator("#pref-token-name");
  await name.fill("keep this draft");
  await expect(name).toBeFocused();
  finish();
  await expect(panel).toContainText("3 reported");
  await expect(name).toBeFocused();
  await expect(name).toHaveValue("keep this draft");
  expect(reads).toBe(1);
});

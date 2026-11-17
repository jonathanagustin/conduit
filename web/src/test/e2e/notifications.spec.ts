import { expect, test, type Page } from "@playwright/test";

import { session, stubHub, type Handler } from "./hub-stub";

const owner = { kind: "USER", id: "riley" };
const entries = (count: number) =>
  Array.from({ length: count }, (_, index) => ({
    recipient: { owner, channel: "IN_APP" },
    event: {
      id: `event-${index}`,
      type: index === 0 ? "A_TYPE_THIS_BUILD_NEVER_HEARD_OF" : "PLAN_FAILURES",
      resourceRid: "environment/prod",
      environment: "prod",
      summary: `Plan ${index} failed on prod`,
      actor: "spoke-agent",
      occurredAt: new Date(Date.UTC(2026, 10, 1, 0, index)).toISOString(),
    },
    preferenceId: "pref-1",
    readAt: "0001-01-01T00:00:00Z",
  }));

/** An inbox route that applies the requested limit, as application.Bound does. */
function inbox(all: unknown[], requests: URL[] = []): Handler {
  return (_request, url) => {
    if (url.pathname === "/v1/session") return { json: session() };
    if (url.pathname !== "/v1/notifications/inbox") return undefined;
    requests.push(url);
    const limit = Math.min(Number(url.searchParams.get("limit")) || 100, 100);
    const page = all.slice(0, limit);
    return {
      json: {
        owner,
        channel: url.searchParams.get("channel"),
        limit,
        entries: page.length ? page : null,
      },
    };
  };
}

const indicator = (page: Page) => page.locator("#notifications-indicator");

test("the documented inbox path shows an empty inbox as empty", async ({
  page,
}) => {
  const requests: URL[] = [];
  await stubHub(page, inbox([], requests));
  await page.goto("/console/notifications");
  await expect(page.locator('[data-inbox="empty"]')).toBeVisible();
  await expect(page.getByText("No notifications")).toBeVisible();
  await expect(page.locator("[data-ntf-owner]")).toHaveText("USER riley");
  await expect(indicator(page).locator("[data-unread]")).toBeHidden();
  // A channel is always stated and an owner never is.
  expect(requests.length).toBeGreaterThan(0);
  for (const url of requests) {
    expect(url.searchParams.get("channel")).toBe("IN_APP");
    expect(url.searchParams.has("ownerId")).toBe(false);
    expect(url.searchParams.has("ownerKind")).toBe(false);
  }
});

test("the inbox lists rows, counts them as new, and widens a bounded page", async ({
  page,
}) => {
  await stubHub(page, inbox(entries(30)));
  await page.goto("/console/");
  // The count comes from the refresh cycle's read, before the inbox is opened.
  await expect(indicator(page).locator("[data-unread]")).toHaveText("30");
  await indicator(page).click();
  await expect(page.locator('[data-inbox="rows"]')).toBeVisible();
  await expect(page.locator("[data-inbox-row]")).toHaveCount(25);
  await expect(page.getByText("Plan 3 failed on prod")).toBeVisible();
  // A type this build does not know still renders as the string the Hub sent.
  await expect(
    page.getByText("A_TYPE_THIS_BUILD_NEVER_HEARD_OF").first(),
  ).toBeVisible();
  await expect(page.locator("[data-inbox-new]")).toHaveCount(25);
  await expect(page.locator("[data-inbox-bound]")).toContainText(
    "filled the limit of 25",
  );
  await page.getByRole("button", { name: "Show up to 50" }).click();
  await expect(page.locator("[data-inbox-row]")).toHaveCount(30);
  await expect(page.locator("[data-inbox-complete]")).toContainText(
    "whole inbox",
  );
  await expect(page.locator("[data-inbox-bound]")).toHaveCount(0);
  // Showing the inbox is what marks its entries seen.
  await expect(indicator(page).locator("[data-unread]")).toBeHidden();
});

test("a failed inbox read is a failure with the Hub's message, then retries", async ({
  page,
}) => {
  let broken = true;
  const working = inbox(entries(2));
  await stubHub(page, (request, url) => {
    if (broken && url.pathname === "/v1/notifications/inbox")
      return { status: 500, json: { error: "inbox store unavailable" } };
    return working(request, url);
  });
  await page.goto("/console/notifications");
  const failed = page.locator('[data-inbox="failed"]');
  await expect(failed).toBeVisible();
  await expect(failed).toContainText("inbox store unavailable");
  await expect(failed).toContainText("not an empty inbox");
  await expect(page.getByText("No notifications")).toHaveCount(0);
  // An unknown count is not shown as zero.
  await expect(indicator(page).locator("[data-unread]")).toHaveText("?");
  broken = false;
  await failed.getByRole("button", { name: "Retry" }).click();
  await expect(page.locator("[data-inbox-row]")).toHaveCount(2);
});

test("preferences disclose unavailable types before saving and allow removal of legacy selections", async ({
  page,
}) => {
  const rid = "product/payments";
  const writes: unknown[] = [];
  const unavailable = [
    "BLUE_GREEN_UPGRADE_FAILED",
    "INSTALLED_PRODUCT_RELEASE_VULNERABILITY_UPDATE",
    "LATEST_PRODUCT_RELEASE_VULNERABILITY_UPDATE",
    "RBAC_CHANGE",
  ];
  let stored: Record<string, unknown> | null = null;
  let refuse = false;
  await stubHub(page, (request, url) => {
    if (url.pathname === "/v1/session")
      return {
        json: session({
          resources: url.searchParams.getAll("resourceRid").map((item) => ({
            resourceRid: item,
            operations: ["resource:read"],
          })),
        }),
      };
    if (url.pathname === "/v1/notifications/contacts")
      return { json: { owner, contacts: [] } };
    if (url.pathname !== "/v1/notifications/preferences") return undefined;
    if (request.method() === "GET")
      return stored
        ? { json: stored }
        : {
            status: 404,
            json: {
              error: "no notification preference",
              typesNotYetEmitted: unavailable,
            },
          };
    const body = request.postDataJSON();
    writes.push(body);
    if (refuse)
      return {
        status: 422,
        json: { error: "notification type list is not acceptable" },
      };
    stored = {
      id: "pref-1",
      owner,
      ...body,
      revision: 1,
      updatedAt: "2026-11-17T00:00:00Z",
      typesNotYetEmitted: unavailable,
    };
    return { json: stored };
  });
  await page.goto("/console/notifications");
  await page.locator('.kit-subnav [data-ntf-view="preferences"]').click();
  await page.getByLabel("Resource").fill(rid);
  await page.getByRole("button", { name: "Load preference" }).click();
  // A 404 is the state "not subscribed", not a failure.
  await expect(page.locator("[data-preference-editor]")).toContainText(
    "You are not subscribed to",
  );
  const save = page.getByRole("button", { name: "Save preference" });
  await expect(save).toBeDisabled();
  await expect(page.locator("[data-save-reason]")).toContainText(
    "can never deliver",
  );
  const silent = page.locator('[data-type-not-emitted="RBAC_CHANGE"]');
  await expect(silent).toContainText("Not yet emitted by this Hub");
  await expect(silent.locator('input[type="checkbox"]')).toBeDisabled();
  await expect(silent.locator('input[type="checkbox"]')).not.toBeChecked();
  await expect(page.locator('[data-ntf-type="RBAC_CHANGE"]')).toHaveCount(0);
  await page.locator('[data-ntf-type="PLAN_FAILURES"]').check();
  await page.locator('[data-ntf-type="PRODUCT_RECALLS"]').check();
  await expect(save).toBeEnabled();
  expect(writes).toEqual([]);

  refuse = true;
  await save.click();
  await expect(page.locator("[data-preference-error]")).toContainText(
    "notification type list is not acceptable",
  );
  refuse = false;
  await save.click();
  await expect(page.locator("[data-preference-saved]")).toContainText(
    "stored revision 1",
  );
  // No owner and no revision: the Hub resolves one and rejects the other.
  expect(writes.at(-1)).toEqual({
    resourceRid: rid,
    channel: "IN_APP",
    types: ["PLAN_FAILURES", "PRODUCT_RECALLS"],
    allFutureTypes: false,
  });
  stored = Object.assign({}, stored, { types: ["PLAN_FAILURES", "RBAC_CHANGE"] });
  await page.getByRole("button", { name: "Load preference" }).click();
  await expect(silent.locator('input[type="checkbox"]')).toBeDisabled();
  await expect(silent.locator('input[type="checkbox"]')).toBeChecked();
  await expect(save).toBeDisabled();
  await expect(page.locator("[data-save-reason]")).toContainText(
    "Remove the selected types",
  );
  await silent.getByRole("button", { name: "Remove" }).click();
  await expect(silent.locator('input[type="checkbox"]')).not.toBeChecked();
  await expect(save).toBeEnabled();
  await save.click();
  await expect(page.locator("[data-preference-saved]")).toBeVisible();
  expect(writes.at(-1)).toMatchObject({
    types: ["PLAN_FAILURES"],
    allFutureTypes: false,
  });
  await expect(page.locator('[data-ntf-type="PLAN_FAILURES"]')).toBeChecked();
  await expect(save).toBeDisabled();
});

test("Save is disabled with the reason when the session lacks the grant", async ({
  page,
}) => {
  const rid = "product/payments";
  let writes = 0;
  await stubHub(page, (request, url) => {
    if (url.pathname === "/v1/session")
      return {
        json: session({
          globalOperations: [],
          resources: url.searchParams
            .getAll("resourceRid")
            .map((item) => ({ resourceRid: item, operations: [] })),
        }),
      };
    if (url.pathname === "/v1/notifications/preferences") {
      if (request.method() !== "GET") writes += 1;
      return {
        json: {
          id: "pref-1",
          owner,
          resourceRid: rid,
          channel: "IN_APP",
          types: ["PLAN_FAILURES"],
          typesNotYetEmitted: [],
          revision: 4,
        },
      };
    }
    return undefined;
  });
  await page.goto("/console/notifications");
  await page.locator('.kit-subnav [data-ntf-view="preferences"]').click();
  await page.getByLabel("Resource").fill(rid);
  await page.getByRole("button", { name: "Load preference" }).click();
  await page.locator('[data-ntf-type="ROLE_CHANGE"]').check();
  await expect(page.locator("[data-save-reason]")).toHaveText(
    `Your session does not hold resource:read on ${rid}.`,
  );
  await expect(
    page.getByRole("button", { name: "Save preference" }),
  ).toBeDisabled();
  expect(writes).toBe(0);
});

test("contacts are listed, saved and deleted by their owner", async ({
  page,
}) => {
  let contacts: Record<string, unknown>[] = [
    {
      id: "c-pager",
      owner,
      kind: "PAGERDUTY",
      address: "svc-key",
      optIntoFutureResources: false,
      deliverable: false,
    },
  ];
  const writes: { method: string; body: unknown; query: string }[] = [];
  await stubHub(page, (request, url) => {
    if (url.pathname === "/v1/session") return { json: session() };
    if (url.pathname !== "/v1/notifications/contacts") return undefined;
    const method = request.method();
    if (method === "GET") return { json: { owner, contacts } };
    writes.push({
      method,
      body: method === "PUT" ? request.postDataJSON() : null,
      query: url.search,
    });
    if (method === "PUT") {
      const body = request.postDataJSON();
      if (!String(body.address).includes("@"))
        return {
          status: 422,
          json: { error: "an EMAIL contact needs an address with an @" },
        };
      contacts = [
        ...contacts,
        { id: "c-email", owner, ...body, deliverable: true },
      ];
      return { json: contacts.at(-1) };
    }
    contacts = contacts.filter(
      (contact) => contact["kind"] !== url.searchParams.get("kind"),
    );
    return {
      json: { owner, kind: url.searchParams.get("kind"), deleted: true },
    };
  });
  await page.goto("/console/notifications");
  await page.locator('.kit-subnav [data-ntf-view="contacts"]').click();
  await expect(page.locator('[data-contact-row="PAGERDUTY"]')).toContainText(
    "Stored, never delivered",
  );
  const address = page.getByLabel("Address");
  await address.fill("not-an-address");
  await page.getByRole("button", { name: "Save contact" }).click();
  await expect(page.locator("[data-contact-error]")).toContainText(
    "needs an address with an @",
  );
  await address.fill("riley@example.test");
  await page.getByRole("button", { name: "Save contact" }).click();
  await expect(page.locator('[data-contact-row="EMAIL"]')).toContainText(
    "riley@example.test",
  );
  expect(writes.at(-1)).toEqual({
    method: "PUT",
    body: {
      kind: "EMAIL",
      address: "riley@example.test",
      optIntoFutureResources: false,
    },
    query: "",
  });
  const row = page.locator('[data-contact-row="PAGERDUTY"]');
  await row.getByRole("button", { name: "Delete" }).click();
  expect(writes.filter((write) => write.method === "DELETE")).toEqual([]);
  await row.getByRole("button", { name: "Confirm delete" }).click();
  await expect(row).toHaveCount(0);
  expect(writes.at(-1)).toMatchObject({
    method: "DELETE",
    query: "?kind=PAGERDUTY",
  });
});

test("the channel is owned by the URL and never forwarded when unknown", async ({
  page,
}) => {
  const requests: URL[] = [];
  await stubHub(page, inbox([], requests));
  const channels = () => [
    ...new Set(requests.map((url) => url.searchParams.get("channel"))),
  ];
  await page.goto("/console/notifications?channel=SLACK");
  await expect(page.locator('[data-inbox="empty"]')).toContainText("SLACK");
  expect(channels()).toContain("SLACK");
  await page.locator("[data-ntf-channel]").selectOption("EMAIL");
  await expect(page.locator('[data-inbox="empty"]')).toContainText("EMAIL");
  await expect(page).toHaveURL(/\?channel=EMAIL#ntf-inbox$/);
  await page.goto("/console/notifications?channel=PAGER");
  await expect(page.locator("[data-ntf-channel]")).toHaveValue("IN_APP");
  await expect(page.locator('[data-inbox="empty"]')).toContainText("IN_APP");
  expect(new Set(channels())).toEqual(new Set(["EMAIL", "IN_APP", "SLACK"]));
});

import { expect, test } from "./harness.js";
import { NotificationsPom } from "./pages/index.js";

/** Notifications -- the caller's own inbox, and the preference that decides what
 * lands in it.
 *
 * The fixtures follow `internal/notifications/domain` and the envelopes that
 * `internal/notifications/adapters/httpapi` assembles: an inbox read answers
 * `{owner, channel, limit, entries}` with `limit` reporting the bound that was
 * APPLIED, and a preference is a `domain.Preference` with its minted id,
 * timestamps and revision. Two properties of that surface are load-bearing here
 * and are asserted rather than assumed:
 *
 *   - Go encodes an empty slice as `null`, so an empty inbox answers
 *     `{"entries":null}` and not `{"entries":[]}`. That is the first thing every
 *     new operator sees, so it gets its own test.
 *   - `ErrNoPreference` is a 404. "You are not subscribed to this" is a state,
 *     not a failure, and it must not render through the failure path -- the
 *     readiness record already carries one defect where a failed request read as
 *     a degraded platform, and this is the same mistake pointed the other way. */

/** Blueprint draws a decorative indicator span over its own checkbox input, so
 * an ordinary click lands on the indicator and Playwright waits for an
 * interception that never clears. The input is the control; forcing the click
 * onto it is not skipping an accessibility check -- the role and accessible name
 * are what the locator resolved on in the first place. */
const FORCE = { force: true } as const;

const ENVIRONMENT_RID = "ri.apollo.main.environment.d41d8cd98f00";
const PRODUCT_RID = "ri.apollo.main.product.7c9a2be40155";

function entry(overrides: Record<string, unknown> = {}) {
  return {
    recipient: { owner: { kind: "USER", id: "browser-operator" }, channel: "IN_APP" },
    event: {
      id: "nev-1",
      type: "PLAN_FAILURES",
      resourceRid: ENVIRONMENT_RID,
      environment: "prod-use1",
      summary: "Reconciliation of installation payments in Environment prod-use1 gave up",
      actor: "a.okonkwo",
      occurredAt: "2026-08-18T09:00:00Z",
    },
    preferenceId: "npf-1",
    ...overrides,
  };
}

function inbox(entries: unknown[] | null, limit = 100) {
  return {
    owner: { kind: "USER", id: "browser-operator" },
    channel: "IN_APP",
    limit,
    entries,
  };
}

const PREFERENCE = {
  id: "npf-1",
  owner: { kind: "USER", id: "browser-operator" },
  resourceRid: ENVIRONMENT_RID,
  channel: "IN_APP",
  types: ["PLAN_FAILURES"],
  createdAt: "2026-08-10T08:00:00Z",
  updatedAt: "2026-08-17T08:00:00Z",
  revision: 3,
};

test.describe("Notification inbox", () => {
  test("shows what the Hub recorded for the caller, and whose inbox it is", async ({ page, hub }) => {
    hub.get("/notifications/inbox", inbox([entry(), entry({
      event: {
        id: "nev-2",
        type: "PRODUCT_RECALLS",
        resourceRid: PRODUCT_RID,
        summary: "app.example:payments 2.4.0 was recalled",
        occurredAt: "2026-08-18T07:30:00Z",
      },
      preferenceId: "npf-2",
    })]));
    await page.goto("notifications");
    const notifications = new NotificationsPom(page);

    const failure = notifications.entry("nev-1");
    await expect(failure).toContainText("PLAN_FAILURES");
    await expect(failure).toContainText(ENVIRONMENT_RID);
    await expect(failure).toContainText("prod-use1");
    await expect(failure).toContainText("gave up");
    await expect(failure).toContainText("a.okonkwo");

    // An empty actor is reserved for events the system raised on its own, so it
    // reads as a fact rather than as a missing value.
    await expect(notifications.entry("nev-2")).toContainText("the system");

    // The owner comes back from the Hub; the console never names one on a read.
    await expect(notifications.root).toContainText("browser-operator");
    expect(hub.requests()).toContain("GET /v1/notifications/inbox?channel=IN_APP");
    expect(hub.requests().some((request) => request.includes("ownerId"))).toBe(false);
  });

  test("says an empty inbox is empty, including the null Go sends for one", async ({ page, hub }) => {
    hub.get("/notifications/inbox", inbox(null));
    await page.goto("notifications");
    const notifications = new NotificationsPom(page);

    await expect(notifications.empty).toContainText("Nothing has reached your IN_APP inbox");
    await expect(notifications.bounded).toHaveCount(0);
  });

  test("a failed read looks like a failed read, and retries", async ({ page, hub }) => {
    hub.fails("/notifications/inbox", 500, "notification store unavailable");
    await page.goto("notifications");
    const notifications = new NotificationsPom(page);

    await expect(page.getByText(/could not be read/i)).toBeVisible();
    await expect(page.getByText("notification store unavailable")).toBeVisible();
    // The failure must not read as an empty inbox: "nothing happened" and "we
    // could not find out" are different answers to the same question.
    await expect(notifications.empty).toHaveCount(0);

    hub.get("/notifications/inbox", inbox([entry()]));
    await page.getByRole("button", { name: "Try again" }).click();
    await expect(notifications.entry("nev-1")).toBeVisible();
  });

  test("a refusal reads as a refusal rather than as a broken console", async ({ page, hub }) => {
    hub.fails("/notifications/inbox", 403, "notifications for USER someone-else belong to that owner");
    await page.goto("notifications");

    await expect(page.getByText("Not visible to you")).toBeVisible();
  });

  test("says a full page is a full page, not the end of the inbox", async ({ page, hub }) => {
    // The Hub reports the limit it APPLIED, so a page that filled it may be
    // hiding older entries. Two entries under a limit of two is the same
    // situation as a hundred under a hundred, and cheaper to render.
    hub.get(
      "/notifications/inbox",
      inbox([entry(), entry({ event: { ...entry().event, id: "nev-2" }, preferenceId: "npf-2" })], 2),
    );
    await page.goto("notifications");
    const notifications = new NotificationsPom(page);

    await expect(notifications.bounded).toContainText("most recent 2 entries");

    // One entry under a limit of two is the end of the inbox, and says nothing.
    hub.get("/notifications/inbox", inbox([entry()], 2));
    await page.reload();
    await expect(notifications.entry("nev-1")).toBeVisible();
    await expect(notifications.bounded).toHaveCount(0);
  });

  test("the channel is owned by the URL and reaches the Hub", async ({ page, hub }) => {
    hub.get("/notifications/inbox", inbox([entry()]));
    await page.goto("notifications");
    const notifications = new NotificationsPom(page);

    await notifications.channel.selectOption("EMAIL");
    await expect(page).toHaveURL(/channel=EMAIL/);
    await expect
      .poll(() => hub.requests().filter((request) => request.includes("channel=EMAIL")).length)
      .toBeGreaterThan(0);

    // An unrecognised channel in a hand-edited link falls back rather than being
    // forwarded: the route refuses one it does not know with a 400, and a bad
    // link should not look like a broken console.
    await page.goto("notifications?channel=CARRIER_PIGEON");
    await expect(notifications.channel).toHaveValue("IN_APP");
    expect(hub.requests().some((request) => request.includes("CARRIER_PIGEON"))).toBe(false);
  });
});

test.describe("Notification preference", () => {
  test("is unreachable until a resource is named, then reads the stored one", async ({ page, hub }) => {
    hub.get("/notifications/inbox", inbox([entry()]));
    hub.get("/notifications/preferences", PREFERENCE);
    await page.goto("notifications");
    const notifications = new NotificationsPom(page);

    await expect(notifications.root.getByTestId("preference-unselected")).toBeVisible();

    await notifications.entry("nev-1").click();
    await expect(page).toHaveURL(/resourceRid=ri\.apollo\.main\.environment\./);
    await expect(notifications.editor).toContainText("npf-1");
    await expect(notifications.editor).toContainText("revision 3");
    await expect(notifications.type("PLAN_FAILURES")).toBeChecked();
    await expect(notifications.type("PRODUCT_RECALLS")).not.toBeChecked();
  });

  test("treats no stored preference as a state rather than a failure", async ({ page, hub }) => {
    hub.get("/notifications/inbox", inbox(null));
    hub.fails(
      "/notifications/preferences",
      404,
      "no notification preference is stored for that owner, resource and channel",
    );
    await page.goto(`notifications?resourceRid=${encodeURIComponent(PRODUCT_RID)}`);
    const notifications = new NotificationsPom(page);

    await expect(notifications.preferenceAbsent).toContainText("no IN_APP preference for this resource");
    await expect(page.getByText(/could not be read/i)).toHaveCount(0);
    // Nothing is enabled, so there is nothing the Hub would accept.
    await expect(notifications.deliversNothing).toBeVisible();
    await expect(notifications.save).toBeDisabled();
  });

  test("saves an edit as the Hub's decoder accepts it, naming no owner", async ({ page, hub }) => {
    hub.get("/notifications/inbox", inbox([entry()]));
    hub.get("/notifications/preferences", PREFERENCE);
    hub.put("/notifications/preferences", {
      ...PREFERENCE,
      types: ["PLAN_FAILURES", "PRODUCT_RECALLS"],
      revision: 4,
      updatedAt: "2026-08-18T10:00:00Z",
    });
    await page.goto(`notifications?resourceRid=${encodeURIComponent(ENVIRONMENT_RID)}`);
    const notifications = new NotificationsPom(page);

    await notifications.type("PRODUCT_RECALLS").check(FORCE);
    await notifications.save.click();
    await expect(notifications.saved).toBeVisible();

    expect(hub.requests()).toContain("PUT /v1/notifications/preferences");
    expect(hub.bodies()).toEqual([
      {
        resourceRid: ENVIRONMENT_RID,
        channel: "IN_APP",
        // Sorted, so two edits enabling the same types produce one document.
        types: ["PLAN_FAILURES", "PRODUCT_RECALLS"],
        allFutureTypes: false,
      },
    ]);
    // An owner is never sent. An omitted one defaults to the caller and cannot
    // name somebody else's subscription.
    expect(JSON.stringify(hub.bodies())).not.toContain("owner");
  });

  test("opting into future types disables the checkboxes it overrides", async ({ page, hub }) => {
    hub.get("/notifications/inbox", inbox(null));
    hub.get("/notifications/preferences", PREFERENCE);
    hub.put("/notifications/preferences", { ...PREFERENCE, allFutureTypes: true, revision: 4 });
    await page.goto(`notifications?resourceRid=${encodeURIComponent(ENVIRONMENT_RID)}`);
    const notifications = new NotificationsPom(page);

    await notifications.allFutureTypes.check(FORCE);
    // The Hub ignores Types while AllFutureTypes is set, so a live checkbox
    // would be one that changes nothing.
    await expect(notifications.type("PRODUCT_RECALLS")).toBeDisabled();
    await expect(notifications.type("PRODUCT_RECALLS")).toBeChecked();

    await notifications.save.click();
    await expect(notifications.saved).toBeVisible();
    expect(hub.bodies()).toEqual([
      {
        resourceRid: ENVIRONMENT_RID,
        channel: "IN_APP",
        types: ["PLAN_FAILURES"],
        allFutureTypes: true,
      },
    ]);
  });

  test("carries a type this build has no checkbox for, and says so", async ({ page, hub }) => {
    // Apollo's vocabulary is open and the Hub's is wider than any one bundle's.
    // A save that silently dropped an unknown type would unsubscribe an operator
    // from something they chose, as a side effect of opening a settings page.
    hub.get("/notifications/inbox", inbox(null));
    hub.get("/notifications/preferences", {
      ...PREFERENCE,
      types: ["PLAN_FAILURES", "SOMETHING_ADDED_LATER"],
    });
    hub.put("/notifications/preferences", { ...PREFERENCE, revision: 4 });
    await page.goto(`notifications?resourceRid=${encodeURIComponent(ENVIRONMENT_RID)}`);
    const notifications = new NotificationsPom(page);

    await expect(notifications.unknownTypes).toContainText("SOMETHING_ADDED_LATER");
    await notifications.save.click();
    await expect(notifications.saved).toBeVisible();
    expect(hub.bodies()).toEqual([
      {
        resourceRid: ENVIRONMENT_RID,
        channel: "IN_APP",
        types: ["PLAN_FAILURES", "SOMETHING_ADDED_LATER"],
        allFutureTypes: false,
      },
    ]);
  });

  test("reports a refused write instead of pretending it was stored", async ({ page, hub }) => {
    hub.get("/notifications/inbox", inbox(null));
    hub.get("/notifications/preferences", PREFERENCE);
    hub.putFails(
      "/notifications/preferences",
      403,
      "notifications for USER someone-else belong to that owner",
    );
    await page.goto(`notifications?resourceRid=${encodeURIComponent(ENVIRONMENT_RID)}`);
    const notifications = new NotificationsPom(page);

    await notifications.type("PRODUCT_RECALLS").check(FORCE);
    await notifications.save.click();

    await expect(notifications.saveFailed).toContainText("belong to that owner");
    await expect(notifications.saved).toHaveCount(0);
  });

  test("reaches a preference for a resource nothing has been recorded about", async ({ page, hub }) => {
    // An operator who was told nothing has no row to click, and is exactly the
    // person who needs to subscribe.
    hub.get("/notifications/inbox", inbox(null));
    hub.get("/notifications/preferences", { ...PREFERENCE, resourceRid: PRODUCT_RID });
    await page.goto("notifications");
    const notifications = new NotificationsPom(page);

    await notifications.resource.fill(PRODUCT_RID);
    await notifications.loadPreference.click();
    await expect(notifications.editor).toBeVisible();
    await expect
      .poll(() =>
        hub
          .requests()
          .some(
            (request) =>
              request.startsWith("GET /v1/notifications/preferences") &&
              request.includes(PRODUCT_RID),
          ),
      )
      .toBe(true);
  });
});

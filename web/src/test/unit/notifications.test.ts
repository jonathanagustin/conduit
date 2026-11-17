// @vitest-environment node
import { describe, expect, it } from "vite-plus/test";

import { EVENT_TYPES } from "../../console/operator/hub-vocabulary.js";
import {
  INBOX_LIMITS,
  contactPayload,
  contactProblem,
  inboxBound,
  inboxPath,
  inboxReduce,
  inboxView,
  indicatorView,
  initialInbox,
  initialNotifications,
  initialPreferenceDraft,
  isUnread,
  newestOccurredAt,
  notificationsMarkup,
  preferenceDiff,
  preferencePayload,
  preferenceProblem,
  requestedChannel,
  staleRevision,
  unreadSummary,
} from "../../console/operator/notifications.js";

const entry = (id: string, occurredAt: string, extra = {}) => ({
  recipient: { owner: { kind: "USER", id: "riley" }, channel: "IN_APP" },
  event: {
    id,
    type: "PLAN_FAILURES",
    resourceRid: "environment/prod",
    summary: `plan ${id} failed`,
    occurredAt,
  },
  preferenceId: "pref-1",
  // Go writes the zero time for a struct field marked omitempty.
  readAt: "0001-01-01T00:00:00Z",
  ...extra,
});
const page = (count: number, limit: number) => ({
  owner: { kind: "USER", id: "riley" },
  channel: "IN_APP",
  limit,
  entries: Array.from({ length: count }, (_, index) =>
    entry(
      `e${index}`,
      `2026-11-${String(index + 1).padStart(2, "0")}T00:00:00Z`,
    ),
  ),
});
const loaded = (body: unknown, from = initialInbox()) =>
  inboxReduce(inboxReduce(from, { type: "request" }), {
    type: "response",
    body,
  });

describe("inbox state", () => {
  it("is loading from the request until the Hub answers", () => {
    expect(inboxView(initialInbox())).toBe("loading");
    expect(inboxView(inboxReduce(initialInbox(), { type: "request" }))).toBe(
      "loading",
    );
  });

  it("reads Go's null entries as an empty inbox", () => {
    const inbox = loaded({
      owner: { kind: "USER", id: "riley" },
      channel: "IN_APP",
      limit: 25,
      entries: null,
    });
    expect(inboxView(inbox)).toBe("empty");
    expect(inbox.owner).toEqual({ kind: "USER", id: "riley" });
  });

  it("shows rows and the limit the Hub applied", () => {
    const inbox = loaded(page(3, 25));
    expect(inboxView(inbox)).toBe("rows");
    expect(inbox.entries).toHaveLength(3);
    expect(inbox.limit).toBe(25);
  });

  it("never turns a failure into an empty inbox", () => {
    const failed = inboxReduce(loaded(page(3, 25)), {
      type: "failure",
      status: 500,
      message: "store unavailable",
    });
    expect(inboxView(failed)).toBe("failed");
    expect(failed.entries).toEqual([]);
    expect(failed.error).toBe("store unavailable");
    expect(inboxView(loaded({ entries: "nope" }))).toBe("failed");
    expect(inboxView(loaded(null))).toBe("failed");
  });

  it("keeps a refusal apart from a failure", () => {
    const refused = inboxReduce(initialInbox(), {
      type: "failure",
      status: 403,
      message: "forbidden",
    });
    expect(inboxView(refused)).toBe("refused");
  });

  it("keeps the requested limit across a failure so retry does not widen", () => {
    const widened = inboxReduce(initialInbox(), { type: "request", limit: 50 });
    const failed = inboxReduce(widened, { type: "failure", status: 502 });
    expect(inboxReduce(failed, { type: "request" }).requested).toBe(50);
    expect(inboxPath(failed.channel, failed.requested)).toBe(
      "/v1/notifications/inbox?channel=IN_APP&limit=50",
    );
  });

  it("drops another channel's rows when the channel changes", () => {
    const next = inboxReduce(loaded(page(2, 25)), {
      type: "request",
      channel: "EMAIL",
    });
    expect(next.entries).toEqual([]);
    expect(next.channel).toBe("EMAIL");
  });

  it("keeps rows on screen during a quiet re-read of the same page", () => {
    const quiet = inboxReduce(loaded(page(2, 25)), {
      type: "request",
      quiet: true,
    });
    expect(inboxView(quiet)).toBe("rows");
  });
});

describe("bounded page", () => {
  it("says a filled page is a bound and offers the next limit", () => {
    const bound = inboxBound(loaded(page(25, 25)));
    expect(bound.bounded).toBe(true);
    expect(bound.next).toBe(50);
    expect(bound.text).toContain("more may exist");
  });

  it("stops widening at the Hub's maximum and says so", () => {
    const bound = inboxBound(loaded(page(100, 100)));
    expect(bound).toMatchObject({ bounded: true, next: 0 });
    expect(bound.text).toContain("maximum of 100");
    expect(INBOX_LIMITS.at(-1)).toBe(100);
  });

  it("says nothing when the page did not fill its limit", () => {
    expect(inboxBound(loaded(page(24, 25)))).toEqual({
      bounded: false,
      next: 0,
      text: "",
    });
    // No limit reported means the bound is not known, not reached.
    expect(inboxBound(loaded({ entries: page(3, 3).entries })).bounded).toBe(
      false,
    );
  });
});

describe("channel", () => {
  it("falls back to IN_APP rather than forwarding one the Hub refuses", () => {
    expect(requestedChannel("email")).toBe("EMAIL");
    expect(requestedChannel("PAGER")).toBe("IN_APP");
    expect(requestedChannel(null)).toBe("IN_APP");
  });
});

describe("unread count", () => {
  const entries = page(4, 100).entries;

  it("counts entries newer than the seen mark", () => {
    expect(unreadSummary(entries, "2026-11-02T00:00:00Z", 100)).toEqual({
      count: 2,
      atLeast: false,
      label: "2",
    });
    expect(unreadSummary(entries, "2026-11-04T00:00:00Z", 100).label).toBe("");
    expect(unreadSummary(entries, "", 100).count).toBe(4);
  });

  it("says at least when every row of a filled page is new", () => {
    expect(unreadSummary(entries, "", 4)).toEqual({
      count: 4,
      atLeast: true,
      label: "4+",
    });
    expect(unreadSummary(entries, "2026-11-01T00:00:00Z", 4).atLeast).toBe(
      false,
    );
  });

  it("honours a real readAt and ignores Go's zero time", () => {
    const read = entry("r", "2026-11-09T00:00:00Z", {
      readAt: "2026-11-10T00:00:00Z",
    });
    expect(isUnread(read, "")).toBe(false);
    expect(isUnread(entry("u", "2026-11-09T00:00:00Z"), "")).toBe(true);
  });

  it("finds the newest event time on a page", () => {
    expect(newestOccurredAt(entries)).toBe("2026-11-04T00:00:00Z");
    expect(newestOccurredAt([])).toBe("");
  });

  it("does not show zero when the count is unknown", () => {
    expect(
      indicatorView({ status: "failed", count: 0, atLeast: false, label: "" }),
    ).toMatchObject({ label: "?" });
    expect(
      indicatorView({ status: "ready", count: 3, atLeast: true, label: "3+" })
        .title,
    ).toContain("3 or more new");
  });
});

describe("preference edit", () => {
  const stored = {
    id: "pref-1",
    owner: { kind: "USER", id: "riley" },
    resourceRid: "product/payments",
    channel: "IN_APP",
    types: ["ROLE_CHANGE", "FUTURE_TYPE_X", "PLAN_FAILURES"],
    allFutureTypes: false,
    revision: 3,
  };

  it("reports no change for an untouched draft", () => {
    expect(preferenceDiff(stored, initialPreferenceDraft(stored))).toEqual({
      added: [],
      removed: [],
      allFutureTypes: false,
      contactId: false,
      changed: false,
    });
  });

  it("names what a save would add and remove", () => {
    const draft = initialPreferenceDraft(stored);
    draft.types = ["PLAN_FAILURES", "FUTURE_TYPE_X", "RBAC_CHANGE"];
    draft.allFutureTypes = true;
    expect(preferenceDiff(stored, draft)).toMatchObject({
      added: ["RBAC_CHANGE"],
      removed: ["ROLE_CHANGE"],
      allFutureTypes: true,
      changed: true,
    });
  });

  it("treats a preference that does not exist yet as a change", () => {
    expect(preferenceDiff(null, initialPreferenceDraft(null)).changed).toBe(
      true,
    );
  });

  it("sends no owner and no revision, and carries unknown types through", () => {
    const draft = initialPreferenceDraft(stored);
    draft.types = [...draft.types, "RBAC_CHANGE"];
    const payload = preferencePayload(" product/payments ", "IN_APP", draft);
    expect(payload).toEqual({
      resourceRid: "product/payments",
      channel: "IN_APP",
      types: ["PLAN_FAILURES", "RBAC_CHANGE", "ROLE_CHANGE", "FUTURE_TYPE_X"],
      allFutureTypes: false,
    });
    expect(
      preferencePayload("product/payments", "EMAIL", {
        ...draft,
        contactId: "contact-1",
      }).contactId,
    ).toBe("contact-1");
  });

  it("refuses first what the Hub would refuse", () => {
    const empty = { types: [], allFutureTypes: false, contactId: "" };
    expect(preferenceProblem("product/payments", empty)).toContain(
      "can never deliver",
    );
    expect(preferenceProblem("", { ...empty, allFutureTypes: true })).toContain(
      "Name the resource",
    );
    expect(
      preferenceProblem("product/payments", { ...empty, allFutureTypes: true }),
    ).toBe("");
    const legacy = { ...empty, types: ["PLAN_FAILURES", "RBAC_CHANGE"] };
    expect(
      preferenceProblem("product/payments", legacy, ["RBAC_CHANGE"]),
    ).toContain("Remove the selected types");
    expect(
      preferenceProblem(
        "product/payments",
        { ...legacy, allFutureTypes: true },
        ["RBAC_CHANGE"],
      ),
    ).toBe("");
  });

  it("detects a revision that moved under the editor", () => {
    expect(staleRevision(stored, { ...stored, revision: 3 })).toBe("");
    expect(staleRevision(stored, { ...stored, revision: 4 })).toContain(
      "revision 3 is now 4",
    );
    expect(staleRevision(null, null)).toBe("");
    expect(staleRevision(null, stored)).toContain("nothing was saved");
    expect(staleRevision(stored, null)).toContain("nothing was saved");
  });
});

describe("contact edit", () => {
  it("trims the address and sends no owner", () => {
    expect(
      contactPayload({
        kind: "EMAIL",
        address: " riley@example.test ",
        optIntoFutureResources: true,
      }),
    ).toEqual({
      kind: "EMAIL",
      address: "riley@example.test",
      optIntoFutureResources: true,
    });
  });

  it("requires a known kind and an address", () => {
    expect(contactProblem({ kind: "FAX", address: "x" })).not.toBe("");
    expect(contactProblem({ kind: "SLACK", address: "  " })).not.toBe("");
    expect(contactProblem({ kind: "SLACK", address: "#ops" })).toBe("");
  });
});

describe("screen markup", () => {
  const model = () => {
    const next = initialNotifications();
    next.inbox = loaded(page(2, 25));
    return next;
  };

  it("renders each of the inbox screens as itself", () => {
    const rows = notificationsMarkup(model());
    expect(rows).toContain('data-inbox="rows"');
    expect(rows).toContain("plan e1 failed");
    expect(rows).toContain("data-inbox-complete");

    const empty = model();
    empty.inbox = loaded({ ...page(0, 25), entries: null });
    expect(notificationsMarkup(empty)).toContain('data-inbox="empty"');

    const failed = model();
    failed.inbox = inboxReduce(failed.inbox, {
      type: "failure",
      status: 500,
      message: "store unavailable",
    });
    const html = notificationsMarkup(failed);
    expect(html).toContain('data-inbox="failed"');
    expect(html).toContain("store unavailable");
    expect(html).not.toContain("No notifications");

    const loading = initialNotifications();
    expect(notificationsMarkup(loading)).toContain('data-inbox="loading"');
  });

  it("marks entries newer than the seen mark as new", () => {
    const html = notificationsMarkup(model(), {
      newSince: "2026-11-01T00:00:00Z",
    });
    expect(html.match(/data-inbox-new/g)).toHaveLength(1);
  });

  it("escapes what the Hub sent", () => {
    const hostile = model();
    hostile.inbox.entries[0].event.summary = '<img src=x onerror="x">';
    hostile.inbox.entries[0].event.type = "<b>T</b>";
    const html = notificationsMarkup(hostile);
    expect(html).not.toContain("<img");
    expect(html).not.toContain("<b>T</b>");
    expect(html).toContain("&lt;b&gt;T&lt;/b&gt;");
  });

  const editor = (
    overrides = {},
    decision: { allowed: boolean | null; reason: string } = {
      allowed: null,
      reason: "",
    },
  ) => {
    const next = model();
    next.view = "preferences";
    Object.assign(next.pref, {
      ridDraft: "product/payments",
      resourceRid: "product/payments",
      status: "ready",
      stored: { types: ["ROLE_CHANGE"], revision: 2 },
      draft: { types: ["ROLE_CHANGE"], allFutureTypes: false, contactId: "" },
      availabilityKnown: true,
      ...overrides,
    });
    return notificationsMarkup(next, { decision });
  };

  it("shows every known type and disables unavailable choices before selection", () => {
    const unavailable = ["BLUE_GREEN_UPGRADE_FAILED", "RBAC_CHANGE"];
    const html = editor({ notYetEmitted: unavailable });
    for (const type of EVENT_TYPES) {
      if (unavailable.includes(type)) {
        expect(html).toContain(`data-type-not-emitted="${type}"`);
        expect(html).not.toContain(`data-ntf-type="${type}"`);
        expect(html).not.toContain(`data-ntf-remove-type="${type}"`);
      } else expect(html).toContain(`data-ntf-type="${type}"`);
    }
    expect(html).toContain("cannot be selected individually");
    expect(html).not.toContain("answer appears after a save");
  });

  it("renders a type nothing raises as disabled with the reason", () => {
    const html = editor({ notYetEmitted: ["ROLE_CHANGE"] });
    expect(html).toContain('data-type-not-emitted="ROLE_CHANGE"');
    expect(html).toContain("Not yet emitted by this Hub");
    expect(html).toContain("checked disabled");
    // It is not also offered as a working checkbox.
    expect(html).not.toContain('data-ntf-type="ROLE_CHANGE"');
    expect(html).toContain('data-ntf-remove-type="ROLE_CHANGE"');
  });

  it("blocks unsupported metadata and legacy unavailable selections without hiding them", () => {
    const unknown = editor({ availabilityKnown: false });
    expect(unknown).toContain("did not report notification availability");
    expect(unknown).toMatch(/data-ntf-type="PLAN_FAILURES"[^>]*disabled/);
    expect(unknown).toMatch(/data-ntf-action="save-preference"[^>]*disabled/);
    const legacy = editor({
      notYetEmitted: ["ROLE_CHANGE"],
      draft: {
        types: ["PLAN_FAILURES", "ROLE_CHANGE"],
        allFutureTypes: false,
        contactId: "",
      },
    });
    expect(legacy).toContain("Remove the selected types");
    expect(legacy).toMatch(/data-ntf-action="save-preference"[^>]*disabled/);
    const repaired = editor({
      notYetEmitted: ["ROLE_CHANGE"],
      draft: { types: ["PLAN_FAILURES"], allFutureTypes: false, contactId: "" },
    });
    expect(repaired).not.toMatch(
      /data-ntf-action="save-preference"[^>]*disabled/,
    );
  });

  it("keeps forward consent available without presenting unavailable types as emitted", () => {
    const html = editor({
      notYetEmitted: ["RBAC_CHANGE"],
      draft: { types: [], allFutureTypes: true, contactId: "" },
    });
    expect(html).toMatch(
      /data-ntf-type="PLAN_FAILURES"[^>]*checked[^>]*disabled/,
    );
    expect(html).toContain('data-type-not-emitted="RBAC_CHANGE"');
    expect(html).toContain("cannot make an unavailable type deliver today");
    expect(html).not.toMatch(/data-ntf-action="save-preference"[^>]*disabled/);
  });

  it("disables Save with the reason when nothing is selected", () => {
    const html = editor({
      draft: { types: [], allFutureTypes: false, contactId: "" },
    });
    expect(html).toMatch(/data-ntf-action="save-preference"[^>]* disabled/);
    expect(html).toContain("can never deliver");
  });

  it("disables Save with the session's reason when the grant is missing", () => {
    const reason =
      "Your session does not hold resource:read on product/payments.";
    const html = editor(
      {
        draft: {
          types: ["PLAN_FAILURES"],
          allFutureTypes: false,
          contactId: "",
        },
      },
      { allowed: false, reason },
    );
    expect(html).toMatch(/data-ntf-action="save-preference"[^>]* disabled/);
    expect(html).toContain(reason);
  });

  it("enables Save for a valid, changed draft", () => {
    const html = editor({
      draft: { types: ["PLAN_FAILURES"], allFutureTypes: false, contactId: "" },
    });
    expect(html).not.toMatch(/data-ntf-action="save-preference"[^>]* disabled/);
    expect(html).toContain("Unsaved: + PLAN_FAILURES, - ROLE_CHANGE");
  });

  it("treats an absent preference as a state to edit, not an error", () => {
    const html = editor({ stored: null });
    expect(html).toContain("You are not subscribed to");
    expect(html).toContain("data-preference-editor");
  });

  it("says a contact with no transport is never delivered", () => {
    const next = model();
    next.view = "contacts";
    next.contacts.status = "ready";
    next.contacts.rows = [
      { id: "c1", kind: "PAGERDUTY", address: "svc", deliverable: false },
    ];
    expect(notificationsMarkup(next)).toContain("Stored, never delivered");
  });
});

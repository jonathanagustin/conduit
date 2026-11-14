import { describe, expect, it } from "vitest";

import {
  filterRequests,
  reviewMarkup,
  reviewState,
} from "../../console/operator/change-review.js";

const rows = [
  {
    id: "cr-1",
    title: "Roll ledger",
    description: "Upgrade ledger",
    environment: "east",
    targetKind: "installation",
    targetId: "inst-1",
    status: "PENDING_APPROVALS",
    author: { username: "alice" },
    changes: [{ id: "ch-1", type: "UPGRADE", installationId: "inst-1" }],
    evaluations: [{ changeId: "ch-1", policyId: "pol-1", outcome: "REVIEW", reason: "prod" }],
    reviews: [],
  },
  { id: "cr-2", title: "Done", status: "APPROVED", author: { username: "bob" } },
  { id: "cr-3", title: "No", status: "REJECTED", author: { username: "bob" } },
  { id: "cr-4", title: "Gone", status: "CANCELLED", author: { username: "bob" } },
];

const decideButtons = (html: string) =>
  [...html.matchAll(/<button[^>]*data-cr-decide="[^"]+"[^>]*>/g)].map((m) => m[0]);

describe("filterRequests", () => {
  it("filters by status and keeps all for 'all'", () => {
    expect(filterRequests(rows, "all")).toHaveLength(4);
    expect(filterRequests(rows, "pending").map((r) => r.id)).toEqual(["cr-1"]);
    expect(filterRequests(rows, "approved").map((r) => r.id)).toEqual(["cr-2"]);
    expect(filterRequests(rows, "rejected").map((r) => r.id)).toEqual(["cr-3"]);
  });
});

describe("reviewState", () => {
  it("blocks the author", () => {
    const s = reviewState(rows[0], "alice");
    expect(s.isAuthor).toBe(true);
    expect(s.canReview).toBe(false);
    expect(s.reason).toBe("You wrote this request. Another person must approve or reject it.");
  });
  it("allows another operator on a pending request", () => {
    expect(reviewState(rows[0], "carol")).toEqual({ isAuthor: false, canReview: true, reason: "" });
  });
  it("blocks a request that is not pending", () => {
    const s = reviewState(rows[1], "carol");
    expect(s.canReview).toBe(false);
    expect(s.reason).toBe("This request is APPROVED; it no longer takes a review.");
  });
});

describe("reviewMarkup", () => {
  it("disables both buttons for the author and shows the reason", () => {
    const html = reviewMarkup(rows, {
      operatorName: "alice",
      draft: { decision: "", rationale: "looks fine" },
    });
    const buttons = decideButtons(html);
    expect(buttons).toHaveLength(2);
    for (const b of buttons) expect(b).toContain("disabled");
    expect(html).toContain("You wrote this request. Another person must approve or reject it.");
    expect(html).toContain("Two-person rule: the author cannot approve their own request.");
  });

  it("enables buttons for another operator only with a rationale", () => {
    const empty = reviewMarkup(rows, {
      operatorName: "carol",
      draft: { decision: "", rationale: "   " },
    });
    for (const b of decideButtons(empty)) expect(b).toContain("disabled");
    const filled = reviewMarkup(rows, {
      operatorName: "carol",
      draft: { decision: "", rationale: "ok" },
    });
    const buttons = decideButtons(filled);
    expect(buttons).toHaveLength(2);
    for (const b of buttons) expect(b).not.toContain("disabled");
  });

  it("disables buttons while pending", () => {
    const html = reviewMarkup(rows, {
      operatorName: "carol",
      draft: { decision: "approve", rationale: "ok" },
      pending: true,
    });
    for (const b of decideButtons(html)) expect(b).toContain("disabled");
    expect(html).not.toContain("data-cr-confirm");
  });

  it("shows the confirm row", () => {
    const html = reviewMarkup(rows, {
      operatorName: "carol",
      draft: { decision: "approve", rationale: "ok" },
    });
    expect(html).toContain("Confirm approve of cr-1?");
    expect(html).toContain("data-cr-confirm");
    expect(html).toContain("data-cr-cancel");
  });

  it("escapes a malicious title", () => {
    const evil = [{ ...rows[0], title: "<img src=x onerror=alert(1)>" }];
    const html = reviewMarkup(evil, { operatorName: "carol" });
    expect(html).not.toContain("<img");
    expect(html).toContain("&lt;img");
  });

  it("never renders an enabled decide button for the author", () => {
    for (const draft of [
      { decision: "", rationale: "" },
      { decision: "approve", rationale: "self" },
    ]) {
      const html = reviewMarkup(rows, { operatorName: "alice", draft });
      for (const b of decideButtons(html)) expect(b).toContain("disabled");
    }
  });

  it("renders empty states and the error card", () => {
    expect(reviewMarkup([], { operatorName: "carol" })).toContain(
      "No change request is recorded.",
    );
    expect(reviewMarkup(rows, { filter: "pending", operatorName: "carol" })).not.toContain(
      "No request matches.",
    );
    expect(reviewMarkup([rows[1]], { filter: "pending", operatorName: "carol" })).toContain(
      "No request matches.",
    );
    expect(reviewMarkup(rows, { operatorName: "carol", error: "boom" })).toContain(
      "kit-card kit-bar-err",
    );
  });
});

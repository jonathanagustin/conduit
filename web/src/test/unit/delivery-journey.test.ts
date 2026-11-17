import { describe, expect, it } from "vite-plus/test";

import {
  bindDeliveryCapabilities,
  buildPlanDecision,
  actionTransition,
  idleAction,
  planReviewState,
} from "../../console/operator/delivery-actions.js";
import {
  actionChromeMarkup,
  decisionMarkup,
  promotionMarkup,
  proposalMarkup,
  requestText,
} from "../../console/operator/delivery-journey.js";

const digest = `sha256:${"a".repeat(64)}`;
const release = (version: string) => ({
  product: "app.example:api",
  version,
  chart: { repository: "r/api", tag: version, digest },
  images: [],
});
const hub = () => ({
  plans: [
    {
      id: "pln_1",
      status: "SUCCEEDED",
      createdAt: "2026-01-01T00:00:00Z",
      plan: { environment: "dev" },
      desiredState: {
        apiVersion: "platform.delivery/v1alpha1",
        kind: "EnvironmentDeployment",
        environment: { id: "dev", generation: "gen-1" },
        releases: [release("0.1.0")],
        entities: [
          { name: "api", product: "app.example:api", version: "0.1.0" },
        ],
      },
    },
  ],
  products: [
    {
      product: "app.example:api",
      releases: [release("0.1.0"), release("0.2.0")],
    },
  ],
  installations: [],
  promotions: [],
});
const plan = {
  id: "pln_9",
  status: "AWAITING_APPROVAL",
  changeRequestId: "crq_9",
  plan: { environment: "dev" },
};
const changes = [
  { id: "crq_9", status: "PENDING_APPROVALS", author: { username: "alice" } },
];
const buttons = (html: string, attribute: string) =>
  [...html.matchAll(new RegExp(`<button[^>]*${attribute}[^>]*>`, "g"))].map(
    (match) => match[0],
  );

describe("confirmation step", () => {
  const request = buildPlanDecision(plan, "approve", "looks <b>right</b>");
  const confirming = actionTransition(idleAction(), {
    type: "review",
    request,
  });

  it("summarises exactly what will be sent, escaped", () => {
    const html = actionChromeMarkup(confirming);
    expect(html).toContain("data-dj-confirm-step");
    expect(html).toContain("Nothing has been sent");
    expect(html).toContain(
      "POST /conduit.plans.v1.PlanApprovalService/ApprovePlan",
    );
    expect(html).toContain("looks &lt;b&gt;right&lt;/b&gt;");
    expect(html).not.toContain("<b>right</b>");
    expect(requestText(request)).toBe(
      'POST /conduit.plans.v1.PlanApprovalService/ApprovePlan\n\n{\n  "planId": "pln_9",\n  "rationale": "looks <b>right</b>"\n}',
    );
  });

  it("shows no confirm button once the request is in flight", () => {
    const inflight = actionTransition(confirming, { type: "confirm" });
    const html = actionChromeMarkup(inflight);
    expect(html).toContain("data-dj-inflight");
    expect(html).toContain("nothing is recorded as done yet");
    expect(buttons(html, "data-dj-confirm")).toEqual([]);
  });

  it("shows the Hub's refusal inline, escaped", () => {
    const failed = actionTransition(
      actionTransition(confirming, { type: "confirm" }),
      { type: "rejected", error: 'no enabled approval policies <img src="x">' },
    );
    const html = actionChromeMarkup(failed);
    expect(html).toContain("data-dj-error");
    expect(html).toContain("no enabled approval policies &lt;img");
    expect(html).not.toContain("<img");
  });
});

describe("proposalMarkup", () => {
  it("limits the target release to the Hub's catalog and needs a reason", () => {
    const html = proposalMarkup(hub(), "dev", {
      action: idleAction(),
      draft: {},
    });
    const versions = html.match(
      /data-dj-field="version"[^>]*>([\s\S]*?)<\/select>/,
    )?.[1];
    expect(versions).toContain('value="0.2.0" selected');
    expect(versions).toContain("0.1.0 (current)");
    expect((versions?.match(/<option/g) || []).length).toBe(2);
    expect(buttons(html, "data-dj-review")[0]).toContain("disabled");
  });

  it("enables review only with a generation and a reason", () => {
    const html = proposalMarkup(hub(), "dev", {
      action: idleAction(),
      draft: { generation: "gen-2", rationale: "roll" },
    });
    expect(buttons(html, "data-dj-review")[0]).not.toContain("disabled");
  });

  it("locks the form while a request is in flight", () => {
    const inflight = {
      phase: "inflight",
      request: { method: "POST", path: "/v1/plans", headers: {}, body: {} },
      error: "",
      result: null,
    };
    const html = proposalMarkup(hub(), "dev", {
      action: inflight,
      draft: { generation: "gen-2", rationale: "roll" },
    });
    expect(buttons(html, "data-dj-review")).toEqual([]);
    expect(html.match(/<(select|input|textarea)[^>]*>/g)).toSatisfy(
      (fields: string[]) => fields.every((field) => field.includes("disabled")),
    );
  });

  it("paints a refused proposal as refused, from the change request", () => {
    const html = proposalMarkup(hub(), "dev", {
      action: {
        phase: "success",
        request: null,
        error: "",
        result: {
          plan: { id: "pln_2", status: "AWAITING_APPROVAL" },
          changeRequest: {
            id: "crq_2",
            status: "ERROR",
            error: "no enabled approval policies",
          },
        },
      },
      draft: {},
    });
    expect(html).toContain('data-dj-outcome="err"');
    expect(html).toContain("no enabled approval policies");
  });

  it("says why there is nothing to propose from", () => {
    expect(
      proposalMarkup({ plans: [] }, "dev", { action: idleAction(), draft: {} }),
    ).toContain("data-dj-unavailable");
  });
});

describe("decisionMarkup", () => {
  const panel = { action: idleAction(), draft: { rationale: "reviewed" } };

  it("disables Approve and Reject for the author, with the reason", () => {
    const html = decisionMarkup(
      plan,
      planReviewState(plan, changes, "alice", ["alice"]),
      panel,
    );
    expect(html).toContain("Two-person rule");
    expect(html).toContain("Another person must approve or reject it.");
    const shown = buttons(html, "so-btn-(primary|danger)");
    expect(shown).toHaveLength(2);
    expect(shown.every((button) => button.includes("disabled"))).toBe(true);
    expect(buttons(html, "data-dj-review")).toEqual([]);
  });

  it("offers a second person both decisions once a reason is given", () => {
    const review = planReviewState(plan, changes, "bob", ["bob"]);
    const ready = buttons(
      decisionMarkup(plan, review, panel),
      "data-dj-review",
    );
    expect(ready).toHaveLength(2);
    expect(ready.some((button) => button.includes("disabled"))).toBe(false);
    const empty = buttons(
      decisionMarkup(plan, review, { action: idleAction(), draft: {} }),
      "data-dj-review",
    );
    expect(empty.every((button) => button.includes("disabled"))).toBe(true);
  });

  it("reports the status the Hub returned, not an assumed one", () => {
    const html = decisionMarkup(
      plan,
      planReviewState(plan, changes, "bob", ["bob"]),
      {
        action: {
          phase: "success",
          request: buildPlanDecision(plan, "approve", "ok"),
          error: "",
          result: { id: "pln_9", status: "APPROVED" },
        },
        draft: {},
      },
    );
    expect(html).toContain("The Hub recorded the approve.");
    expect(html).toContain("pln_9 is now APPROVED.");
  });
});

describe("promotionMarkup", () => {
  it("paints a BLOCKED evaluation as not promoted, with its blockers", () => {
    const html = promotionMarkup(hub(), {
      action: {
        phase: "success",
        request: null,
        error: "",
        result: {
          id: "pro_1",
          product: "app.example:api",
          version: "0.2.0",
          fromChannel: "DEV",
          toChannel: "STAGING",
          status: "BLOCKED",
          lifecycle: "RUNNING",
          blockers: [
            {
              kind: "HEALTHY_ENTITIES_BELOW_MINIMUM",
              message: "healthy entity count 0 is below required 1",
            },
          ],
        },
      },
      draft: {},
    });
    expect(html).toContain('data-dj-outcome="err"');
    expect(html).toContain("BLOCKED:");
    expect(html).toContain("was not promoted");
    expect(html).toContain("HEALTHY_ENTITIES_BELOW_MINIMUM");
    expect(html).toContain("healthy entity count 0 is below required 1");
  });

  it("does not offer an evaluation without a policy the Hub holds", () => {
    const html = promotionMarkup(hub(), { action: idleAction(), draft: {} });
    expect(html).toContain("data-dj-unavailable");
    expect(buttons(html, "data-dj-review")).toEqual([]);
  });

  it("offers a recorded stage and says what evidence will be sent", () => {
    const state = hub();
    const html = promotionMarkup(
      {
        ...state,
        promotions: [
          {
            id: "pro_0",
            product: "app.example:api",
            fromChannel: "DEV",
            toChannel: "STAGING",
            policy: { minimumHealthyEntities: 1 },
          },
        ],
      },
      { action: idleAction(), draft: {} },
    );
    expect(buttons(html, "data-dj-review")).toHaveLength(1);
    expect(html).toContain("no health evidence will be submitted");
  });
});

it("uses operate for CR-less decisions without imposing it on governed reviewers", () => {
  bindDeliveryCapabilities((operation) =>
    operation === "resource:operate"
      ? { allowed: false, reason: "Operate is not held." }
      : { allowed: null, reason: "" },
  );
  try {
    const standalone = { ...plan, changeRequestId: "" };
    const panel = { action: idleAction(), draft: { rationale: "reviewed" } };
    const html = decisionMarkup(
      standalone,
      planReviewState(standalone, [], "bob", ["bob"]),
      panel,
    );
    expect(buttons(html, "data-dj-review")).toHaveLength(2);
    expect(
      buttons(html, "data-dj-review").every((button) =>
        button.includes("disabled"),
      ),
    ).toBe(true);
    expect(html).toContain("Operate is not held.");
    expect(
      buildPlanDecision(standalone, "approve", "reviewed").capability.operation,
    ).toBe("resource:operate");
    const governed = decisionMarkup(
      plan,
      planReviewState(plan, changes, "bob", ["bob"]),
      panel,
    );
    expect(
      buttons(governed, "data-dj-review").some((button) =>
        button.includes("disabled"),
      ),
    ).toBe(false);
  } finally {
    bindDeliveryCapabilities(null);
  }
});

describe("approval outcome chrome", () => {
  it("renders a recorded approval receipt without pretending it is a full Plan", () => {
    const html = decisionMarkup(
      null,
      { canReview: false },
      {
        action: {
          phase: "success",
          request: buildPlanDecision({ id: "pln_1" }, "approve", "reviewed"),
          result: {
            kind: "conduit.plan-approval/v1",
            plan: { id: "pln_1", status: "AWAITING_APPROVAL" },
          },
        },
      },
    );
    expect(html).toContain("pln_1 is now AWAITING_APPROVAL.");
    expect(html).not.toContain("status unavailable");
  });
  it("does not call an uncertain or recorded outcome refused and offers only a reload", () => {
    const html = actionChromeMarkup({
      phase: "uncertain",
      error: "Approval was recorded <img>.",
      outcome: { approvalRecorded: true },
    });
    expect(html).toContain('data-dj-approval-recorded="true"');
    expect(html).toContain("Approval was recorded &lt;img&gt;");
    expect(html).toContain("Reload Plan state");
    expect(html).not.toContain("did not accept");
    expect(html).not.toContain("data-dj-confirm");
  });
});

import { describe, expect, it, vi } from "vite-plus/test";

import {
  actionTransition,
  buildPlanDecision,
  buildPlanProposal,
  buildPromotionEvaluation,
  catalogReleases,
  defaultProposalBase,
  deliveryCapabilityHook,
  hubReason,
  idleAction,
  planReviewState,
  promotionEvidence,
  promotionOutcome,
  promotionStages,
  proposalOutcome,
  readValidationDetail,
  sendAction,
  validationDetail,
} from "../../console/operator/delivery-actions.js";

const digest = `sha256:${"a".repeat(64)}`;
const release = (version: string) => ({
  product: "app.example:api",
  version,
  chart: { repository: "registry.example/api", tag: version, digest },
  images: [{ repository: "registry.example/api", tag: version, digest }],
});
const desiredState = () => ({
  apiVersion: "platform.delivery/v1alpha1",
  kind: "EnvironmentDeployment",
  environment: { id: "dev", generation: "gen-1" },
  releases: [release("0.1.0")],
  entities: [
    {
      name: "api",
      product: "app.example:api",
      version: "0.1.0",
      namespace: "example",
    },
  ],
});
const plans = () => [
  {
    id: "pln_old",
    status: "SUCCEEDED",
    createdAt: "2026-01-01T00:00:00Z",
    plan: { environment: "dev" },
    desiredState: desiredState(),
  },
  {
    id: "pln_cancelled",
    status: "CANCELLED",
    createdAt: "2026-03-01T00:00:00Z",
    plan: { environment: "dev" },
    desiredState: {
      ...desiredState(),
      environment: { id: "dev", generation: "gen-x" },
    },
  },
  {
    id: "pln_other",
    status: "SUCCEEDED",
    createdAt: "2026-04-01T00:00:00Z",
    plan: { environment: "prod" },
    desiredState: {
      ...desiredState(),
      environment: { id: "prod", generation: "gen-9" },
    },
  },
];
const products = () => [
  {
    product: "app.example:api",
    releases: [release("0.1.0"), release("0.2.0")],
  },
];
const proposal = (overrides: Record<string, unknown> = {}) =>
  buildPlanProposal({
    plans: plans(),
    products: products(),
    environmentId: "dev",
    entityName: "api",
    version: "0.2.0",
    generation: "gen-2",
    rationale: "roll api forward",
    ...overrides,
  });

describe("buildPlanProposal", () => {
  it("posts the next DesiredState document and nothing else", () => {
    const request = proposal();
    expect(request.method).toBe("POST");
    expect(request.path).toBe("/v1/plans");
    // decodePlanProposal reads title and description from these two headers.
    expect(request.headers).toEqual({
      "X-Change-Title": "Set api to 0.2.0 in dev",
      "X-Change-Description": "roll api forward",
    });
    // The body is the DesiredState itself: the Hub refuses unknown fields.
    expect(request.body).toEqual({
      apiVersion: "platform.delivery/v1alpha1",
      kind: "EnvironmentDeployment",
      environment: { id: "dev", generation: "gen-2" },
      releases: [release("0.1.0"), release("0.2.0")],
      entities: [
        {
          name: "api",
          product: "app.example:api",
          version: "0.2.0",
          namespace: "example",
        },
      ],
    });
    expect(request.capability).toEqual({
      operation: "change:propose",
      resourceRid: "environment/dev",
    });
  });

  it("edits a copy: the loaded plan record is left as the Hub returned it", () => {
    const loaded = plans();
    buildPlanProposal({
      plans: loaded,
      products: products(),
      environmentId: "dev",
      entityName: "api",
      version: "0.2.0",
      generation: "gen-2",
      rationale: "roll",
    });
    expect(loaded[0].desiredState).toEqual(desiredState());
  });

  it("bases on the newest plan of that environment that was not cancelled or failed", () => {
    expect(defaultProposalBase(plans(), "dev").id).toBe("pln_old");
    expect(
      proposal().summary.find(
        ([label]: [string, string]) => label === "Based on plan",
      )?.[1],
    ).toBe("pln_old (SUCCEEDED)");
  });

  it("does not add a release the desired state already carries", () => {
    const request = proposal({ version: "0.1.0" });
    expect(request.body.releases).toEqual([release("0.1.0")]);
  });

  it("offers only releases the Hub's catalog lists", () => {
    expect(catalogReleases(products(), "app.example:api")).toHaveLength(2);
    expect(proposal({ version: "9.9.9" }).error).toMatch(/Hub's catalog/);
    expect(proposal({ products: [] }).error).toMatch(/Hub's catalog/);
  });

  it("refuses before the confirm step when the request cannot be valid", () => {
    expect(proposal({ generation: " " }).error).toMatch(/generation/i);
    expect(proposal({ generation: "gen-1" }).error).toMatch(/must differ/);
    expect(proposal({ rationale: "" }).error).toMatch(/reason/i);
    expect(proposal({ entityName: "missing" }).error).toMatch(/installation/i);
    expect(proposal({ environmentId: "empty" }).error).toMatch(
      /No plan with a desired state/,
    );
    // A header value outside printable ASCII would throw inside fetch.
    expect(proposal({ rationale: "naïve" }).error).toMatch(/ASCII/);
  });
});

describe("validation detail for a refused proposal", () => {
  it("joins the validator's errors as path: message", () => {
    expect(
      validationDetail({
        valid: false,
        errors: [
          {
            path: "entities[0]",
            message: "requires app.example:api <= 0.1.999",
          },
          { path: "", message: "second rule" },
        ],
      }),
    ).toBe("entities[0]: requires app.example:api <= 0.1.999; second rule");
    expect(validationDetail({ valid: true, errors: null })).toBe("");
  });

  it("asks POST /v1/validate with the refused document, and survives no answer", async () => {
    const fetchImpl = vi.fn(
      async (_path: string, _init: RequestInit) =>
        new Response(
          JSON.stringify({
            valid: false,
            errors: [{ path: "a", message: "b" }],
          }),
          { status: 422 },
        ),
    );
    const document = { kind: "EnvironmentDeployment" };
    expect(
      await readValidationDetail(
        document,
        fetchImpl as unknown as typeof fetch,
      ),
    ).toBe("a: b");
    expect(fetchImpl.mock.calls[0][0]).toBe("/v1/validate");
    expect(fetchImpl.mock.calls[0][1].method).toBe("POST");
    expect(fetchImpl.mock.calls[0][1].body).toBe(JSON.stringify(document));
    expect(
      await readValidationDetail(document, (async () => {
        throw new Error("offline");
      }) as unknown as typeof fetch),
    ).toBe("");
  });
});

describe("proposalOutcome", () => {
  it("surfaces the Hub's refusal carried by the change request", () => {
    const outcome = proposalOutcome(
      { id: "pln_1", status: "AWAITING_APPROVAL", changeRequestId: "crq_1" },
      {
        id: "crq_1",
        status: "ERROR",
        error:
          "all enabled policies returned no_op for change chg_1; no enabled approval policies",
      },
    );
    expect(outcome.tone).toBe("err");
    expect(outcome.lines.join(" ")).toContain("no enabled approval policies");
  });

  it("does not call a pending review an approval", () => {
    const outcome = proposalOutcome(
      { id: "pln_1", status: "AWAITING_APPROVAL" },
      { id: "crq_1", status: "PENDING_APPROVALS" },
    );
    expect(outcome.tone).toBe("ok");
    expect(outcome.lines.join(" ")).toContain("PENDING_APPROVALS");
    expect(outcome.lines.join(" ")).not.toMatch(/approved|deployed/i);
  });

  it("says so when the change request could not be read", () => {
    expect(
      proposalOutcome({ id: "pln_1", changeRequestId: "crq_1" }, null).tone,
    ).toBe("warn");
  });
});

describe("planReviewState", () => {
  const plan = {
    id: "pln_1",
    status: "AWAITING_APPROVAL",
    changeRequestId: "crq_1",
    plan: { environment: "dev" },
  };
  const changes = [
    { id: "crq_1", status: "PENDING_APPROVALS", author: { username: "alice" } },
  ];

  it("does not offer the author an approval of their own change", () => {
    const state = planReviewState(plan, changes, "alice", ["alice"]);
    expect(state.canReview).toBe(false);
    expect(state.isAuthor).toBe(true);
    expect(state.reason).toMatch(/Another person must approve/);
  });

  it("treats a delegated identity as the author too", () => {
    expect(
      planReviewState(plan, changes, "svc", ["svc", "alice"]).canReview,
    ).toBe(false);
  });

  it("offers a second person the decision", () => {
    expect(planReviewState(plan, changes, "bob", ["bob"])).toMatchObject({
      canReview: true,
      isAuthor: false,
    });
  });

  it("disables review when the author cannot be established", () => {
    expect(planReviewState(plan, [], "bob", ["bob"]).reason).toMatch(
      /author is unknown/,
    );
    expect(planReviewState(plan, changes, "", []).canReview).toBe(false);
  });

  it("does not offer a decision on a plan that is not awaiting approval", () => {
    const state = planReviewState(
      { ...plan, status: "APPROVED" },
      changes,
      "bob",
      ["bob"],
    );
    expect(state.canReview).toBe(false);
    expect(state.reason).toContain("APPROVED");
  });

  it("explains the ungoverned branch instead of inventing an author", () => {
    const state = planReviewState(
      { id: "pln_2", status: "AWAITING_APPROVAL" },
      changes,
      "bob",
      ["bob"],
    );
    expect(state.canReview).toBe(true);
    expect(state.note).toMatch(/Operate/);
  });
});

describe("buildPlanDecision", () => {
  const governed = {
    id: "pln/1",
    changeRequestId: "crq_1",
    plan: { environment: "dev" },
  };

  it("approves through the generated procedure with the exact Plan identity and rationale", () => {
    const request = buildPlanDecision(governed, "approve", " reviewed ");
    expect(request.method).toBe("POST");
    expect(request.path).toBe(
      "/conduit.plans.v1.PlanApprovalService/ApprovePlan",
    );
    expect(request.transport).toBe("connect-plan-approval");
    expect(request.body).toEqual({ planId: "pln/1", rationale: "reviewed" });
  });

  it("rejects a governed plan through its change request", () => {
    const request = buildPlanDecision(governed, "reject", "wrong version");
    expect(request.path).toBe("/v1/change-requests/crq_1/reject");
    expect(request.body).toEqual({ rationale: "wrong version" });
  });

  it("rejects an ungoverned plan by cancelling it, with `reason`", () => {
    const request = buildPlanDecision(
      { id: "pln_2", plan: { environment: "dev" } },
      "reject",
      "not wanted",
    );
    expect(request.path).toBe("/v1/plans/pln_2/cancel");
    expect(request.body).toEqual({ reason: "not wanted" });
  });

  it("needs a reason and a known decision", () => {
    expect(buildPlanDecision(governed, "approve", " ").error).toMatch(/reason/);
    expect(buildPlanDecision(governed, "merge", "x").error).toMatch(/Unknown/);
    expect(buildPlanDecision(null, "approve", "x").error).toMatch(/No plan/);
  });
});

describe("promotion evaluation", () => {
  const policy = {
    minimumHealthyEntities: 1,
    maximumUnhealthyEntities: 0,
    minimumHealthyDurationSeconds: 0,
  };
  const stage = {
    key: "k",
    product: "app.example:api",
    fromChannel: "DEV",
    toChannel: "STAGING",
    policy,
    source: "declared in plan pln_1",
  };
  const installations = [
    {
      environment: "dev",
      installationId: "api",
      product: "app.example:api",
      observedVersion: "0.2.0",
      health: "HEALTHY",
      observedAt: "2026-05-01T00:00:00Z",
    },
    {
      environment: "dev",
      installationId: "api-old",
      product: "app.example:api",
      observedVersion: "0.1.0",
      health: "HEALTHY",
      observedAt: "2026-05-01T00:00:00Z",
    },
    {
      environment: "dev",
      installationId: "web",
      product: "app.example:web",
      observedVersion: "0.2.0",
      health: "HEALTHY",
    },
  ];

  it("sends release, channels, the Hub's policy, and matching evidence", () => {
    const request = buildPromotionEvaluation({
      stage,
      products: products(),
      version: "0.2.0",
      installations,
    });
    expect(request.method).toBe("POST");
    expect(request.path).toBe("/v1/promotions/evaluations");
    // PromotionEvaluationRequest: exactly these fields.
    expect(request.body).toEqual({
      release: release("0.2.0"),
      fromChannel: "DEV",
      toChannel: "STAGING",
      policy,
      evidence: [
        {
          environment: "dev",
          installationId: "api",
          health: "HEALTHY",
          observedAt: "2026-05-01T00:00:00Z",
        },
      ],
    });
  });

  it("submits no evidence rather than evidence about another version", () => {
    expect(promotionEvidence(installations, release("0.3.0"))).toEqual([]);
  });

  it("only evaluates a catalogued release across a stage with a policy", () => {
    expect(
      buildPromotionEvaluation({ stage, products: products(), version: "7" })
        .error,
    ).toMatch(/catalog/);
    expect(
      buildPromotionEvaluation({ products: products(), version: "0.2.0" })
        .error,
    ).toMatch(/stage/);
  });

  it("lists stages from declared pipelines and recorded evaluations, once each", () => {
    const stages = promotionStages(
      [
        {
          id: "pln_1",
          createdAt: "2026-01-01T00:00:00Z",
          desiredState: {
            productSettings: [
              {
                product: "app.example:api",
                promotionPipeline: {
                  enabled: true,
                  stages: [
                    { fromChannel: "DEV", toChannel: "STAGING", policy },
                  ],
                },
              },
            ],
          },
        },
      ],
      [
        {
          id: "pro_1",
          product: "app.example:api",
          fromChannel: "DEV",
          toChannel: "STAGING",
          policy: { ...policy, minimumHealthyEntities: 9 },
        },
        {
          id: "pro_2",
          product: "app.example:api",
          fromChannel: "STAGING",
          toChannel: "STABLE",
          policy,
        },
      ],
    );
    expect(stages.map((row) => [row.toChannel, row.source])).toEqual([
      ["STAGING", "declared in plan pln_1"],
      ["STABLE", "policy of recorded evaluation pro_2"],
    ]);
    expect(stages[0].policy.minimumHealthyEntities).toBe(1);
  });
});

describe("promotionOutcome", () => {
  const record = {
    id: "pro_1",
    product: "app.example:api",
    version: "0.2.0",
    fromChannel: "DEV",
    toChannel: "STAGING",
  };

  it("states BLOCKED with every reason the Hub gave", () => {
    const outcome = promotionOutcome({
      ...record,
      status: "BLOCKED",
      lifecycle: "RUNNING",
      blockers: [
        {
          kind: "HEALTHY_ENTITIES_BELOW_MINIMUM",
          message: "healthy entity count 0 is below required 1",
        },
      ],
    });
    expect(outcome.tone).toBe("err");
    expect(outcome.headline).toMatch(/^BLOCKED: .* was not promoted$/);
    expect(outcome.reasons).toEqual([
      {
        kind: "HEALTHY_ENTITIES_BELOW_MINIMUM",
        message: "healthy entity count 0 is below required 1",
      },
    ]);
  });

  it("states ABORTED and its reason, not a generic block", () => {
    const outcome = promotionOutcome({
      ...record,
      status: "BLOCKED",
      lifecycle: "ABORTED",
      lifecycleReason: "evaluation timed out",
      reasons: ["evaluation timed out"],
    });
    expect(outcome.headline).toMatch(/^ABORTED: /);
    expect(outcome.detail).toContain("evaluation timed out");
    expect(outcome.reasons).toEqual([
      { kind: "", message: "evaluation timed out" },
    ]);
  });

  it("only calls PASSED a promotion", () => {
    expect(promotionOutcome({ ...record, status: "PASSED" }).tone).toBe("ok");
    expect(promotionOutcome({ ...record }).tone).toBe("err");
    expect(promotionOutcome({ ...record }).headline).toMatch(/^UNKNOWN: /);
  });
});

describe("actionTransition", () => {
  const request = { method: "POST", path: "/v1/plans", headers: {}, body: {} };

  it("walks confirm -> in-flight -> success only on the Hub's answer", () => {
    let action = actionTransition(idleAction(), { type: "review", request });
    expect(action.phase).toBe("confirm");
    action = actionTransition(action, { type: "confirm" });
    expect(action.phase).toBe("inflight");
    expect(action.result).toBeNull();
    action = actionTransition(action, { type: "resolved", result: { id: 1 } });
    expect(action).toMatchObject({ phase: "success", result: { id: 1 } });
  });

  it("walks confirm -> in-flight -> failure with the Hub's reason", () => {
    let action = actionTransition(idleAction(), { type: "review", request });
    action = actionTransition(action, { type: "confirm" });
    action = actionTransition(action, {
      type: "rejected",
      error: "no enabled approval policies",
    });
    expect(action).toMatchObject({
      phase: "failure",
      error: "no enabled approval policies",
      request,
    });
  });

  it("never sends without the confirm step", () => {
    expect(actionTransition(idleAction(), { type: "confirm" }).phase).toBe(
      "idle",
    );
  });

  it("never reports done before the Hub answers", () => {
    const confirming = actionTransition(idleAction(), {
      type: "review",
      request,
    });
    expect(
      actionTransition(confirming, { type: "resolved", result: {} }).phase,
    ).toBe("confirm");
    expect(
      actionTransition(idleAction(), { type: "resolved", result: {} }).phase,
    ).toBe("idle");
  });

  it("holds an in-flight request against cancel, reset and a second review", () => {
    let action = actionTransition(idleAction(), { type: "review", request });
    action = actionTransition(action, { type: "confirm" });
    for (const type of ["cancel", "reset", "confirm"])
      expect(actionTransition(action, { type }).phase).toBe("inflight");
    expect(actionTransition(action, { type: "review", request }).phase).toBe(
      "inflight",
    );
  });

  it("returns an unbuildable request to the form with its reason", () => {
    const action = actionTransition(idleAction(), {
      type: "review",
      request: { error: "Enter a reason first." },
    });
    expect(action).toMatchObject({
      phase: "idle",
      error: "Enter a reason first.",
    });
  });

  it("cancels a confirmation back to the form", () => {
    const action = actionTransition(
      actionTransition(idleAction(), { type: "review", request }),
      { type: "cancel" },
    );
    expect(action).toEqual(idleAction());
  });
});

describe("sendAction", () => {
  const request = {
    method: "POST",
    path: "/v1/change-requests/crq_1/reject",
    headers: {},
    body: { rationale: "ok" },
  };

  it("posts same-origin JSON with no Authorization header", async () => {
    const fetchImpl = vi.fn(
      async () =>
        new Response(JSON.stringify({ id: "pln_1" }), { status: 200 }),
    );
    const answer = await sendAction(request, fetchImpl);
    expect(answer.body).toEqual({ id: "pln_1" });
    const [path, init] = fetchImpl.mock.calls[0] as unknown as [
      string,
      RequestInit,
    ];
    expect(path).toBe("/v1/change-requests/crq_1/reject");
    expect(init.method).toBe("POST");
    expect(init.credentials).toBe("same-origin");
    expect(init.body).toBe('{"rationale":"ok"}');
    expect(init.headers).toEqual({
      "content-type": "application/json",
      accept: "application/json",
    });
  });

  it("throws the Hub's own reason on a refusal", async () => {
    const fetchImpl = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            error:
              "forbidden: the author of a change request cannot approve it",
            status: "403",
          }),
          { status: 403 },
        ),
    );
    await expect(sendAction(request, fetchImpl)).rejects.toMatchObject({
      message: "forbidden: the author of a change request cannot approve it",
      status: 403,
    });
  });

  it("falls back to the status when the Hub gave no readable reason", async () => {
    const fetchImpl = vi.fn(async () => new Response("nope", { status: 502 }));
    await expect(sendAction(request, fetchImpl)).rejects.toThrow(
      "the Hub returned 502",
    );
    expect(hubReason(409, { error: " conflict " })).toBe("conflict");
  });
});

describe("deliveryCapabilityHook", () => {
  it("does not decide until the session capabilities are bound", () => {
    expect(deliveryCapabilityHook("change:propose", "environment/dev")).toEqual(
      { allowed: null, reason: "" },
    );
  });
});

describe("active operator generated Plan approval", () => {
  const receipt = () => ({
    plan: {
      id: "pln/1",
      environment: "dev",
      status: "AWAITING_APPROVAL",
      stepCount: "1",
      approvalCount: "1",
      requiredApprovals: "2",
      revision: "18446744073709551615",
      createdAt: "2026-11-17T00:00:00.123456789Z",
      updatedAt: "2026-11-17T00:00:01Z",
    },
    approvals: [
      {
        subject: "reviewer",
        createdAt: "2026-11-17T00:00:01Z",
        rationale: "reviewed",
      },
    ],
    changeRequestId: "crq_1",
  });
  const command = () =>
    buildPlanDecision(
      { id: "pln/1", plan: { environment: "dev" } },
      "approve",
      "reviewed",
    );
  it("sends one exact generated command and keeps the summary distinct from a Plan", async () => {
    vi.stubGlobal("window", { location: { origin: "https://hub.example" } });
    const fetchImpl = vi.fn(async (input: RequestInfo | URL) => {
      const request = input as Request;
      expect(request.url).toBe(
        "https://hub.example/conduit.plans.v1.PlanApprovalService/ApprovePlan",
      );
      expect(request.method).toBe("POST");
      expect(request.credentials).toBe("same-origin");
      expect(request.redirect).toBe("error");
      expect(await request.json()).toEqual({
        planId: "pln/1",
        rationale: "reviewed",
      });
      return new Response(JSON.stringify(receipt()), {
        headers: { "Content-Type": "application/json" },
      });
    });
    try {
      const answer = await sendAction(command(), fetchImpl);
      expect(answer.body).toMatchObject({
        kind: "conduit.plan-approval/v1",
        plan: {
          id: "pln/1",
          status: "AWAITING_APPROVAL",
          revision: "18446744073709551615",
          createdAt: "2026-11-17T00:00:00.123456789Z",
        },
      });
      expect(answer.body).not.toHaveProperty("id");
      expect(answer.body).not.toHaveProperty("desiredState");
      expect(fetchImpl).toHaveBeenCalledTimes(1);
    } finally {
      vi.unstubAllGlobals();
    }
  });
  it.each(["lost-response", "foreign-receipt"])(
    "never replays through REST after %s",
    async (failure) => {
      vi.stubGlobal("window", { location: { origin: "https://hub.example" } });
      const fetchImpl = vi.fn(async () => {
        if (failure === "lost-response") throw new TypeError("connection lost");
        const foreign = receipt();
        foreign.plan.id = "another-plan";
        return new Response(JSON.stringify(foreign), {
          headers: { "Content-Type": "application/json" },
        });
      });
      try {
        await expect(sendAction(command(), fetchImpl)).rejects.toMatchObject({
          name: "PlanApprovalError",
          outcomeKnown: false,
          approvalRecorded: false,
        });
        expect(fetchImpl).toHaveBeenCalledTimes(1);
      } finally {
        vi.unstubAllGlobals();
      }
    },
  );
  it("requires a fresh read after an uncertain command despite reset or repeated review", () => {
    const request = command();
    const sent = actionTransition(
      actionTransition(idleAction(), { type: "review", request }),
      { type: "confirm" },
    );
    const uncertain = actionTransition(sent, {
      type: "uncertain",
      error: "Approval was recorded, handoff failed.",
      outcome: {
        outcomeKnown: true,
        approvalRecorded: true,
        handoffFailed: true,
      },
    });
    expect(uncertain).toMatchObject({
      phase: "uncertain",
      outcome: { approvalRecorded: true, handoffFailed: true },
    });
    for (const type of ["reset", "review", "confirm", "cancel", "resolved"])
      expect(actionTransition(uncertain, { type, request })).toBe(uncertain);
  });
});

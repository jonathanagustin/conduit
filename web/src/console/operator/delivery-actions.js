/*
 * The delivery journey's writes, as pure functions: what the console sends to
 * the Hub to propose a Plan, decide one, and evaluate a promotion, plus the
 * confirm -> in-flight -> answered state every one of those writes moves
 * through. Nothing here touches the DOM or the network except sendAction.
 *
 * Each request shape was read from the Hub handler that decodes it
 * (internal/controlplane/http.go). Those handlers refuse unknown fields, so a
 * builder never adds one.
 */
import { reviewState } from "./change-review.js";

const text = (value) => (typeof value === "string" ? value.trim() : "");
const list = (value) => (Array.isArray(value) ? value : []);
const clone = (value) => JSON.parse(JSON.stringify(value));

/** @typedef {{ allowed: boolean | null, reason: string }} CapabilityDecision */

/** @type {null | ((operation: string, resourceRid: string) => CapabilityDecision)} */
let canPerform = null;

/**
 * Plugs the session's `capabilities.canPerform` into the delivery journey.
 * The engine binds it once; null unbinds.
 * @param {null | ((operation: string, resourceRid: string) => CapabilityDecision)} decide
 */
export function bindDeliveryCapabilities(decide) {
  canPerform = typeof decide === "function" ? decide : null;
}

/**
 * The one place the session's capabilities plug in. Every delivery write asks
 * here before it offers its button.
 *
 * `allowed` is false only when the session answer says the operation is not
 * held; a control is disabled on false alone and shows `reason`. null means
 * the session did not say (not bound, not read, or a resource or operation it
 * does not report): the control stays live and the Hub decides at the write.
 *
 * @param {string} operation a Hub operation, e.g. "change:propose"
 * @param {string} resourceRid a Hub resource id, e.g. "environment/dev-use2"
 * @returns {CapabilityDecision}
 */
export function deliveryCapabilityHook(operation, resourceRid) {
  if (!canPerform || !operation || !resourceRid)
    return { allowed: null, reason: "" };
  const decision = canPerform(operation, resourceRid);
  return decision?.allowed === false
    ? { allowed: false, reason: String(decision.reason || "") }
    : { allowed: decision?.allowed === true ? true : null, reason: "" };
}

/** Whether a gate disables its control: only a stated refusal does. */
export const capabilityBlocks = (gate) => gate?.allowed === false;

/** What the Hub said, or the status when it said nothing readable. */
export function hubReason(status, body) {
  const reason = text(body?.error);
  return reason || `the Hub returned ${status}`;
}

// ---------------------------------------------------------------------------
// Confirm -> in-flight -> answered
// ---------------------------------------------------------------------------

export function idleAction() {
  return { phase: "idle", request: null, error: "", result: null };
}

/**
 * One write's lifecycle. A request is only sent from `confirm`, an answer is
 * only accepted while `inflight`, and nothing reaches `success` except the
 * Hub's own answer.
 */
export function actionTransition(action, event) {
  const current = action || idleAction();
  switch (event?.type) {
    case "review":
      if (current.phase === "inflight") return current;
      if (event.request?.error)
        return { ...idleAction(), error: event.request.error };
      return {
        phase: "confirm",
        request: event.request,
        error: "",
        result: null,
      };
    case "cancel":
      return current.phase === "confirm" ? idleAction() : current;
    case "confirm":
      return current.phase === "confirm"
        ? { ...current, phase: "inflight" }
        : current;
    case "resolved":
      return current.phase === "inflight"
        ? { ...current, phase: "success", result: event.result ?? null }
        : current;
    case "rejected":
      return current.phase === "inflight"
        ? {
            ...current,
            phase: "failure",
            error: text(event.error) || "The Hub did not answer.",
          }
        : current;
    case "reset":
      return current.phase === "inflight" ? current : idleAction();
    default:
      return current;
  }
}

/**
 * Send one confirmed request. Same-origin, no Authorization header: the Hub
 * session cookie or its identity proxy names the operator. The Hub's write
 * routes take no idempotency key, so none is sent.
 */
export async function sendAction(request, fetchImpl = fetch) {
  const response = await fetchImpl(request.path, {
    method: request.method,
    headers: {
      "content-type": "application/json",
      accept: "application/json",
      ...request.headers,
    },
    body: JSON.stringify(request.body),
    credentials: "same-origin",
  });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw Object.assign(new Error(hubReason(response.status, body)), {
      status: response.status,
    });
  }
  return { status: response.status, body, headers: response.headers };
}

// ---------------------------------------------------------------------------
// Propose a Plan: POST /v1/plans
// ---------------------------------------------------------------------------

const UNUSABLE_BASE = new Set(["CANCELLED", "FAILED"]);

/**
 * Plans of one environment that carry the desired state the Hub accepted,
 * newest first. A proposal edits one of these documents; the console never
 * authors a desired state from nothing.
 */
export function proposalBases(plans, environmentId) {
  return list(plans)
    .filter(
      (plan) =>
        plan?.desiredState &&
        plan.desiredState.environment?.id === environmentId &&
        Array.isArray(plan.desiredState.entities),
    )
    .sort((a, b) =>
      String(b.createdAt || "").localeCompare(String(a.createdAt || "")),
    );
}

/**
 * The base offered first: the newest plan that was not cancelled or failed.
 * @returns {any}
 */
export function defaultProposalBase(plans, environmentId) {
  const bases = proposalBases(plans, environmentId);
  return bases.find((plan) => !UNUSABLE_BASE.has(plan.status)) || bases[0];
}

/** Releases the Hub's catalog lists for a product. Nothing else is offered. */
export function catalogReleases(products, product) {
  const record = list(products).find((row) => row?.product === product);
  return list(record?.releases).filter(
    (release) => release?.product === product && text(release.version),
  );
}

// Header values travel as Latin-1; anything else would throw inside fetch
// after the operator confirmed, so it is refused before the confirm step.
const HEADER_SAFE = /^[\x20-\x7e]*$/;

/**
 * The next desired state for one entity moved to one catalogued release.
 *
 * Body: the DesiredState document itself (decodePlanProposal, the
 * application/json branch). Title and description travel in X-Change-Title
 * and X-Change-Description, as the handler reads them.
 *
 * @param {any} [input]
 * @returns {any} a request, or `{ error }` when one cannot be built
 */
export function buildPlanProposal({
  plans,
  products,
  environmentId,
  basePlanId = "",
  entityName,
  version,
  generation,
  rationale,
} = {}) {
  const environment = text(environmentId);
  if (!environment) return { error: "Select an environment first." };
  const bases = proposalBases(plans, environment);
  const base = basePlanId
    ? bases.find((plan) => plan.id === basePlanId)
    : defaultProposalBase(plans, environment);
  if (!base)
    return {
      error:
        "No plan with a desired state is loaded for this environment. The console edits a desired state the Hub already accepted; the first one is proposed with the CLI.",
    };
  const desired = clone(base.desiredState);
  const entity = list(desired.entities).find((row) => row?.name === entityName);
  if (!entity)
    return { error: "Select an installation from the base desired state." };
  const release = catalogReleases(products, entity.product).find(
    (row) => row.version === version,
  );
  if (!release)
    return {
      error: "Select a target release from the Hub's catalog for this product.",
    };
  const nextGeneration = text(generation);
  if (!nextGeneration)
    return {
      error:
        "Enter the generation: the commit or other immutable revision this desired state is pinned to.",
    };
  if (nextGeneration === desired.environment?.generation)
    return {
      error:
        "The generation must differ from the base plan's. The Hub refuses a different desired state under a generation it has already seen.",
    };
  const reason = text(rationale);
  if (!reason) return { error: "Enter a reason for the change." };
  const title = `Set ${entity.name} to ${release.version} in ${environment}`;
  if (!HEADER_SAFE.test(title) || !HEADER_SAFE.test(reason))
    return {
      error:
        "The reason travels in a request header and must be plain printable ASCII.",
    };
  const previousVersion = entity.version;
  entity.version = release.version;
  desired.environment = { ...desired.environment, generation: nextGeneration };
  desired.releases = list(desired.releases);
  if (
    !desired.releases.some(
      (row) =>
        row?.product === release.product && row.version === release.version,
    )
  )
    desired.releases.push(clone(release));
  return {
    method: "POST",
    path: "/v1/plans",
    headers: { "X-Change-Title": title, "X-Change-Description": reason },
    body: desired,
    summary: [
      ["Environment", environment],
      ["Based on plan", `${base.id} (${base.status || "status unavailable"})`],
      ["Installation", entity.name],
      ["Product", entity.product],
      ["Version", `${previousVersion || "none"} -> ${release.version}`],
      [
        "Generation",
        `${base.desiredState.environment?.generation || "none"} -> ${nextGeneration}`,
      ],
      ["Title", title],
      ["Reason", reason],
    ],
    capability: {
      operation: "change:propose",
      resourceRid: `environment/${environment}`,
    },
  };
}

/**
 * The Hub's own validator on a desired state: POST /v1/validate answers
 * `{ valid, errors: [{ path, message }] }` and records nothing. POST /v1/plans
 * refuses an invalid document with only a count ("desired state has 1
 * validation error(s)"), so this is where the operator learns which rule.
 */
export function validationDetail(body) {
  return list(body?.errors)
    .map((row) =>
      [text(row?.path), text(row?.message)].filter(Boolean).join(": "),
    )
    .filter(Boolean)
    .join("; ");
}

/** Reads the validator's detail for a refused proposal; "" when it has none. */
export async function readValidationDetail(desiredState, fetchImpl = fetch) {
  try {
    const response = await fetchImpl("/v1/validate", {
      method: "POST",
      headers: {
        "content-type": "application/json",
        accept: "application/json",
      },
      body: JSON.stringify(desiredState),
      credentials: "same-origin",
    });
    return validationDetail(await response.json());
  } catch {
    return "";
  }
}

/**
 * What the Hub decided about a freshly proposed Plan. A 201 is not an
 * approval and is not necessarily reviewable: with no enabled approval policy
 * the Change Request is created in ERROR and carries the reason.
 */
export function proposalOutcome(plan, changeRequest) {
  const lines = [
    `Plan ${text(plan?.id) || "id unavailable"} recorded with status ${text(plan?.status) || "unavailable"}.`,
  ];
  const requestId = text(changeRequest?.id) || text(plan?.changeRequestId);
  if (!requestId) return { tone: "info", lines };
  if (!changeRequest) {
    lines.push(
      `Change request ${requestId} governs it; its status could not be read.`,
    );
    return { tone: "warn", lines };
  }
  lines.push(
    `Change request ${requestId} is ${text(changeRequest.status) || "status unavailable"}.`,
  );
  const failed = text(changeRequest.error);
  if (failed || changeRequest.status === "ERROR") {
    lines.push(
      failed
        ? `The Hub's reason: ${failed}`
        : "The Hub recorded an error without a reason.",
    );
    return { tone: "err", lines };
  }
  return { tone: "ok", lines };
}

// ---------------------------------------------------------------------------
// Decide a Plan: POST /v1/plans/{planID}/approvals, or the refusal routes
// ---------------------------------------------------------------------------

/**
 * Whether this operator may be offered Approve and Reject on a plan. The
 * presentation follows change-review.js: the author of the governing change
 * request is shown why they cannot decide it. The Hub enforces the rule; this
 * only declines to offer a button the Hub would refuse.
 *
 * @returns {{ canReview: boolean, isAuthor: boolean, reason: string, note?: string }}
 */
export function planReviewState(
  plan,
  changeRequests,
  operatorName,
  operatorIdentities = [operatorName],
) {
  if (!plan)
    return {
      canReview: false,
      isAuthor: false,
      reason: "No plan is selected.",
    };
  if (plan.status !== "AWAITING_APPROVAL")
    return {
      canReview: false,
      isAuthor: false,
      reason: `This plan is ${plan.status ?? "unknown"}; it does not take an approval.`,
    };
  if (!plan.changeRequestId) {
    if (!text(operatorName))
      return {
        canReview: false,
        isAuthor: false,
        reason:
          "Signed-in operator identity is unavailable; review is disabled.",
      };
    return {
      canReview: true,
      isAuthor: false,
      reason: "",
      note: "This plan has no change request. The Hub approves it only for an operator who holds Operate on the environment.",
    };
  }
  const request = list(changeRequests).find(
    (row) => row?.id === plan.changeRequestId,
  );
  if (!request)
    return {
      canReview: false,
      isAuthor: false,
      reason: `Change request ${plan.changeRequestId} governs this plan and is not in the loaded index, so its author is unknown; review is disabled.`,
    };
  return reviewState(request, operatorName, operatorIdentities);
}

/**
 * Approve: POST /v1/plans/{planID}/approvals {rationale}.
 * Reject: a governed plan is refused through its change request
 * (POST /v1/change-requests/{id}/reject {rationale}); a plan with none is
 * cancelled (POST /v1/plans/{planID}/cancel {reason}).
 *
 * @param {any} plan
 * @param {string} decision
 * @param {string} rationale
 * @returns {any} a request, or `{ error }` when one cannot be built
 */
export function buildPlanDecision(plan, decision, rationale) {
  const reason = text(rationale);
  if (!plan?.id) return { error: "No plan is selected." };
  if (decision !== "approve" && decision !== "reject")
    return { error: "Unknown decision." };
  if (!reason) return { error: "Enter a reason first." };
  const id = encodeURIComponent(plan.id);
  const environment = text(plan.plan?.environment);
  const summary = [
    ["Plan", plan.id],
    ["Environment", environment || "unavailable"],
    ["Decision", decision],
    ["Reason", reason],
  ];
  if (decision === "approve")
    return {
      method: "POST",
      path: `/v1/plans/${id}/approvals`,
      headers: {},
      body: { rationale: reason },
      summary,
      capability: {
        operation: plan.changeRequestId ? "change:review" : "resource:operate",
        resourceRid: `environment/${environment}`,
      },
    };
  if (plan.changeRequestId)
    return {
      method: "POST",
      path: `/v1/change-requests/${encodeURIComponent(plan.changeRequestId)}/reject`,
      headers: {},
      body: { rationale: reason },
      summary: [...summary, ["Change request", plan.changeRequestId]],
      capability: {
        operation: "change:review",
        resourceRid: `environment/${environment}`,
      },
    };
  return {
    method: "POST",
    path: `/v1/plans/${id}/cancel`,
    headers: {},
    body: { reason },
    summary: [
      ...summary,
      ["Effect", "cancels the plan (it has no change request)"],
    ],
    capability: {
      operation: "resource:operate",
      resourceRid: `environment/${environment}`,
    },
  };
}

// ---------------------------------------------------------------------------
// Promote a Release: POST /v1/promotions/evaluations
// ---------------------------------------------------------------------------

/**
 * Channel edges the console can evaluate, each with the policy the Hub
 * already holds for it: a stage declared in a loaded desired state, or the
 * policy of a recorded evaluation. The console does not author a policy.
 */
export function promotionStages(plans, promotions) {
  const stages = new Map();
  const add = (stage) => {
    const key = `${stage.product}\n${stage.fromChannel}\n${stage.toChannel}`;
    if (!stages.has(key)) stages.set(key, { key, ...stage });
  };
  const ordered = list(plans)
    .slice()
    .sort((a, b) =>
      String(b.createdAt || "").localeCompare(String(a.createdAt || "")),
    );
  for (const plan of ordered) {
    for (const setting of list(plan?.desiredState?.productSettings)) {
      for (const stage of list(setting?.promotionPipeline?.stages)) {
        if (!text(setting.product) || !stage?.policy) continue;
        add({
          product: setting.product,
          fromChannel: stage.fromChannel,
          toChannel: stage.toChannel,
          policy: stage.policy,
          source: `declared in plan ${plan.id}`,
        });
      }
    }
  }
  for (const record of list(promotions)) {
    if (!text(record?.product) || !record.policy) continue;
    add({
      product: record.product,
      fromChannel: record.fromChannel,
      toChannel: record.toChannel,
      policy: record.policy,
      source: `policy of recorded evaluation ${record.id}`,
    });
  }
  return [...stages.values()];
}

/**
 * Health evidence for one release: the reported installations running that
 * exact version. healthySince is not in the fleet index, so it is not sent;
 * a stage that needs a healthy duration is blocked by the Hub, not guessed.
 */
export function promotionEvidence(installations, release) {
  return list(installations)
    .filter(
      (row) =>
        row?.product === release?.product &&
        row.observedVersion === release?.version &&
        text(row.environment) &&
        text(row.installationId),
    )
    .map((row) => ({
      environment: row.environment,
      installationId: row.installationId,
      health: row.health,
      ...(text(row.observedAt) ? { observedAt: row.observedAt } : {}),
    }));
}

/**
 * Body: PromotionEvaluationRequest — release, fromChannel, toChannel, policy,
 * evidence (internal/catalog/domain/promotion.go).
 *
 * @param {any} [input]
 * @returns {any} a request, or `{ error }` when one cannot be built
 */
export function buildPromotionEvaluation({
  stage,
  products,
  version,
  installations,
} = {}) {
  if (!stage?.product || !stage.policy)
    return { error: "Select a promotion stage the Hub has a policy for." };
  const release = catalogReleases(products, stage.product).find(
    (row) => row.version === version,
  );
  if (!release)
    return {
      error: "Select a release from the Hub's catalog for this product.",
    };
  const evidence = promotionEvidence(installations, release);
  return {
    method: "POST",
    path: "/v1/promotions/evaluations",
    headers: {},
    body: {
      release: clone(release),
      fromChannel: stage.fromChannel,
      toChannel: stage.toChannel,
      policy: clone(stage.policy),
      evidence,
    },
    summary: [
      ["Product", release.product],
      ["Release", release.version],
      ["From channel", stage.fromChannel],
      ["To channel", stage.toChannel],
      ["Policy", stage.source],
      [
        "Evidence",
        evidence.length
          ? `${evidence.length} reported installation(s) at this version`
          : "none: no loaded installation reports this version",
      ],
    ],
    // POST /v1/promotions/evaluations requires resource:operate at the root.
    capability: { operation: "resource:operate", resourceRid: "platform/root" },
  };
}

/**
 * What an evaluation record says. PASSED is the only promotion; BLOCKED,
 * ABORTED and CANCELLED are stated as such with every reason the Hub gave.
 */
export function promotionOutcome(record) {
  const status = text(record?.status);
  const lifecycle = text(record?.lifecycle);
  const blockers = list(record?.blockers);
  const reasons = blockers.length
    ? blockers.map((row) => ({
        kind: text(row?.kind),
        message: text(row?.message),
      }))
    : list(record?.reasons).map((message) => ({
        kind: "",
        message: text(message),
      }));
  const edge = `${text(record?.product)} ${text(record?.version)} ${text(record?.fromChannel)} -> ${text(record?.toChannel)}`;
  if (status === "PASSED")
    return {
      tone: "ok",
      headline: `PASSED: ${edge}`,
      detail: `Recorded as evaluation ${text(record?.id)}.`,
      reasons: [],
    };
  const stopped = lifecycle === "ABORTED" || lifecycle === "CANCELLED";
  const label = stopped ? lifecycle : status || "UNKNOWN";
  return {
    tone: "err",
    headline: `${label}: ${edge} was not promoted`,
    detail:
      [
        text(record?.id) ? `Recorded as evaluation ${record.id}.` : "",
        text(record?.lifecycleReason),
      ]
        .filter(Boolean)
        .join(" ") || "",
    reasons,
  };
}

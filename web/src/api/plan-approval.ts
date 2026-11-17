import { create, toJson } from "@bufbuild/protobuf";
import { TimestampSchema, type Timestamp } from "@bufbuild/protobuf/wkt";
import {
  ConnectError,
  createClient,
  type Transport,
} from "@connectrpc/connect";
import {
  ApprovePlanRequestSchema,
  PlanApprovalOutcomeSchema,
  PlanApprovalService,
} from "./gen/conduit/plans/v1/plan_approval_pb.js";
import { hubConnectError, hubConnectTransport } from "./hub-connect.js";
import { HubError } from "./hub-error.js";

/** Commands must never follow a redirect or replay through REST. */
export function planApprovalTransport(
  baseUrl: string,
  fetchImpl: typeof fetch = fetch,
): Transport {
  return hubConnectTransport(baseUrl, (input, init) =>
    fetchImpl(new Request(input, { ...init, redirect: "error" })),
  );
}

export class PlanApprovalError extends HubError {
  readonly outcomeKnown: boolean;
  readonly approvalRecorded: boolean;
  readonly handoffFailed: boolean;
  constructor(status: number, outcomeKnown = false, handoffFailed = false) {
    super(
      status,
      outcomeKnown
        ? handoffFailed
          ? "Approval was recorded, but execution handoff failed. Refresh the Plan before taking another action."
          : "Approval was recorded. Refresh the Plan before taking another action."
        : "Plan approval outcome is unknown. Refresh the Plan before taking another action.",
    );
    this.name = "PlanApprovalError";
    this.outcomeKnown = outcomeKnown;
    this.approvalRecorded = outcomeKnown;
    this.handoffFailed = outcomeKnown && handoffFailed;
  }
}

function validTimestamp(value: Timestamp | undefined): boolean {
  if (!value) return false;
  try {
    return typeof toJson(TimestampSchema, value) === "string";
  } catch {
    return false;
  }
}

/** One native review command. Returned evidence is typed, never a new permit. */
export async function submitPlanApproval(
  transport: Transport,
  id: string,
  rationale: string,
  signal?: AbortSignal,
) {
  if (!id.trim() || !rationale.trim())
    throw new HubError(
      422,
      "Plan identity and approval rationale are required.",
    );
  try {
    const receipt = await createClient(
      PlanApprovalService,
      transport,
    ).approvePlan(create(ApprovePlanRequestSchema, { planId: id, rationale }), {
      signal,
      timeoutMs: 10000,
    });
    const plan = receipt.plan;
    if (
      !plan ||
      plan.id !== id ||
      !plan.status ||
      plan.stepCount < BigInt(0) ||
      plan.requiredApprovals < BigInt(0) ||
      plan.approvalCount < BigInt(1) ||
      plan.approvalCount !== BigInt(receipt.approvals.length) ||
      !validTimestamp(plan.createdAt) ||
      !validTimestamp(plan.updatedAt) ||
      receipt.approvals.some(
        (review) => !review.subject || !validTimestamp(review.createdAt),
      )
    )
      throw new PlanApprovalError(502);
    return receipt;
  } catch (error) {
    if (error instanceof PlanApprovalError) throw error;
    const failure = ConnectError.from(error);
    const details = failure.findDetails(PlanApprovalOutcomeSchema);
    const outcome = details.length === 1 ? details[0] : undefined;
    const known = outcome?.planId === id && outcome.approvalRecorded === true;
    throw new PlanApprovalError(
      hubConnectError(failure).status,
      known,
      known && outcome?.handoffFailed === true,
    );
  }
}

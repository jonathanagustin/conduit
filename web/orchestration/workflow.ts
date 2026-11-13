import {
  condition,
  defineQuery,
  defineSignal,
  isCancellation,
  proxyActivities,
  setHandler,
  workflowInfo,
} from "@temporalio/workflow";
import type * as activities from "./activities";
import {
  APPROVAL_SIGNAL,
  STATE_QUERY,
  type DeliveryState,
  type Proposal,
} from "./contracts";

export const deliveryState = defineQuery<DeliveryState>(STATE_QUERY);
export const deliveryApproval = defineSignal<[boolean]>(APPROVAL_SIGNAL);
const { validateProposal, simulateDeployment } = proxyActivities<
  typeof activities
>({
  startToCloseTimeout: "30 seconds",
  scheduleToCloseTimeout: "2 minutes",
  retry: {
    initialInterval: "1 second",
    maximumInterval: "5 seconds",
    maximumAttempts: 3,
  },
});

export async function deliveryRehearsal(
  proposal: Proposal,
): Promise<DeliveryState> {
  const workflowId = workflowInfo().workflowId;
  const state: DeliveryState = {
    workflowId,
    simulation: true,
    status: "validating",
    proposal,
  };
  let decision: boolean | undefined;
  setHandler(deliveryState, () => state);
  setHandler(deliveryApproval, (approved) => {
    // First explicit decision wins. Early and duplicate decisions cannot skip
    // validation, reverse a denial, or create another simulated deployment.
    if (state.status === "awaiting_approval" && decision === undefined)
      decision = approved;
  });
  try {
    state.evidence = await validateProposal({
      stepId: `${workflowId}/validate`,
      proposal,
    });
    if (!state.evidence.passed) {
      state.status = "failed";
      return state;
    }
    state.status = "awaiting_approval";
    if (!(await condition(() => decision !== undefined, "24 hours"))) {
      state.status = "expired";
      return state;
    }
    if (!decision) {
      state.status = "denied";
      return state;
    }
    state.receipt = await simulateDeployment({
      stepId: `${workflowId}/deploy-rehearsal`,
      approved: decision,
      evidence: state.evidence,
    });
    state.status = "completed";
  } catch (error) {
    state.status = isCancellation(error) ? "cancelled" : "failed";
    if (!isCancellation(error))
      state.error = "Rehearsal activity failed after bounded retries";
  }
  return state;
}

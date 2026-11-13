export const TASK_QUEUE = "conduit-delivery-rehearsal";
export const STATE_QUERY = "deliveryState";
export const APPROVAL_SIGNAL = "deliveryApproval";
export type Proposal = { summary: string; failValidation?: boolean };
export type Evidence = {
  simulation: true;
  passed: boolean;
  checks: { name: string; passed: boolean }[];
};
export type DeliveryState = {
  workflowId: string;
  simulation: true;
  status:
    | "validating"
    | "awaiting_approval"
    | "completed"
    | "denied"
    | "failed"
    | "expired"
    | "cancelled";
  proposal: Proposal;
  evidence?: Evidence;
  receipt?: { id: string; simulation: true; deployed: false };
  error?: string;
};

import { Tag, type Intent } from "@blueprintjs/core";

// Plan statuses, from internal/controlplane/lifecycle.go. A Plan an operator
// has to act on reads loud; one that has finished cleanly does not.
// The PlanStatus vocabulary is the Hub's, from internal/controlplane/lifecycle.go.
// SUPERSEDED was invented and never emitted; the real terminal record for a
// non-executing Environment is RECORDED (a reviewed generation carrying no
// executable work), which was missing -- so a RECORDED Plan rendered as an
// unstyled "none" tag instead of a deliberately calm one.
//
// This table is read by `scripts/verify-console-contract.mjs` by path, which is
// why it sits in its own file rather than inside a screen: it moved out of
// `features/plans/PlansPage.tsx` when `features/environments` stopped being
// allowed to import it from there, and a vocabulary a gate reads should not be
// reachable only through a page component.
export const PLAN_INTENT: Record<string, Intent> = {
  AWAITING_APPROVAL: "warning",
  APPROVED: "primary",
  LEASED: "primary",
  RUNNING: "primary",
  SUCCEEDED: "success",
  FAILED: "danger",
  CANCELLED: "none",
  RECORDED: "none",
};

export function PlanStatusTag({ value }: { value: string }) {
  const intent = PLAN_INTENT[value];
  return (
    // Calm states stay minimal so a FAILED or REJECTED row carries the
    // colour on a screen full of finished work.
    <Tag intent={intent ?? "none"} minimal={intent === undefined || value === "SUCCEEDED" || value === "RECORDED"}>
      {value}
    </Tag>
  );
}

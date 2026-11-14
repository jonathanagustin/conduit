"use client";

import { makeAssistantToolUI } from "@assistant-ui/react";

import { PlanCardView, RunCard } from "./components/domain";
import type { PackagingRun, PlanCard } from "./data/hub";

function record(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === "object"
    ? (value as Record<string, unknown>)
    : {};
}

const PackagingRunTool = makeAssistantToolUI<Record<string, unknown>, unknown>({
  toolName: "packaging_run",
  render: ({ args }) => {
    const fields = record(args);
    const run: PackagingRun = {
      configured: fields["configured"] !== false,
      ...(typeof fields["runId"] === "string"
        ? { runId: fields["runId"] }
        : {}),
      ...(typeof fields["phase"] === "string"
        ? { phase: fields["phase"] }
        : {}),
      ...(typeof fields["planId"] === "string"
        ? { planId: fields["planId"] }
        : {}),
    };
    return <RunCard run={run} />;
  },
});

const PlanTool = makeAssistantToolUI<Record<string, unknown>, unknown>({
  toolName: "plan",
  render: ({ args }) => {
    const fields = record(args);
    const plan: PlanCard = {
      id: typeof fields["id"] === "string" ? fields["id"] : "plan",
      environment:
        typeof fields["environment"] === "string" ? fields["environment"] : "",
      status:
        typeof fields["status"] === "string"
          ? fields["status"]
          : "AWAITING_APPROVAL",
      stepCount: 0,
      approvalCount:
        typeof fields["approvalCount"] === "number"
          ? fields["approvalCount"]
          : 0,
      requiredApprovals:
        typeof fields["requiredApprovals"] === "number"
          ? fields["requiredApprovals"]
          : 1,
    };
    return <PlanCardView plan={plan} />;
  },
});

export function ContextToolUIs() {
  return (
    <>
      <PackagingRunTool />
      <PlanTool />
    </>
  );
}

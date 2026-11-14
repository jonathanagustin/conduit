export type Gate = "GO" | "CAUTION" | "NO-GO";

export type CheckItem = {
  id: string;
  label: string;
  passed: boolean;
  detail: string;
};

export type BoardColumn = "queued" | "progress" | "approval" | "done";

export const PACKAGING_STAGES = [
  { id: "assess", label: "Assess" },
  { id: "propose", label: "Propose" },
  { id: "validate", label: "Build / validate" },
  { id: "review", label: "Review wait" },
  { id: "handoff", label: "Handoff" },
] as const;

export type StageMark = "done" | "current" | "waiting";

const ORDER = PACKAGING_STAGES.map((stage) => stage.id);

export function healthShares(rows: readonly { health: string }[]): {
  ok: number;
  bad: number;
  other: number;
} {
  let ok = 0;
  let bad = 0;
  for (const row of rows) {
    if (row.health === "HEALTHY") ok += 1;
    else if (
      row.health === "UNHEALTHY" ||
      row.health === "ERROR" ||
      row.health === "TERMINAL"
    )
      bad += 1;
  }
  return { ok, bad, other: rows.length - ok - bad };
}

export function planGate(status: string): Gate {
  if (status === "SUCCEEDED" || status === "APPROVED") return "GO";
  if (
    status === "AWAITING_APPROVAL" ||
    status === "LEASED" ||
    status === "RUNNING" ||
    status === "RECORDED"
  ) {
    return "CAUTION";
  }
  return "NO-GO";
}

export function runGate(run: {
  configured: boolean;
  phase?: string;
  outcome?: string;
}): Gate {
  if (!run.configured) return "CAUTION";
  if (run.phase === "complete" || run.outcome === "HANDOFF_RECORDED")
    return "GO";
  if (
    run.phase === "blocked" ||
    run.outcome === "REJECTED" ||
    run.outcome === "UNSUPPORTED"
  ) {
    return "NO-GO";
  }
  return "CAUTION";
}

export function fleetGate(health: string): Gate {
  if (health === "HEALTHY") return "GO";
  if (
    health === "WARNING" ||
    health === "UNKNOWN" ||
    health === "INDETERMINATE"
  ) {
    return "CAUTION";
  }
  return "NO-GO";
}

export function columnOf(status: string): BoardColumn {
  switch (status) {
    case "AWAITING_APPROVAL":
      return "approval";
    case "LEASED":
    case "RUNNING":
      return "progress";
    case "SUCCEEDED":
    case "FAILED":
    case "CANCELLED":
      return "done";
    default:
      return "queued";
  }
}

export function stageMarks(phase: string | undefined): StageMark[] {
  if (phase === "complete") return PACKAGING_STAGES.map(() => "done");
  const at = (ORDER as readonly string[]).indexOf(phase ?? "");
  if (phase === "blocked") {
    return PACKAGING_STAGES.map((_, index) =>
      index === 0 ? "current" : "waiting",
    );
  }
  if (at < 0) return PACKAGING_STAGES.map(() => "waiting");
  return PACKAGING_STAGES.map((_, index) => {
    if (index < at) return "done";
    if (index === at) return "current";
    return "waiting";
  });
}

export function stageProgress(phase: string | undefined): number {
  const marks = stageMarks(phase);
  const done = marks.filter((mark) => mark === "done").length;
  const current = marks.some((mark) => mark === "current") ? 0.5 : 0;
  return Math.min(1, (done + current) / PACKAGING_STAGES.length);
}

export function handoffChecklist(input: {
  validationPassed: boolean;
  approvalCount: number;
  requiredApprovals: number;
  rejections: number;
  phase?: string;
}): CheckItem[] {
  const before = !input.phase || !["handoff", "complete"].includes(input.phase);
  return [
    {
      id: "validation",
      label: "Validation passed",
      passed: input.validationPassed,
      detail: input.validationPassed
        ? "Build and validate reported success."
        : "Validation has not passed.",
    },
    {
      id: "approvals",
      label: "Required approvals",
      passed: input.approvalCount >= input.requiredApprovals,
      detail: `${input.approvalCount}/${input.requiredApprovals} approved`,
    },
    {
      id: "rejections",
      label: "No rejections",
      passed: input.rejections === 0,
      detail:
        input.rejections === 0
          ? "No rejection is recorded."
          : `${input.rejections} rejection(s)`,
    },
    {
      id: "before-handoff",
      label: "Before handoff",
      passed: before,
      detail: before ? "Handoff has not started." : "Handoff already started.",
    },
  ];
}

export type AuditLine = { id: string; at: string; text: string };

export function runAudit(run: {
  configured: boolean;
  phase?: string;
  outcome?: string;
  reason?: string;
  lastReview?: string;
  planId?: string;
}): AuditLine[] {
  if (!run.configured) return [];
  const lines: AuditLine[] = [];
  if (run.phase)
    lines.push({ id: "phase", at: "", text: `Phase ${run.phase}` });
  if (run.outcome)
    lines.push({ id: "outcome", at: "", text: `Outcome ${run.outcome}` });
  if (run.reason) lines.push({ id: "reason", at: "", text: run.reason });
  if (run.lastReview)
    lines.push({ id: "review", at: "", text: `Last review ${run.lastReview}` });
  if (run.planId)
    lines.push({ id: "plan", at: "", text: `Plan ${run.planId}` });
  return lines.reverse();
}

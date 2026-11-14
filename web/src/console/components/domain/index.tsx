import type { ReactNode } from "react";

import type { ChangeRequestCard, FleetRow, PackagingRun, PlanCard } from "../../data/hub";
import { CONDUIT_ACTIONS } from "../../ontology";
import {
  columnOf,
  fleetGate,
  handoffChecklist,
  PACKAGING_STAGES,
  planGate,
  runAudit,
  runGate,
  stageMarks,
} from "../../logic";
import type { EnvironmentDetail } from "../../data/hub";
import {
  ActivityTimeline,
  AuditRail,
  Checklist,
  ConstraintRow,
  DataTable,
  DenseTable,
  DiffPane,
  DisabledWithReason,
  EmptyState,
  EntityCard,
  ErrorState,
  FilterBar,
  ImpactSummary,
  InboxList,
  Kanban,
  ObjectHeader,
  OutputPane,
  PageHeader,
  PropertyCard,
  RequirementChecklist,
  ReviewHistory,
  SplitListDetail,
  SplitView,
  StatusTag,
  StepTimeline,
  SummaryBar,
  TabBar,
  TaskList,
} from "../base";

export function GateTag({ status }: { status: string }) {
  const gate = planGate(status);
  return <StatusTag gate={gate}>{gate}</StatusTag>;
}

export function EnvironmentCard({ id, reported }: { id: string; reported: boolean }) {
  return (
    <EntityCard title={id} meta={<StatusTag gate={reported ? "GO" : "CAUTION"}>{reported ? "in sync" : "no report"}</StatusTag>}>
      <p className="conduit-muted">Environment from GET /v1/environments.</p>
    </EntityCard>
  );
}

export function FleetTable({ rows }: { rows: readonly FleetRow[] }) {
  if (rows.length === 0) {
    return <EmptyState title="No installations" detail="GET /v1/fleet/installations returned none." />;
  }
  return (
    <DataTable
      columns={["Installation", "Environment", "Product", "Health", "Gate"]}
      rows={rows.map((row) => [
        row.id,
        row.environment,
        row.product,
        row.health,
        <StatusTag key={row.id} gate={fleetGate(row.health)}>{fleetGate(row.health)}</StatusTag>,
      ])}
    />
  );
}

export function ChangeRequestCard({ request }: { request: ChangeRequestCard }) {
  const blocked = !request.requirementsSatisfied;
  return (
    <EntityCard title={request.title} meta={<StatusTag gate={blocked ? "NO-GO" : "CAUTION"}>{request.status}</StatusTag>}>
      <p className="conduit-muted">{request.environment} · {request.author || "unknown author"}</p>
      {blocked ? <p>Blocked until reviewer requirements are satisfied.</p> : null}
    </EntityCard>
  );
}

export function RunCard({ run }: { run: PackagingRun }) {
  return (
    <EntityCard title={run.runId || "Packaging run"} meta={<StatusTag gate={runGate(run)}>{runGate(run)}</StatusTag>}>
      <p className="conduit-muted">{run.configured ? run.phase || "configured" : "configured: false"}</p>
      {run.planId ? <p>Plan {run.planId}</p> : null}
    </EntityCard>
  );
}

export function PlanCardView({ plan }: { plan: PlanCard }) {
  return (
    <EntityCard title={plan.id} meta={<GateTag status={plan.status} />}>
      <p className="conduit-muted">{plan.environment || "no environment"} · {plan.status}</p>
      <p>{plan.approvalCount}/{plan.requiredApprovals} approved</p>
    </EntityCard>
  );
}

export function PackagingRunList({
  runs,
  selected,
  onSelect,
}: {
  runs: readonly { id: string; phase: string }[];
  selected: string;
  onSelect: (id: string) => void;
}) {
  return (
    <div>
      {runs.map((run) => (
        <button key={run.id} type="button" aria-current={run.id === selected ? "true" : undefined} onClick={() => onSelect(run.id)}>
          {run.id}
          <span className="conduit-muted"> {run.phase}</span>
        </button>
      ))}
    </div>
  );
}

export function PackagingRunDetail({
  run,
  error,
  onReload,
}: {
  run: PackagingRun | null;
  error: string;
  onReload: () => void;
}) {
  if (error) return <ErrorState title="Packaging run unavailable" detail={error} />;
  if (!run) return <EmptyState title="No run loaded" detail="Enter a run id and load it." />;
  const marks = stageMarks(run.configured ? run.phase : undefined);
  const stepsDone = marks.filter((mark) => mark === "done").length;
  const validationPassed = run.phase === "review" || run.phase === "handoff" || run.phase === "complete";
  const checks = handoffChecklist({
    validationPassed,
    approvalCount: run.lastReview === "accepted" ? 1 : 0,
    requiredApprovals: 1,
    rejections: run.lastReview === "rejected" ? 1 : 0,
    phase: run.phase,
  });
  return (
    <div>
      <PageHeader title={run.runId || "Packaging run"} detail={run.configured ? "GET /v1/packaging-runs" : "configured: false"} />
      <div className="conduit-row">
        <StatusTag gate={runGate(run)}>{run.configured ? run.phase || "configured" : "not configured"}</StatusTag>
        <button type="button" onClick={onReload}>Load</button>
      </div>
      {!run.configured ? (
        <ErrorState title="Worker not configured" detail="This Hub has no packaging worker. The route answered configured: false." />
      ) : null}
      <div className="conduit-kpi">
        <Stat label="Status" value={run.phase || (run.configured ? "unknown" : "off")} />
        <Stat label="Steps done" value={`${stepsDone}/${marks.length}`} />
        <Stat label="Duration" value="—" />
        <Stat label="Trigger" value={run.proposalRevision || "—"} />
      </div>
      <div className="conduit-stages">
        {PACKAGING_STAGES.map((stage, index) => (
          <EntityCard key={stage.id} title={stage.label} meta={<StatusTag>{marks[index]}</StatusTag>}>
            {marks[index] === "current" ? (
              <div>
                {run.sourceDigest ? <StatusTag>source</StatusTag> : null}
                {run.artifactDigest ? <StatusTag>artifact</StatusTag> : null}
                {run.planId ? <StatusTag>plan</StatusTag> : null}
              </div>
            ) : null}
          </EntityCard>
        ))}
      </div>
      <StepTimeline
        steps={PACKAGING_STAGES.map((stage, index) => ({
          id: stage.id,
          label: stage.label,
          mark: marks[index] ?? "waiting",
        }))}
      />
      <TaskList tasks={run.planId ? [{ id: "plan", label: `Plan ${run.planId}`, status: run.phase || "" }] : []} />
      <OutputPane text={[run.reason, run.outcome, run.sourceDigest].filter(Boolean).join("\n")} />
      <RequirementChecklist items={checks} />
      <ConstraintRow
        label="Handoff"
        passed={checks.every((item) => item.passed)}
        reason={checks.find((item) => !item.passed)?.detail ?? "Every merge requirement passed."}
      />
      <ImpactSummary
        lines={[run.planId ? `Plan ${run.planId}` : "", run.artifactDigest ? "Artifact digest is present." : ""].filter(Boolean)}
      />
      <AuditRail title="Audit" lines={runAudit(run)} empty="No events on this route. An event timeline is not invented." />
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="conduit-stat">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

export function PlanTable({ plans, onSelect }: { plans: readonly PlanCard[]; onSelect: (id: string) => void }) {
  if (plans.length === 0) return <EmptyState title="No plans" detail="GET /v1/plans returned none." />;
  return (
    <DataTable
      columns={["Plan", "Environment", "Status", "Gate", "Approvals"]}
      rows={plans.map((plan) => [
        <button key={plan.id} type="button" onClick={() => onSelect(plan.id)}>{plan.id}</button>,
        plan.environment,
        plan.status,
        <GateTag key={`${plan.id}-gate`} status={plan.status} />,
        `${plan.approvalCount}/${plan.requiredApprovals}`,
      ])}
    />
  );
}

export function PlanDetail({ plan }: { plan: PlanCard | null }) {
  if (!plan) return <EmptyState title="No plan selected" detail="Choose a plan from the table or the board." />;
  const checks = handoffChecklist({
    validationPassed: plan.status !== "FAILED",
    approvalCount: plan.approvalCount,
    requiredApprovals: plan.requiredApprovals,
    rejections: plan.status === "CANCELLED" ? 1 : 0,
    phase: plan.status === "SUCCEEDED" ? "complete" : "review",
  });
  return (
    <div>
      <PageHeader title={plan.id} detail={plan.environment || "no environment"} />
      <PlanCardView plan={plan} />
      {plan.lease ? <p>Lease attempt {plan.lease.attempt ?? "?"}{plan.lease.expiresAt ? ` until ${plan.lease.expiresAt}` : ""}</p> : null}
      <TaskList
        tasks={(plan.execution?.tasks ?? []).map((task, index) => ({
          id: String(index),
          label: task.phase || "task",
          status: task.status || "",
        }))}
      />
      <Checklist items={checks} />
    </div>
  );
}

export function ApprovalInbox({
  filter,
  onFilter,
  plans,
  selected,
  onSelect,
}: {
  filter: "inbox" | "mine" | "all";
  onFilter: (filter: "inbox" | "mine" | "all") => void;
  plans: readonly PlanCard[];
  selected: string;
  onSelect: (id: string) => void;
}) {
  const visible = plans.filter((plan) => {
    if (filter === "inbox") return plan.status === "AWAITING_APPROVAL";
    if (filter === "mine") return false;
    return true;
  });
  return (
    <div>
      <div className="conduit-row">
        {(["inbox", "mine", "all"] as const).map((item) => (
          <button key={item} type="button" aria-pressed={filter === item} onClick={() => onFilter(item)}>
            {item === "inbox" ? "Inbox" : item === "mine" ? "Created by you" : "All"}
          </button>
        ))}
      </div>
      {filter === "mine" ? (
        <EmptyState title="Created by you" detail="The plan list does not include the author, so this inbox stays empty rather than guessing." />
      ) : null}
      {visible.map((plan) => (
        <button key={plan.id} type="button" aria-current={plan.id === selected ? "true" : undefined} onClick={() => onSelect(plan.id)}>
          {plan.id} {plan.status}
        </button>
      ))}
    </div>
  );
}

export function ApprovalDetail({
  plan,
  blocked,
  rationale,
  onRationale,
  onApprove,
  onReject,
}: {
  plan: PlanCard | null;
  blocked: boolean;
  rationale: string;
  onRationale: (value: string) => void;
  onApprove: () => void;
  onReject: () => void;
}) {
  if (!plan) return <EmptyState title="No request selected" detail="Choose a plan that needs approval." />;
  const timeline = [
    { id: "created", text: `${plan.id} is ${plan.status}` },
    ...Array.from({ length: plan.approvalCount }, (_, index) => ({
      id: `approval-${index}`,
      text: `Approval ${index + 1} recorded`,
    })),
  ].reverse();
  return (
    <div>
      <PageHeader title={plan.id} detail={`${plan.approvalCount}/${plan.requiredApprovals} approved`} />
      <ConstraintRow
        label="Approval"
        passed={!blocked}
        reason={
          blocked
            ? (CONDUIT_ACTIONS.find((action) => action.actionId === "approve_plan")?.blockingRule ?? "")
            : "Requirements on this request are satisfied."
        }
      />
      <label>
        Rationale
        <input aria-label="Decision rationale" value={rationale} onChange={(event) => onRationale(event.target.value)} />
      </label>
      <div className="conduit-row">
        {blocked ? (
          <DisabledWithReason reason="Blocked until reviewer requirements are satisfied." label="Approve" />
        ) : (
          <button type="button" onClick={onApprove}>Approve</button>
        )}
        <button type="button" onClick={onReject}>Reject</button>
      </div>
      <ActivityTimeline items={timeline} />
    </div>
  );
}

export { BundlePreview, ChangeRequestReview, EnvironmentPage, FailedPlanDetail, PromotionReview, ReleaseRollout } from "./release";

export function PlanBoard({
  plans,
  onSelect,
  renderActions,
}: {
  plans: readonly PlanCard[];
  onSelect: (id: string) => void;
  renderActions: (plan: PlanCard) => ReactNode;
}) {
  return (
    <Kanban
      columns={[
        { id: "queued", title: "Queued" },
        { id: "progress", title: "In progress" },
        { id: "approval", title: "Needs approval" },
        { id: "done", title: "Done" },
      ]}
      cards={plans}
      columnOf={(plan) => columnOf(plan.status)}
      renderCard={(plan) => (
        <EntityCard title={plan.id} meta={<GateTag status={plan.status} />}>
          <button type="button" onClick={() => onSelect(plan.id)}>Open</button>
          {renderActions(plan)}
        </EntityCard>
      )}
    />
  );
}

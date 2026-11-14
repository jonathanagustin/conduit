import { useState } from "react";

import type {
  ChangeRequestCard,
  EnvironmentDetail,
  FleetRow,
  PackagingRun,
  PlanCard,
  ProductRelease,
  PromotionEdge,
} from "../../data/hub";
import { healthShares } from "../../logic";
import { ArtifactLineage } from "./run-workspace";
import { CONDUIT_ACTIONS } from "../../ontology";
import {
  ActivityTimeline,
  ConstraintRow,
  DataTable,
  DiffPane,
  DisabledWithReason,
  EmptyState,
  EntityCard,
  GraphView,
  KeyValue,
  HealthBar,
  ObjectHeader,
  OutputPane,
  PageHeader,
  PipelineGraph,
  PropertyCard,
  RangeTimeline,
  ReviewHistory,
  StatusChip,
  StatusTag,
  TabBar,
  TargetBoard,
  TaskList,
} from "../base";

const ENV_TABS = [
  { id: "overview", label: "Overview" },
  { id: "activity", label: "Activity" },
  { id: "installed", label: "Installed" },
  { id: "changes", label: "Changes" },
  { id: "config", label: "Config" },
  { id: "settings", label: "Settings" },
] as const;

const ACTIVITY = [
  { id: "plans", label: "Plans" },
  { id: "changes", label: "Changes" },
] as const;

function rule(actionId: string): string {
  return (
    CONDUIT_ACTIONS.find((action) => action.actionId === actionId)
      ?.blockingRule ?? ""
  );
}

export function EnvironmentPage({
  detail,
  fleet,
  plans,
  requests,
  onSelectPlan,
}: {
  detail: EnvironmentDetail;
  fleet: readonly FleetRow[];
  plans: readonly PlanCard[];
  requests: readonly ChangeRequestCard[];
  onSelectPlan: (id: string) => void;
}) {
  const [tab, setTab] = useState<(typeof ENV_TABS)[number]["id"]>("overview");
  const [activity, setActivity] =
    useState<(typeof ACTIVITY)[number]["id"]>("plans");
  const here = fleet.filter((row) => row.environment === detail.id);
  const shares = healthShares(here);
  const localPlans = plans.filter((plan) => plan.environment === detail.id);
  const localRequests = requests.filter(
    (request) => request.environment === detail.id,
  );
  const maintenanceBlocked = detail.maintenance.length === 0;
  return (
    <div>
      <ObjectHeader
        title={detail.id}
        eyebrow={detail.connection || "no connection setting"}
        tags={
          <StatusTag>{detail.reported ? "reported" : "no report"}</StatusTag>
        }
      />
      <TabBar
        tabs={ENV_TABS}
        current={tab}
        onSelect={(id) => setTab(id as (typeof ENV_TABS)[number]["id"])}
      />
      {tab === "overview" ? (
        <div>
          <HealthBar ok={shares.ok} bad={shares.bad} other={shares.other} />
          <p className="conduit-muted">
            {here.length} installations on this environment.
          </p>
          <div className="conduit-kpi">
            <PropertyCard
              label="Release channel"
              value={detail.releaseChannel || "not recorded"}
            />
            <PropertyCard
              label="Labels"
              value={detail.labels || "none recorded"}
            />
            <PropertyCard label="Owners" value="not on this route" />
            <PropertyCard
              label="Maintenance window"
              value={detail.maintenance || "not recorded"}
            />
          </div>
          <ConstraintRow
            label="Maintenance window"
            passed={!maintenanceBlocked}
            reason={
              maintenanceBlocked
                ? "No maintenance window is recorded, so a rollout that requires one stays blocked."
                : detail.maintenance
            }
          />
          <RangeTimeline
            rows={[
              {
                id: detail.id,
                label: detail.maintenance
                  ? "Recorded window"
                  : "Example window",
                bars: [
                  {
                    id: "window",
                    label: detail.maintenance || "Example",
                    start: 0.15,
                    end: 0.4,
                    tone: maintenanceBlocked ? "warn" : "ok",
                  },
                ],
              },
              ...localPlans.map((plan, index) => ({
                id: plan.id,
                label: plan.id,
                bars: [
                  {
                    id: plan.id,
                    label: plan.status,
                    start: 0.05 + index * 0.08,
                    end: 0.35 + index * 0.08,
                    tone:
                      plan.status === "FAILED"
                        ? ("bad" as const)
                        : ("info" as const),
                  },
                ],
              })),
            ]}
          />
          <DataTable
            columns={["Plan", "Status"]}
            rows={localPlans.map((plan) => [
              <button
                key={plan.id}
                type="button"
                onClick={() => onSelectPlan(plan.id)}
              >
                {plan.id}
              </button>,
              plan.status,
            ])}
          />
        </div>
      ) : null}
      {tab === "activity" ? (
        <div>
          <TabBar
            tabs={ACTIVITY}
            current={activity}
            onSelect={(id) =>
              setActivity(id as (typeof ACTIVITY)[number]["id"])
            }
          />
          {activity === "plans" ? (
            <DataTable
              columns={["Plan", "Status", "Approvals"]}
              rows={localPlans.map((plan) => [
                plan.id,
                plan.status,
                `${plan.approvalCount}/${plan.requiredApprovals}`,
              ])}
            />
          ) : null}
          {activity === "changes"
            ? localRequests.map((request) => (
                <ChangeRequestReview key={request.id} request={request} />
              ))
            : null}
        </div>
      ) : null}
      {tab === "installed" ? (
        <DataTable
          columns={["Installation", "Product", "Health"]}
          rows={here.map((row) => [row.id, row.product, row.health])}
        />
      ) : null}
      {tab === "changes" ? (
        localRequests.length === 0 ? (
          <EmptyState
            title="No change requests"
            detail="None of the loaded requests name this environment."
          />
        ) : (
          localRequests.map((request) => (
            <ChangeRequestReview key={request.id} request={request} />
          ))
        )
      ) : null}
      {tab === "config" ? (
        <PropertyCard
          label="Config keys"
          value={detail.configKeys || "none recorded"}
        />
      ) : null}
      {tab === "settings" ? (
        <div className="conduit-kpi">
          <PropertyCard
            label="Generation"
            value={detail.generation || "not recorded"}
          />
          <PropertyCard
            label="Connection"
            value={detail.connection || "not recorded"}
          />
          <PropertyCard
            label="Maintenance window"
            value={detail.maintenance || "not recorded"}
          />
        </div>
      ) : null}
    </div>
  );
}

export function FailedPlanDetail({ plan }: { plan: PlanCard }) {
  const tasks = plan.execution?.tasks ?? [];
  const received = Boolean(plan.lease) || tasks.length > 0;
  const output = [
    plan.failure,
    ...tasks.map((task) => task.agentOutput || task.failure || ""),
  ]
    .filter(
      (line): line is string => typeof line === "string" && line.length > 0,
    )
    .join("\n");
  return (
    <div>
      <PageHeader
        title={plan.id}
        detail="Issued, received, and failed come from the plan status, lease, and execution tasks."
      />
      <ActivityTimeline
        items={[
          { id: "issued", text: "Plan issued" },
          {
            id: "received",
            text: received
              ? "Worker received the plan"
              : "No receipt is on this plan",
          },
          {
            id: "failed",
            text: plan.failure
              ? `Failed. ${plan.failure}`
              : "Failed. The plan has no failure text.",
          },
        ]}
      />
      <TaskList
        tasks={tasks.map((task, index) => ({
          id: String(index),
          label: task.phase || "task",
          status: task.status || "",
        }))}
      />
      <OutputPane text={output} />
    </div>
  );
}

export function ChangeRequestReview({
  request,
}: {
  request: ChangeRequestCard;
}) {
  const blocked = !request.requirementsSatisfied;
  return (
    <div>
      <PageHeader title={request.title} detail={request.status} />
      <ConstraintRow
        label="Approval"
        passed={!blocked}
        reason={
          blocked
            ? rule("approve_plan")
            : "Requirements on this request are satisfied."
        }
      />
      <DiffPane before={request.diffBefore} after={request.diffAfter} />
      <ReviewHistory
        items={request.reviews.map((review, index) => ({
          id: `${request.id}-${index}`,
          text: `${review.decision || "review"}${review.rationale ? `: ${review.rationale}` : ""}`,
        }))}
      />
    </div>
  );
}

export function ReleasePacket({
  product,
  version,
  channels,
  installations,
}: {
  product: string;
  version: string;
  channels: readonly string[];
  installations: number;
}) {
  return (
    <EntityCard
      title={`${product} ${version}`}
      meta={
        <StatusChip label={`${installations} installations`} intent="primary" />
      }
    >
      <p className="conduit-muted">
        {channels.join(", ") || "no channel recorded"}
      </p>
    </EntityCard>
  );
}

export function ReleaseDetail({
  release,
  installations,
}: {
  release: ProductRelease;
  installations: number;
}) {
  return (
    <div>
      <ReleasePacket
        product={release.product}
        version={release.version}
        channels={release.channels}
        installations={installations}
      />
      <KeyValue
        items={[
          { label: "Product", value: release.product },
          { label: "Version", value: release.version },
          { label: "Channels", value: release.channels.join(", ") },
          { label: "Installations", value: String(installations) },
        ]}
      />
    </div>
  );
}

export function ReleaseRollout({
  releases,
  fleet,
}: {
  releases: readonly ProductRelease[];
  fleet: readonly FleetRow[];
}) {
  if (releases.length === 0) {
    return (
      <EmptyState
        title="No releases"
        detail="The product catalog did not include release versions."
      />
    );
  }
  return (
    <DataTable
      columns={["Product", "Version", "Channels", "Installations"]}
      rows={releases.map((release) => [
        release.product,
        release.version,
        release.channels.join(", ") || "none recorded",
        String(fleet.filter((row) => row.product === release.product).length),
      ])}
    />
  );
}

export function PromotionReview({
  promotions,
}: {
  promotions: readonly PromotionEdge[];
}) {
  const [selected, setSelected] = useState(promotions[0]?.id ?? "");
  const nodes = promotions.map((edge) => ({
    id: edge.id,
    label: `${edge.fromChannel} → ${edge.toChannel}`,
  }));
  const current = promotions.find((edge) => edge.id === selected) ?? null;
  if (promotions.length === 0) {
    return (
      <EmptyState
        title="No promotions"
        detail="GET /v1/promotions returned none."
      />
    );
  }
  const columns = [
    {
      id: "from",
      title: "From",
      cards: promotions.map((edge) => ({
        id: `${edge.id}-from`,
        title: edge.fromChannel,
        detail: edge.product,
      })),
    },
    {
      id: "to",
      title: "To",
      cards: promotions.map((edge) => ({
        id: `${edge.id}-to`,
        title: edge.toChannel,
        detail: edge.status || "recorded",
      })),
    },
  ];
  return (
    <div>
      <GraphView
        nodes={promotions.flatMap((edge) => [
          { id: `${edge.id}-from`, label: edge.fromChannel },
          { id: `${edge.id}-to`, label: edge.toChannel },
        ])}
        edges={promotions.map((edge) => ({
          from: `${edge.id}-from`,
          to: `${edge.id}-to`,
        }))}
        selected={selected}
        onSelect={setSelected}
      />
      <TargetBoard columns={columns} />
      <PipelineGraph nodes={nodes} selected={selected} onSelect={setSelected} />
      {current ? (
        <div>
          <ArtifactLineage
            source={current.fromChannel}
            artifact={current.version}
            plan={current.toChannel}
          />
          <PropertyCard
            label="Selected transition"
            value={`${current.product} ${current.version} ${current.fromChannel} to ${current.toChannel}`}
          />
          <ConstraintRow
            label={current.status || "Promotion"}
            passed={current.reason.length === 0 && current.status !== "BLOCKED"}
            reason={current.reason || "This record did not include a blocker."}
          />
        </div>
      ) : null}
    </div>
  );
}

export function BundlePreview({ run }: { run: PackagingRun | null }) {
  const signed = Boolean(run?.artifactDigest);
  return (
    <div>
      <PageHeader
        title="Bundle preview"
        detail="The preview is the artifact digest. This route does not return file contents."
      />
      <PropertyCard label="Artifact" value={run?.artifactDigest || "none"} />
      <ConstraintRow
        label="Download"
        passed={signed}
        reason={rule("download_bundle")}
      />
      {signed ? (
        <DisabledWithReason
          reason="The digest is recorded. This console has no download route."
          label="Download"
        />
      ) : (
        <DisabledWithReason reason={rule("download_bundle")} label="Download" />
      )}
    </div>
  );
}

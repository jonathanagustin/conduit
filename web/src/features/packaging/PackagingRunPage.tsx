import { Card, Tag } from "@blueprintjs/core";
import type { ReactNode } from "react";
import { useParams } from "react-router-dom";

import {
  usePackagingRunDetail,
  type PackagingRunDetail as PackagingRunRecord,
  type Transport,
} from "../../api/packaging-run.js";
import {
  ConduitEntityTitle,
  DigestPill,
  MetadataGrid,
  PipelineStepper,
  ViewStateBoundary,
  type MetadataGridItem,
  type PipelineStep,
  type PipelineStepState,
} from "../../components/index.js";

function RunTitleHeading({
  children,
  className,
}: {
  readonly children?: ReactNode;
  readonly className?: string;
}) {
  return <h1 className={className}>{children}</h1>;
}

export function derivePackagingSteps(
  detail: PackagingRunRecord,
): PipelineStep[] {
  const phase = detail.phase.toLowerCase();
  const outcome = detail.outcome.toUpperCase();
  const isRejected = outcome === "REJECTED";
  const isFailed = outcome === "FAILED";

  const stageOrder = [
    "assess",
    "build",
    "verify",
    "review",
    "handoff",
  ] as const;
  const currentStageIndex = stageOrder.indexOf(
    phase as (typeof stageOrder)[number],
  );

  return stageOrder.map((stage, idx): PipelineStep => {
    let state: PipelineStepState;

    if (stage === "handoff" && detail.handoffReconciled) {
      state = "done";
    } else if (currentStageIndex === -1) {
      state = idx === 0 ? "active" : "waiting";
    } else if (idx < currentStageIndex) {
      state = "done";
    } else if (idx === currentStageIndex) {
      if (isRejected) {
        state = "failed";
      } else if (isFailed) {
        state = "failed";
      } else {
        state = "active";
      }
    } else {
      state = isRejected || isFailed ? "blocked" : "waiting";
    }

    const labels: Record<(typeof stageOrder)[number], string> = {
      assess: "Assess",
      build: "Build",
      verify: "Verify",
      review: "Review",
      handoff: "Handoff",
    };

    return {
      id: stage,
      label: labels[stage],
      state,
      detail: idx === currentStageIndex ? detail.outcome : undefined,
    };
  });
}

export function PackagingRunDetailView({
  detail,
}: {
  detail: PackagingRunRecord;
}) {
  const steps = derivePackagingSteps(detail);

  const outcomeUpper = detail.outcome.toUpperCase();
  const outcomeIntent =
    outcomeUpper === "ACCEPTED" || outcomeUpper === "SUCCESS"
      ? "success"
      : outcomeUpper === "REJECTED" || outcomeUpper === "FAILED"
        ? "danger"
        : "warning";

  const metadataItems: MetadataGridItem[] = [
    { label: "Run", value: detail.runId, mono: true },
    { label: "Phase", value: detail.phase },
    { label: "Outcome", value: detail.outcome },
    { label: "Configured", value: detail.configured ? "true" : "false" },
    { label: "Authority", value: detail.authority },
    { label: "Plan authority", value: detail.planAuthority },
    { label: "Approval authority", value: detail.approvalAuthority },
    { label: "Plan", value: detail.planId, mono: true },
    { label: "Tier", value: detail.tier },
    { label: "Reason", value: detail.reason },
    {
      label: "Source digest",
      value: <DigestPill value={detail.sourceDigest} label="sha256" />,
      mono: true,
    },
    {
      label: "Artifact digest",
      value: <DigestPill value={detail.artifactDigest} label="sha256" />,
      mono: true,
    },
    { label: "Proposal revision", value: detail.proposalRevision },
    { label: "Target plan", value: detail.targetPlanRef },
    {
      label: "Handoff reconciled",
      value: detail.handoffReconciled ? "true" : "false",
    },
    { label: "Last review", value: detail.lastReview },
    { label: "Duplicate reviews", value: String(detail.duplicateReviews) },
  ];

  return (
    <section className="cnd-packaging-run" data-testid="packaging-run-detail">
      <Card style={{ marginBottom: 16 }}>
        <ConduitEntityTitle
          title={`Packaging run ${detail.runId}`}
          subtitle={`Plan: ${detail.planId} · Authority: ${detail.authority}`}
          tags={
            <>
              <Tag minimal intent={outcomeIntent}>
                {detail.outcome}
              </Tag>
              <Tag minimal>{detail.tier}</Tag>
              <Tag minimal>{detail.phase}</Tag>
            </>
          }
          heading={RunTitleHeading}
        />
      </Card>

      <div style={{ marginBottom: 16 }}>
        <PipelineStepper steps={steps} ariaLabel="Packaging pipeline stages" />
      </div>

      <MetadataGrid items={metadataItems} />
    </section>
  );
}

export function PackagingRunPage({ transport }: { transport?: Transport }) {
  const { runId = "" } = useParams();
  const state = usePackagingRunDetail(runId, transport);

  if (state.status === "loading") {
    return (
      <ViewStateBoundary
        state="loading"
        testId="packaging-run-loading"
        loadingLabel="Loading packaging run"
      />
    );
  }

  if (state.status === "error") {
    return (
      <ViewStateBoundary
        state="disconnected"
        testId="packaging-run-error"
        title="Packaging run error"
        description={state.error.message}
      />
    );
  }

  return <PackagingRunDetailView detail={state.detail} />;
}

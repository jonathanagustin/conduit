import { useParams } from "react-router-dom";

import {
  usePackagingRunDetail,
  type PackagingRunDetail as PackagingRunRecord,
  type Transport,
} from "../../api/packaging-run.js";

type Row = readonly [string, string];

export function PackagingRunDetailView({ detail }: { detail: PackagingRunRecord }) {
  const rows: Row[] = [
    ["Run", detail.runId],
    ["Phase", detail.phase],
    ["Outcome", detail.outcome],
    ["Configured", detail.configured ? "true" : "false"],
    ["Authority", detail.authority],
    ["Plan authority", detail.planAuthority],
    ["Approval authority", detail.approvalAuthority],
    ["Plan", detail.planId],
    ["Tier", detail.tier],
    ["Reason", detail.reason],
    ["Source digest", detail.sourceDigest],
    ["Artifact digest", detail.artifactDigest],
    ["Proposal revision", detail.proposalRevision],
    ["Target plan", detail.targetPlanRef],
    ["Handoff reconciled", detail.handoffReconciled ? "true" : "false"],
    ["Last review", detail.lastReview],
    ["Duplicate reviews", String(detail.duplicateReviews)],
  ];
  return (
    <section data-testid="packaging-run-detail">
      <h1>Packaging run {detail.runId}</h1>
      <dl>
        {rows.map(([label, value]) => (
          <div key={label}>
            <dt>{label}</dt>
            <dd>{value}</dd>
          </div>
        ))}
      </dl>
    </section>
  );
}

export function PackagingRunPage({ transport }: { transport?: Transport }) {
  const { runId = "" } = useParams();
  const state = usePackagingRunDetail(runId, transport);
  if (state.status === "loading") {
    return <p data-testid="packaging-run-loading">Loading packaging run</p>;
  }
  if (state.status === "error") {
    return (
      <p role="alert" data-testid="packaging-run-error">
        {state.error.message}
      </p>
    );
  }
  return <PackagingRunDetailView detail={state.detail} />;
}

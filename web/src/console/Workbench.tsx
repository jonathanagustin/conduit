"use client";

import { useAui } from "@assistant-ui/react";
import { useMemo, useState } from "react";

import { Thread } from "@/components/assistant-ui/elements/thread.aui";
import { AcpPanels } from "@/src/embed/acp-panels";
import { ComposerCommands } from "@/src/embed/composer-commands";
import { ThreadWelcome } from "@/src/embed/thread-welcome";
import { ActionButton, AppShell, DisabledWithReason, EmptyState, ErrorState, KpiStrip, PageHeader, SidebarNav, SplitListDetail, StatCard, StatusTag } from "./components/base";
import {
  ApprovalDetail,
  ApprovalInbox,
  BundlePreview,
  ChangeRequestCard,
  ChangeRequestReview,
  EnvironmentCard,
  EnvironmentPage,
  FailedPlanDetail,
  FleetTable,
  PackagingRunDetail,
  PackagingRunList,
  PlanBoard,
  PromotionReview,
  ReleaseRollout,
  PlanDetail,
  PlanTable,
} from "./components/domain";
import { useConsoleData } from "./data/useConsoleData";
import { runGate } from "./logic";
import "./theme/tokens.css";

const NAV = [
  { id: "overview", label: "Overview" },
  { id: "environments", label: "Environments" },
  { id: "fleet", label: "Fleet" },
  { id: "products", label: "Software catalog" },
  { id: "packaging", label: "Packaging run" },
  { id: "plans", label: "Plans & approvals" },
  { id: "changes", label: "Change requests" },
  { id: "notifications", label: "Notifications" },
  { id: "audit", label: "Audit" },
] as const;

type ViewId = (typeof NAV)[number]["id"];

const THREAD_COMPONENTS = {
  ViewportHeader: AcpPanels,
  ComposerExtra: ComposerCommands,
  Welcome: ThreadWelcome,
};

export function Workbench({ dark }: { dark: boolean }) {
  const aui = useAui();
  const [view, setView] = useState<ViewId>("overview");
  const [runId, setRunId] = useState("sample-app-1");
  const [planId, setPlanId] = useState("");
  const [environmentId, setEnvironmentId] = useState("");
  const [requestId, setRequestId] = useState("");
  const [filter, setFilter] = useState<"inbox" | "mine" | "all">("inbox");
  const [rationale, setRationale] = useState("Reviewed from the workbench.");
  const data = useConsoleData(view === "packaging" || view === "overview" || view === "audit" ? runId : "");
  const selected = data.plans.find((plan) => plan.id === planId) ?? null;
  const inFlight = data.run?.configured && data.run.phase !== "complete" ? 1 : 0;
  const pending = data.plans.filter((plan) => plan.status === "AWAITING_APPROVAL").length;
  const inSync = data.environments.filter((row) => row.reported).length;
  const blockedRequest = data.requests.find((request) => request.id === selected?.changeRequestId);
  const scope = `${runId}${selected ? ` · ${selected.id}` : ""}`;

  const runs = useMemo(
    () => [{ id: runId, phase: data.run?.phase || (data.run?.configured === false ? "off" : "…") }],
    [runId, data.run],
  );

  return (
    <AppShell
      theme={dark ? "dark" : "light"}
      kpi={
        <KpiStrip>
          <StatCard label="Environments in sync" value={`${inSync}/${data.environments.length}`} />
          <StatCard label="Packaging runs in flight" value={String(inFlight)} />
          <StatCard label="Pending approvals" value={String(pending)} />
          <StatCard label="Spokes online" value="—" detail="Spoke liveness is not on the catalog route." />
          <StatCard label="Hub" value={data.live ? "LIVE" : data.live === false ? "Down" : "…"} />
        </KpiStrip>
      }
      sidebar={<SidebarNav items={NAV} current={view} onSelect={(id) => setView(id as ViewId)} />}
      rail={
        <>
          <div className="conduit-row" style={{ padding: 8 }}>
            <StatusTag>Scoped to {scope}</StatusTag>
            <button
              type="button"
              onClick={() => {
                aui.composer.setText(`show packaging run ${runId}${selected ? ` and show plan ${selected.id}` : ""}`);
              }}
            >
              Ask about this scope
            </button>
          </div>
          <div style={{ position: "relative", minHeight: 0, flex: 1 }}>
            <Thread components={THREAD_COMPONENTS} />
          </div>
        </>
      }
    >
      {data.error ? <ErrorState title="Hub is not answering" detail={data.error} /> : null}
      {view === "overview" ? (
        <div>
          <PageHeader title="Overview" detail="Counts come from the Hub catalog and plan list." />
          <StatusTag gate={data.run ? runGate(data.run) : "CAUTION"}>{data.run ? runGate(data.run) : "CAUTION"}</StatusTag>
          {data.environments.map((row) => (
            <EnvironmentCard key={row.id} id={row.id} reported={row.reported} />
          ))}
          {data.products.map((product) => (
            <button key={product.name} type="button" onClick={() => setView("products")}>
              {product.name}
            </button>
          ))}
        </div>
      ) : null}
      {view === "environments" ? (
        <div>
          <PageHeader title="Environments" detail="GET /v1/environments" />
          {data.environments.length === 0 ? <EmptyState title="No environments" detail="The catalog is empty or the Hub refused the read." /> : null}
          {data.environments.map((row) => (
            <button
              key={row.id}
              type="button"
              onClick={() => {
                setEnvironmentId(row.id);
                void data.loadEnvironment(row.id);
              }}
            >
              <EnvironmentCard id={row.id} reported={row.reported} />
            </button>
          ))}
          {data.environmentDetail && data.environmentDetail.id === environmentId ? (
            <EnvironmentPage
              detail={data.environmentDetail}
              fleet={data.fleet}
              plans={data.plans}
              requests={data.requests}
              onSelectPlan={(id) => {
                setPlanId(id);
                setView("plans");
              }}
            />
          ) : null}
        </div>
      ) : null}
      {view === "fleet" ? (
        <div>
          <PageHeader title="Fleet" detail="GET /v1/fleet/installations" />
          <FleetTable rows={data.fleet} />
        </div>
      ) : null}
      {view === "products" ? (
        <div>
          <PageHeader title="Software catalog" detail="GET /v1/products" />
          {data.products.length === 0 ? <EmptyState title="No products" detail="The catalog is empty." /> : null}
          <ul>
            {data.products.map((product) => (
              <li key={product.name}>{product.name}</li>
            ))}
          </ul>
          <ReleaseRollout releases={data.releases} fleet={data.fleet} />
          <PromotionReview promotions={data.promotions} />
        </div>
      ) : null}
      {view === "packaging" ? (
        <div>
          <PageHeader title="Packaging run" detail="Index is GET /v1/packaging-runs. The selected run is Connect GetPackagingRun. Events, cancel, and rerun use the matching REST routes." />
          <label>
            Run id
            <input aria-label="Packaging run id" value={runId} onChange={(event) => setRunId(event.target.value)} />
          </label>
          <SplitListDetail
            list={
              <PackagingRunList
                runs={
                  data.runs.length > 0
                    ? data.runs.map((run) => ({ id: run.runId || runId, phase: run.phase || "" }))
                    : runs
                }
                selected={runId}
                onSelect={setRunId}
              />
            }
            detail={
              <div>
                <PackagingRunDetail run={data.run} error={data.runError} onReload={() => void data.loadRun(runId)} />
                <ActionButton
                  intent="danger"
                  disabled={!data.run?.sourceDigest || !data.run.targetPlanRef}
                  onClick={() => {
                    if (!data.run) return;
                    void data
                      .cancelRun(runId, {
                        decisionId: `decision-${Date.now()}`,
                        sourceDigest: data.run.sourceDigest ?? "",
                        proposalRevision: data.run.proposalRevision ?? "",
                        targetPlanRef: data.run.targetPlanRef ?? "",
                      })
                      .then(() => data.loadRun(runId));
                  }}
                >
                  Cancel run
                </ActionButton>
                <DisabledWithReason
                  reason="Rerun needs a template version and tool versions, and the read model does not carry them."
                  label="Rerun"
                />
                <BundlePreview run={data.run} />
                <ul aria-label="Packaging events">
                  {data.events.map((event) => (
                    <li key={event.sequence}>
                      {event.step} {event.outcome} {event.reason}
                    </li>
                  ))}
                </ul>
              </div>
            }
          />
        </div>
      ) : null}
      {view === "plans" ? (
        <div>
          <PageHeader title="Plans and approvals" detail="Approve and reject use the existing governance routes." />
          <PlanBoard
            plans={data.plans}
            onSelect={setPlanId}
            renderActions={(plan) =>
              plan.status === "AWAITING_APPROVAL" ? (
                <button type="button" onClick={() => setPlanId(plan.id)}>Review</button>
              ) : null
            }
          />
          <PlanTable plans={data.plans} onSelect={setPlanId} />
          <SplitListDetail
            list={
              <ApprovalInbox
                filter={filter}
                onFilter={setFilter}
                plans={data.plans}
                selected={planId}
                onSelect={setPlanId}
              />
            }
            detail={
              <div>
                <PlanDetail plan={selected} />
                {selected?.status === "FAILED" ? <FailedPlanDetail plan={selected} /> : null}
                <ApprovalDetail
                  plan={selected?.status === "AWAITING_APPROVAL" ? selected : null}
                  blocked={blockedRequest?.requirementsSatisfied === false}
                  rationale={rationale}
                  onRationale={setRationale}
                  onApprove={() => selected && void data.decide(selected, "approve", rationale).catch((reason: unknown) => data.refresh())}
                  onReject={() => selected && void data.decide(selected, "reject", rationale)}
                />
              </div>
            }
          />
        </div>
      ) : null}
      {view === "changes" ? (
        <div>
          <PageHeader title="Change requests" detail="GET /v1/change-requests" />
          {data.requests.length === 0 ? <EmptyState title="No change requests" detail="The Hub returned none." /> : null}
          {data.requests.map((request) => (
            <button key={request.id} type="button" onClick={() => setRequestId(request.id)}>
              <ChangeRequestCard request={request} />
            </button>
          ))}
          {data.requests.find((request) => request.id === requestId) ? (
            <ChangeRequestReview request={data.requests.find((request) => request.id === requestId)!} />
          ) : null}
        </div>
      ) : null}
      {view === "audit" ? (
        <div>
          <PageHeader title="Audit" detail="Recorded packaging step events. An empty list means the Hub returned none." />
          {data.events.length === 0 ? <EmptyState title="No events" detail="Select a packaging run that has a step journal." /> : null}
          <ul aria-label="Audit events">
            {data.events.map((event) => (
              <li key={event.sequence}>{event.at} {event.step} {event.outcome} {event.reason}</li>
            ))}
          </ul>
        </div>
      ) : null}
      {view === "notifications" ? (
        <EmptyState
          title="Notifications stay on the Hub inbox"
          detail="The old inbox called GET /v1/notifications/inbox. It is not mounted here yet because the preference editor is a separate surface. The route is unchanged."
        />
      ) : null}
    </AppShell>
  );
}

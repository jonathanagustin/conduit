"use client";

import { InputGroup, Tag } from "@blueprintjs/core";
import { useAui } from "@assistant-ui/react";
import { useMemo, useState } from "react";
import "@fontsource/ibm-plex-mono/400.css";

import { Thread } from "@/components/assistant-ui/elements/thread.aui";
import { AcpPanels } from "@/src/embed/acp-panels";
import { ActionButton, AppShell, DataTable, DisabledWithReason, EmptyState, ErrorState, NavTree, PageHeader, SidebarNav, SplitListDetail, StatusTag } from "./components/base";
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
import { EXAMPLE, EXAMPLE_ENVIRONMENTS, EXAMPLE_PROFILES, EXAMPLE_SAVED_VIEWS, EXAMPLE_SPOKES } from "./examples";
import { runGate } from "./logic";
import "./theme/tokens.css";

function RailWelcome() {
  return <p className="conduit-muted">Ask about the record open in the center.</p>;
}

const RAIL_THREAD = {
  ViewportHeader: AcpPanels,
  Welcome: RailWelcome,
};

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

export function Workbench({ dark }: { dark: boolean }) {
  void dark;
  const aui = useAui();
  const [view, setView] = useState<ViewId>("overview");
  const [openTabs, setOpenTabs] = useState<ViewId[]>(["overview"]);
  const [filterText, setFilterText] = useState("");
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
  const openView = (id: ViewId) => {
    setView(id);
    setOpenTabs((current) => (current.includes(id) ? current : [...current, id]));
  };
  const closeTab = (id: ViewId) => {
    setOpenTabs((current) => {
      const next = current.filter((tab) => tab !== id);
      const fallback = next[next.length - 1] ?? "overview";
      if (view === id) setView(next.length === 0 ? "overview" : fallback);
      return next.length === 0 ? ["overview"] : next;
    });
  };
  const query = filterText.trim().toLowerCase();
  const visibleEnvironments = data.environments.filter((row) => row.id.toLowerCase().includes(query));

  return (
    <AppShell
      kpi={
        <>
          <span><strong className="conduit-mono">{inSync}/{data.environments.length}</strong> in sync</span>
          <span><strong className="conduit-mono">{inFlight}</strong> runs</span>
          <span><strong className="conduit-mono">{pending}</strong> approvals</span>
          <Tag round intent={data.live ? "success" : "danger"} minimal>{data.live ? "LIVE" : data.live === false ? "Down" : "…"}</Tag>
        </>
      }
      toolbar={
        <InputGroup
          leftIcon="search"
          placeholder="Filter this view"
          value={filterText}
          onChange={(event) => setFilterText(event.currentTarget.value)}
          aria-label="Filter this view"
        />
      }
      sidebar={
        <>
          <p className="conduit-sec">Navigation</p>
          <SidebarNav items={NAV} current={view} onSelect={(id) => openView(id as ViewId)} />
          <p className="conduit-sec">Work profiles</p>
          <NavTree nodes={EXAMPLE_PROFILES.map((row) => ({ id: row.id, label: `${row.label} · ${EXAMPLE}` }))} onSelect={() => openView("overview")} />
          <p className="conduit-sec">Environments</p>
          <NavTree
            nodes={[
              ...visibleEnvironments.map((row) => ({ id: row.id, label: row.id })),
              ...EXAMPLE_ENVIRONMENTS.map((row) => ({ id: row.id, label: `${row.id} · ${EXAMPLE}` })),
            ]}
            onSelect={(id) => {
              openView("environments");
              if (!id.startsWith("example-")) {
                setEnvironmentId(id);
                void data.loadEnvironment(id);
              }
            }}
          />
          <p className="conduit-sec">Spokes</p>
          <NavTree nodes={EXAMPLE_SPOKES.map((row) => ({ id: row.id, label: `${row.label} · ${EXAMPLE}` }))} onSelect={() => openView("fleet")} />
          <p className="conduit-sec">Saved views</p>
          {EXAMPLE_SAVED_VIEWS.map((row) => (
            <button key={row.id} type="button" onClick={() => openView(row.view)}>
              {row.label}
            </button>
          ))}
        </>
      }
      tabs={openTabs.map((id) => {
        const label = NAV.find((item) => item.id === id)?.label ?? id;
        return (
          <span key={id} className={id === view ? "conduit-tab is-active" : "conduit-tab"}>
            <button type="button" onClick={() => setView(id)}>{label}</button>
            <button type="button" aria-label={`Close ${label}`} onClick={() => closeTab(id)}>×</button>
          </span>
        );
      })}
      rail={
        <>
          <div className="conduit-row" style={{ padding: 8 }}>
            <StatusTag>Scoped to {scope}</StatusTag>
            <ActionButton
              emphasis="minimal"
              onClick={() => {
                aui.composer.setText(`show packaging run ${runId}${selected ? ` and show plan ${selected.id}` : ""}`);
              }}
            >
              Ask about this scope
            </ActionButton>
          </div>
          <div style={{ position: "relative", minHeight: 0, flex: 1 }}>
            <Thread components={RAIL_THREAD} />
          </div>
        </>
      }
    >
      {data.error ? <ErrorState title="Hub is not answering" detail={data.error} /> : null}
      {view === "overview" ? (
        <div>
          <PageHeader title="Overview" detail="Hub catalog rows, plus rows marked Example." />
          <StatusTag gate={data.run ? runGate(data.run) : "CAUTION"}>{data.run ? runGate(data.run) : "CAUTION"}</StatusTag>
          <DataTable
            density="compact"
            columns={["Environment", "Source", "Report"]}
            rows={[
              ...visibleEnvironments.map((row) => [row.id, "Hub", row.reported ? "reported" : "no report"]),
              ...EXAMPLE_ENVIRONMENTS.filter((row) => row.id.includes(query)).map((row) => [
                <span key={row.id} className="conduit-mono">{row.id}</span>,
                EXAMPLE,
                "reported",
              ]),
            ]}
          />
          {data.products.map((product) => (
            <button key={product.name} type="button" onClick={() => openView("products")}>
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
                openView("plans");
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
                <DataTable
                  density="compact"
                  columns={["Artifact", "Source", "Digest"]}
                  rows={[
                    [
                      <span key="hub" className="conduit-mono">{data.run?.runId || runId}</span>,
                      "Hub",
                      data.run?.artifactDigest || "none recorded",
                    ],
                    [
                      <span key="example" className="conduit-mono">example-bundle.tar</span>,
                      EXAMPLE,
                      "sha256:example",
                    ],
                  ]}
                />
                <DataTable
                  density="compact"
                  columns={["Step", "Outcome", "Reason"]}
                  rows={data.events.map((event) => [event.step, event.outcome, event.reason])}
                />
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
          {data.events.length === 0 ? <EmptyState title="No events" detail="Select a packaging run that has a step journal." /> : (
            <DataTable
              density="compact"
              columns={["When", "Step", "Outcome", "Reason"]}
              rows={data.events.map((event) => [event.at, event.step, event.outcome, event.reason])}
            />
          )}
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

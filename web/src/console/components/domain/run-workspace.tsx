import { useState } from "react";
import type { ReactNode } from "react";

import type { PackagingRun, StepEvent } from "../../data/hub";
import { DataTable, GraphView, KeyValue, RangeTimeline, SegmentedTabs, StatusChip } from "../base";

const TABS = [
  { id: "overview", label: "Overview" },
  { id: "jobs", label: "Jobs" },
  { id: "log", label: "Log" },
  { id: "schedule", label: "Schedule" },
] as const;

export function RunWorkspace({
  run,
  events,
  children,
}: {
  run: PackagingRun | null;
  events: readonly StepEvent[];
  children: ReactNode;
}) {
  const [tab, setTab] = useState<(typeof TABS)[number]["id"]>("overview");
  const jobs = events.length > 0
    ? events.map((event) => ({ id: String(event.sequence), name: event.step, status: event.outcome || "recorded" }))
    : [
        { id: "fetch", name: "Example fetch", status: "done" },
        { id: "scan", name: "Example scan", status: "current" },
        { id: "package", name: "Example package", status: "waiting" },
      ];
  return (
    <div>
      <SegmentedTabs tabs={TABS} current={tab} onSelect={(id) => setTab(id as (typeof TABS)[number]["id"])} />
      {tab === "overview" ? children : null}
      {tab === "jobs" ? (
        <div>
          <RangeTimeline
            rows={jobs.map((job, index) => ({
              id: job.id,
              label: job.name,
              bars: [{ id: job.id, label: job.status, start: index * 0.12, end: Math.min(0.95, 0.2 + index * 0.12), tone: job.status === "done" ? "ok" : "info" }],
            }))}
          />
          <DataTable
            density="compact"
            columns={["Job", "Status", "Source"]}
            rows={jobs.map((job) => [job.name, job.status, events.length > 0 ? "Hub" : "Example"])}
          />
        </div>
      ) : null}
      {tab === "log" ? (
        <DataTable
          density="compact"
          columns={["Step", "Level", "Message", "Source"]}
          rows={(events.length > 0 ? events : [{ sequence: 1, at: "", step: "scan", outcome: "INFO", reason: "Example finding: no signed digest yet" }]).map((event) => [
            event.step,
            <StatusChip key={event.step} label={event.outcome || "INFO"} intent={event.outcome === "waiting" ? "warning" : "primary"} />,
            event.reason || "—",
            events.length > 0 ? "Hub" : "Example",
          ])}
        />
      ) : null}
      {tab === "schedule" ? (
        <KeyValue
          items={[
            { label: "Phase", value: run?.phase || "unknown" },
            { label: "Schedule", value: "Example · weekdays 02:00 UTC" },
            { label: "Source", value: "Example. The run payload has no schedule." },
          ]}
        />
      ) : null}
    </div>
  );
}

export function ArtifactLineage({ source, artifact, plan }: { source: string; artifact: string; plan: string }) {
  return (
    <div>
      <GraphView
        nodes={[
          { id: "source", label: source || "source" },
          { id: "artifact", label: artifact || "artifact" },
          { id: "plan", label: plan || "plan" },
        ]}
        edges={[{ from: "source", to: "artifact" }, { from: "artifact", to: "plan" }]}
      />
      <KeyValue
        items={[
          { label: "Source", value: source || "none recorded" },
          { label: "Artifact", value: artifact || "none recorded" },
          { label: "Plan", value: plan || "none recorded" },
        ]}
      />
    </div>
  );
}

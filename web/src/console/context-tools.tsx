"use client";

import type { ReactNode } from "react";
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

function ToolCard({
  title,
  label,
  children,
}: {
  title: string;
  label?: string;
  children: ReactNode;
}) {
  return (
    <div className="space-y-1.5 rounded-[6px] border border-[#1f2a35] bg-[#10161c] p-2.5 text-[11.5px] text-[#dce4ee]">
      <div className="flex items-center justify-between gap-2">
        <span className="font-medium">{title}</span>
        <span className="rounded border border-[#1f4737] bg-[#10241d] px-1.5 py-0.5 text-[10px] text-[#34d399] tabular-nums">
          {label || "No model call"}
        </span>
      </div>
      {children}
    </div>
  );
}

const OntologyQueryTool = makeAssistantToolUI<
  Record<string, unknown>,
  Record<string, unknown>
>({
  toolName: "query_ontology_objects",
  render: ({ result }) => {
    const fields = record(result);
    const entities = Array.isArray(fields["entities"])
      ? fields["entities"]
      : [];
    return (
      <ToolCard
        title="Query ontology"
        label={
          typeof fields["label"] === "string" ? fields["label"] : undefined
        }
      >
        <div>
          R:{String(fields["readOps"] ?? 0)} W:{String(fields["writeOps"] ?? 0)}{" "}
          · {String(fields["matchedCount"] ?? 0)} matched
        </div>
        {typeof fields["error"] === "string" ? (
          <div>{fields["error"]}</div>
        ) : null}
        <div className="mt-1 flex flex-wrap gap-1">
          {entities.map((entity) => {
            const item = record(entity);
            return (
              <span
                key={String(item["id"])}
                className="rounded border border-[#28405c] px-1.5 text-[10.5px] text-[#7cb4ff]"
              >
                {String(item["label"] || item["id"] || "")}
              </span>
            );
          })}
        </div>
      </ToolCard>
    );
  },
});

const GovernedActionTool = makeAssistantToolUI<
  Record<string, unknown>,
  Record<string, unknown>
>({
  toolName: "propose_governed_action",
  render: ({ result }) => {
    const fields = record(result);
    const constraints = Array.isArray(fields["constraints"])
      ? fields["constraints"]
      : [];
    return (
      <ToolCard
        title="Governed action"
        label={
          typeof fields["label"] === "string" ? fields["label"] : undefined
        }
      >
        <div>{fields["allowed"] === true ? "ALLOW" : "DENY"}</div>
        {typeof fields["reason"] === "string" ? (
          <div>{fields["reason"]}</div>
        ) : null}
        {constraints.map((row) => {
          const item = record(row);
          return (
            <div key={String(item["id"])}>
              {String(item["status"] || "")} {String(item["id"] || "")} —{" "}
              {String(item["reason"] || "")}
            </div>
          );
        })}
        {typeof fields["changeRequestId"] === "string" &&
        fields["changeRequestId"] !== "" ? (
          <div>
            Change request {fields["changeRequestId"]}{" "}
            {String(fields["changeRequestStatus"] || "")}
          </div>
        ) : null}
        {typeof fields["decisionAuditEventId"] === "string" ? (
          <div>Rule audit {fields["decisionAuditEventId"]}</div>
        ) : null}
      </ToolCard>
    );
  },
});

const ChangeDiffTool = makeAssistantToolUI<
  Record<string, unknown>,
  Record<string, unknown>
>({
  toolName: "review_change_diff",
  render: ({ result }) => {
    const fields = record(result);
    const hunks = Array.isArray(fields["hunks"]) ? fields["hunks"] : [];
    return (
      <ToolCard
        title="Change diff"
        label={
          typeof fields["label"] === "string" ? fields["label"] : undefined
        }
      >
        <div>
          {typeof fields["title"] === "string"
            ? fields["title"]
            : "Stored diff"}
        </div>
        {hunks.length === 0 ? (
          <div>This change request has no stored field diff.</div>
        ) : null}
        {hunks.map((row) => {
          const item = record(row);
          return (
            <pre
              key={String(item["path"])}
              className="mt-1 font-mono text-[10.5px] whitespace-pre-wrap"
            >
              {`- ${String(item["path"])}: ${String(item["before"] ?? "")}\n+ ${String(item["path"])}: ${String(item["after"] ?? "")}`}
            </pre>
          );
        })}
      </ToolCard>
    );
  },
});

export function ContextToolUIs() {
  return (
    <>
      <PackagingRunTool />
      <PlanTool />
      <OntologyQueryTool />
      <GovernedActionTool />
      <ChangeDiffTool />
    </>
  );
}

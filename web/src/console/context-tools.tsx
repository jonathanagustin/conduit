"use client";

import { useState, type FormEvent, type ReactNode } from "react";
import {
  makeAssistantToolUI,
  useAuiState,
  type ToolCallMessagePartComponent,
} from "@assistant-ui/react";

import { useAssistantState, useAssistantStore } from "./assistant-runtime";
import {
  effectiveResult,
  toolOutcome,
  type HubResult,
  type ToolOutcome,
  type ToolRow,
} from "./assistant-turn";
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

const TONE: Record<ToolOutcome["tone"], string> = {
  running: "border-[#28405c] bg-[#14202e] text-[#7cb4ff]",
  ok: "border-[#1f4737] bg-[#10241d] text-[#34d399]",
  refused: "border-[#5a2a2d] bg-[#2a1416] text-[#f5a3a6]",
  failed: "border-[#5a2a2d] bg-[#2a1416] text-[#f5a3a6]",
  confirm: "border-[#4f391b] bg-[#2a2012] text-[#eab854]",
  input: "border-[#4f391b] bg-[#2a2012] text-[#eab854]",
};

function shown(value: unknown): string {
  if (typeof value === "string") return value;
  if (value === null || value === undefined) return "";
  return JSON.stringify(value);
}

function Arguments({ args }: { args: Record<string, unknown> }) {
  const entries = Object.entries(args);
  if (!entries.length)
    return (
      <div data-tool-args className="text-[#8b96a5]">
        No arguments
      </div>
    );
  return (
    <dl
      data-tool-args
      className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-2 font-mono text-[10.5px]"
    >
      {entries.map(([name, value]) => (
        <div key={name} className="contents">
          <dt className="text-[#8b96a5]">{name}</dt>
          <dd className="break-all text-[#dce4ee]">{shown(value)}</dd>
        </div>
      ))}
    </dl>
  );
}

/** What the Hub returned, field by field; nothing is derived or assumed. */
function ResultDetail({ result }: { result: HubResult }) {
  const entities = Array.isArray(result["entities"]) ? result["entities"] : [];
  const constraints = Array.isArray(result["constraints"])
    ? result["constraints"]
    : [];
  const hunks = Array.isArray(result["hunks"]) ? result["hunks"] : [];
  const line = (label: string, key: string) => {
    const value = result[key];
    return typeof value === "string" && value !== "" ? (
      <div className="text-[#8b96a5]">
        {label} <span className="font-mono text-white">{value}</span>
      </div>
    ) : null;
  };
  return (
    <div data-tool-result className="space-y-1">
      <div className="text-[#8b96a5]">
        R:{shown(result["readOps"] ?? 0)} W:{shown(result["writeOps"] ?? 0)}
        {typeof result["label"] === "string" ? ` · ${result["label"]}` : ""}
      </div>
      {typeof result["title"] === "string" ? (
        <div>{result["title"]}</div>
      ) : null}
      {entities.length ? (
        <div className="flex flex-wrap gap-1">
          {entities.map((entity) => {
            const item = record(entity);
            return (
              <span
                key={shown(item["id"])}
                className="rounded border border-[#28405c] px-1.5 text-[10.5px] text-[#7cb4ff]"
              >
                {shown(item["label"] || item["id"] || "")}
                {item["status"] ? ` · ${shown(item["status"])}` : ""}
              </span>
            );
          })}
        </div>
      ) : null}
      {constraints.map((row) => {
        const item = record(row);
        return (
          <div key={shown(item["id"])}>
            {shown(item["status"] || "")} {shown(item["id"] || "")} —{" "}
            {shown(item["reason"] || "")}
          </div>
        );
      })}
      {hunks.map((row) => {
        const item = record(row);
        return (
          <pre
            key={shown(item["path"])}
            className="font-mono text-[10.5px] whitespace-pre-wrap"
          >
            {`- ${shown(item["path"])}: ${shown(item["before"] ?? "")}\n+ ${shown(item["path"])}: ${shown(item["after"] ?? "")}`}
          </pre>
        );
      })}
      {line("Change request", "changeRequestId")}
      {line("Object", "objectId")}
      {line("State", "state")}
      {line("Rule audit", "decisionAuditEventId")}
      {line("Tool audit", "auditEventId")}
    </div>
  );
}

/**
 * The inputs a result asked for and its Continue or Confirm button. Confirm
 * sends the same tool call back with confirm set, through the store.
 */
function Elicitation({
  result,
  pending,
  onSubmit,
}: {
  result: HubResult;
  pending: boolean;
  onSubmit: (values: Record<string, string>, confirm: boolean) => void;
}) {
  const fields = Array.isArray(result["fields"]) ? result["fields"] : [];
  const confirm = result["needsConfirm"] === true;
  const [values, setValues] = useState<Record<string, string>>({});
  if (!confirm && !fields.length) return null;
  const initial = (field: Record<string, unknown>) => {
    const options = Array.isArray(field["options"]) ? field["options"] : [];
    return shown(field["value"] ?? options[0] ?? "");
  };
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const filled: Record<string, string> = {};
    for (const raw of fields) {
      const field = record(raw);
      const name = shown(field["name"] || "");
      filled[name] = values[name] ?? initial(field);
    }
    onSubmit(filled, confirm);
  };
  return (
    <form data-tool-elicit onSubmit={submit} className="space-y-1.5">
      {fields.map((raw) => {
        const field = record(raw);
        const name = shown(field["name"] || "");
        const options = Array.isArray(field["options"]) ? field["options"] : [];
        const value = values[name] ?? initial(field);
        const change = (next: string) =>
          setValues((current) => ({ ...current, [name]: next }));
        return (
          <label key={name} className="block text-[#8b96a5]">
            {shown(field["label"] || name)}
            {options.length ? (
              <select
                name={name}
                value={value}
                onChange={(event) => change(event.target.value)}
                className="kit-select"
              >
                {options.map((option) => (
                  <option key={shown(option)} value={shown(option)}>
                    {shown(option)}
                  </option>
                ))}
              </select>
            ) : (
              <input
                name={name}
                value={value}
                required={field["required"] === true}
                onChange={(event) => change(event.target.value)}
                className="kit-input"
              />
            )}
          </label>
        );
      })}
      <button
        type="submit"
        disabled={pending}
        data-tool-confirm={confirm ? "confirm" : "continue"}
        className="kit-btn kit-btn-sm kit-btn-ok"
      >
        {pending ? "Sending" : confirm ? "Confirm" : "Continue"}
      </button>
    </form>
  );
}

/**
 * One tool call of an assistant turn: the arguments the Hub ran, then what it
 * returned. A refusal shows the Hub's reason; a result that needs a person
 * shows Confirm and reports whatever the Hub answers.
 */
export const HubToolCard: ToolCallMessagePartComponent = ({
  toolCallId,
  toolName,
  args,
  result,
}) => {
  const store = useAssistantStore();
  const messageId = useAuiState((state) => state.message.id);
  const running = useAuiState(
    (state) => state.message.status?.type === "running",
  );
  const stored = useAssistantState((state) => {
    const message = state.messages.find((item) => item.id === messageId);
    return message?.role === "assistant"
      ? message.turn.tools.find((item) => item.id === toolCallId)
      : undefined;
  });
  const row: ToolRow = stored ?? {
    id: toolCallId,
    name: toolName,
    round: 0,
    argsText: "",
    args: record(args),
    ...(result === undefined ? {} : { result: record(result) }),
    followUps: [],
    pending: false,
  };
  const current = effectiveResult(row);
  const outcome: ToolOutcome =
    !current && !running
      ? { tone: "failed", summary: "No result received" }
      : toolOutcome(row);
  return (
    <div
      data-assistant-tool={row.name}
      data-tool-call-id={row.id}
      data-tool-state={outcome.tone}
      className="my-1.5 space-y-1.5 rounded-[6px] border border-[#1f2a35] bg-[#10161c] p-2.5 text-[11.5px] text-[#dce4ee]"
    >
      <div className="flex items-center justify-between gap-2">
        <span className="font-medium">
          {row.name}
          {row.round ? (
            <span className="text-[#8b96a5]"> · round {row.round}</span>
          ) : null}
        </span>
        <span
          data-tool-outcome
          role="status"
          className={`max-w-[60%] truncate rounded border px-1.5 py-0.5 text-[10px] ${TONE[outcome.tone]}`}
          title={outcome.summary}
        >
          {outcome.summary}
        </span>
      </div>
      <Arguments args={row.args} />
      {row.result ? <ResultDetail result={row.result} /> : null}
      {row.followUps.map((answer, index) => (
        <div
          key={index}
          data-tool-follow-up
          className="space-y-1 border-t border-[#1f2a35] pt-1.5"
        >
          <div className="text-[#8b96a5]">You submitted this call</div>
          <ResultDetail result={answer} />
        </div>
      ))}
      {outcome.tone === "refused" || outcome.tone === "failed" ? (
        <div data-tool-refusal className="text-[#f5a3a6]">
          {outcome.summary}
        </div>
      ) : null}
      {current ? (
        <Elicitation
          key={row.followUps.length}
          result={current}
          pending={row.pending}
          onSubmit={(values, confirm) =>
            void store.submitTool(messageId, row.id, values, confirm)
          }
        />
      ) : null}
    </div>
  );
};

/** Tool cards stay open in the rail; a collapsed group would hide a Confirm. */
export function OpenToolGroup({ children }: { children?: ReactNode }) {
  return <div data-assistant-tools>{children}</div>;
}

export function ContextToolUIs() {
  return (
    <>
      <PackagingRunTool />
      <PlanTool />
    </>
  );
}

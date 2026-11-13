import { useEffect, useRef } from "react";
import { useAuiState } from "@assistant-ui/react";

import { AgentPlan } from "@/components/assistant-ui/elements/agent-plan";
import { ContextDisplay } from "@/components/assistant-ui/elements/context-display.aui";
import { decidePermission } from "@/src/api/client";

type PlanEntry = { content: string; status: string; priority: string };

function planOf(value: unknown): PlanEntry[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((entry) => {
    if (entry === null || typeof entry !== "object") return [];
    const record = entry as { content?: unknown; status?: unknown };
    if (typeof record.content !== "string") return [];
    return [{ content: record.content, status: String(record.status ?? "pending"), priority: "medium" }];
  });
}

function PermissionRelay() {
  const decision = useAuiState((state) => {
    for (const message of state.thread.messages) {
      if (message.role !== "assistant") continue;
      for (const part of message.content) {
        if (part.type !== "tool-call" || !part.approval || part.approval.approved === undefined) continue;
        const fallback = part.approval.options?.find((option) =>
          part.approval?.approved ? String(option.kind).includes("allow") : String(option.kind).includes("reject"),
        )?.id;
        return `${part.approval.id}|${part.approval.optionId ?? fallback ?? ""}`;
      }
    }
    return "";
  });
  const posted = useRef(new Set<string>());
  useEffect(() => {
    if (decision.length === 0 || posted.current.has(decision)) return;
    const [requestId, optionId] = decision.split("|");
    const [threadId, runId] = (requestId ?? "").split("::");
    if (!requestId || !optionId || !threadId || !runId) return;
    posted.current.add(decision);
    void decidePermission({ requestId, threadId, runId, optionId }).catch(() => undefined);
  }, [decision]);
  return null;
}

export function AcpPanels() {
  const storedPlan = useAuiState((state) => {
    const messages = state.thread.messages;
    for (let index = messages.length - 1; index >= 0; index -= 1) {
      const message = messages[index];
      if (!message || message.role !== "assistant") continue;
      const plan = message.metadata?.custom?.["plan"];
      return Array.isArray(plan) ? plan : undefined;
    }
    return undefined;
  });
  const plan = planOf(storedPlan);
  const contextWindow = useAuiState((state) => {
    const messages = state.thread.messages;
    for (let index = messages.length - 1; index >= 0; index -= 1) {
      const message = messages[index];
      if (!message || message.role !== "assistant") continue;
      const window = message.metadata?.custom?.["contextWindow"];
      return typeof window === "number" ? window : 0;
    }
    return 0;
  });
  const activeIndex = plan.findIndex((entry) => entry.status !== "completed");

  return (
    <div className="mx-auto flex w-full max-w-(--thread-max-width) flex-col gap-3 px-4">
      <PermissionRelay />
      {contextWindow > 0 ? <ContextDisplay.Bar modelContextWindow={contextWindow} /> : null}
      {plan.length > 0 ? (
        <AgentPlan steps={plan.map((entry) => entry.content)} activeIndex={activeIndex === -1 ? plan.length : activeIndex} />
      ) : null}
    </div>
  );
}

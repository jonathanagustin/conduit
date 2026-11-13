import { useEffect, useRef } from "react";
import { useAui, useAuiState } from "@assistant-ui/react";

import { AgentPlan } from "@/components/assistant-ui/elements/agent-plan";
import { ContextDisplay } from "@/components/assistant-ui/elements/context-display.aui";
import { ErrorState } from "@/components/assistant-ui/elements/error-state";
import { decidePermission } from "@/src/api/client";
import { useBridgeHealth } from "@/src/embed/bridge-health";

type PlanEntry = { content: string; status: string; priority: string };

function planOf(value: unknown): PlanEntry[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((entry) => {
    if (entry === null || typeof entry !== "object") return [];
    const record = entry as { content?: unknown; status?: unknown };
    if (typeof record.content !== "string") return [];
    return [
      {
        content: record.content,
        status: String(record.status ?? "pending"),
        priority: "medium",
      },
    ];
  });
}

function PermissionRelay() {
  const decision = useAuiState((state) => {
    const messages = state.thread.messages;
    for (let index = messages.length - 1; index >= 0; index -= 1) {
      const message = messages[index];
      if (!message || message.role !== "assistant") continue;
      for (
        let partIndex = message.content.length - 1;
        partIndex >= 0;
        partIndex -= 1
      ) {
        const part = message.content[partIndex];
        if (
          !part ||
          part.type !== "tool-call" ||
          !part.approval ||
          part.approval.approved === undefined
        )
          continue;
        const fallback = part.approval.options?.find((option) =>
          part.approval?.approved
            ? String(option.kind).includes("allow")
            : String(option.kind).includes("reject"),
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
    void decidePermission({ requestId, threadId, runId, optionId }).catch(
      () => undefined,
    );
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
      if (typeof window === "number" && window > 0) return window;
    }
    return 0;
  });
  const activeIndex = plan.findIndex((entry) => entry.status !== "completed");

  return (
    <>
      <PermissionRelay />
      <DiagramReveal />
      <ChatShortcuts />
      <BridgeStatus />
      {contextWindow > 0 || plan.length > 0 ? (
        <div className="flex w-full flex-col gap-3 pb-3">
          {contextWindow > 0 ? (
            <ContextDisplay.Bar modelContextWindow={contextWindow} />
          ) : null}
          {plan.length > 0 ? (
            <AgentPlan
              steps={plan.map((entry) => entry.content)}
              activeIndex={activeIndex === -1 ? plan.length : activeIndex}
            />
          ) : null}
        </div>
      ) : null}
    </>
  );
}

// The stock thread anchors a turn at the top and pins the composer, so a
// diagram at the end of a long reply sits under the footer. Nudge the
// viewport once the run settles so the graphic clears that footer.
function clearActionBar(): void {
  const viewport = document.querySelector<HTMLElement>(
    "[data-slot=aui_thread-viewport]",
  );
  const footer = viewport?.querySelector<HTMLElement>(
    ".aui-thread-viewport-footer",
  );
  const bar = viewport?.querySelector<HTMLElement>(
    "[data-slot=aui_assistant-message-footer]",
  );
  if (!viewport || !footer || !bar) return;
  const limit =
    viewport.getBoundingClientRect().bottom -
    footer.getBoundingClientRect().height -
    16;
  const overflow = bar.getBoundingClientRect().bottom - limit;
  if (overflow > 1) viewport.scrollTop += overflow;
}

function DiagramReveal() {
  const running = useAuiState((state) => state.thread.isRunning);
  useEffect(() => {
    if (running) return;
    const viewport = document.querySelector<HTMLElement>(
      "[data-slot=aui_thread-viewport]",
    );
    if (!viewport) return;
    const observer = new ResizeObserver(() => clearActionBar());
    observer.observe(viewport);
    const watch = new MutationObserver(() => {
      const diagram = viewport.querySelector("[data-slot=mermaid-diagram]");
      if (diagram) observer.observe(diagram);
      clearActionBar();
    });
    watch.observe(viewport, { childList: true, subtree: true });
    const timer = window.setInterval(clearActionBar, 200);
    const stop = window.setTimeout(() => window.clearInterval(timer), 5000);
    clearActionBar();
    return () => {
      observer.disconnect();
      watch.disconnect();
      window.clearInterval(timer);
      window.clearTimeout(stop);
    };
  }, [running]);
  return null;
}

function ChatShortcuts() {
  const aui = useAui();
  const running = useAuiState((state) => state.thread.isRunning);
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape" && running) {
        event.preventDefault();
        aui.thread.cancelRun();
        return;
      }
      const meta = event.metaKey || event.ctrlKey;
      if (meta && event.shiftKey && event.key.toLowerCase() === "o") {
        event.preventDefault();
        document
          .querySelector<HTMLButtonElement>("[data-slot=aui_thread-list-new]")
          ?.click();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [aui, running]);
  return null;
}

function BridgeStatus() {
  const health = useBridgeHealth();
  useEffect(() => {
    void health.check();
  }, [health.check]);
  if (health.phase !== "dropped" && health.phase !== "reconnecting")
    return null;
  return (
    <ErrorState
      title="Codex bridge is unreachable"
      detail="The assistant bridge on 127.0.0.1 is not answering. Start it, then retry. Nothing is sent until the bridge is up."
      retrying={health.phase === "reconnecting"}
      onRetry={() => {
        void health.check();
      }}
    />
  );
}

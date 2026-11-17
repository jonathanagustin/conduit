"use client";

import { useEffect } from "react";
import { appendAssistantDraft } from "./assistant-draft";
import { servedModelLabel, type ServedModel } from "./model-identity";

import {
  useLocalRuntime,
  type ChatModelAdapter,
  type ThreadAssistantMessagePart,
  type ThreadMessage,
} from "@assistant-ui/react";

type AssistantChip = { id: string; objectType: string; label: string };
type AssistantContext = { mode?: string; chips?: AssistantChip[] };
type ToolResult = {
  tool?: string;
  label?: string;
  text?: string;
  error?: string;
  arguments?: Record<string, unknown>;
  results?: ToolResult[];
};

function assistantContext(): AssistantContext {
  const value = (
    window as Window & { __conduitAssistantContext?: AssistantContext }
  ).__conduitAssistantContext;
  return value ?? { mode: "explore", chips: [] };
}

function userText(messages: readonly ThreadMessage[]): string {
  for (let index = messages.length - 1; index >= 0; index -= 1) {
    const message = messages[index];
    if (!message || message.role !== "user") continue;
    return message.content
      .map((part) => (part.type === "text" ? part.text : ""))
      .join("")
      .trim();
  }
  return "";
}

const workbenchModel: ChatModelAdapter = {
  async run({ messages, abortSignal }) {
    const context = assistantContext();
    const response = await fetch("/v1/assistant/turns", {
      method: "POST",
      signal: abortSignal,
      headers: {
        "content-type": "application/json",
        accept: "application/json",
      },
      body: JSON.stringify({
        mode: context.mode || "explore",
        message: userText(messages),
        context: context.chips || [],
      }),
    });
    const body = (await response.json()) as ToolResult & {
      results?: ToolResult[];
      model?: ServedModel;
    };
    if (response.status === 422) {
      return {
        content: [
          {
            type: "text" as const,
            text: "No model call. Use a context chip to run a tool. The local model CLI is not configured.",
          },
        ],
      };
    }
    if (!response.ok) {
      return {
        content: [
          {
            type: "text" as const,
            text: body.error || "The assistant could not complete the turn.",
          },
        ],
      };
    }
    const content: ThreadAssistantMessagePart[] = [
      {
        type: "text",
        text: `${body.label || "Live model call"}. ${servedModelLabel(body.model)}. ${body.text || ""}`.trim(),
      },
    ];
    for (const result of body.results || []) {
      content.push({
        type: "tool-call",
        toolCallId: crypto.randomUUID(),
        toolName: result.tool || "query_ontology_objects",
        argsText: "{}",
        args: {},
        result,
      });
      window.dispatchEvent(
        new CustomEvent("conduit-assistant-result", { detail: result }),
      );
    }
    return { content };
  },
};

export function useWorkbenchRuntime() {
  const runtime = useLocalRuntime(workbenchModel);
  useEffect(() => {
    const addDraft = (event: Event) => {
      const detail = (
        event as CustomEvent<{ text?: unknown; accepted?: boolean }>
      ).detail;
      if (!detail || typeof detail !== "object") return;
      const next = appendAssistantDraft(
        runtime.thread.composer.getState().text,
        detail.text,
      );
      if (next === null) return;
      runtime.thread.composer.setText(next);
      detail.accepted = true;
    };
    window.addEventListener("conduit-assistant-draft", addDraft);
    return () =>
      window.removeEventListener("conduit-assistant-draft", addDraft);
  }, [runtime]);
  return runtime;
}

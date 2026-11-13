import {
  CompositeAttachmentAdapter,
  SimpleImageAttachmentAdapter,
  SimpleTextAttachmentAdapter,
  WebSpeechDictationAdapter,
  WebSpeechSynthesisAdapter,
  useLocalRuntime,
  useRemoteThreadListRuntime,
  type ChatModelAdapter,
  type FeedbackAdapter,
  type RemoteThreadListAdapter,
  type SuggestionAdapter,
  type ThreadAssistantMessagePart,
  type ThreadMessage,
  type ToolApprovalOptionKind,
  type ToolCallMessagePart,
} from "@assistant-ui/react";
import { createAssistantStream } from "assistant-stream";

import {
  archiveThread,
  cancelRun,
  createThread,
  decidePermission,
  deleteThread,
  listThreads,
  renameThread,
  streamRun,
  type ThreadRecord,
} from "./client.js";
import type { RunSnapshot, ToolSnapshot } from "./protocol.js";

const attachmentAdapter = new CompositeAttachmentAdapter([
  new SimpleImageAttachmentAdapter(),
  new SimpleTextAttachmentAdapter(),
]);
const dictationAdapter = new WebSpeechDictationAdapter();
const speechAdapter = new WebSpeechSynthesisAdapter();

const feedbackLog: { messageId: string; type: "positive" | "negative" }[] = [];

const feedbackAdapter: FeedbackAdapter = {
  submit(feedback) {
    // Local UI state only. A thumb is not a tool approval and not a deployment approval.
    feedbackLog.push({ messageId: feedback.message.id, type: feedback.type });
  },
};

export function recordedFeedback(): readonly { messageId: string; type: "positive" | "negative" }[] {
  return feedbackLog;
}

export const STARTERS = [
  {
    title: "Assess this workspace",
    label: "blockers and unknowns",
    prompt: "Assess this workspace for packaging blockers. List what you can verify and what is unknown. Do not deploy anything.",
  },
  {
    title: "Review proposed diffs",
    label: "read only",
    prompt: "Review the files in this workspace and summarize proposed changes. Do not write outside the workspace and do not deploy.",
  },
];

const FOLLOW_UPS = [
  {
    title: "What is still unknown?",
    prompt: "List the claims that are still unknown, and the evidence that would resolve each one.",
  },
  {
    title: "Show the tool boundary",
    prompt: "Which actions would need a separate approval before anything is deployed? Tool success is not deployment approval.",
  },
];

const suggestionAdapter: SuggestionAdapter = {
  async generate({ messages }) {
    return messages.length === 0 ? STARTERS : FOLLOW_UPS;
  },
};

function userText(message: ThreadMessage): string {
  if (message.role !== "user") return "";
  return message.content
    .map((part) => (part.type === "text" ? part.text : ""))
    .join("")
    .trim();
}

function dataOf(value: string): string {
  const marker = "base64,";
  const index = value.indexOf(marker);
  return index === -1 ? value : value.slice(index + marker.length);
}

function attachmentsOf(message: ThreadMessage): { name: string; mimeType: string; data: string }[] {
  if (message.role !== "user") return [];
  const attachments = [];
  for (const part of message.content) {
    if (part.type === "image") {
      attachments.push({
        name: part.filename ?? "image",
        mimeType: "image/png",
        data: dataOf(part.image),
      });
    } else if (part.type === "file") {
      attachments.push({
        name: part.filename ?? "file",
        mimeType: part.mimeType,
        data: dataOf(part.data),
      });
    }
  }
  return attachments;
}

function optionKind(kind: string): ToolApprovalOptionKind | (string & {}) {
  if (kind === "allow_once") return "allow-once";
  if (kind === "allow_always") return "allow-always";
  if (kind === "reject_once") return "reject-once";
  if (kind === "reject_always") return "reject-always";
  return kind;
}

function toolPart(tool: ToolSnapshot): ToolCallMessagePart {
  let args: ToolCallMessagePart["args"] = {};
  if (tool.argsText.startsWith("{")) {
    try {
      const parsed = JSON.parse(tool.argsText) as unknown;
      if (parsed !== null && typeof parsed === "object" && !Array.isArray(parsed)) {
        args = parsed as ToolCallMessagePart["args"];
      }
    } catch {
      args = {};
    }
  }
  const running = tool.status === "pending" || tool.status === "in_progress";
  const decision = tool.approval?.decision;
  const part: ToolCallMessagePart = {
    type: "tool-call",
    toolCallId: tool.toolCallId,
    toolName: tool.toolName,
    argsText: tool.argsText,
    args,
    ...(tool.resultText.length > 0 ? { result: tool.resultText } : {}),
    ...(tool.isError ? { isError: true } : {}),
    ...(running ? { isPreliminary: true } : {}),
    ...(tool.approval
      ? {
          approval: {
            id: tool.approval.requestId,
            prompt: tool.approval.prompt,
            options: tool.approval.options.map((option) => ({
              id: option.optionId,
              kind: optionKind(option.kind),
              label: option.name,
            })),
            ...(decision && "resolution" in decision
              ? { resolution: "cancelled" as const }
              : decision
                ? { approved: decision.approved, optionId: decision.optionId }
                : {}),
          },
        }
      : {}),
  };
  return part;
}

function partsOf(snapshot: RunSnapshot): ThreadAssistantMessagePart[] {
  const parts: ThreadAssistantMessagePart[] = [];
  if (snapshot.reasoning.length > 0) {
    parts.push({ type: "reasoning", text: snapshot.reasoning });
  }
  if (snapshot.text.length > 0) parts.push({ type: "text", text: snapshot.text });
  for (const tool of snapshot.tools) parts.push(toolPart(tool));
  for (const source of snapshot.sources) {
    parts.push({ type: "source", sourceType: "url", id: source.id, url: source.url, title: source.title });
  }
  if (snapshot.plan.length > 0) {
    parts.push({
      type: "text",
      text: `\n\nPlan\n${snapshot.plan.map((entry) => `- ${entry.status} (${entry.priority}) ${entry.content}`).join("\n")}`,
    });
  }
  return parts;
}

type Yield = {
  content: readonly ThreadAssistantMessagePart[];
  status?: { type: "requires-action"; reason: "tool-calls" } | { type: "running" };
  metadata?: ChatYieldMetadata;
};

type ParkedRun = {
  runId: string;
  queue: RunSnapshot[];
  notify: () => void;
  finished?: { stopReason: string };
  failure?: Error;
  pending: Promise<unknown>;
};

const parkedRuns = new Map<string, ParkedRun>();

function approvalPending(snapshot: RunSnapshot): boolean {
  return snapshot.tools.some((tool) => tool.approval !== undefined && tool.approval.decision === undefined);
}

function answeredApproval(messages: readonly ThreadMessage[]): { requestId: string; optionId: string } | undefined {
  for (let index = messages.length - 1; index >= 0; index -= 1) {
    const message = messages[index];
    if (!message || message.role !== "assistant") continue;
    for (const part of message.content) {
      if (part.type !== "tool-call" || !part.approval || part.approval.approved === undefined || !part.approval.optionId) continue;
      return { requestId: part.approval.id, optionId: part.approval.optionId };
    }
  }
  return undefined;
}

type ChatYieldMetadata = {
  timing: {
    streamStartTime: number;
    totalChunks: number;
    toolCallCount: number;
    firstTokenTime?: number;
    totalStreamTime?: number;
  };
  custom: { usage?: string; plan?: RunSnapshot["plan"] };
};

async function* queueRun({
  messages,
  abortSignal,
  unstable_threadId,
}: Parameters<ChatModelAdapter["run"]>[0]): AsyncGenerator<Yield, void> {
  const threadId = unstable_threadId;
  if (!threadId) throw new Error("the assistant thread is not connected to a Codex session");
  const userMessages = messages.filter((message) => message.role === "user");
  const last = userMessages.at(-1);
  if (!last) throw new Error("a run needs a user message");

  const started = Date.now();
  let firstToken: number | undefined;
  let chunks = 0;
  const existing = parkedRuns.get(threadId);
  let runId: string;
  let queue: RunSnapshot[];
  let notify: () => void;
  let finished: { stopReason: string } | undefined;
  let failure: Error | undefined;
  let pending: Promise<unknown>;

  if (existing) {
    runId = existing.runId;
    queue = existing.queue;
    notify = existing.notify;
    finished = existing.finished;
    failure = existing.failure;
    pending = existing.pending;
    const decision = answeredApproval(messages);
    if (decision) await decidePermission({ requestId: decision.requestId, threadId, runId, optionId: decision.optionId });
  } else {
    runId = crypto.randomUUID();
    queue = [];
    notify = () => undefined;
    const state: ParkedRun = { runId, queue, notify, pending: Promise.resolve() };
    parkedRuns.set(threadId, state);
    pending = streamRun(
      {
        threadId,
        runId,
        userTexts: userMessages.map((message) => userText(message)),
        attachments: attachmentsOf(last),
      },
      {
        signal: abortSignal,
        onSnapshot(snapshot) {
          queue.push(snapshot);
          chunks += 1;
          if (firstToken === undefined && (snapshot.text.length > 0 || snapshot.reasoning.length > 0)) {
            firstToken = Date.now();
          }
          notify();
        },
      },
    ).then(
      (result) => {
        finished = result;
        state.finished = result;
        notify();
      },
      (error: unknown) => {
        failure = error instanceof Error ? error : new Error("the Codex run failed");
        state.failure = failure;
        notify();
      },
    );
    state.pending = pending;
  }

  const onAbort = () => {
    void cancelRun(threadId, runId).catch(() => undefined);
  };
  abortSignal.addEventListener("abort", onAbort);

  try {
    for (;;) {
      if (!finished && !failure && queue.length === 0) {
        await new Promise<void>((resolve) => {
          notify = resolve;
          const current = parkedRuns.get(threadId);
          if (current && !existing) current.notify = resolve;
          if (current && existing) current.notify = resolve;
        });
        continue;
      }
      const snapshot = queue.shift();
      if (snapshot) {
        const waiting = approvalPending(snapshot);
        yield {
          content: partsOf(snapshot),
          status: waiting ? { type: "requires-action", reason: "tool-calls" } : { type: "running" },
          metadata: metadata(started, chunks, snapshot, firstToken, finished ? Date.now() - started : undefined),
        };
        if (waiting && !finished) return;
        continue;
      }
      if (failure) throw failure;
      if (finished) {
        parkedRuns.delete(threadId);
        await pending;
        return;
      }
    }
  } finally {
    abortSignal.removeEventListener("abort", onAbort);
  }
}

function metadata(
  started: number,
  chunks: number,
  snapshot: RunSnapshot,
  firstToken: number | undefined,
  totalStreamTime?: number,
): ChatYieldMetadata {
  const timing: ChatYieldMetadata["timing"] = {
    streamStartTime: started,
    totalChunks: chunks,
    toolCallCount: snapshot.tools.length,
  };
  if (firstToken !== undefined) timing.firstTokenTime = firstToken - started;
  if (totalStreamTime !== undefined) timing.totalStreamTime = totalStreamTime;
  const custom: ChatYieldMetadata["custom"] = {};
  if (snapshot.usage) custom.usage = `${snapshot.usage.used} / ${snapshot.usage.size}`;
  if (snapshot.plan.length > 0) custom.plan = snapshot.plan;
  return { timing, custom };
}

export const codexModel: ChatModelAdapter = {
  run: queueRun,
};

function titleFrom(messages: readonly ThreadMessage[]): string {
  const text = messages.map((message) => userText(message)).find((value) => value.length > 0);
  if (!text) return "New chat";
  return text.length > 80 ? `${text.slice(0, 77)}…` : text;
}

export const threadListAdapter: RemoteThreadListAdapter = {
  async list() {
    try {
      const threads = await listThreads();
      return { threads: threads.map(toRemote) };
    } catch {
      return { threads: [] };
    }
  },
  async rename(remoteId, newTitle) {
    await renameThread(remoteId, newTitle);
  },
  async archive(remoteId) {
    await archiveThread(remoteId, true);
  },
  async unarchive(remoteId) {
    await archiveThread(remoteId, false);
  },
  async delete(remoteId) {
    await deleteThread(remoteId);
  },
  async initialize() {
    try {
      const thread = await createThread();
      return { remoteId: thread.id };
    } catch {
      return { remoteId: `offline-${crypto.randomUUID()}` };
    }
  },
  async fetch(threadId) {
    const threads = await listThreads();
    const thread = threads.find((candidate) => candidate.id === threadId);
    if (!thread) throw new Error("that conversation is no longer on the bridge");
    return toRemote(thread);
  },
  async generateTitle(remoteId, messages) {
    const title = titleFrom(messages);
    await renameThread(remoteId, title);
    return createAssistantStream((controller) => {
      controller.appendText(title);
    });
  },
};

function toRemote(thread: ThreadRecord) {
  return {
    status: thread.archived ? ("archived" as const) : ("regular" as const),
    remoteId: thread.id,
    title: thread.title,
    lastMessageAt: new Date(thread.updatedAt),
  };
}

function useModelRuntime() {
  return useLocalRuntime(codexModel, {
    adapters: {
      attachments: attachmentAdapter,
      dictation: dictationAdapter,
      speech: speechAdapter,
      feedback: feedbackAdapter,
      suggestion: suggestionAdapter,
    },
  });
}

export function useCodexRuntime() {
  return useRemoteThreadListRuntime({
    runtimeHook: useModelRuntime,
    adapter: threadListAdapter,
  });
}

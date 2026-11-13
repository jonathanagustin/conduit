import { loadSettings, saveSettings } from "./console-settings.js";
import type {
  RunSnapshot,
  SessionConfigOption,
  UsageSnapshot,
} from "./protocol.js";

export class AssistantApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "AssistantApiError";
    this.status = status;
  }
}

export type BridgeInfo = {
  ok: boolean;
  runtime: string;
  fixture: boolean;
  workspace: string;
  mode: string;
  adapter: string;
};

export type ThreadRecord = {
  id: string;
  title: string;
  archived: boolean;
  updatedAt: string;
};

type RunHandlers = {
  onSnapshot: (snapshot: RunSnapshot) => void;
  signal: AbortSignal;
};

async function readError(response: Response): Promise<string> {
  try {
    const body = (await response.json()) as { error?: string };
    if (body.error && body.error.length > 0) return body.error;
  } catch {
    // The body was not the error document. The status is the fact.
  }
  return `assistant bridge returned ${response.status}`;
}

async function send(path: string, init: RequestInit): Promise<Response> {
  const response = await fetch(path, {
    ...init,
    headers: { "content-type": "application/json", ...init.headers },
  });
  if (!response.ok)
    throw new AssistantApiError(response.status, await readError(response));
  return response;
}

export async function bridgeInfo(signal?: AbortSignal): Promise<BridgeInfo> {
  const response = await send(
    "/api/assistant/health",
    signal ? { method: "GET", signal } : { method: "GET" },
  );
  return (await response.json()) as BridgeInfo;
}

export async function listThreads(): Promise<ThreadRecord[]> {
  const response = await send("/api/assistant/threads", { method: "GET" });
  const body = (await response.json()) as { threads: ThreadRecord[] };
  return body.threads;
}

export async function createThread(): Promise<ThreadRecord> {
  const response = await send("/api/assistant/threads", {
    method: "POST",
    body: "{}",
  });
  return (await response.json()) as ThreadRecord;
}

export async function renameThread(id: string, title: string): Promise<void> {
  await send(`/api/assistant/threads/${encodeURIComponent(id)}`, {
    method: "PATCH",
    body: JSON.stringify({ title }),
  });
}

export async function archiveThread(
  id: string,
  archived: boolean,
): Promise<void> {
  await send(`/api/assistant/threads/${encodeURIComponent(id)}`, {
    method: "PATCH",
    body: JSON.stringify({ archived }),
  });
}

const choice: { model?: string; effort?: string } = {};
const choiceListeners = new Set<() => void>();

export function subscribeSessionChoice(listener: () => void): () => void {
  choiceListeners.add(listener);
  return () => {
    choiceListeners.delete(listener);
  };
}

function storedChoice(): { model?: string; effort?: string } {
  if (typeof localStorage === "undefined") return {};
  const saved = loadSettings(localStorage);
  return {
    ...(saved.model ? { model: saved.model } : {}),
    ...(saved.effort ? { effort: saved.effort } : {}),
  };
}

export function rememberedSessionChoice(): { model?: string; effort?: string } {
  const saved = storedChoice();
  if (saved.model) choice.model = saved.model;
  if (saved.effort) choice.effort = saved.effort;
  return { ...choice };
}

export function rememberSessionChoice(next: {
  model?: string;
  effort?: string;
}): void {
  if (next.model !== undefined) choice.model = next.model;
  if (next.effort !== undefined) choice.effort = next.effort;
  if (typeof localStorage !== "undefined") {
    const saved = loadSettings(localStorage);
    saveSettings(localStorage, {
      ...saved,
      ...(next.model !== undefined ? { model: next.model } : {}),
      ...(next.effort !== undefined ? { effort: next.effort } : {}),
    });
  }
  for (const listener of choiceListeners) listener();
}

export async function defaultSessionConfig(): Promise<SessionConfigOption[]> {
  const response = await send("/api/assistant/config", { method: "GET" });
  const body = (await response.json()) as { options?: SessionConfigOption[] };
  return body.options ?? [];
}

export async function sessionConfigOf(
  id: string,
): Promise<SessionConfigOption[]> {
  const response = await send(
    `/api/assistant/threads/${encodeURIComponent(id)}/config`,
    { method: "GET" },
  );
  const body = (await response.json()) as { options?: SessionConfigOption[] };
  return body.options ?? [];
}

export async function setSessionConfig(
  id: string,
  configId: string,
  value: string | boolean,
): Promise<SessionConfigOption[]> {
  const response = await send(
    `/api/assistant/threads/${encodeURIComponent(id)}/config`,
    {
      method: "POST",
      body: JSON.stringify({ configId, value }),
    },
  );
  const body = (await response.json()) as { options?: SessionConfigOption[] };
  return body.options ?? [];
}

export async function deleteThread(id: string): Promise<void> {
  await send(`/api/assistant/threads/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export async function cancelRun(
  threadId: string,
  runId: string,
): Promise<void> {
  await send(`/api/assistant/threads/${encodeURIComponent(threadId)}/cancel`, {
    method: "POST",
    body: JSON.stringify({ runId }),
  });
}

export async function decidePermission(input: {
  requestId: string;
  threadId: string;
  runId: string;
  optionId: string;
}): Promise<void> {
  await send(
    `/api/assistant/permissions/${encodeURIComponent(input.requestId)}`,
    {
      method: "POST",
      body: JSON.stringify({
        threadId: input.threadId,
        runId: input.runId,
        optionId: input.optionId,
      }),
    },
  );
}

export type RunRequest = {
  threadId: string;
  runId: string;
  userTexts: string[];
  attachments: { name: string; mimeType: string; data: string }[];
};

export async function streamRun(
  request: RunRequest,
  handlers: RunHandlers,
): Promise<{ stopReason: string }> {
  const response = await fetch(
    `/api/assistant/threads/${encodeURIComponent(request.threadId)}/runs`,
    {
      method: "POST",
      headers: {
        "content-type": "application/json",
        accept: "text/event-stream",
      },
      body: JSON.stringify({
        runId: request.runId,
        userTexts: request.userTexts,
        attachments: request.attachments,
        ...rememberedSessionChoice(),
      }),
      signal: handlers.signal,
    },
  );
  if (!response.ok)
    throw new AssistantApiError(response.status, await readError(response));
  if (!response.body)
    throw new AssistantApiError(
      502,
      "assistant bridge returned an empty stream",
    );

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let stopReason = "end_turn";
  const onAbort = () => {
    void reader.cancel().catch(() => undefined);
  };
  handlers.signal.addEventListener("abort", onAbort);
  try {
    for (;;) {
      const chunk = await reader.read();
      if (chunk.done) break;
      buffer += decoder.decode(chunk.value, { stream: true });
      const frames = buffer.split("\n\n");
      buffer = frames.pop() ?? "";
      for (const frame of frames) {
        const data = frame
          .split("\n")
          .filter((line) => line.startsWith("data:"))
          .map((line) => line.slice(5).trim())
          .join("\n");
        if (data.length === 0) continue;
        const event = JSON.parse(data) as
          | { type: "snapshot"; snapshot: RunSnapshot }
          | { type: "done"; stopReason: string }
          | { type: "error"; message: string };
        if (event.type === "snapshot") handlers.onSnapshot(event.snapshot);
        else if (event.type === "done") stopReason = event.stopReason;
        else throw new AssistantApiError(502, event.message);
      }
    }
  } finally {
    handlers.signal.removeEventListener("abort", onAbort);
  }
  return { stopReason };
}

export function usageLabel(
  usage: UsageSnapshot | undefined,
): string | undefined {
  if (!usage || usage.size <= 0) return undefined;
  return `${usage.used} / ${usage.size} tokens in context`;
}

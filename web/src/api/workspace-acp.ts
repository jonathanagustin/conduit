import {
  PINNED_ACP_PROTOCOL_VERSION,
  type GovernedRiskTier,
  type RunSnapshot,
  type SessionConfigOption,
} from "./protocol.js";

export type HubPermissionEvent = {
  requestId: string;
  sessionId: string;
  threadId: string;
  runId: string;
  toolCallId: string;
  title: string;
  riskTier: GovernedRiskTier;
  status: string;
  changeRequestId?: string;
  options: { optionId: string; name: string; kind: string }[];
};

export type WorkspaceAcpStreamEvent =
  | { type: "snapshot"; snapshot: RunSnapshot }
  | { type: "permission_request"; request: HubPermissionEvent }
  | { type: "activity"; [key: string]: unknown }
  | { type: "acp_notification"; method: string; params: unknown }
  | {
      type: "rpc_response";
      response: { jsonrpc: "2.0"; id: unknown; result: { stopReason: string } };
    }
  | {
      type: "rpc_error";
      response: {
        jsonrpc: "2.0";
        id: unknown;
        error: { code: number; message: string };
      };
    };

export function hubWorkspaceAcpPath(workspaceId: string): string {
  const trimmed = workspaceId.trim();
  if (trimmed.length === 0 || trimmed.includes("/") || trimmed.includes("..")) {
    throw new Error("a valid workspaceId is required for Hub ACP routing");
  }
  return `/v1/dev-workspaces/${encodeURIComponent(trimmed)}/acp`;
}

export function createWorkspaceAcpClient(options: {
  workspaceId: string;
  branch?: string;
  fetchImpl?: typeof globalThis.fetch;
}) {
  const endpoint = hubWorkspaceAcpPath(options.workspaceId);
  const fetcher = options.fetchImpl ?? globalThis.fetch;
  let nextRpcId = 1;

  async function rpc<T>(
    method: string,
    params: Record<string, unknown>,
    signal?: AbortSignal,
  ): Promise<T> {
    const id = nextRpcId++;
    const meta: Record<string, unknown> =
      params["_meta"] !== null && typeof params["_meta"] === "object"
        ? { ...(params["_meta"] as Record<string, unknown>) }
        : {};
    if (options.branch && meta["branch"] === undefined) {
      meta["branch"] = options.branch;
    }
    const response = await fetcher(endpoint, {
      method: "POST",
      credentials: "same-origin",
      headers: {
        accept: "application/json",
        "content-type": "application/json",
      },
      body: JSON.stringify({
        jsonrpc: "2.0",
        id,
        method,
        params: {
          ...params,
          ...(Object.keys(meta).length > 0 ? { _meta: meta } : {}),
        },
      }),
      ...(signal ? { signal } : {}),
    });
    const payload = (await response.json()) as {
      jsonrpc?: string;
      result?: T;
      error?: { code?: number; message?: string };
    };
    if (!response.ok || payload.error) {
      throw new Error(
        payload.error?.message ?? `Hub ACP request failed (${response.status})`,
      );
    }
    return payload.result as T;
  }

  return {
    endpoint,

    async initialize(signal?: AbortSignal) {
      const result = await rpc<{
        protocolVersion: number;
        agentInfo?: { name: string; version: string };
      }>(
        "initialize",
        { protocolVersion: PINNED_ACP_PROTOCOL_VERSION },
        signal,
      );
      if (result.protocolVersion !== PINNED_ACP_PROTOCOL_VERSION) {
        throw new Error(
          `Hub negotiated unsupported ACP protocolVersion ${String(result.protocolVersion)}, expected ${PINNED_ACP_PROTOCOL_VERSION}`,
        );
      }
      return result;
    },

    async newSession(signal?: AbortSignal) {
      return rpc<{
        sessionId: string;
        configOptions?: SessionConfigOption[];
        _meta?: { threadId?: string; branch?: string; owner?: string };
      }>("session/new", {}, signal);
    },

    async prompt(
      input: {
        sessionId: string;
        runId?: string;
        prompt: { type: "text"; text: string }[];
        onEvent?: (event: WorkspaceAcpStreamEvent) => void;
      },
      signal?: AbortSignal,
    ): Promise<{ stopReason: string }> {
      const id = nextRpcId++;
      const meta: Record<string, unknown> = {};
      if (options.branch) meta["branch"] = options.branch;
      if (input.runId) meta["runId"] = input.runId;
      const response = await fetcher(endpoint, {
        method: "POST",
        credentials: "same-origin",
        headers: {
          accept: "text/event-stream",
          "content-type": "application/json",
        },
        body: JSON.stringify({
          jsonrpc: "2.0",
          id,
          method: "session/prompt",
          params: {
            sessionId: input.sessionId,
            prompt: input.prompt,
            ...(Object.keys(meta).length > 0 ? { _meta: meta } : {}),
          },
        }),
        ...(signal ? { signal } : {}),
      });
      if (!response.ok || !response.body) {
        let message = `Hub ACP prompt failed (${response.status})`;
        try {
          const body = (await response.json()) as {
            error?: { message?: string } | string;
          };
          if (typeof body.error === "string") message = body.error;
          else if (body.error?.message) message = body.error.message;
        } catch {
          // Keep HTTP status fallback.
        }
        throw new Error(message);
      }

      const reader = response.body.getReader();
      const decoder = new TextDecoder("utf-8", { fatal: true });
      let buffer = "";
      let stopReason: string | undefined;
      try {
        for (;;) {
          const { value, done } = await reader.read();
          buffer += decoder.decode(value ?? new Uint8Array(), {
            stream: !done,
          });
          let boundary: RegExpExecArray | null;
          while ((boundary = /\r?\n\r?\n/u.exec(buffer))) {
            const frame = buffer.slice(0, boundary.index);
            buffer = buffer.slice(boundary.index + boundary[0].length);
            const data = frame
              .split(/\r?\n/u)
              .filter((line) => line.startsWith("data:"))
              .map((line) => line.slice(5).trim())
              .join("\n");
            if (!data) continue;
            const event = JSON.parse(data) as WorkspaceAcpStreamEvent;
            input.onEvent?.(event);
            if (event.type === "rpc_response") {
              stopReason = event.response.result.stopReason;
            } else if (event.type === "rpc_error") {
              throw new Error(event.response.error.message);
            }
          }
          if (done) break;
        }
      } finally {
        reader.releaseLock();
      }
      if (!stopReason) {
        throw new Error(
          "Hub ACP prompt stream ended without a terminal response",
        );
      }
      return { stopReason };
    },

    async decidePermission(
      input: {
        requestId: string;
        sessionId: string;
        runId: string;
        decision: "confirm" | "deny";
        optionId?: string;
      },
      signal?: AbortSignal,
    ) {
      const response = await fetcher(
        `${endpoint}/permissions/${encodeURIComponent(input.requestId)}`,
        {
          method: "POST",
          credentials: "same-origin",
          headers: {
            accept: "application/json",
            "content-type": "application/json",
          },
          body: JSON.stringify({
            sessionId: input.sessionId,
            runId: input.runId,
            decision: input.decision,
            ...(input.optionId ? { optionId: input.optionId } : {}),
            ...(options.branch ? { branch: options.branch } : {}),
          }),
          ...(signal ? { signal } : {}),
        },
      );
      const body = (await response.json()) as Record<string, unknown>;
      if (!response.ok) {
        throw new Error(
          typeof body["error"] === "string"
            ? body["error"]
            : `Permission settlement failed (${response.status})`,
        );
      }
      return body;
    },

    async cancel(sessionId: string, signal?: AbortSignal) {
      return rpc("session/cancel", { sessionId }, signal);
    },

    async close(sessionId: string, signal?: AbortSignal) {
      return rpc("session/close", { sessionId }, signal);
    },
  };
}

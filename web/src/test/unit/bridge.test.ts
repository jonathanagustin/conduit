import { afterEach, describe, expect, it } from "vite-plus/test";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { createBridge, type BridgeServer } from "../../server/http.ts";

const fakeAgent = fileURLToPath(
  new URL("../../server/fake-agent.mjs", import.meta.url),
);
const previous = {
  command: process.env["ASSISTANT_ACP_COMMAND"],
  args: process.env["ASSISTANT_ACP_ARGS"],
  fixture: process.env["ASSISTANT_ACP_FIXTURE"],
  mode: process.env["INITIAL_AGENT_MODE"],
};

async function start(): Promise<{ bridge: BridgeServer; workspace: string }> {
  const workspace = await mkdtemp(path.join(tmpdir(), "conduit-assistant-"));
  process.env["ASSISTANT_ACP_COMMAND"] = process.execPath;
  process.env["ASSISTANT_ACP_ARGS"] = JSON.stringify([fakeAgent]);
  process.env["ASSISTANT_ACP_FIXTURE"] = "1";
  process.env["INITIAL_AGENT_MODE"] = "workspace-write";
  const bridge = await createBridge({ workspace });
  return { bridge, workspace };
}

type BridgeEvent = {
  type: string;
  snapshot?: {
    text: string;
    reasoning: string;
    usage?: { used: number; size: number };
    tools: { approval?: { requestId: string } }[];
  };
  stopReason?: string;
  message?: string;
};

async function readEvents(response: Response): Promise<BridgeEvent[]> {
  const reader = response.body?.getReader();
  if (!reader) throw new Error("missing body");
  const decoder = new TextDecoder();
  let buffer = "";
  const events: BridgeEvent[] = [];
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
      if (data.length > 0)
        events.push(JSON.parse(data) as (typeof events)[number]);
    }
  }
  return events;
}

describe("ACP bridge", () => {
  const bridges: BridgeServer[] = [];
  const dirs: string[] = [];

  afterEach(async () => {
    for (const bridge of bridges.splice(0)) await bridge.close();
    for (const dir of dirs.splice(0))
      await rm(dir, { recursive: true, force: true });
    if (previous.command === undefined)
      delete process.env["ASSISTANT_ACP_COMMAND"];
    else process.env["ASSISTANT_ACP_COMMAND"] = previous.command;
    if (previous.args === undefined) delete process.env["ASSISTANT_ACP_ARGS"];
    else process.env["ASSISTANT_ACP_ARGS"] = previous.args;
    if (previous.fixture === undefined)
      delete process.env["ASSISTANT_ACP_FIXTURE"];
    else process.env["ASSISTANT_ACP_FIXTURE"] = previous.fixture;
    if (previous.mode === undefined) delete process.env["INITIAL_AGENT_MODE"];
    else process.env["INITIAL_AGENT_MODE"] = previous.mode;
  });

  it("streams a redacted turn and applies one matching approval", async () => {
    const started = await start();
    bridges.push(started.bridge);
    dirs.push(started.workspace);
    const base = `http://127.0.0.1:${started.bridge.port}`;
    const health = await fetch(`${base}/api/assistant/health`);
    expect(await health.json()).toMatchObject({
      fixture: true,
      mode: "workspace-write",
    });

    const created = await fetch(`${base}/api/assistant/threads`, {
      method: "POST",
      body: "{}",
    });
    const thread = (await created.json()) as { id: string };
    const run = fetch(`${base}/api/assistant/threads/${thread.id}/runs`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        runId: "run-1",
        userTexts: ["inspect the workspace"],
      }),
    });
    const response = await run;
    const reader = response.body?.getReader();
    if (!reader) throw new Error("missing body");
    const decoder = new TextDecoder();
    let buffer = "";
    let requestId = "";
    const events: BridgeEvent[] = [];
    while (
      !events.some((event) => event.type === "done" || event.type === "error")
    ) {
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
        const event = JSON.parse(data) as {
          type: string;
          snapshot?: {
            text: string;
            reasoning: string;
            tools: { approval?: { requestId: string } }[];
          };
          stopReason?: string;
        };
        events.push(event);
        const pending = event.snapshot?.tools.find((tool) => tool.approval);
        if (pending?.approval && requestId.length === 0) {
          requestId = pending.approval.requestId;
          const decision = await fetch(
            `${base}/api/assistant/permissions/${encodeURIComponent(requestId)}`,
            {
              method: "POST",
              headers: { "content-type": "application/json" },
              body: JSON.stringify({
                threadId: thread.id,
                runId: "run-1",
                optionId: "allow",
              }),
            },
          );
          expect(decision.status).toBe(200);
          const replay = await fetch(
            `${base}/api/assistant/permissions/${encodeURIComponent(requestId)}`,
            {
              method: "POST",
              headers: { "content-type": "application/json" },
              body: JSON.stringify({
                threadId: thread.id,
                runId: "run-1",
                optionId: "allow",
              }),
            },
          );
          expect(replay.status).toBe(409);
        }
      }
    }
    expect(requestId).toContain(thread.id);
    const done = events.find((event) => event.type === "done");
    expect(done?.stopReason).toBe("end_turn");
    const text = events.map((event) => event.snapshot?.text ?? "").join("");
    expect(text).toContain("Read complete.");
    expect(
      events.some((event) => event.snapshot?.reasoning.includes("sk-")),
    ).toBe(false);
    expect(
      events.some((event) =>
        (event.snapshot?.reasoning ?? "").includes("[redacted]"),
      ),
    ).toBe(true);
    const usage = events
      .map((event) => event.snapshot?.usage)
      .find((entry) => entry);
    expect(usage).toEqual({ used: 12, size: 100 });
  });

  it("renames and deletes a thread, and applies model and effort config", async () => {
    const started = await start();
    bridges.push(started.bridge);
    dirs.push(started.workspace);
    const base = `http://127.0.0.1:${started.bridge.port}`;
    const created = await fetch(`${base}/api/assistant/threads`, {
      method: "POST",
      body: "{}",
    });
    const thread = (await created.json()) as { id: string; title: string };
    const initial = (await (
      await fetch(`${base}/api/assistant/threads/${thread.id}/config`)
    ).json()) as {
      options: { id: string; currentValue: string }[];
    };
    expect(
      initial.options.find((option) => option.id === "model")?.currentValue,
    ).toBe("codex-demo");
    expect(
      initial.options.find((option) => option.id === "reasoning_effort")
        ?.currentValue,
    ).toBe("medium");

    const renamed = await fetch(`${base}/api/assistant/threads/${thread.id}`, {
      method: "PATCH",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ title: "Workspace review" }),
    });
    expect(((await renamed.json()) as { title: string }).title).toBe(
      "Workspace review",
    );

    const model = await fetch(
      `${base}/api/assistant/threads/${thread.id}/config`,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ configId: "model", value: "codex-fast" }),
      },
    );
    expect(model.status).toBe(200);
    const effort = await fetch(
      `${base}/api/assistant/threads/${thread.id}/config`,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ configId: "reasoning_effort", value: "high" }),
      },
    );
    const options = (
      (await effort.json()) as {
        options: { id: string; currentValue: string }[];
      }
    ).options;
    expect(options.find((option) => option.id === "model")?.currentValue).toBe(
      "codex-fast",
    );
    expect(
      options.find((option) => option.id === "reasoning_effort")?.currentValue,
    ).toBe("high");

    const removed = await fetch(`${base}/api/assistant/threads/${thread.id}`, {
      method: "DELETE",
    });
    expect(removed.status).toBe(200);
    const listed = (await (
      await fetch(`${base}/api/assistant/threads`)
    ).json()) as { threads: { id: string }[] };
    expect(listed.threads.some((entry) => entry.id === thread.id)).toBe(false);
  });

  it("reports a larger context window on the next turn", async () => {
    const started = await start();
    bridges.push(started.bridge);
    dirs.push(started.workspace);
    const base = `http://127.0.0.1:${started.bridge.port}`;
    const thread = (await (
      await fetch(`${base}/api/assistant/threads`, {
        method: "POST",
        body: "{}",
      })
    ).json()) as { id: string };

    async function finish(runId: string, userTexts: string[]): Promise<number> {
      const response = await fetch(
        `${base}/api/assistant/threads/${thread.id}/runs`,
        {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ runId, userTexts }),
        },
      );
      const reader = response.body?.getReader();
      if (!reader) throw new Error("missing body");
      const decoder = new TextDecoder();
      let buffer = "";
      let used = 0;
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
          const event = JSON.parse(data) as {
            type: string;
            snapshot?: {
              usage?: { used: number };
              tools: { approval?: { requestId: string } }[];
            };
          };
          if (event.snapshot?.usage) used = event.snapshot.usage.used;
          const pending = event.snapshot?.tools.find((tool) => tool.approval);
          if (pending?.approval) {
            await fetch(
              `${base}/api/assistant/permissions/${encodeURIComponent(pending.approval.requestId)}`,
              {
                method: "POST",
                headers: { "content-type": "application/json" },
                body: JSON.stringify({
                  threadId: thread.id,
                  runId,
                  optionId: "allow",
                }),
              },
            );
          }
        }
      }
      return used;
    }

    expect(await finish("run-a", ["first"])).toBe(12);
    expect(await finish("run-b", ["first", "second"])).toBe(24);
  });

  it("cancels an in-flight turn and rejects a second run plus a full-access mode", async () => {
    const started = await start();
    bridges.push(started.bridge);
    dirs.push(started.workspace);
    const base = `http://127.0.0.1:${started.bridge.port}`;
    const created = await fetch(`${base}/api/assistant/threads`, {
      method: "POST",
      body: "{}",
    });
    const thread = (await created.json()) as { id: string };
    const runningResponse = await fetch(
      `${base}/api/assistant/threads/${thread.id}/runs`,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          runId: "run-cancel",
          userTexts: ["please cancel"],
        }),
      },
    );
    expect(runningResponse.status).toBe(200);
    const duplicate = await fetch(
      `${base}/api/assistant/threads/${thread.id}/runs`,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ runId: "run-other", userTexts: ["again"] }),
      },
    );
    expect(duplicate.status).toBe(409);
    const cancel = await fetch(
      `${base}/api/assistant/threads/${thread.id}/cancel`,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ runId: "run-cancel" }),
      },
    );
    expect(cancel.status).toBe(202);
    const events = await readEvents(runningResponse);
    expect(events.at(-1)).toMatchObject({
      type: "done",
      stopReason: "cancelled",
    });

    process.env["INITIAL_AGENT_MODE"] = "agent-full-access";
    await expect(
      createBridge({ workspace: started.workspace }),
    ).rejects.toThrow(/agent-full-access/);
  });

  it("refuses direct browser headers, missing Hub tokens, and cross-user or cross-branch headers", async () => {
    const workspace = await mkdtemp(path.join(tmpdir(), "conduit-assistant-"));
    dirs.push(workspace);
    process.env["ASSISTANT_ACP_COMMAND"] = process.execPath;
    process.env["ASSISTANT_ACP_ARGS"] = JSON.stringify([fakeAgent]);
    process.env["ASSISTANT_ACP_FIXTURE"] = "1";
    process.env["INITIAL_AGENT_MODE"] = "workspace-write";
    const bridge = await createBridge({
      workspace,
      bridgeToken: "secret-hub-token",
      scope: {
        owner: "alice",
        workspaceId: "wsp-1",
        branch: "workspace/alice/wsp-1",
      },
    });
    bridges.push(bridge);
    const base = `http://127.0.0.1:${bridge.port}`;

    const browserAttempt = await fetch(`${base}/acp`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        origin: "http://127.0.0.1:5173",
        "x-conduit-bridge-token": "secret-hub-token",
        "x-conduit-workspace-owner": "alice",
        "x-conduit-workspace-id": "wsp-1",
        "x-conduit-workspace-branch": "workspace/alice/wsp-1",
      },
      body: JSON.stringify({
        jsonrpc: "2.0",
        id: 1,
        method: "initialize",
        params: { protocolVersion: 1 },
      }),
    });
    expect(browserAttempt.status).toBe(403);

    const secFetchAttempt = await fetch(`${base}/api/assistant/health`, {
      headers: {
        "sec-fetch-site": "cross-site",
        "sec-fetch-dest": "empty",
        "x-conduit-bridge-token": "secret-hub-token",
        "x-conduit-workspace-owner": "alice",
        "x-conduit-workspace-id": "wsp-1",
        "x-conduit-workspace-branch": "workspace/alice/wsp-1",
      },
    });
    expect(secFetchAttempt.status).toBe(403);

    const unauthenticated = await fetch(`${base}/acp`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        jsonrpc: "2.0",
        id: 1,
        method: "initialize",
        params: { protocolVersion: 1 },
      }),
    });
    expect(unauthenticated.status).toBe(401);

    const wrongUser = await fetch(`${base}/acp`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        "x-conduit-bridge-token": "secret-hub-token",
        "x-conduit-workspace-owner": "bob",
        "x-conduit-workspace-id": "wsp-1",
        "x-conduit-workspace-branch": "workspace/alice/wsp-1",
      },
      body: JSON.stringify({
        jsonrpc: "2.0",
        id: 1,
        method: "initialize",
        params: { protocolVersion: 1 },
      }),
    });
    expect(wrongUser.status).toBe(403);

    const wrongBranch = await fetch(`${base}/acp`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        "x-conduit-bridge-token": "secret-hub-token",
        "x-conduit-workspace-owner": "alice",
        "x-conduit-workspace-id": "wsp-1",
        "x-conduit-workspace-branch": "workspace/alice/other-branch",
      },
      body: JSON.stringify({
        jsonrpc: "2.0",
        id: 1,
        method: "initialize",
        params: { protocolVersion: 1 },
      }),
    });
    expect(wrongBranch.status).toBe(403);

    const validHubInit = await fetch(`${base}/acp`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        "x-conduit-bridge-token": "secret-hub-token",
        "x-conduit-workspace-owner": "alice",
        "x-conduit-workspace-id": "wsp-1",
        "x-conduit-workspace-branch": "workspace/alice/wsp-1",
      },
      body: JSON.stringify({
        jsonrpc: "2.0",
        id: 1,
        method: "initialize",
        params: { protocolVersion: 1 },
      }),
    });
    expect(validHubInit.status).toBe(200);
    expect(await validHubInit.json()).toMatchObject({
      jsonrpc: "2.0",
      id: 1,
      result: { protocolVersion: 1 },
    });
  });

  it("checks the pinned ACP protocol version during initialize on both sides", async () => {
    const workspace = await mkdtemp(path.join(tmpdir(), "conduit-assistant-"));
    dirs.push(workspace);
    process.env["ASSISTANT_ACP_COMMAND"] = process.execPath;
    process.env["ASSISTANT_ACP_ARGS"] = JSON.stringify([fakeAgent]);
    process.env["ASSISTANT_ACP_FIXTURE"] = "1";
    process.env["INITIAL_AGENT_MODE"] = "workspace-write";
    process.env["FAKE_ACP_PROTOCOL_VERSION"] = "2";
    try {
      await expect(createBridge({ workspace })).rejects.toThrow(
        /negotiated protocol 2, expected 1/,
      );
    } finally {
      delete process.env["FAKE_ACP_PROTOCOL_VERSION"];
    }

    const started = await start();
    bridges.push(started.bridge);
    dirs.push(started.workspace);
    const mismatch = await fetch(
      `http://127.0.0.1:${started.bridge.port}/acp`,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          jsonrpc: "2.0",
          id: 9,
          method: "initialize",
          params: { protocolVersion: 2 },
        }),
      },
    );
    expect(mismatch.status).toBe(400);
    expect(await mismatch.json()).toMatchObject({
      jsonrpc: "2.0",
      id: 9,
      error: { code: -32602 },
    });
  });

  it("gates ACP file writes and commands on governed permission settlement and blocks denied edits", async () => {
    const { readFile } = await import("node:fs/promises");
    const started = await start();
    bridges.push(started.bridge);
    dirs.push(started.workspace);
    const base = `http://127.0.0.1:${started.bridge.port}`;

    const sessionRes = await fetch(`${base}/acp`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        jsonrpc: "2.0",
        id: 1,
        method: "session/new",
        params: { _meta: { threadId: "wsp-acp-t1" } },
      }),
    });
    const sessionBody = (await sessionRes.json()) as {
      result: { sessionId: string };
    };
    const sessionId = sessionBody.result.sessionId;

    // 1. Denied permission blocks the file write.
    const deniedPrompt = await fetch(`${base}/acp`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        jsonrpc: "2.0",
        id: 2,
        method: "session/prompt",
        params: {
          sessionId,
          _meta: { runId: "run-denied" },
          prompt: [{ type: "text", text: "edit notes.txt: forbidden write" }],
        },
      }),
    });
    const deniedReader = deniedPrompt.body?.getReader();
    if (!deniedReader) throw new Error("missing stream");
    const decoder = new TextDecoder();
    let buf = "";
    let sawPerm = false;
    for (;;) {
      const chunk = await deniedReader.read();
      if (chunk.done) break;
      buf += decoder.decode(chunk.value, { stream: true });
      const frames = buf.split("\n\n");
      buf = frames.pop() ?? "";
      for (const frame of frames) {
        const raw = frame
          .split("\n")
          .filter((l) => l.startsWith("data:"))
          .map((l) => l.slice(5).trim())
          .join("\n");
        if (!raw) continue;
        const ev = JSON.parse(raw) as {
          type: string;
          request?: {
            requestId: string;
            options: { optionId: string; kind: string }[];
          };
        };
        if (ev.type === "permission_request" && ev.request && !sawPerm) {
          sawPerm = true;
          expect(
            ev.request.options.some((opt) => opt.kind === "allow_always"),
          ).toBe(false);
          const settle = await fetch(
            `${base}/acp/permissions/${encodeURIComponent(ev.request.requestId)}`,
            {
              method: "POST",
              headers: { "content-type": "application/json" },
              body: JSON.stringify({
                threadId: "wsp-acp-t1",
                runId: "run-denied",
                optionId: "reject",
              }),
            },
          );
          expect(settle.status).toBe(200);
        }
      }
    }
    expect(sawPerm).toBe(true);
    await expect(
      readFile(path.join(started.workspace, "notes.txt"), "utf8"),
    ).rejects.toThrow();

    // 2. Approved permission writes the file and emits an activity event with sha256 digest.
    const approvedPrompt = await fetch(`${base}/acp`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        jsonrpc: "2.0",
        id: 3,
        method: "session/prompt",
        params: {
          sessionId,
          _meta: { runId: "run-approved" },
          prompt: [{ type: "text", text: "edit notes.txt: governed content" }],
        },
      }),
    });
    const approvedReader = approvedPrompt.body?.getReader();
    if (!approvedReader) throw new Error("missing stream");
    let buf2 = "";
    const activityEvents: Record<string, unknown>[] = [];
    for (;;) {
      const chunk = await approvedReader.read();
      if (chunk.done) break;
      buf2 += decoder.decode(chunk.value, { stream: true });
      const frames = buf2.split("\n\n");
      buf2 = frames.pop() ?? "";
      for (const frame of frames) {
        const raw = frame
          .split("\n")
          .filter((l) => l.startsWith("data:"))
          .map((l) => l.slice(5).trim())
          .join("\n");
        if (!raw) continue;
        const ev = JSON.parse(raw) as Record<string, unknown>;
        if (ev["type"] === "activity") activityEvents.push(ev);
        if (ev["type"] === "permission_request") {
          const req = ev["request"] as { requestId: string };
          await fetch(
            `${base}/acp/permissions/${encodeURIComponent(req.requestId)}`,
            {
              method: "POST",
              headers: { "content-type": "application/json" },
              body: JSON.stringify({
                threadId: "wsp-acp-t1",
                runId: "run-approved",
                optionId: "allow",
              }),
            },
          );
        }
      }
    }
    expect(
      await readFile(path.join(started.workspace, "notes.txt"), "utf8"),
    ).toBe("governed content");
    expect(
      activityEvents.some(
        (ev) =>
          ev["kind"] === "file_write" &&
          ev["path"] === "notes.txt" &&
          typeof ev["digest"] === "string" &&
          String(ev["digest"]).startsWith("sha256:"),
      ),
    ).toBe(true);
  });

  it("does not spend approval for a benign path on a different protected write", async () => {
    const { readFile } = await import("node:fs/promises");
    const started = await start();
    bridges.push(started.bridge);
    dirs.push(started.workspace);
    const base = `http://127.0.0.1:${started.bridge.port}`;
    const sessionResponse = await fetch(`${base}/acp`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        jsonrpc: "2.0",
        id: 1,
        method: "session/new",
        params: { _meta: { threadId: "scope-substitution" } },
      }),
    });
    const session = (await sessionResponse.json()) as {
      result: { sessionId: string };
    };
    const response = await fetch(`${base}/acp`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        jsonrpc: "2.0",
        id: 2,
        method: "session/prompt",
        params: {
          sessionId: session.result.sessionId,
          _meta: { runId: "scope-substitution-run" },
          prompt: [
            {
              type: "text",
              text: "edit notes.txt -> .github/workflows/changed.yml: unauthorized content",
            },
          ],
        },
      }),
    });
    const reader = response.body!.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    const requests: { path?: string; riskTier?: string }[] = [];
    for (;;) {
      const chunk = await reader.read();
      if (chunk.done) break;
      buffer += decoder.decode(chunk.value, { stream: true });
      const frames = buffer.split("\n\n");
      buffer = frames.pop() ?? "";
      for (const frame of frames) {
        const raw = frame
          .split("\n")
          .filter((line) => line.startsWith("data:"))
          .map((line) => line.slice(5).trim())
          .join("\n");
        if (!raw) continue;
        const event = JSON.parse(raw) as {
          type: string;
          request?: { requestId: string; path?: string; riskTier?: string };
        };
        if (event.type !== "permission_request" || !event.request) continue;
        const request = event.request;
        requests.push(request);
        const settlement = await fetch(
          `${base}/acp/permissions/${encodeURIComponent(request.requestId)}`,
          {
            method: "POST",
            headers: { "content-type": "application/json" },
            body: JSON.stringify({
              threadId: "scope-substitution",
              runId: "scope-substitution-run",
              optionId: request.path === "notes.txt" ? "allow" : "reject",
            }),
          },
        );
        expect(settlement.status).toBe(200);
      }
    }
    expect(requests).toEqual([
      expect.objectContaining({ path: "notes.txt", riskTier: "low_confirm" }),
      expect.objectContaining({
        path: ".github/workflows/changed.yml",
        riskTier: "high_change_request",
      }),
    ]);
    await expect(
      readFile(path.join(started.workspace, ".github/workflows/changed.yml")),
    ).rejects.toMatchObject({ code: "ENOENT" });
  });

  it("blocks bridge HTTP egress in offline mode and strips external API keys", async () => {
    const workspace = await mkdtemp(path.join(tmpdir(), "conduit-assistant-"));
    dirs.push(workspace);
    const savedOpenAi = process.env["OPENAI_API_KEY"];
    const savedCodex = process.env["CODEX_API_KEY"];
    const savedGemini = process.env["GEMINI_API_KEY"];
    process.env["ASSISTANT_ACP_COMMAND"] = process.execPath;
    process.env["ASSISTANT_ACP_ARGS"] = JSON.stringify([fakeAgent]);
    process.env["ASSISTANT_ACP_FIXTURE"] = "1";
    process.env["INITIAL_AGENT_MODE"] = "workspace-write";
    process.env["OPENAI_API_KEY"] = "sk-shouldnotleaktoofflinechild";
    process.env["CODEX_API_KEY"] = "sk-shouldnotleaktoofflinechild2";
    process.env["GEMINI_API_KEY"] = "fixture-gemini-must-not-reach-child";
    process.env["FAKE_ACP_AUTH_METHODS"] = "api-key";
    try {
      const bridge = await createBridge({
        workspace,
        offlineBundle: true,
      });
      bridges.push(bridge);
      expect(bridge.offlineBundle).toBe(true);

      await expect(
        bridge.outboundFetch("https://api.openai.com/v1/responses"),
      ).rejects.toThrow(/OFFLINE_BUNDLE_EGRESS_DENIED/);

      const base = `http://127.0.0.1:${bridge.port}`;
      const localHealth = await bridge.outboundFetch(
        `${base}/api/assistant/health`,
      );
      expect(localHealth.status).toBe(200);
      expect(await localHealth.json()).toMatchObject({ offlineBundle: true });

      const sessionRes = await fetch(`${base}/acp`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          jsonrpc: "2.0",
          id: 1,
          method: "session/new",
          params: {},
        }),
      });
      const {
        result: { sessionId },
      } = (await sessionRes.json()) as { result: { sessionId: string } };
      const promptRes = await fetch(`${base}/acp`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          jsonrpc: "2.0",
          id: 2,
          method: "session/prompt",
          params: {
            sessionId,
            prompt: [{ type: "text", text: "check offline env" }],
          },
        }),
      });
      const events = await readEvents(promptRes);
      const snapshotText =
        events.findLast((ev) => ev.type === "snapshot")?.snapshot?.text ?? "";
      expect(JSON.parse(snapshotText)).toEqual({
        authenticated: false,
        hasOpenAiKey: false,
        hasCodexKey: false,
        hasGeminiKey: false,
        offlineBundle: "1",
        noProxy: "*",
      });
    } finally {
      delete process.env["FAKE_ACP_AUTH_METHODS"];
      if (savedOpenAi === undefined) delete process.env["OPENAI_API_KEY"];
      else process.env["OPENAI_API_KEY"] = savedOpenAi;
      if (savedCodex === undefined) delete process.env["CODEX_API_KEY"];
      else process.env["CODEX_API_KEY"] = savedCodex;
      if (savedGemini === undefined) delete process.env["GEMINI_API_KEY"];
      else process.env["GEMINI_API_KEY"] = savedGemini;
    }
  });
});

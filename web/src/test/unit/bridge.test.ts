import { afterEach, describe, expect, it } from "vitest";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { createBridge, type BridgeServer } from "../../server/http.ts";

const fakeAgent = fileURLToPath(new URL("../../server/fake-agent.mjs", import.meta.url));
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
  snapshot?: { text: string; reasoning: string; tools: { approval?: { requestId: string } }[] };
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
      if (data.length > 0) events.push(JSON.parse(data) as (typeof events)[number]);
    }
  }
  return events;
}

describe("ACP bridge", () => {
  const bridges: BridgeServer[] = [];
  const dirs: string[] = [];

  afterEach(async () => {
    for (const bridge of bridges.splice(0)) await bridge.close();
    for (const dir of dirs.splice(0)) await rm(dir, { recursive: true, force: true });
    if (previous.command === undefined) delete process.env["ASSISTANT_ACP_COMMAND"];
    else process.env["ASSISTANT_ACP_COMMAND"] = previous.command;
    if (previous.args === undefined) delete process.env["ASSISTANT_ACP_ARGS"];
    else process.env["ASSISTANT_ACP_ARGS"] = previous.args;
    if (previous.fixture === undefined) delete process.env["ASSISTANT_ACP_FIXTURE"];
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
    expect(await health.json()).toMatchObject({ fixture: true, mode: "workspace-write" });

    const created = await fetch(`${base}/api/assistant/threads`, { method: "POST", body: "{}" });
    const thread = (await created.json()) as { id: string };
    const run = fetch(`${base}/api/assistant/threads/${thread.id}/runs`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ runId: "run-1", userTexts: ["inspect the workspace"] }),
    });
    const response = await run;
    const reader = response.body?.getReader();
    if (!reader) throw new Error("missing body");
    const decoder = new TextDecoder();
    let buffer = "";
    let requestId = "";
    const events: BridgeEvent[] = [];
    while (!events.some((event) => event.type === "done" || event.type === "error")) {
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
          snapshot?: { text: string; reasoning: string; tools: { approval?: { requestId: string } }[] };
          stopReason?: string;
        };
        events.push(event);
        const pending = event.snapshot?.tools.find((tool) => tool.approval);
        if (pending?.approval && requestId.length === 0) {
          requestId = pending.approval.requestId;
          const decision = await fetch(`${base}/api/assistant/permissions/${encodeURIComponent(requestId)}`, {
            method: "POST",
            headers: { "content-type": "application/json" },
            body: JSON.stringify({ threadId: thread.id, runId: "run-1", optionId: "allow" }),
          });
          expect(decision.status).toBe(200);
          const replay = await fetch(`${base}/api/assistant/permissions/${encodeURIComponent(requestId)}`, {
            method: "POST",
            headers: { "content-type": "application/json" },
            body: JSON.stringify({ threadId: thread.id, runId: "run-1", optionId: "allow" }),
          });
          expect(replay.status).toBe(409);
        }
      }
    }
    expect(requestId).toContain(thread.id);
    const done = events.find((event) => event.type === "done");
    expect(done?.stopReason).toBe("end_turn");
    const text = events.map((event) => event.snapshot?.text ?? "").join("");
    expect(text).toContain("Read complete.");
    expect(events.some((event) => event.snapshot?.reasoning.includes("sk-"))).toBe(false);
    expect(events.some((event) => (event.snapshot?.reasoning ?? "").includes("[redacted]"))).toBe(true);
  });

  it("cancels an in-flight turn and rejects a second run plus a full-access mode", async () => {
    const started = await start();
    bridges.push(started.bridge);
    dirs.push(started.workspace);
    const base = `http://127.0.0.1:${started.bridge.port}`;
    const created = await fetch(`${base}/api/assistant/threads`, { method: "POST", body: "{}" });
    const thread = (await created.json()) as { id: string };
    const runningResponse = await fetch(`${base}/api/assistant/threads/${thread.id}/runs`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ runId: "run-cancel", userTexts: ["please cancel"] }),
    });
    expect(runningResponse.status).toBe(200);
    const duplicate = await fetch(`${base}/api/assistant/threads/${thread.id}/runs`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ runId: "run-other", userTexts: ["again"] }),
    });
    expect(duplicate.status).toBe(409);
    const cancel = await fetch(`${base}/api/assistant/threads/${thread.id}/cancel`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ runId: "run-cancel" }),
    });
    expect(cancel.status).toBe(202);
    const events = await readEvents(runningResponse);
    expect(events.at(-1)).toMatchObject({ type: "done", stopReason: "cancelled" });

    process.env["INITIAL_AGENT_MODE"] = "agent-full-access";
    await expect(createBridge({ workspace: started.workspace })).rejects.toThrow(/agent-full-access/);
  });
});

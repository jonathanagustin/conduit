import { describe, expect, it, vi } from "vitest";
import {
  AgentExchange,
  readAgentStream,
  runAgent,
} from "../../console/operator/agent-runs.js";
const start = { type: "RUN_STARTED", threadId: "s1", runId: "r1" };
const finish = {
  type: "RUN_FINISHED",
  threadId: "s1",
  runId: "r1",
  outcome: { type: "success" },
};
const snapshot = {
  type: "STATE_SNAPSHOT",
  snapshot: { session: { status: "inspecting" }, toolCalls: [] },
};
const exchange = () => new AgentExchange("s1", "r1");
const frames = (events: unknown[]) =>
  events.map((event) => `data: ${JSON.stringify(event)}\n\n`).join("");
function response(text: string, split = 7) {
  const bytes = new TextEncoder().encode(text);
  return new Response(
    new ReadableStream({
      start(controller) {
        for (let n = 0; n < bytes.length; n += split)
          controller.enqueue(bytes.slice(n, n + split));
        controller.close();
      },
    }),
    { headers: { "content-type": "text/event-stream" } },
  );
}
describe("Hub AG-UI browser exchange", () => {
  it("replays snapshots, state patches, message replacement and split UTF-8", async () => {
    const run = await readAgentStream(
      response(
        ": heartbeat\n\n" +
          frames([
            start,
            snapshot,
            {
              type: "STATE_DELTA",
              delta: [
                { op: "replace", path: "/session/status", value: "ready" },
              ],
            },
            {
              type: "MESSAGES_SNAPSHOT",
              messages: [{ id: "old", role: "user", content: "inspect" }],
            },
            { type: "TEXT_MESSAGE_START", messageId: "new", role: "assistant" },
            {
              type: "TEXT_MESSAGE_CONTENT",
              messageId: "new",
              delta: "Ready ✓",
            },
            { type: "TEXT_MESSAGE_END", messageId: "new" },
            {
              type: "MESSAGES_SNAPSHOT",
              messages: [{ id: "new", role: "assistant", content: "Ready ✓" }],
            },
            finish,
          ]),
        1,
      ),
      exchange(),
    );
    expect(run.state.session.status).toBe("ready");
    expect([...run.messages.values()]).toEqual([
      { id: "new", role: "assistant", content: "Ready ✓" },
    ]);
    expect(run.terminal).toBe(true);
  });
  it("orders tool boundaries and keeps backend results as display data", () => {
    const run = exchange().accept(start);
    for (const event of [
      { type: "TEXT_MESSAGE_START", messageId: "m", role: "assistant" },
      {
        type: "TOOL_CALL_START",
        toolCallId: "t",
        toolCallName: "read_file",
        parentMessageId: "m",
      },
      { type: "TOOL_CALL_ARGS", toolCallId: "t", delta: '{"path":"go.mod"}' },
      { type: "TOOL_CALL_END", toolCallId: "t" },
      { type: "TEXT_MESSAGE_END", messageId: "m" },
      {
        type: "TOOL_CALL_RESULT",
        toolCallId: "t",
        messageId: "result",
        content: '{"ok":true}',
      },
      finish,
    ])
      run.accept(event);
    expect(run.tools.get("t").status).toBe("complete");
    expect(run.messages.get("result").role).toBe("tool");
  });
  it.each(
    [
      [{ type: "TEXT_MESSAGE_CONTENT", messageId: "missing", delta: "x" }],
      [start, start],
      [start, { type: "STEP_FINISHED", stepName: "missing" }],
      [
        start,
        { type: "MESSAGES_SNAPSHOT", messages: [{ id: "old", role: "user" }] },
        { type: "MESSAGES_SNAPSHOT", messages: [] },
        { type: "TEXT_MESSAGE_START", messageId: "old", role: "assistant" },
      ],
      [start, { type: "RUN_FINISHED", threadId: "other", runId: "r1" }],
      [
        start,
        { type: "TEXT_MESSAGE_START", messageId: "m", role: "assistant" },
        finish,
      ],
      [start, { type: "TOOL_CALL_ARGS", toolCallId: "missing", delta: "{}" }],
      [
        start,
        { type: "TEXT_MESSAGE_START", messageId: "m", role: "assistant" },
        { type: "MESSAGES_SNAPSHOT", messages: [] },
      ],
      [start, finish, snapshot],
    ].map((events) => ({ events })),
  )("rejects broken event order $events", ({ events }) => {
    expect(() => {
      const run = exchange();
      for (const event of events) run.accept(event);
    }).toThrow("invalid agent stream");
  });
  it("does not turn truncated, malformed or failed streams into success", async () => {
    await expect(
      readAgentStream(response(frames([start, snapshot])), exchange()),
    ).rejects.toThrow("interrupted");
    await expect(
      readAgentStream(
        response(frames([start]) + "data: {bad}\n\n"),
        exchange(),
      ),
    ).rejects.toThrow();
    await expect(
      readAgentStream(
        response(
          frames([
            start,
            { type: "RUN_ERROR", message: "refused", code: "DENIED" },
          ]),
        ),
        exchange(),
      ),
    ).rejects.toMatchObject({ code: "DENIED" });
  });
  it("sends snapshot recovery as a fresh run without any fallback turn", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(response(frames([start, snapshot, finish])));
    await runAgent(
      {
        threadId: "s1",
        runId: "r1",
        protocolVersion: "1.0",
        messages: [],
        forwardedProps: { conduit: { mode: "snapshot" } },
      },
      null,
      fetcher,
    );
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(fetcher.mock.calls[0][0]).toBe("/v1/agent-runs");
    expect(
      JSON.parse(fetcher.mock.calls[0][1].body).forwardedProps.conduit.mode,
    ).toBe("snapshot");
  });
});

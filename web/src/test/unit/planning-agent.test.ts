import { describe, expect, it, vi } from "vite-plus/test";
import { PlanningAgent } from "../../console/operator/planning-agent.js";
const session = {
  id: "s1",
  status: "needs_input",
  needsInput: [{ id: "port", type: "string", required: true }],
};
function stream(
  input: { threadId: string; runId: string },
  state: unknown,
  outcome: Record<string, unknown> = { type: "success" },
) {
  return new Response(
    [
      { type: "RUN_STARTED", threadId: input.threadId, runId: input.runId },
      { type: "STATE_SNAPSHOT", snapshot: state },
      {
        type: "MESSAGES_SNAPSHOT",
        messages: [{ id: "user", role: "user", content: "inspect" }],
      },
      {
        type: "RUN_FINISHED",
        threadId: input.threadId,
        runId: input.runId,
        outcome,
      },
    ]
      .map((event) => `data: ${JSON.stringify(event)}\n\n`)
      .join(""),
    { headers: { "content-type": "text/event-stream" } },
  );
}
const binding = () => ({
  fields: () =>
    new Map([["question:port", { type: "string", readOnly: false }]]),
  dirty: () => false,
  set: vi.fn(),
  focus: vi.fn(),
  confirm: vi.fn(async () => true),
});
describe("planning agent exchange controller", () => {
  it("resumes the exact form with human values instead of calling native answers", async () => {
    const seen: Record<string, unknown>[] = [];
    const fetcher = vi.fn(async (_url, init) => {
      const input = JSON.parse(init.body);
      seen.push(input);
      return stream(
        input,
        { session, pendingClientTools: [] },
        seen.length === 1
          ? {
              type: "interrupt",
              interrupts: [{ id: "form-1", reason: "needs_input" }],
            }
          : { type: "success" },
      );
    });
    const agent = new PlanningAgent("s1", fetcher);
    await agent.run({ message: "inspect" }, binding(), () => {});
    await agent.run(
      { answers: [{ id: "port", value: "9090" }] },
      binding(),
      () => {},
    );
    expect(seen[1].resume).toEqual([
      {
        interruptId: "form-1",
        status: "resolved",
        payload: { "question:port": "9090" },
      },
    ]);
    expect(seen[0].runId).not.toBe(seen[1].runId);
    expect(fetcher.mock.calls.every(([url]) => url === "/v1/agent-runs")).toBe(
      true,
    );
  });
  it("does not replay model input after truncation and recovers replaced messages", async () => {
    const seen: Record<string, unknown>[] = [];
    const fetcher = vi.fn(async (_url, init) => {
      const input = JSON.parse(init.body);
      seen.push(input);
      if (seen.length === 1)
        return new Response(
          `data: ${JSON.stringify({ type: "RUN_STARTED", threadId: input.threadId, runId: input.runId })}\n\n`,
          { headers: { "content-type": "text/event-stream" } },
        );
      return stream(input, { session, pendingClientTools: [] });
    });
    const agent = new PlanningAgent("s1", fetcher);
    await expect(
      agent.run({ message: "inspect" }, binding(), () => {}),
    ).rejects.toThrow("interrupted");
    await expect(
      agent.run({ message: "again" }, binding(), () => {}),
    ).rejects.toThrow("Recover");
    expect(fetcher).toHaveBeenCalledTimes(1);
    await agent.run({ recover: true }, binding(), () => {});
    expect(seen[1].forwardedProps).toEqual({ conduit: { mode: "snapshot" } });
    expect(agent.messages).toEqual([
      { id: "user", role: "user", content: "inspect" },
    ]);
    expect(agent.recoveryRequired).toBe(false);
  });
  it("replays only eligible pending tools once, and sends results in a new exchange", async () => {
    const seen: any[] = [];
    const pending = {
      id: "old-tool",
      name: "fill_field",
      arguments: { field: "question:port", value: "8080" },
      eligible: true,
    };
    const fetcher = vi.fn(async (_url, init) => {
      const input = JSON.parse(init.body);
      seen.push(input);
      return stream(input, {
        session,
        pendingClientTools: seen.length === 1 ? [pending] : [],
      });
    });
    const form = binding();
    const agent = new PlanningAgent("s1", fetcher);
    await agent.run({ recover: true }, form, () => {});
    expect(form.set).toHaveBeenCalledExactlyOnceWith("question:port", "8080");
    expect(seen[1].messages.at(-1)).toMatchObject({
      role: "tool",
      toolCallId: "old-tool",
      content: '{"ok":true}',
    });
    expect(seen[1].forwardedProps).toBeUndefined();
  });
  it("does not execute requests left ineligible after a failed prior run", async () => {
    const form = binding();
    const fetcher = vi.fn(async (_url, init) =>
      stream(JSON.parse(init.body), {
        session,
        pendingClientTools: [
          {
            id: "t",
            eligible: false,
            name: "fill_field",
            arguments: { field: "question:port", value: "bad" },
          },
        ],
      }),
    );
    await new PlanningAgent("s1", fetcher).run(
      { recover: true },
      form,
      () => {},
    );
    expect(form.set).not.toHaveBeenCalled();
    expect(fetcher).toHaveBeenCalledTimes(1);
  });
  it("preserves earlier messages across subsequent turns when server snapshots omit prior client ids (#695)", async () => {
    let call = 0;
    const fetcher = vi.fn(async (_url, init) => {
      call++;
      const input = JSON.parse(init.body);
      const messages =
        call === 1
          ? [
              { id: "srv-u1", role: "user", content: "first turn" },
              { id: "srv-m1", role: "assistant", content: "need port" },
            ]
          : [
              { id: "srv-u2", role: "user", content: "second turn" },
              { id: "srv-m2", role: "assistant", content: "plan ready" },
            ];
      return new Response(
        [
          { type: "RUN_STARTED", threadId: input.threadId, runId: input.runId },
          {
            type: "STATE_SNAPSHOT",
            snapshot: { session, pendingClientTools: [] },
          },
          { type: "MESSAGES_SNAPSHOT", messages },
          {
            type: "RUN_FINISHED",
            threadId: input.threadId,
            runId: input.runId,
            outcome: { type: "success" },
          },
        ]
          .map((event) => `data: ${JSON.stringify(event)}\n\n`)
          .join(""),
        { headers: { "content-type": "text/event-stream" } },
      );
    });
    const agent = new PlanningAgent("s1", fetcher);
    await agent.run({ message: "first turn" }, binding(), () => {});
    await agent.run({ message: "second turn" }, binding(), () => {});
    expect(agent.messages.map((m) => m.content)).toEqual([
      "first turn",
      "need port",
      "second turn",
      "plan ready",
    ]);
  });
});

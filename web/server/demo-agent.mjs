// Deterministic ACP fixture; never invokes a model, command, or deployment.
import { createInterface } from "node:readline";
const sessionId = "conduit-simulation";
let pending, timer;
let sequence = 0;
const send = (m) =>
  process.stdout.write(JSON.stringify({ jsonrpc: "2.0", ...m }) + "\n");
const update = (m) =>
  send({ method: "session/update", params: { sessionId, update: m } });
const text = (value) =>
  update({
    sessionUpdate: "agent_message_chunk",
    content: { type: "text", text: value },
  });
const finish = (stopReason = "end_turn") => {
  clearTimeout(timer);
  if (pending) send({ id: pending.promptId, result: { stopReason } });
  pending = undefined;
};
createInterface({ input: process.stdin }).on("line", (line) => {
  const m = JSON.parse(line);
  if (m.method === "initialize")
    send({
      id: m.id,
      result: {
        protocolVersion: 1,
        agentCapabilities: {},
        agentInfo: {
          name: "conduit-simulation",
          title: "Simulated agent",
          version: "0.1.0",
        },
      },
    });
  else if (m.method === "session/new")
    send({ id: m.id, result: { sessionId } });
  else if (m.method === "session/prompt") {
    if (pending) {
      send({
        id: m.id,
        error: { code: -32600, message: "Simulation already running" },
      });
      return;
    }
    const id = `demo-${++sequence}`;
    pending = {
      promptId: m.id,
      permissionId: id,
      toolCallId: `${id}-approval`,
    };
    text("**Simulation — no model or deployment is running.**\n\n");
    update({
      sessionUpdate: "plan",
      entries: [
        {
          content: "Inspect a sample proposal",
          priority: "medium",
          status: "completed",
        },
        {
          content: "Show fixture validation results",
          priority: "medium",
          status: "in_progress",
        },
        {
          content: "Request simulated approval",
          priority: "medium",
          status: "pending",
        },
      ],
    });
    timer = setTimeout(() => {
      text(
        "Sample proposal: update a dependency. Fixture results: build passed, tests passed, scan passed. These are illustrative results, not executed checks.\n\n",
      );
      const toolCall = {
        toolCallId: pending.toolCallId,
        title: "Simulated deployment approval",
        kind: "execute",
        status: "pending",
        rawInput: { simulation: true },
      };
      update({ sessionUpdate: "tool_call", ...toolCall });
      send({
        id,
        method: "session/request_permission",
        params: {
          sessionId,
          toolCall,
          options: [
            {
              optionId: "allow",
              name: "Approve simulation",
              kind: "allow_once",
            },
            { optionId: "deny", name: "Deny simulation", kind: "reject_once" },
          ],
        },
      });
    }, 600);
  } else if (m.method === "session/cancel") {
    if (pending) text("\nSimulation cancelled. Nothing was executed.\n");
    finish("cancelled");
  } else if (pending && m.id === pending.permissionId && m.result) {
    const allowed = m.result.outcome?.optionId === "allow";
    update({
      sessionUpdate: "tool_call_update",
      toolCallId: pending.toolCallId,
      status: allowed ? "completed" : "failed",
      rawOutput: {
        simulation: true,
        decision: allowed ? "approved" : "denied",
        deployed: false,
      },
    });
    text(
      allowed
        ? "\nSimulation approved. No application was deployed.\n"
        : "\nSimulation denied. Nothing was deployed.\n",
    );
    finish();
  } else if (m.id !== undefined && m.method)
    send({
      id: m.id,
      error: { code: -32601, message: "Unsupported simulation method" },
    });
});

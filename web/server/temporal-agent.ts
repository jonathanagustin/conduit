// ACP rehearsal agent. Temporal is real; validation/deployment activities are fixtures.
import { createInterface } from "node:readline";
import { randomUUID } from "node:crypto";
import { setTimeout as delay } from "node:timers/promises";
import { Client, Connection, type WorkflowHandle } from "@temporalio/client";
import {
  APPROVAL_SIGNAL,
  STATE_QUERY,
  TASK_QUEUE,
  type DeliveryState,
} from "../orchestration/contracts";

const sessionId = randomUUID();
let pending:
  | {
      id: string | number;
      handle?: WorkflowHandle;
      permissionId?: string;
      toolCallId?: string;
      cancelRequested?: boolean;
    }
  | undefined;
const connection = await Connection.connect({
  address: process.env.TEMPORAL_ADDRESS ?? "localhost:7233",
});
const client = new Client({
  connection,
  namespace: process.env.TEMPORAL_NAMESPACE ?? "default",
});
const send = (message: object) =>
  process.stdout.write(JSON.stringify({ jsonrpc: "2.0", ...message }) + "\n");
const update = (value: object) =>
  send({ method: "session/update", params: { sessionId, update: value } });
const text = (value: string) =>
  update({
    sessionUpdate: "agent_message_chunk",
    content: { type: "text", text: value },
  });

async function runPrompt(id: string | number, prompt: string) {
  const turn = { id } as NonNullable<typeof pending>;
  pending = turn;
  try {
    const resume = /^resume (conduit-rehearsal-[a-z0-9-]+)$/i.exec(
      prompt.trim(),
    );
    const workflowId = resume?.[1] ?? `conduit-rehearsal-${randomUUID()}`;
    turn.handle = resume
      ? client.workflow.getHandle(workflowId)
      : await client.workflow.start("deliveryRehearsal", {
          workflowId,
          taskQueue: TASK_QUEUE,
          args: [
            {
              summary: prompt,
              failValidation: prompt.trim() === "fail-validation",
            },
          ],
        });
    if (turn.cancelRequested) await turn.handle.cancel();
    text(
      `**Temporal rehearsal — fixture activities, no model or deployment.**\n\nWorkflow: \`${workflowId}\`. To reconnect later, send \`resume ${workflowId}\`.\n\n`,
    );
    let state: DeliveryState;
    do {
      state = await turn.handle.query<DeliveryState>(STATE_QUERY);
      if (state.status === "validating") await delay(150);
    } while (state.status === "validating");
    if (state.evidence)
      text(
        `Fixture validation: ${state.evidence.checks.map((check) => `${check.name}: ${check.passed ? "passed" : "failed"}`).join(", ")}. These checks were not executed against an application.\n\n`,
      );
    if (state.status === "awaiting_approval") {
      turn.permissionId = randomUUID();
      turn.toolCallId = `${workflowId}/approval`;
      const toolCall = {
        toolCallId: turn.toolCallId,
        title: "Approve Temporal rehearsal",
        kind: "execute",
        status: "pending",
        rawInput: { workflowId, simulation: true },
      };
      update({ sessionUpdate: "tool_call", ...toolCall });
      send({
        id: turn.permissionId,
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
    }
    state = (await turn.handle.result()) as DeliveryState;
    if (turn.toolCallId)
      update({
        sessionUpdate: "tool_call_update",
        toolCallId: turn.toolCallId,
        status: state.status === "completed" ? "completed" : "failed",
        rawOutput: state,
      });
    text(
      `\nTemporal rehearsal: **${state.status}**. No application was deployed.\n`,
    );
    send({
      id,
      result: {
        stopReason: state.status === "cancelled" ? "cancelled" : "end_turn",
      },
    });
  } catch {
    send({
      id,
      error: {
        code: -32603,
        message:
          "Temporal rehearsal unavailable. Check the local Temporal server and worker; the workflow may still be pending.",
      },
    });
  } finally {
    if (pending === turn) pending = undefined;
  }
}

createInterface({ input: process.stdin }).on("line", (line) => {
  const message = JSON.parse(line);
  if (message.method === "initialize")
    send({
      id: message.id,
      result: {
        protocolVersion: 1,
        agentCapabilities: {},
        agentInfo: {
          name: "conduit-temporal-rehearsal",
          title: "Temporal rehearsal",
          version: "0.1.0",
        },
      },
    });
  else if (message.method === "session/new")
    send({ id: message.id, result: { sessionId } });
  else if (message.method === "session/prompt") {
    if (pending) {
      send({
        id: message.id,
        error: { code: -32600, message: "Rehearsal already running" },
      });
      return;
    }
    const prompt = (message.params?.prompt ?? [])
      .filter((part: { type: string }) => part.type === "text")
      .map((part: { text: string }) => part.text)
      .join("\n");
    void runPrompt(message.id, prompt);
  } else if (message.method === "session/cancel") {
    if (pending) pending.cancelRequested = true;
    void pending?.handle
      ?.cancel()
      .catch(() =>
        text(
          "Cancellation could not be confirmed; inspect the workflow in Temporal.\n",
        ),
      );
  } else if (pending?.permissionId === message.id && message.result) {
    void pending?.handle
      ?.signal(APPROVAL_SIGNAL, message.result.outcome?.optionId === "allow")
      .catch(() =>
        text(
          "Approval could not be recorded; inspect the workflow in Temporal.\n",
        ),
      );
  } else if (message.id !== undefined && message.method)
    send({
      id: message.id,
      error: { code: -32601, message: "Unsupported rehearsal method" },
    });
});

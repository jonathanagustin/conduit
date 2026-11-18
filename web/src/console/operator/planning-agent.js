import { runAgent } from "./agent-runs.js";
import { FormToolExecutor, formToolOffers } from "./agent-form-tools.js";

export function mergeAgentMessages(prior, current) {
  if (!current || !current.length) return prior;
  const merged = [...prior];
  const byId = new Map();
  merged.forEach((m, idx) => {
    if (m?.id) byId.set(m.id, idx);
  });
  for (const msg of current) {
    if (!msg?.id) continue;
    if (byId.has(msg.id)) {
      merged[byId.get(msg.id)] = msg;
    } else {
      const matchingIdx = merged.findIndex(
        (m) =>
          m.role === msg.role &&
          m.content === msg.content &&
          !current.some((c) => c.id === m.id),
      );
      if (matchingIdx !== -1) {
        byId.delete(merged[matchingIdx].id);
        merged[matchingIdx] = msg;
        byId.set(msg.id, matchingIdx);
      } else {
        byId.set(msg.id, merged.length);
        merged.push(msg);
      }
    }
  }
  return merged;
}

export class PlanningAgent {
  constructor(threadId, fetchImpl = fetch) {
    this.threadId = threadId;
    this.fetch = fetchImpl;
    this.messages = [];
    this.offers = formToolOffers();
    this.executor = new FormToolExecutor();
    this.interrupt = null;
    this.recoveryRequired = false;
    this.active = false;
    this.activeRun = null;
    this.forms = null;
  }
  /**
   * @param {{message?: string, answers?: Array<{id: string, value: string}>, formAction?: any, recover?: boolean}} options
   * @param {any} binding
   * @param {(run: any, event: any) => void} update
   * @param {AbortSignal} [signal] aborts browser reading and subsequent form effects; Hub cancellation is a separate owner-bound request
   */
  async run(
    { message = "", answers, formAction, recover = false } = {},
    binding,
    update,
    signal,
  ) {
    if (this.active) throw new Error("An agent exchange is already running.");
    if (this.recoveryRequired && !recover)
      throw new Error("Recover a snapshot before continuing.");
    this.active = true;
    try {
      let resume;
      if (formAction) {
        answers = Object.entries(formAction.action.context).map(
          ([id, value]) => ({
            id: id.replace(/^question:/u, ""),
            value: String(value),
          }),
        );
      }
      if (answers) {
        if (!this.interrupt)
          throw new Error(
            "Recover the current Needs Input form before answering.",
          );
        const payload = Object.fromEntries(
          answers.map(({ id, value }) => [`question:${id}`, value]),
        );
        resume = [
          { interruptId: this.interrupt.id, status: "resolved", payload },
        ];
      }
      if (message.trim() && !recover)
        this.messages.push({
          id: crypto.randomUUID(),
          role: "user",
          content: message,
        });
      let results = [];
      for (let round = 0; round < 5; round++) {
        // Stop ends the exchange between rounds too, before any form tool runs.
        signal?.throwIfAborted();
        const input = {
          protocolVersion: "1.0",
          threadId: this.threadId,
          runId: crypto.randomUUID(),
          messages: [...this.messages, ...results],
          tools: this.offers,
          ...(resume ? { resume } : {}),
          ...(formAction
            ? { forwardedProps: { conduit: { a2uiAction: formAction } } }
            : {}),
          ...(recover
            ? { forwardedProps: { conduit: { mode: "snapshot" } } }
            : {}),
        };
        this.activeRun = {
          threadId: this.threadId,
          runId: input.runId,
          readOnly: recover,
        };
        const run = await runAgent(
          input,
          (current, event) => {
            this.forms = current.forms;
            update(current, event);
          },
          this.fetch,
          signal,
        );
        signal?.throwIfAborted();
        this.messages = mergeAgentMessages(this.messages, [
          ...run.messages.values(),
        ]);
        this.interrupt =
          run.outcome?.type === "interrupt"
            ? run.outcome.interrupts?.find(
                (item) => item.reason === "needs_input",
              ) || null
            : null;
        this.recoveryRequired = false;
        const justFinished = new Set(
          run.outcome?.type === "success"
            ? run.outcome.pendingToolCallIds || []
            : [],
        );
        const pending = (run.state.pendingClientTools || []).filter(
          (item) => item.eligible || justFinished.has(item.id),
        );
        // No browser effects until a terminal success establishes eligibility.
        // An interrupted/error exchange leaves its requests for explicit recovery.
        results = [];
        for (const item of pending) {
          signal?.throwIfAborted();
          const observed = run.tools.get(item.id);
          const call = {
            id: item.id,
            name: item.name,
            arguments: observed?.arguments ?? JSON.stringify(item.arguments),
          };
          const result = await this.executor.execute(
            call,
            this.offers,
            binding,
          );
          results.push({
            id: crypto.randomUUID(),
            role: "tool",
            toolCallId: item.id,
            content: JSON.stringify(result),
          });
        }
        if (!results.length) return run;
        // An ACK is a new run, never a resume of a broken SSE connection.
        resume = undefined;
        formAction = undefined;
        recover = false;
      }
      throw new Error(
        "The agent requested too many form-tool rounds. Recover a snapshot to review its state.",
      );
    } catch (error) {
      this.recoveryRequired = true;
      throw error;
    } finally {
      this.active = false;
      this.activeRun = null;
    }
  }
}

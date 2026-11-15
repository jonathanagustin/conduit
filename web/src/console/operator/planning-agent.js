import { runAgent } from "./agent-runs.js";
import { FormToolExecutor, formToolOffers } from "./agent-form-tools.js";

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
  }
  /** @param {{message?: string, answers?: Array<{id: string, value: string}>, recover?: boolean}} options */
  async run({ message = "", answers, recover = false } = {}, binding, update) {
    if (this.active) throw new Error("An agent exchange is already running.");
    if (this.recoveryRequired && !recover)
      throw new Error("Recover a snapshot before continuing.");
    this.active = true;
    try {
      let resume;
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
        const input = {
          protocolVersion: "1.0",
          threadId: this.threadId,
          runId: crypto.randomUUID(),
          messages: [...this.messages, ...results],
          tools: this.offers,
          ...(resume ? { resume } : {}),
          ...(recover
            ? { forwardedProps: { conduit: { mode: "snapshot" } } }
            : {}),
        };
        const run = await runAgent(
          input,
          (current, event) => update(current, event),
          this.fetch,
        );
        this.messages = [...run.messages.values()];
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
    }
  }
}

/*
 * The assistant's conversation store. It owns the open thread's messages and
 * the thread list, and is the only caller of the assistant Hub client
 * (operator/assistant-api.js). assistant-ui renders it through
 * useExternalStoreRuntime; the Hub is the system of record for a saved thread.
 */
import {
  AssistantExchange,
  type AssistantTurn,
  type ChatMessage,
  type ThreadList,
  type ToolRow,
  THREADS_LOADING,
  applyTurnDocument,
  confirmRequest,
  failTurn,
  messagesFromThread,
  newTurn,
  threadDeleted,
  threadOpened,
  threadSelected,
  threadsLoaded,
  threadsUnavailable,
  turnErrorFrom,
} from "./assistant-turn";

export type AssistantContext = {
  mode: string;
  chips: { id: string; objectType: string; label: string }[];
};

type TurnBody = {
  mode: string;
  message: string;
  context: AssistantContext["chips"];
  threadId?: string;
};

type RequestOptions = { signal?: AbortSignal };

/** The Hub calls the store makes; operator/assistant-api.js in the console. */
export type AssistantApi = {
  listThreads: (options?: RequestOptions) => Promise<unknown[]>;
  createThread: (title?: string, options?: RequestOptions) => Promise<unknown>;
  readThread: (id: string, options?: RequestOptions) => Promise<unknown>;
  deleteThread: (id: string, options?: RequestOptions) => Promise<unknown>;
  postTool: (body: {
    mode: string;
    tool: string;
    arguments: unknown;
  }) => Promise<Record<string, unknown>>;
  streamTurn: (
    body: TurnBody,
    exchange: AssistantExchange,
    onEvent: (exchange: AssistantExchange) => void,
    options?: RequestOptions,
  ) => Promise<unknown>;
};

export type AssistantState = {
  threads: ThreadList;
  messages: ChatMessage[];
  /** A turn is in flight. */
  running: boolean;
  /** A stored thread is being read. */
  loading: boolean;
  /** A thread list action that failed, in the Hub's words. */
  threadError: string;
};

type Storage = Pick<globalThis.Storage, "getItem" | "setItem" | "removeItem">;

export const OPEN_THREAD_KEY = "conduit.console.assistantThread";

export type AssistantStoreOptions = {
  api: AssistantApi;
  context: () => AssistantContext;
  storage?: Storage | null;
  now?: () => string;
};

export class AssistantStore {
  private state: AssistantState = {
    threads: THREADS_LOADING,
    messages: [],
    running: false,
    loading: false,
    threadError: "",
  };
  private listeners = new Set<() => void>();
  private controller: AbortController | null = null;
  private sequence = 0;
  /** Bumped whenever the open conversation changes; stale reads are dropped. */
  private generation = 0;
  private readonly api: AssistantApi;
  private readonly context: () => AssistantContext;
  private readonly storage: Storage | null;
  private readonly now: () => string;

  constructor(options: AssistantStoreOptions) {
    this.api = options.api;
    this.context = options.context;
    this.storage = options.storage ?? null;
    this.now = options.now ?? (() => new Date().toISOString());
  }

  getState = (): AssistantState => this.state;

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  private set(change: Partial<AssistantState>): void {
    this.state = { ...this.state, ...change };
    for (const listener of this.listeners) listener();
  }

  private remember(id: string | null): void {
    try {
      if (id) this.storage?.setItem(OPEN_THREAD_KEY, id);
      else this.storage?.removeItem(OPEN_THREAD_KEY);
    } catch {
      /* A browser that refuses storage only loses the reopen-on-reload. */
    }
  }

  private remembered(): string | null {
    try {
      return this.storage?.getItem(OPEN_THREAD_KEY) ?? null;
    } catch {
      return null;
    }
  }

  private nextId(): string {
    this.sequence += 1;
    return `local-${this.sequence}`;
  }

  /** Read the thread list and reopen the thread that was open before reload. */
  async init(): Promise<void> {
    const mine = ++this.generation;
    let threads: ThreadList;
    try {
      threads = threadsLoaded(await this.api.listThreads(), this.remembered());
    } catch (error) {
      threads = threadsUnavailable(error);
    }
    if (mine !== this.generation) return;
    this.set({ threads, threadError: "" });
    if (threads.status !== "ready") return;
    if (threads.openId) await this.openThread(threads.openId);
    else this.remember(null);
  }

  /** Re-read the list only; the open conversation is left as it is. */
  private async refreshList(): Promise<void> {
    if (this.state.threads.status !== "ready") return;
    try {
      const rows = await this.api.listThreads();
      const { openId } = this.state.threads;
      const next = threadsLoaded(rows, openId);
      // A thread just created may not be listed until its first message.
      this.set({ threads: { ...next, openId } });
    } catch {
      /* The list is a convenience; the conversation itself is unaffected. */
    }
  }

  async openThread(id: string): Promise<void> {
    if (this.state.running) return;
    const mine = ++this.generation;
    this.set({
      threads: threadSelected(this.state.threads, id),
      messages: [],
      loading: true,
      threadError: "",
    });
    try {
      const thread = await this.api.readThread(id);
      if (mine !== this.generation) return;
      this.remember(id);
      this.set({ messages: messagesFromThread(thread), loading: false });
    } catch (error) {
      if (mine !== this.generation) return;
      const { code, message } = turnErrorFrom(error);
      this.remember(null);
      this.set({
        threads:
          code === "THREAD_NOT_FOUND"
            ? threadDeleted(this.state.threads, id)
            : threadSelected(this.state.threads, null),
        loading: false,
        threadError:
          code === "THREAD_NOT_FOUND"
            ? "That conversation no longer exists on the Hub."
            : `The conversation could not be read: ${message || code}`,
      });
    }
  }

  /** An empty conversation. The Hub thread is created by its first message. */
  newThread(): void {
    if (this.state.running) return;
    this.generation += 1;
    this.remember(null);
    this.set({
      threads: threadSelected(this.state.threads, null),
      messages: [],
      loading: false,
      threadError: "",
    });
  }

  async deleteThread(id: string): Promise<void> {
    if (this.state.running) return;
    try {
      await this.api.deleteThread(id);
    } catch (error) {
      const { code, message } = turnErrorFrom(error);
      // Already gone is the outcome that was asked for.
      if (code !== "THREAD_NOT_FOUND") {
        this.set({
          threadError: `The conversation was not deleted: ${message || code}`,
        });
        return;
      }
    }
    const wasOpen = this.state.threads.openId === id;
    if (wasOpen) {
      this.generation += 1;
      this.remember(null);
    }
    this.set({
      threads: threadDeleted(this.state.threads, id),
      threadError: "",
      ...(wasOpen ? { messages: [], loading: false } : {}),
    });
  }

  private patchTurn(
    id: string,
    change: (turn: AssistantTurn) => AssistantTurn,
  ): void {
    this.set({
      messages: this.state.messages.map((message) =>
        message.id === id && message.role === "assistant"
          ? { ...message, turn: change(message.turn) }
          : message,
      ),
    });
  }

  /** Send one message as a streamed turn. */
  async send(text: string, mode?: string): Promise<void> {
    const message = text.trim();
    if (!message || this.state.running) return;
    const context = this.context();
    const turnMode = mode || context.mode || "explore";
    const mine = this.generation;
    const userId = this.nextId();
    const answerId = this.nextId();
    const createdAt = this.now();
    this.set({
      running: true,
      threadError: "",
      messages: [
        ...this.state.messages,
        { id: userId, role: "user", text: message, mode: turnMode, createdAt },
        {
          id: answerId,
          role: "assistant",
          mode: turnMode,
          createdAt,
          turn: newTurn(),
        },
      ],
    });
    const controller = new AbortController();
    this.controller = controller;
    const signal = controller.signal;
    try {
      let threadId = "";
      if (this.state.threads.status === "ready") {
        threadId = this.state.threads.openId ?? "";
        if (!threadId) {
          try {
            const created = await this.api.createThread("", { signal });
            this.set({ threads: threadOpened(this.state.threads, created) });
            threadId = this.state.threads.openId ?? "";
            this.remember(threadId);
          } catch (error) {
            if (turnErrorFrom(error).code !== "THREADS_NOT_CONFIGURED")
              throw error;
            this.set({ threads: threadsUnavailable(error) });
          }
        }
      }
      const run = async (id: string) => {
        const exchange = new AssistantExchange();
        const document = await this.api.streamTurn(
          {
            mode: turnMode,
            message,
            context: context.chips,
            ...(id ? { threadId: id } : {}),
          },
          exchange,
          (current) => this.patchTurn(answerId, () => current.turn),
          { signal },
        );
        this.patchTurn(answerId, (turn) =>
          document
            ? { ...applyTurnDocument(turn, document), status: "complete" }
            : exchange.turn,
        );
      };
      try {
        await run(threadId);
      } catch (error) {
        // A Hub with no conversation store still answers a turn with no thread.
        if (!threadId || turnErrorFrom(error).code !== "THREADS_NOT_CONFIGURED")
          throw error;
        this.remember(null);
        this.set({ threads: threadsUnavailable(error) });
        this.patchTurn(answerId, () => newTurn());
        await run("");
      }
    } catch (error) {
      const failure = signal.aborted
        ? { name: "AbortError", message: "cancelled" }
        : error;
      this.patchTurn(answerId, (turn) => failTurn(turn, failure));
      if (turnErrorFrom(failure).code === "THREAD_NOT_FOUND") {
        // The next message starts a new thread rather than failing again.
        const gone = this.state.threads.openId;
        this.remember(null);
        if (gone)
          this.set({ threads: threadDeleted(this.state.threads, gone) });
      }
    } finally {
      if (this.controller === controller) this.controller = null;
      this.set({ running: false });
      if (mine === this.generation) void this.refreshList();
    }
  }

  /** Stop the turn in flight. Closing the request cancels the model call. */
  cancel(): void {
    this.controller?.abort();
  }

  /**
   * Send the last message again. A failed attempt the Hub never started is
   * replaced; one it started is in the stored thread, so it stays on screen
   * and the retry is appended after it, as a reload would show it.
   */
  async retry(): Promise<void> {
    if (this.state.running) return;
    const { messages } = this.state;
    const answer = messages.at(-1);
    const asked = messages.at(-2);
    if (answer?.role !== "assistant" || asked?.role !== "user") return;
    const recorded =
      answer.turn.started &&
      this.state.threads.status === "ready" &&
      this.state.threads.openId !== null;
    if (!recorded) this.set({ messages: messages.slice(0, -2) });
    await this.send(asked.text, asked.mode);
  }

  /**
   * Submit a tool card: Continue with the fields a result asked for, or
   * Confirm. The answer is the Hub's and is shown whatever it says.
   */
  async submitTool(
    messageId: string,
    toolId: string,
    values: Record<string, string>,
    confirm: boolean,
  ): Promise<void> {
    const message = this.state.messages.find((item) => item.id === messageId);
    const row =
      message?.role === "assistant"
        ? message.turn.tools.find((item) => item.id === toolId)
        : undefined;
    if (!message || !row || row.pending) return;
    const request = confirmRequest(message.mode, row, values, confirm);
    const update = (change: (current: ToolRow) => ToolRow) =>
      this.patchTurn(messageId, (turn) => ({
        ...turn,
        tools: turn.tools.map((item) =>
          item.id === toolId ? change(item) : item,
        ),
      }));
    update((current) => ({ ...current, pending: true }));
    let result: Record<string, unknown>;
    try {
      result = await this.api.postTool(request);
    } catch (error) {
      result = {
        tool: request.tool,
        ok: false,
        error:
          turnErrorFrom(error).message || "The Hub did not answer the request.",
      };
    }
    update((current) => ({
      ...current,
      pending: false,
      followUps: [...current.followUps, result],
    }));
  }
}

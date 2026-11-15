import {
  LiveProtocolError,
  LiveResetError,
  LiveSSEDecoder,
  liveWatermark,
  type LiveEnvelope,
  type LiveWatermark,
} from "./live-event-contract.js";
export {
  liveAreas,
  type LiveArea,
  type LiveEnvelope,
} from "./live-event-contract.js";

export type LiveState =
  | "idle"
  | "connecting"
  | "catching-up"
  | "live"
  | "offline"
  | "resyncing"
  | "auth-required"
  | "unsupported"
  | "stopped";
export type LiveStatus = Readonly<{
  state: LiveState;
  message: string;
  status?: 401 | 403;
}>;
export type LiveContext = Readonly<{ signal: AbortSignal; identity: string }>;
export type LiveOptions = {
  fetch?: typeof fetch;
  catchUp: (context: LiveContext) => Promise<void>;
  invalidate: (
    events: readonly LiveEnvelope[],
    context: LiveContext,
  ) => Promise<void>;
  random?: () => number;
};
const messages: Record<LiveState, string> = {
  idle: "Live updates are waiting for an operator session.",
  connecting: "Connecting live updates.",
  "catching-up": "Catching up authorized reads.",
  live: "Live updates connected.",
  offline: "Live updates unavailable; using polling.",
  resyncing: "Live history requires catch-up; using polling.",
  "auth-required":
    "Live updates require a renewed authorized session; using polling.",
  unsupported: "Live updates use an unsupported contract; using polling.",
  stopped: "Live updates stopped.",
};
export const idleLiveStatus: LiveStatus = Object.freeze({
  state: "idle",
  message: messages.idle,
});
class AuthenticationError extends Error {
  constructor(readonly status: 401 | 403) {
    super("Live authorization required");
  }
}
const abortError = () =>
  new DOMException("Live operation aborted", "AbortError");
function delay(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) {
      reject(abortError());
      return;
    }
    const timer = setTimeout(() => {
      signal.removeEventListener("abort", cancel);
      resolve();
    }, ms);
    const cancel = () => {
      clearTimeout(timer);
      signal.removeEventListener("abort", cancel);
      reject(abortError());
    };
    signal.addEventListener("abort", cancel, { once: true });
  });
}
function bounded<T>(
  operation: Promise<T>,
  ms: number,
  controller: AbortController,
): Promise<T> {
  return new Promise((resolve, reject) => {
    const cleanup = () => {
      clearTimeout(timer);
      controller.signal.removeEventListener("abort", cancel);
    };
    const cancel = () => {
      cleanup();
      reject(abortError());
    };
    const timer = setTimeout(() => {
      cleanup();
      reject(new Error("Live read timed out"));
      controller.abort();
    }, ms);
    if (controller.signal.aborted) {
      cancel();
      return;
    }
    controller.signal.addEventListener("abort", cancel, { once: true });
    operation.then(
      (value) => {
        cleanup();
        resolve(value);
      },
      (error) => {
        cleanup();
        reject(error);
      },
    );
  });
}

// Cursors and replay metadata live only in this identity-scoped instance. Model
// state remains in the owning read model; events only request authorized reads.
export class LiveSubscription {
  private status: LiveStatus = idleLiveStatus;
  private listeners = new Set<() => void>();
  private owner = "";
  private generation = 0;
  private root: AbortController | null = null;
  private applied: LiveWatermark | null = null;
  private revisions = new Map<string, number>();
  private retries = 0;
  constructor(private options: LiveOptions) {}
  getSnapshot = (): LiveStatus => this.status;
  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };
  start(identity: string): void {
    if (identity === this.owner && this.root && !this.root.signal.aborted)
      return;
    this.stop();
    this.owner = identity;
    if (!identity) {
      this.publish("idle");
      return;
    }
    this.root = new AbortController();
    const generation = ++this.generation;
    void this.run(generation, this.root.signal);
  }
  retry(): void {
    const owner = this.owner;
    this.stop();
    this.start(owner);
  }
  stop(): void {
    this.generation++;
    this.root?.abort();
    this.root = null;
    this.owner = "";
    this.clearPosition();
    this.retries = 0;
    this.publish("stopped");
  }
  private clearPosition(): void {
    this.applied = null;
    this.revisions.clear();
  }
  private publish(state: LiveState, status?: 401 | 403): void {
    if (this.status.state === state && this.status.status === status) return;
    this.status = Object.freeze({
      state,
      message: messages[state],
      ...(status ? { status } : {}),
    });
    for (const listener of this.listeners) {
      try {
        listener();
      } catch {
        /* A view listener cannot govern delivery. */
      }
    }
  }
  private async run(generation: number, signal: AbortSignal): Promise<void> {
    while (!signal.aborted && generation === this.generation) {
      this.publish("connecting");
      try {
        await this.connection(generation, signal);
      } catch (error) {
        if (signal.aborted || generation !== this.generation) return;
        if (error instanceof AuthenticationError) {
          this.clearPosition();
          this.publish("auth-required", error.status);
          return;
        }
        if (error instanceof LiveProtocolError) {
          this.clearPosition();
          this.publish("unsupported");
          return;
        }
        if (error instanceof LiveResetError) {
          this.clearPosition();
          this.publish("resyncing");
        } else {
          this.publish("offline");
        }
      }
      const random = this.options.random?.() ?? Math.random();
      const backoff =
        Math.min(30000, 1000 * 2 ** Math.min(this.retries++, 5)) *
        (1 + Math.max(0, Math.min(1, random)) * 0.25);
      try {
        await delay(backoff, signal);
      } catch {
        return;
      }
    }
  }
  private async connection(
    generation: number,
    root: AbortSignal,
  ): Promise<void> {
    const controller = new AbortController();
    const cancel = () => controller.abort();
    root.addEventListener("abort", cancel, { once: true });
    if (root.aborted) controller.abort();
    const context = Object.freeze({
      signal: controller.signal,
      identity: this.owner,
    });
    let reader: ReadableStreamDefaultReader<Uint8Array> | undefined;
    let readTask: Promise<void> | undefined;
    let failure: unknown;
    const current = () => {
      if (failure) throw failure;
      if (controller.signal.aborted || generation !== this.generation)
        throw abortError();
    };
    try {
      const previous = this.applied;
      const response = await bounded(
        Promise.resolve().then(() =>
          (this.options.fetch ?? fetch)("/v1/events", {
            method: "GET",
            credentials: "same-origin",
            cache: "no-store",
            redirect: "error",
            signal: controller.signal,
            headers: {
              accept: "text/event-stream",
              ...(previous ? { "Last-Event-ID": previous.cursor } : {}),
            },
          }),
        ),
        20000,
        controller,
      );
      if (!response.ok) {
        void response.body?.cancel().catch(() => {});
        if (response.status === 401 || response.status === 403)
          throw new AuthenticationError(response.status);
        if (response.status === 409)
          throw new LiveResetError("Live history expired");
        throw new Error("Live stream unavailable");
      }
      const ready = liveWatermark(response, previous);
      reader = response.body!.getReader();
      const pending: { event: LiveEnvelope; bytes: number }[] = [];
      let received = ready.sequence,
        buffered = 0,
        bytes = 0,
        wake: (() => void) | undefined;
      let fail!: (error: unknown) => void;
      const failed = new Promise<never>((_resolve, reject) => {
        fail = reject;
      });
      // Always attach a handler, including when a synchronous parser error
      // precedes the first race against catch-up.
      void failed.catch(() => {});
      const parser = new LiveSSEDecoder();
      const enqueue = (event: LiveEnvelope, size: number) => {
        current();
        if (event.streamID !== ready.streamID)
          throw new LiveResetError("Live epoch changed");
        if (event.sequence <= received) return;
        if (event.sequence !== received + 1)
          throw new LiveResetError("Live sequence gap");
        if (++buffered > 256 || (bytes += size) > 1024 * 1024)
          throw new LiveResetError("Live catch-up queue overflow");
        received = event.sequence;
        pending.push({ event, bytes: size });
        wake?.();
        wake = undefined;
      };
      readTask = (async () => {
        try {
          while (true) {
            const chunk = await bounded(reader!.read(), 45000, controller);
            if (chunk.done) {
              parser.finish();
              throw new Error("Live connection closed");
            }
            parser.push(chunk.value, enqueue);
          }
        } catch (error) {
          failure = error;
          fail(error);
          controller.abort();
          wake?.();
          wake = undefined;
        }
      })();
      const apply = async () => {
        const batch = pending.splice(0);
        if (!batch.length) return;
        const updates = new Map<string, number>(),
          visible: LiveEnvelope[] = [];
        let added = 0;
        for (const { event } of batch) {
          const key = `${event.resource.type}:${event.resource.id}`,
            last = updates.get(key) ?? this.revisions.get(key) ?? 0;
          if (event.resource.revision <= last) continue;
          if (!updates.has(key) && !this.revisions.has(key)) added++;
          updates.set(key, event.resource.revision);
          visible.push(event);
        }
        if (this.revisions.size + added > 10000)
          throw new LiveResetError("Live revision window exhausted");
        if (visible.length)
          await Promise.race([
            bounded(
              this.options.invalidate(Object.freeze(visible), context),
              20000,
              controller,
            ),
            failed,
          ]);
        current();
        if (failure) throw failure;
        for (const [key, revision] of updates)
          this.revisions.set(key, revision);
        const last = batch[batch.length - 1]!.event;
        this.applied = Object.freeze({
          streamID: last.streamID,
          sequence: last.sequence,
          cursor: last.cursor,
        });
        buffered -= batch.length;
        for (const item of batch) bytes -= item.bytes;
      };
      this.publish("catching-up");
      if (!previous) {
        await Promise.race([
          bounded(this.options.catchUp(context), 20000, controller),
          failed,
        ]);
        current();
        this.applied = ready;
      }
      while (pending.length) {
        await apply();
      }
      current();
      this.retries = 0;
      this.publish("live");
      while (true) {
        if (pending.length) await apply();
        else
          await Promise.race([
            new Promise<void>((resolve) => {
              wake = resolve;
            }),
            failed,
          ]);
        if (failure) throw failure;
        current();
      }
    } finally {
      controller.abort();
      root.removeEventListener("abort", cancel);
      void reader?.cancel().catch(() => {});
      if (reader && readTask)
        void readTask.then(() => {
          try {
            reader?.releaseLock();
          } catch {
            /* Already released by a cancelled fetch. */
          }
        });
    }
  }
}

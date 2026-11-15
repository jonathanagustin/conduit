import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import fixtures from "../../../../api/live-events/resume-fixtures.json";
import {
  LiveSSEDecoder,
  parseLiveEnvelope,
  type LiveEnvelope,
} from "../../api/live-event-contract.js";
import { LiveSubscription } from "../../api/live-events.js";

const encoder = new TextEncoder();
const event = (sequence: number, revision = sequence, streamID = "s_one") => ({
  version: 1,
  streamID,
  sequence,
  cursor: `c_${streamID}_${sequence}`,
  occurredAt: "2026-11-15T12:00:00Z",
  kind: "plan.changed",
  resource: { type: "plan", id: "pln_one", revision },
  data: { status: "RUNNING" },
});
function stream(id = "s_one", sequence = 0, cursor = `c_${id}_${sequence}`) {
  let controller!: ReadableStreamDefaultController<Uint8Array>;
  const body = new ReadableStream<Uint8Array>({
    start(value) {
      controller = value;
    },
  });
  const response = new Response(body, {
    headers: {
      "content-type": "text/event-stream",
      "cache-control": "no-store",
      "X-Conduit-Stream-ID": id,
      "X-Conduit-Event-Sequence": String(sequence),
      "X-Conduit-Event-Cursor": cursor,
    },
  });
  return {
    response,
    send(value: ReturnType<typeof event>) {
      controller.enqueue(
        encoder.encode(
          `id: ${value.cursor}\nevent: ${value.kind}\ndata: ${JSON.stringify(value)}\n\n`,
        ),
      );
    },
    raw(wire: string) {
      controller.enqueue(encoder.encode(wire));
    },
    close() {
      controller.close();
    },
  };
}
const tick = async (rounds = 30) => {
  for (let i = 0; i < rounds; i++) await Promise.resolve();
};
const clients: LiveSubscription[] = [];
function client(options: ConstructorParameters<typeof LiveSubscription>[0]) {
  const value = new LiveSubscription({ ...options, random: () => 0 });
  clients.push(value);
  return value;
}
afterEach(() => {
  for (const value of clients.splice(0)) value.stop();
  vi.useRealTimers();
});

describe("bounded live event contract", () => {
  it("accepts the committed resume fixtures and rejects schema drift/private payloads", () => {
    for (const fixture of fixtures)
      for (const value of fixture.events)
        expect(parseLiveEnvelope(JSON.stringify(value))).toEqual(value);
    for (const mutate of [
      (v: ReturnType<typeof event>) => (v.version = 2),
      (v: ReturnType<typeof event>) =>
        (v.resource.revision = Number.MAX_SAFE_INTEGER + 1),
      (v: ReturnType<typeof event>) =>
        Object.assign(v.data, { rawOutput: "private" }),
      (v: ReturnType<typeof event>) => (v.occurredAt = "2026-02-30T12:00:00Z"),
    ]) {
      const value = event(1);
      mutate(value);
      expect(() => parseLiveEnvelope(JSON.stringify(value))).toThrow();
    }
  });
  it("decodes chunked CRLF comments and data without treating partial EOF as an event", () => {
    const decoder = new LiveSSEDecoder(),
      received: LiveEnvelope[] = [];
    const value = event(1),
      wire = `: keepalive\r\n\r\nid: ${value.cursor}\r\nevent: ${value.kind}\r\ndata: ${JSON.stringify(value)}\r\n\r\n`;
    for (const byte of encoder.encode(wire))
      decoder.push(new Uint8Array([byte]), (value) => received.push(value));
    decoder.push(encoder.encode('id: unfinished\ndata: {"private":'), (value) =>
      received.push(value),
    );
    decoder.finish();
    expect(received).toEqual([value]);
    expect(() =>
      new LiveSSEDecoder().push(
        encoder.encode(`id: first\nid: second\n`),
        () => {},
      ),
    ).toThrow();
    expect(() =>
      new LiveSSEDecoder().push(encoder.encode("x".repeat(8193)), () => {}),
    ).toThrow();
  });
});

describe("identity-scoped live subscriptions", () => {
  it("buffers before catch-up, coalesces reads and resumes only the applied cursor", async () => {
    vi.useFakeTimers();
    const first = stream();
    let release!: () => void;
    const catchUp = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          release = resolve;
        }),
    );
    const invalidate = vi.fn(async (_events: readonly LiveEnvelope[]) => {});
    const calls: RequestInit[] = [];
    const second = stream("s_one", 2, "c_s_one_2");
    const fetchImpl = vi.fn(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        calls.push(init!);
        return calls.length === 1 ? first.response : second.response;
      },
    );
    const live = client({ fetch: fetchImpl, catchUp, invalidate });
    live.start("alice");
    await tick();
    expect(live.getSnapshot().state).toBe("catching-up");
    first.send(event(1));
    first.send(event(2));
    await tick();
    expect(invalidate).not.toHaveBeenCalled();
    release();
    await tick();
    expect(invalidate).toHaveBeenCalledTimes(1);
    expect(invalidate.mock.calls[0]![0]).toHaveLength(2);
    expect(live.getSnapshot().state).toBe("live");
    first.close();
    await tick();
    expect(live.getSnapshot().state).toBe("offline");
    await vi.advanceTimersByTimeAsync(1000);
    await tick();
    expect((calls[1]!.headers as Record<string, string>)["Last-Event-ID"]).toBe(
      "c_s_one_2",
    );
    expect(catchUp).toHaveBeenCalledTimes(1);
    expect(live.getSnapshot().state).toBe("live");
    expect(calls[0]).toMatchObject({
      method: "GET",
      credentials: "same-origin",
      redirect: "error",
      cache: "no-store",
    });
  });
  it("ignores duplicate and older resource revisions while advancing a contiguous cursor", async () => {
    vi.useFakeTimers();
    const source = stream();
    const seen: LiveEnvelope[][] = [];
    const live = client({
      fetch: vi.fn(async () => source.response),
      catchUp: async () => {},
      invalidate: async (events) => {
        seen.push([...events]);
      },
    });
    live.start("alice");
    await tick();
    source.send(event(1, 5));
    await tick();
    source.send(event(1, 5));
    source.send(event(2, 4));
    source.send(event(3, 6));
    await tick();
    expect(seen.flat().map((value) => value.resource.revision)).toEqual([5, 6]);
    expect(live.getSnapshot().state).toBe("live");
  });
  it("requires a fresh epoch after a gap or 409 and never reconnects with a retired cursor", async () => {
    vi.useFakeTimers();
    const source = stream(),
      fresh = stream("s_two");
    const calls: RequestInit[] = [];
    let attempt = 0;
    const live = client({
      fetch: vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
        calls.push(init!);
        attempt++;
        return attempt === 1
          ? source.response
          : attempt === 2
            ? new Response("private", { status: 409 })
            : fresh.response;
      }),
      catchUp: async () => {},
      invalidate: async () => {},
    });
    live.start("alice");
    await tick();
    source.send(event(2));
    await tick();
    expect(live.getSnapshot().state).toBe("resyncing");
    await vi.advanceTimersByTimeAsync(1000);
    await tick();
    expect(live.getSnapshot().state).toBe("resyncing");
    await vi.advanceTimersByTimeAsync(2000);
    await tick();
    expect(live.getSnapshot().state).toBe("live");
    expect(
      calls
        .slice(1)
        .every((init) => !("Last-Event-ID" in (init.headers as object))),
    ).toBe(true);
  });
  it.each([401, 403])(
    "stops reconnect on %s until explicitly retried or identity changes",
    async (status) => {
      vi.useFakeTimers();
      const fetchImpl = vi.fn(
        async () => new Response("PRIVATE_ERROR", { status }),
      );
      const live = client({
        fetch: fetchImpl,
        catchUp: async () => {},
        invalidate: async () => {},
      });
      live.start("alice");
      await tick();
      expect(live.getSnapshot()).toMatchObject({
        state: "auth-required",
        status,
      });
      expect(live.getSnapshot().message).not.toContain("PRIVATE_ERROR");
      await vi.advanceTimersByTimeAsync(120000);
      expect(fetchImpl).toHaveBeenCalledTimes(1);
      live.retry();
      await tick();
      expect(fetchImpl).toHaveBeenCalledTimes(2);
    },
  );
  it("clears pending cursors and aborts old callbacks when the identity changes", async () => {
    vi.useFakeTimers();
    const a = stream(),
      b = stream("s_b");
    let release!: () => void;
    const contexts: AbortSignal[] = [];
    let attempt = 0;
    const calls: RequestInit[] = [];
    const live = client({
      fetch: vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
        calls.push(init!);
        return ++attempt === 1 ? a.response : b.response;
      }),
      catchUp: async (context) => {
        contexts.push(context.signal);
        if (context.identity === "alice")
          await new Promise<void>((resolve) => {
            release = resolve;
          });
      },
      invalidate: async () => {},
    });
    live.start("alice");
    await tick();
    a.send(event(1));
    await tick();
    live.start("bob");
    await tick();
    expect(contexts[0]!.aborted).toBe(true);
    release();
    await tick();
    expect(live.getSnapshot().state).toBe("live");
    expect(
      (calls[1]!.headers as Record<string, string>)["Last-Event-ID"],
    ).toBeUndefined();
  });
  it("bounds pending plus in-flight frames and does not mark a failed catch-up live", async () => {
    vi.useFakeTimers();
    const source = stream();
    const live = client({
      fetch: vi.fn(async () => source.response),
      catchUp: async (context) =>
        new Promise<void>((_resolve, reject) =>
          context.signal.addEventListener("abort", () =>
            reject(new Error("aborted")),
          ),
        ),
      invalidate: async () => {},
    });
    live.start("alice");
    await tick();
    for (let sequence = 1; sequence <= 257; sequence++)
      source.send(event(sequence));
    await tick(4000);
    expect(live.getSnapshot().state).toBe("resyncing");
  });
  it("makes unsupported frames visible without interpreting or looping on them", async () => {
    vi.useFakeTimers();
    const source = stream(),
      invalidate = vi.fn(async () => {}),
      fetchImpl = vi.fn(async () => source.response);
    const live = client({
      fetch: fetchImpl,
      catchUp: async () => {},
      invalidate,
    });
    live.start("alice");
    await tick();
    source.send({ ...event(1), version: 2 });
    await tick();
    expect(live.getSnapshot().state).toBe("unsupported");
    expect(invalidate).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(60000);
    expect(fetchImpl).toHaveBeenCalledTimes(1);
  });
});

it("counts in-flight application work inside the 256-frame bound", async () => {
  vi.useFakeTimers();
  const source = stream();
  const invalidate = vi.fn(
    async (
      _events: readonly LiveEnvelope[],
      context: { signal: AbortSignal },
    ) =>
      new Promise<void>((_resolve, reject) =>
        context.signal.addEventListener("abort", () =>
          reject(new Error("aborted")),
        ),
      ),
  );
  const live = client({
    fetch: vi.fn(async () => source.response),
    catchUp: async () => {},
    invalidate,
  });
  live.start("alice");
  await tick();
  source.send(event(1));
  await tick();
  expect(invalidate).toHaveBeenCalledTimes(1);
  for (let sequence = 2; sequence <= 257; sequence++)
    source.send(event(sequence));
  await tick(4000);
  expect(live.getSnapshot().state).toBe("resyncing");
});
it("enforces the encoded 1 MiB bound before reaching the frame-count limit", async () => {
  vi.useFakeTimers();
  const source = stream();
  const live = client({
    fetch: vi.fn(async () => source.response),
    catchUp: async (context) =>
      new Promise<void>((_resolve, reject) =>
        context.signal.addEventListener("abort", () =>
          reject(new Error("aborted")),
        ),
      ),
    invalidate: async () => {},
  });
  live.start("alice");
  await tick();
  for (let sequence = 1; sequence <= 255; sequence++) {
    const value = event(sequence),
      json = JSON.stringify(value).padEnd(4096, " ");
    source.raw(`id: ${value.cursor}\nevent: ${value.kind}\ndata: ${json}\n\n`);
  }
  await tick(4000);
  expect(live.getSnapshot().state).toBe("resyncing");
});
it("bounds a silent connection and preserves the applied cursor for retry", async () => {
  vi.useFakeTimers();
  const source = stream(),
    next = stream();
  let attempt = 0;
  const calls: RequestInit[] = [];
  const live = client({
    fetch: vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      calls.push(init!);
      return ++attempt === 1 ? source.response : next.response;
    }),
    catchUp: async () => {},
    invalidate: async () => {},
  });
  live.start("alice");
  await tick();
  await vi.advanceTimersByTimeAsync(45000);
  await tick();
  expect(live.getSnapshot().state).toBe("offline");
  expect(calls[0]!.signal!.aborted).toBe(true);
  await vi.advanceTimersByTimeAsync(1000);
  await tick();
  expect(new Headers(calls[1]!.headers).get("Last-Event-ID")).toBe("c_s_one_0");
});

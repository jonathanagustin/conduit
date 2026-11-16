// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { createAuditEventStream } from "../../console/operator/audit-event-stream.js";
import { fetchNotificationEvents } from "../../api/notification-events.js";

vi.mock("../../api/notification-events.js", () => ({
  fetchNotificationEvents: vi.fn(),
  notificationEventsTransport: vi.fn(),
}));

function root() {
  return {
    isConnected: true,
    innerHTML: "",
    onchange: null,
    onclick: null,
  } as unknown as HTMLElement;
}
function choose(node: HTMLElement, resource: string) {
  node.onchange?.({
    target: { value: resource, matches: () => true },
  } as unknown as Event);
}
function start(node: HTMLElement) {
  const button = {
    disabled: false,
    matches: (selector: string) => selector === "[data-audit-poll]",
  };
  node.onclick?.({
    target: { closest: () => button },
  } as unknown as PointerEvent);
}
function fixture() {
  vi.stubGlobal("window", { location: { origin: "https://conduit.test" } });
  vi.stubGlobal("document", { hidden: false });
  let identity = { actor: "alice", names: ["alice"] };
  let entries = [{ resourceRid: "environment/east" }];
  let resolve!: (
    value: Awaited<ReturnType<typeof fetchNotificationEvents>>,
  ) => void;
  vi.mocked(fetchNotificationEvents).mockReturnValue(
    new Promise((done) => {
      resolve = done;
    }),
  );
  const mount = createAuditEventStream({
    getEntries: () => entries,
    getIdentity: () => identity,
  });
  const first = root();
  mount(first);
  choose(first, "environment/east");
  start(first);
  const signal = vi.mocked(fetchNotificationEvents).mock
    .calls[0]?.[2] as AbortSignal;
  return {
    mount,
    first,
    signal,
    resolve: () =>
      resolve({
        resourceRid: "environment/east",
        limit: 100,
        events: [
          {
            id: "one",
            resourceRid: "environment/east",
            summary: "private event",
            type: "PLAN_REVIEW",
            environment: "east",
            actor: "operator",
            occurredAt: "2026-11-15T12:00:00Z",
          },
        ],
      }),
    setIdentity: () => {
      identity = { actor: "bob", names: ["bob"] };
    },
    removeResource: () => {
      entries = [{ resourceRid: "environment/west" }];
    },
  };
}

afterEach(() => {
  vi.clearAllMocks();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("Audit polling session refresh", () => {
  it("moves a pending authorized read to the refreshed view without a second request", async () => {
    const f = fixture();
    const next = root();
    f.mount(next);
    expect(f.signal.aborted).toBe(false);
    expect(next.innerHTML).toContain("Pause polling");
    expect(next.innerHTML).toContain('value="environment/east" selected');
    expect(fetchNotificationEvents).toHaveBeenCalledTimes(1);
    f.resolve();
    await vi.waitFor(() => expect(next.innerHTML).toContain("private event"));
    expect(f.first.onclick).toBeNull();
    f.mount.dispose();
  });
  it("keeps a paused request's late result out of the view", async () => {
    const f = fixture();
    start(f.first);
    expect(f.signal.aborted).toBe(true);
    f.resolve();
    await new Promise((done) => setTimeout(done, 0));
    expect(f.first.innerHTML).not.toContain("private event");
    expect(f.first.innerHTML).toContain("Start polling");
    f.mount.dispose();
  });
  for (const change of ["identity", "resource", "dispose"] as const) {
    it(`aborts and drops a late result after ${change} changes`, async () => {
      const f = fixture();
      const next = root();
      if (change === "identity") {
        f.setIdentity();
        f.mount(next);
      }
      if (change === "resource") {
        f.removeResource();
        f.mount(next);
      }
      if (change === "dispose") f.mount.dispose();
      expect(f.signal.aborted).toBe(true);
      f.resolve();
      await new Promise((done) => setTimeout(done, 0));
      expect(next.innerHTML).not.toContain("private event");
      expect(next.innerHTML).not.toContain("Pause polling");
      expect(next.innerHTML).not.toContain('value="environment/east" selected');
      f.mount.dispose();
    });
  }
});

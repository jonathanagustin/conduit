import { describe, expect, it } from "vite-plus/test";
import {
  fetchNotificationEvents,
  notificationEventsTransport,
} from "../../api/notification-events.js";
import { HubError } from "../../api/hub-error.js";
const resource = "environment/east";
const event = {
  id: "event-1",
  type: "FUTURE_REVIEW",
  resourceRid: resource,
  environment: "east",
  actor: "operator",
  summary: "<untrusted>",
  occurredAt: "2026-11-15T12:00:00.123456789Z",
};
const payload = { resourceRid: resource, limit: 100, events: [event] };
const response = (body: unknown) =>
  new Response(JSON.stringify(body), {
    headers: { "content-type": "application/json" },
  });
describe("native notification RPC client", () => {
  it("preserves the read coordinate, optional limit and precise native event values", async () => {
    const bodies: unknown[] = [];
    const transport = notificationEventsTransport(
      "http://hub.local",
      async (input, init) => {
        const request =
          input instanceof Request ? input : new Request(input, init);
        expect(request.url).toBe(
          "http://hub.local/conduit.notifications.v1.NotificationService/ListNotificationEvents",
        );
        expect(request.method).toBe("POST");
        expect(request.credentials).toBe("same-origin");
        expect(
          Number(request.headers.get("Connect-Timeout-Ms")),
        ).toBeGreaterThan(0);
        expect(
          Number(request.headers.get("Connect-Timeout-Ms")),
        ).toBeLessThanOrEqual(10000);
        bodies.push(await request.json());
        return response(payload);
      },
    );
    await expect(
      fetchNotificationEvents(resource, transport, undefined, BigInt(100)),
    ).resolves.toEqual(payload);
    await expect(fetchNotificationEvents(resource, transport)).resolves.toEqual(
      payload,
    );
    expect(bodies).toEqual([
      { resourceRid: resource, limit: "100" },
      { resourceRid: resource },
    ]);
  });
  it.each([
    { resourceRid: "environment/foreign", limit: 100, events: [] },
    {
      resourceRid: resource,
      limit: 100,
      events: [{ ...event, resourceRid: "environment/foreign" }],
    },
    {
      resourceRid: resource,
      limit: 1,
      events: [event, { ...event, id: "event-2" }],
    },
    {
      resourceRid: resource,
      limit: 100,
      events: [{ ...event, occurredAt: undefined }],
    },
  ])("refuses mismatched or malformed event evidence %#", async (body) => {
    const transport = notificationEventsTransport(
      "http://hub.local",
      async () => response(body),
    );
    await expect(
      fetchNotificationEvents(resource, transport),
    ).rejects.toMatchObject({ name: "HubError", status: 502 });
  });
  it.each([
    [401, "unauthenticated", 401],
    [403, "permission_denied", 403],
    [400, "invalid_argument", 400],
    [500, "internal", 500],
  ])(
    "preserves native HTTP %i error semantics",
    async (status, code, expected) => {
      const transport = notificationEventsTransport(
        "http://hub.local",
        async () =>
          new Response(JSON.stringify({ code, message: "refused" }), {
            status,
            headers: { "content-type": "application/json" },
          }),
      );
      await expect(
        fetchNotificationEvents(resource, transport),
      ).rejects.toBeInstanceOf(HubError);
      await expect(
        fetchNotificationEvents(resource, transport),
      ).rejects.toMatchObject({ status: expected });
    },
  );
  it("keeps an absent optional event reader unavailable instead of inventing an empty stream", async () => {
    const transport = notificationEventsTransport(
      "http://hub.local",
      async () => new Response("404 page not found", { status: 404 }),
    );
    await expect(
      fetchNotificationEvents(resource, transport),
    ).rejects.toMatchObject({ name: "HubError", status: 404 });
  });
  it("cancels the actual event fetch", async () => {
    const controller = new AbortController();
    let start!: () => void;
    const ready = new Promise<void>((resolve) => {
      start = resolve;
    });
    let wireSignal: AbortSignal | undefined;
    const transport = notificationEventsTransport(
      "http://hub.local",
      async (input, init) => {
        const request =
          input instanceof Request ? input : new Request(input, init);
        wireSignal = request.signal;
        start();
        return await new Promise<Response>((_, reject) =>
          request.signal.addEventListener(
            "abort",
            () => reject(new DOMException("Aborted", "AbortError")),
            { once: true },
          ),
        );
      },
    );
    const pending = fetchNotificationEvents(
      resource,
      transport,
      controller.signal,
    );
    const failure = expect(pending).rejects.toMatchObject({
      name: "HubError",
      status: 499,
    });
    await ready;
    controller.abort();
    await failure;
    expect(wireSignal?.aborted).toBe(true);
  });
});

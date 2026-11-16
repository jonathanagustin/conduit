import { describe, expect, it } from "vite-plus/test";
import {
  fetchPackagingEvents,
  packagingEventsTransport,
} from "../../api/packaging-events.js";
import { readPackagingHistory } from "../../console/operator/packaging-pipeline.js";

const event = {
  sequence: "1",
  at: "2026-11-16T09:00:00.123456789Z",
  step: "assess",
  outcome: "WAITING",
  reason: "<redacted>",
  sourceDigest: "sha256:source",
  artifactDigest: "sha256:artifact",
  proposalRevision: "revision",
  approvalId: "approval",
  planId: "plan",
  targetPlanRef: "dev/app",
};
const payload = {
  configured: true,
  runId: "r/1",
  tier: "CONDUIT_NATIVE",
  authority: "packaging-run",
  planAuthority: "conduit-plan",
  approvalAuthority: "conduit-governance",
  items: [event],
  pageSize: 1,
  limit: 20,
  total: "9007199254740993",
  nextCursor: "opaque/+=",
};
const query = { runId: "r/1", cursor: "" };
const reply = (body: unknown) =>
  new Response(JSON.stringify(body), {
    headers: { "content-type": "application/json" },
  });

describe("packaging history generated client", () => {
  it("keeps exact counts, timestamps, references and opaque cursors on the read procedure", async () => {
    const bodies: unknown[] = [];
    const transport = packagingEventsTransport(
      "http://hub.local",
      async (input, init) => {
        const request =
          input instanceof Request ? input : new Request(input, init);
        expect(request.url).toBe(
          "http://hub.local/conduit.packaging.v1.PackagingRunService/ListPackagingRunEvents",
        );
        expect(request.method).toBe("POST");
        expect(request.credentials).toBe("same-origin");
        expect(
          Number(request.headers.get("Connect-Timeout-Ms")),
        ).toBeLessThanOrEqual(10000);
        bodies.push(await request.json());
        return reply(payload);
      },
    );
    const page = await fetchPackagingEvents(transport, undefined, query);
    expect(page.total).toBe(BigInt(payload.total));
    expect(page.nextCursor).toBe("opaque/+=");
    expect(page.items[0]).toMatchObject({ ...event, sequence: 1 });
    expect(page.approvalAuthority).toBe("conduit-governance");
    await fetchPackagingEvents(transport, undefined, {
      ...query,
      cursor: page.nextCursor,
    });
    expect(bodies).toEqual([
      { runId: "r/1" },
      { runId: "r/1", cursor: "opaque/+=" },
    ]);
  });
  it.each([
    [401, "unauthenticated", 401],
    [403, "permission_denied", 403],
    [404, "not_found", 404],
    [400, "invalid_argument", 422],
    [503, "unavailable", 503],
    [501, "unimplemented", 501],
  ])(
    "preserves error %i without REST replay",
    async (status, code, expected) => {
      let calls = 0;
      const transport = packagingEventsTransport(
        "http://hub.local",
        async () => {
          calls++;
          return new Response(JSON.stringify({ code, message: "refused" }), {
            status,
            headers: { "content-type": "application/json" },
          });
        },
      );
      await expect(
        fetchPackagingEvents(transport, undefined, query),
      ).rejects.toMatchObject({ name: "HubError", status: expected });
      expect(calls).toBe(1);
    },
  );
  it.each([
    { ...payload, runId: "other" },
    { ...payload, pageSize: 2 },
    { ...payload, total: "0" },
    { ...payload, limit: 0 },
    { ...payload, limit: 21 },
    { ...payload, configured: false },
    { ...payload, items: [{ ...event, sequence: "9007199254740993" }] },
    { ...payload, items: [{ ...event, sequence: "0" }] },
    { ...payload, items: [{ sequence: "1", step: "assess" }] },
  ])("refuses inconsistent or unrepresentable evidence %#", async (body) => {
    await expect(
      fetchPackagingEvents(
        packagingEventsTransport("http://hub.local", async () => reply(body)),
        undefined,
        query,
      ),
    ).rejects.toMatchObject({ name: "HubError", status: 502 });
  });
  it("retains the unconfigured empty model", async () => {
    const body = {
      ...payload,
      configured: false,
      runId: "",
      items: [],
      pageSize: 0,
      total: "0",
      nextCursor: "",
    };
    const page = await fetchPackagingEvents(
      packagingEventsTransport("http://hub.local", async () => reply(body)),
      undefined,
      query,
    );
    expect(page.configured).toBe(false);
    expect(page.total).toBe(BigInt(0));
  });
  it("does not mark a bounded prefix complete after rounding an int64 total", async () => {
    const transport = packagingEventsTransport("http://hub.local", async () =>
      reply({ ...payload, nextCursor: "" }),
    );
    const history = await readPackagingHistory(query.runId, (q) =>
      fetchPackagingEvents(transport, undefined, q),
    );
    expect(history.total).toBe(BigInt(payload.total));
    expect(history.complete).toBe(false);
    expect(history.items).toHaveLength(1);
  });
  it("cancels the actual fetch", async () => {
    const controller = new AbortController();
    let ready!: () => void;
    const started = new Promise<void>((resolve) => {
      ready = resolve;
    });
    const transport = packagingEventsTransport(
      "http://hub.local",
      async (input, init) => {
        const request =
          input instanceof Request ? input : new Request(input, init);
        ready();
        return await new Promise<Response>((_, reject) =>
          request.signal.addEventListener(
            "abort",
            () => reject(new DOMException("Aborted", "AbortError")),
            { once: true },
          ),
        );
      },
    );
    const pending = fetchPackagingEvents(transport, controller.signal, query);
    const failure = expect(pending).rejects.toMatchObject({ status: 499 });
    await started;
    controller.abort();
    await failure;
  });
});

import { describe, expect, it } from "vite-plus/test";
import {
  fetchPackagingRuns,
  packagingRunsTransport,
} from "../../api/packaging-runs.js";
const payload = {
  tier: "CONDUIT_NATIVE",
  configured: true,
  authority: "packaging-run",
  planAuthority: "conduit-plan",
  approvalAuthority: "conduit-governance",
  items: [
    {
      runId: "run-a",
      phase: "review",
      reason: "<native>",
      targetPlanRef: "dev/app",
    },
  ],
  pageSize: 1,
  limit: 20,
  total: "9007199254740993",
  nextCursor: "opaque/+=",
};
const reply = (body: unknown) =>
  new Response(JSON.stringify(body), {
    headers: { "content-type": "application/json" },
  });
describe("packaging index generated client", () => {
  it("uses the read procedure, same-origin identity and exact counts without following cursors", async () => {
    const bodies: unknown[] = [];
    const transport = packagingRunsTransport(
      "http://hub.local",
      async (input, init) => {
        const req = input instanceof Request ? input : new Request(input, init);
        expect(req.url).toBe(
          "http://hub.local/conduit.packaging.v1.PackagingRunService/ListPackagingRuns",
        );
        expect(req.method).toBe("POST");
        expect(req.credentials).toBe("same-origin");
        expect(
          Number(req.headers.get("Connect-Timeout-Ms")),
        ).toBeLessThanOrEqual(10000);
        bodies.push(await req.json());
        return reply(payload);
      },
    );
    const page = await fetchPackagingRuns(transport);
    expect(page.total).toBe(BigInt("9007199254740993"));
    expect(page.nextCursor).toBe("opaque/+=");
    expect(page.items[0]).toMatchObject(payload.items[0]);
    expect(page.approvalAuthority).toBe("conduit-governance");
    await fetchPackagingRuns(transport, undefined, {
      limit: 0,
      cursor: "opaque/+=",
      phase: "review",
      outcome: "WAITING",
      planId: "p",
      search: "a",
    });
    expect(bodies).toEqual([
      {},
      {
        limit: 0,
        cursor: "opaque/+=",
        phase: "review",
        outcome: "WAITING",
        planId: "p",
        search: "a",
      },
    ]);
  });
  it.each([
    [401, "unauthenticated", 401],
    [403, "permission_denied", 403],
    [400, "invalid_argument", 422],
    [503, "unavailable", 503],
  ])("keeps native error semantics %i", async (status, code, expected) => {
    let calls = 0;
    const transport = packagingRunsTransport("http://hub.local", async () => {
      calls++;
      return new Response(JSON.stringify({ code, message: "refused" }), {
        status,
        headers: { "content-type": "application/json" },
      });
    });
    await expect(fetchPackagingRuns(transport)).rejects.toMatchObject({
      name: "HubError",
      status: expected,
    });
    expect(calls).toBe(1);
  });
  it.each([
    { ...payload, pageSize: 2 },
    { ...payload, limit: 0 },
    { ...payload, total: "0" },
    { ...payload, items: [{}] },
  ])("rejects inconsistent pages %#", async (page) => {
    await expect(
      fetchPackagingRuns(
        packagingRunsTransport("http://hub.local", async () => reply(page)),
      ),
    ).rejects.toMatchObject({ name: "HubError", status: 502 });
  });
  it("cancels the actual fetch", async () => {
    const controller = new AbortController();
    let ready!: () => void;
    const started = new Promise<void>((resolve) => {
      ready = resolve;
    });
    const transport = packagingRunsTransport(
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
    const pending = fetchPackagingRuns(transport, controller.signal);
    const failure = expect(pending).rejects.toMatchObject({ status: 499 });
    await started;
    controller.abort();
    await failure;
  });
});

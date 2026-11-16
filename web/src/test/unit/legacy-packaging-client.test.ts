import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import {
  listPackagingRuns,
  listPackagingEvents,
} from "../../console/data/hub.js";

const service = "http://hub.local/conduit.packaging.v1.PackagingRunService/";
const at = "2026-11-16T12:00:00.123456789Z";
const reply = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
function install(respond: (request: Request) => Response | Promise<Response>) {
  vi.stubGlobal("window", {
    location: { origin: "http://hub.local", pathname: "/console/" },
  });
  const calls: Request[] = [];
  vi.stubGlobal("fetch", async (input: RequestInfo | URL) => {
    expect(input).toBeInstanceOf(Request);
    const request = input as Request;
    calls.push(request);
    expect(request.method).toBe("POST");
    expect(request.credentials).toBe("same-origin");
    expect(request.url.startsWith(service)).toBe(true);
    return respond(request);
  });
  return calls;
}
afterEach(() => vi.unstubAllGlobals());

describe("legacy packaging helper generated reads", () => {
  it("keeps the list DTO and first-page limit without following a cursor", async () => {
    const row = {
      configured: true,
      tier: "CONDUIT_NATIVE",
      phase: "review",
      outcome: "WAITING",
      reason: "<review>",
      runId: "run-a",
      sourceDigest: "sha256:source",
      artifactDigest: "sha256:artifact",
      proposalRevision: "revision-a",
      targetPlanRef: "plan-ref",
      planId: "plan-a",
      handoffReconciled: true,
      lastReview: "accepted",
    };
    const calls = install(async (request) => {
      expect(request.url).toBe(service + "ListPackagingRuns");
      expect(await request.json()).toEqual({ limit: 20 });
      return reply({
        configured: true,
        items: [row],
        limit: 20,
        pageSize: 1,
        total: "99",
        nextCursor: "opaque/+=",
        approvalAuthority: "conduit-governance",
      });
    });
    expect(await listPackagingRuns()).toEqual({
      configured: true,
      items: [row],
    });
    expect(calls).toHaveLength(1);
  });

  it("keeps exact safe sequences, nanoseconds and the existing first-page event DTO", async () => {
    const calls = install(async (request) => {
      expect(request.url).toBe(service + "ListPackagingRunEvents");
      expect(await request.json()).toEqual({ runId: "run /+=" });
      return reply({
        configured: true,
        runId: "run /+=",
        items: [
          {
            sequence: "9007199254740991",
            at,
            step: "inspect",
            outcome: "PASS",
            reason: "<native>",
          },
        ],
        limit: 20,
        pageSize: 1,
        total: "2",
        nextCursor: "next",
      });
    });
    expect(await listPackagingEvents("run /+=")).toEqual([
      {
        sequence: Number.MAX_SAFE_INTEGER,
        at,
        step: "inspect",
        outcome: "PASS",
        reason: "<native>",
      },
    ]);
    expect(calls).toHaveLength(1);
  });

  it("retains the unconfigured empty state for both reads", async () => {
    const calls = install(() =>
      reply({
        configured: false,
        items: [],
        limit: 20,
        pageSize: 0,
        total: "0",
      }),
    );
    expect(await listPackagingRuns()).toEqual({ configured: false, items: [] });
    expect(await listPackagingEvents("run-a")).toEqual([]);
    expect(calls).toHaveLength(2);
  });

  it.each(["9007199254740993", "0"])(
    "rejects unsafe evidence sequence %s instead of rendering zero",
    async (sequence) => {
      const calls = install(() =>
        reply({
          configured: true,
          runId: "run-a",
          items: [{ sequence, at }],
          limit: 20,
          pageSize: 1,
          total: "1",
        }),
      );
      await expect(listPackagingEvents("run-a")).rejects.toMatchObject({
        name: "HubError",
        status: 502,
      });
      expect(calls).toHaveLength(1);
    },
  );

  for (const [name, read] of [
    ["list", (signal?: AbortSignal) => listPackagingRuns(signal)],
    ["events", (signal?: AbortSignal) => listPackagingEvents("run-a", signal)],
  ] as const) {
    it.each([
      [401, "unauthenticated", 401],
      [403, "permission_denied", 403],
      [400, "invalid_argument", 422],
      [501, "unimplemented", 501],
    ] as const)(
      `${name} propagates %i without a REST retry`,
      async (status, code, expected) => {
        const calls = install(() =>
          reply({ code, message: "refused" }, status),
        );
        await expect(read()).rejects.toMatchObject({
          name: "HubError",
          status: expected,
        });
        expect(calls).toHaveLength(1);
      },
    );

    it(`${name} forwards cancellation to the actual generated-client fetch`, async () => {
      let ready!: () => void;
      const started = new Promise<void>((resolve) => {
        ready = resolve;
      });
      const calls = install((request) => {
        ready();
        return new Promise<Response>((_, reject) =>
          request.signal.addEventListener(
            "abort",
            () => reject(new DOMException("Aborted", "AbortError")),
            { once: true },
          ),
        );
      });
      const controller = new AbortController();
      const pending = expect(read(controller.signal)).rejects.toMatchObject({
        status: 499,
      });
      await started;
      controller.abort();
      await pending;
      expect(calls).toHaveLength(1);
      expect(calls[0]?.signal.aborted).toBe(true);
    });
  }
});

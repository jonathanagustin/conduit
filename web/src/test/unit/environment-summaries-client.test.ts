import { describe, expect, it } from "vite-plus/test";
import {
  environmentSummariesTransport,
  fetchEnvironmentSummaries,
} from "../../api/environment-summaries.js";

const row = {
  environment: { id: "env-a", generation: "opaque/+=" },
  spoke: { state: "FUTURE_STATE" },
};
const payload = {
  environments: [row],
  total: "9007199254740993",
  pageSize: 1,
  limit: 20,
  nextCursor: "opaque/+==",
  hasPrevious: true,
  previousCursor: "older/+==",
};
const reply = (body: unknown) =>
  new Response(JSON.stringify(body), {
    headers: { "content-type": "application/json" },
  });
const path =
  "/v1/environments?view=summary&limit=20&search=hello&spaceId=space_selected&cursor=held%2F%2B%3D";
describe("native Environment summary reader", () => {
  it("preserves coordinates, opaque generation, exact total and absent observations", async () => {
    const bodies: unknown[] = [];
    const transport = environmentSummariesTransport(
      "http://hub.local",
      async (input, init) => {
        const request =
          input instanceof Request ? input : new Request(input, init);
        expect(request.url).toBe(
          "http://hub.local/conduit.environments.v1.EnvironmentReadService/ListEnvironmentSummaries",
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
    const page = await fetchEnvironmentSummaries(transport, undefined, path);
    expect(bodies).toEqual([
      {
        limit: 20,
        search: "hello",
        spaceId: "space_selected",
        cursor: "held/+=",
      },
    ]);
    expect(page.total).toBe(BigInt("9007199254740993"));
    expect(page.environments[0]).toMatchObject(row);
    expect(page.environments[0]).not.toHaveProperty("reported");
    expect(page.environments[0].spoke.observedAt).toBeUndefined();
    expect(page.nextCursor).toBe("opaque/+==");
  });
  it("retains nanoseconds and an observed empty report without inventing entity bodies", async () => {
    const observedAt = "2026-11-20T12:00:00.123456789Z";
    const transport = environmentSummariesTransport(
      "http://hub.local",
      async () =>
        reply({
          ...payload,
          environments: [
            {
              ...row,
              reported: {
                environment: "env-a",
                agentId: "agent",
                entityCount: "0",
                observedAt,
              },
            },
          ],
        }),
    );
    const page = await fetchEnvironmentSummaries(transport, undefined, path);
    expect(page.environments[0].reported).toEqual({
      environment: "env-a",
      agentId: "agent",
      entityCount: 0,
      observedAt,
    });
    expect(page.environments[0].reported).not.toHaveProperty("entities");
  });
  for (const [name, changes] of [
    ["missing identity", { environments: [{ spoke: row.spoke }] }],
    ["missing liveness", { environments: [{ environment: row.environment }] }],
    ["duplicate identity", { environments: [row, row], pageSize: 2 }],
    [
      "wrong report binding",
      { environments: [{ ...row, reported: { environment: "other" } }] },
    ],
    [
      "negative count",
      {
        environments: [
          { ...row, reported: { environment: "env-a", entityCount: "-1" } },
        ],
      },
    ],
    [
      "unsafe entity count",
      {
        environments: [
          {
            ...row,
            reported: { environment: "env-a", entityCount: "9007199254740993" },
          },
        ],
      },
    ],
    ["negative total", { total: "-1" }],
    ["wrong page size", { pageSize: 2 }],
    ["wrong limit", { limit: 100 }],
    ["oversized cursor", { nextCursor: "x".repeat(2049) }],
    [
      "invalid timestamp",
      {
        environments: [
          { ...row, spoke: { state: "FUTURE", observedAt: "invalid" } },
        ],
      },
    ],
  ] as const)
    it(`refuses ${name} without replay`, async () => {
      let calls = 0;
      const transport = environmentSummariesTransport(
        "http://hub.local",
        async () => {
          calls++;
          return reply({ ...payload, ...changes });
        },
      );
      await expect(
        fetchEnvironmentSummaries(transport, undefined, path),
      ).rejects.toThrow();
      expect(calls).toBe(1);
    });
  for (const query of [
    "view=full",
    "limit=20",
    "view=summary&limit=0",
    "view=summary&limit=201",
    "view=summary&limit=1&limit=2",
    "view=summary&unknown=x",
  ])
    it(`rejects unsupported selection before HTTP: ${query}`, async () => {
      let calls = 0;
      const transport = environmentSummariesTransport(
        "http://hub.local",
        async () => {
          calls++;
          return reply(payload);
        },
      );
      await expect(
        fetchEnvironmentSummaries(
          transport,
          undefined,
          "/v1/environments?" + query,
        ),
      ).rejects.toThrow();
      expect(calls).toBe(0);
    });
  it("maps a denied read once and propagates cancellation", async () => {
    let calls = 0;
    const transport = environmentSummariesTransport(
      "http://hub.local",
      async () => {
        calls++;
        return new Response(
          JSON.stringify({ code: "permission_denied", message: "refused" }),
          { status: 403, headers: { "content-type": "application/json" } },
        );
      },
    );
    await expect(
      fetchEnvironmentSummaries(transport, undefined, path),
    ).rejects.toMatchObject({ status: 403 });
    expect(calls).toBe(1);
    const controller = new AbortController();
    controller.abort();
    await expect(
      fetchEnvironmentSummaries(transport, controller.signal, path),
    ).rejects.toMatchObject({ status: 499 });
    expect(calls).toBe(1);
  });
});

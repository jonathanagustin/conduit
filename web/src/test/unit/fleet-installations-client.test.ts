import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import {
  fetchFleetInstallations,
  fleetInstallationsTransport,
} from "../../api/fleet-installations.js";
import { listFleet } from "../../console/data/hub.js";
afterEach(() => vi.unstubAllGlobals());
const row = {
  environment: "env-a",
  installationId: "install-a",
  product: "app",
  namespace: "ns",
  desiredVersion: "2",
  observedVersion: "1",
  desiredReleaseDigest: "sha256:2",
  observedReleaseDigest: "sha256:1",
  desiredConfigDigest: "sha256:cfg2",
  observedConfigDigest: "sha256:cfg1",
  health: "FUTURE_HEALTH",
  rolloutStatus: "PENDING",
  configStatus: "UNKNOWN",
  lifecycle: "ACTIVE",
  observationAgeSeconds: "3",
  stale: true,
};
const payload = {
  generatedAt: "2026-11-20T01:02:03.123456789Z",
  maxResultAgeSeconds: "120",
  installations: [row],
  summary: {
    total: "9007199254740993",
    health: { FUTURE_HEALTH: "9007199254740993" },
    stale: "1",
  },
  facets: { environment: { "env-a": "9007199254740993" } },
  pageSize: 1,
  limit: 20,
  nextCursor: "opaque/+=",
  previousCursor: "old/+=",
  hasPrevious: true,
};
const reply = (body: unknown) =>
  new Response(JSON.stringify(body), {
    headers: { "content-type": "application/json" },
  });
describe("native Fleet generated reader", () => {
  it("retains the native unset maximum-age policy as zero", async () => {
    const transport = fleetInstallationsTransport(
      "http://hub.local",
      async () => reply({ ...payload, maxResultAgeSeconds: undefined }),
    );
    expect(
      (
        await fetchFleetInstallations(
          transport,
          undefined,
          "/v1/fleet/installations",
        )
      ).maxResultAgeSeconds,
    ).toBe(0);
  });

  it("preserves filter and Space coordinates, exact counts, timestamps and absent observations", async () => {
    const bodies: unknown[] = [];
    const transport = fleetInstallationsTransport(
      "http://hub.local",
      async (input, init) => {
        const request =
          input instanceof Request ? input : new Request(input, init);
        expect(request.url).toBe(
          "http://hub.local/conduit.fleet.v1.FleetReadService/ListFleetInstallations",
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
    const page = await fetchFleetInstallations(
      transport,
      undefined,
      "/v1/fleet/installations?limit=20&environment=env-a&installationId=install-a&product=app&health=FUTURE_HEALTH&rolloutStatus=PENDING&configStatus=UNKNOWN&lifecycle=ACTIVE&search=hello&cursor=opaque%2F%2B%3D&maxAgeSeconds=120&spaceId=space_selected",
    );
    expect(bodies).toEqual([
      {
        environment: "env-a",
        installationId: "install-a",
        product: "app",
        health: "FUTURE_HEALTH",
        rolloutStatus: "PENDING",
        configStatus: "UNKNOWN",
        lifecycle: "ACTIVE",
        search: "hello",
        cursor: "opaque/+=",
        limit: 20,
        maxAgeSeconds: 120,
        spaceId: "space_selected",
      },
    ]);
    expect(page.summary?.total).toBe(BigInt(payload.summary.total));
    expect(page.facets?.environment["env-a"]).toBe(
      BigInt(payload.summary.total),
    );
    expect(page.generatedAt).toBe(payload.generatedAt);
    expect(page.installations[0]).toMatchObject({
      ...row,
      observationAgeSeconds: 3,
      observedAt: undefined,
    });
    expect(page.nextCursor).toBe(payload.nextCursor);
    expect(page.previousCursor).toBe(payload.previousCursor);
    expect(page.hasPrevious).toBe(true);
  });
  it("retains a recorded observation's nanoseconds", async () => {
    const transport = fleetInstallationsTransport(
      "http://hub.local",
      async () =>
        reply({
          ...payload,
          installations: [{ ...row, observedAt: payload.generatedAt }],
        }),
    );
    expect(
      (
        await fetchFleetInstallations(
          transport,
          undefined,
          "/v1/fleet/installations",
        )
      ).installations[0].observedAt,
    ).toBe(payload.generatedAt);
  });
  it.each([
    [401, "unauthenticated", 401],
    [403, "permission_denied", 403],
    [400, "invalid_argument", 422],
    [404, "not_found", 404],
    [503, "unavailable", 503],
  ])("keeps refusal %i without replay", async (status, code, expected) => {
    let calls = 0;
    const transport = fleetInstallationsTransport(
      "http://hub.local",
      async () => {
        calls++;
        return new Response(JSON.stringify({ code, message: "refused" }), {
          status: Number(status),
          headers: { "content-type": "application/json" },
        });
      },
    );
    await expect(
      fetchFleetInstallations(transport, undefined, "/v1/fleet/installations"),
    ).rejects.toMatchObject({ status: expected });
    expect(calls).toBe(1);
  });
  it.each([
    "foreign",
    "identity",
    "duplicate",
    "page-size",
    "total",
    "stale",
    "negative-facet",
    "huge-age",
    "huge-max-age",
    "no-generated-time",
    "long-cursor",
  ])("rejects invalid %s evidence", async (name) => {
    const body = structuredClone(payload);
    if (name === "foreign") body.installations[0].environment = "foreign";
    if (name === "identity") body.installations[0].installationId = "";
    if (name === "duplicate") {
      body.installations.push({ ...row });
      body.pageSize = 2;
    }
    if (name === "page-size") body.pageSize = 2;
    if (name === "total") body.summary.total = "0";
    if (name === "stale") body.summary.stale = "-1";
    if (name === "negative-facet") body.facets.environment["env-a"] = "-1";
    if (name === "huge-age")
      body.installations[0].observationAgeSeconds = "9007199254740993";
    if (name === "huge-max-age") body.maxResultAgeSeconds = "9007199254740993";
    if (name === "no-generated-time") body.generatedAt = "";
    if (name === "long-cursor") body.nextCursor = "x".repeat(2049);
    const transport = fleetInstallationsTransport(
      "http://hub.local",
      async () => reply(body),
    );
    await expect(
      fetchFleetInstallations(
        transport,
        undefined,
        "/v1/fleet/installations?environment=env-a",
      ),
    ).rejects.toMatchObject({ status: 502 });
  });
  it.each([
    "?limit=0",
    "?limit=201",
    "?limit=1&limit=2",
    "?maxAgeSeconds=86401",
    "?unknown=1",
  ])("rejects unsupported request %s before network", async (query) => {
    let calls = 0;
    const transport = fleetInstallationsTransport(
      "http://hub.local",
      async () => {
        calls++;
        return reply(payload);
      },
    );
    await expect(
      fetchFleetInstallations(
        transport,
        undefined,
        "/v1/fleet/installations" + query,
      ),
    ).rejects.toMatchObject({ status: 502 });
    expect(calls).toBe(0);
  });
  it("forwards cancellation without a second request", async () => {
    const controller = new AbortController();
    let calls = 0;
    const transport = fleetInstallationsTransport(
      "http://hub.local",
      async (input, init) => {
        calls++;
        const request =
          input instanceof Request ? input : new Request(input, init);
        return new Promise<Response>((_resolve, reject) => {
          request.signal.addEventListener(
            "abort",
            () => reject(new DOMException("Aborted", "AbortError")),
            { once: true },
          );
          controller.abort();
        });
      },
    );
    await expect(
      fetchFleetInstallations(
        transport,
        controller.signal,
        "/v1/fleet/installations",
      ),
    ).rejects.toMatchObject({ status: 499 });
    expect(calls).toBe(1);
  });
});

describe("legacy Fleet caller", () => {
  function install(body: unknown, status = 200) {
    vi.stubGlobal("window", { location: { origin: "http://hub.local" } });
    const calls: Request[] = [];
    vi.stubGlobal("fetch", async (input: RequestInfo | URL) => {
      expect(input).toBeInstanceOf(Request);
      const request = input as Request;
      calls.push(request);
      expect(request.url).toBe(
        "http://hub.local/conduit.fleet.v1.FleetReadService/ListFleetInstallations",
      );
      expect(request.method).toBe("POST");
      expect(request.credentials).toBe("same-origin");
      expect(await request.json()).toEqual({ limit: 20 });
      return new Response(JSON.stringify(body), {
        status,
        headers: { "content-type": "application/json" },
      });
    });
    return calls;
  }
  it("preserves its row DTO and first-page limit without following cursors", async () => {
    const calls = install(payload);
    expect(await listFleet()).toEqual([
      {
        id: "install-a",
        environment: "env-a",
        product: "app",
        health: "FUTURE_HEALTH",
      },
    ]);
    expect(calls).toHaveLength(1);
  });
  it("retains an authorized empty projection", async () => {
    const calls = install({
      ...payload,
      installations: [],
      pageSize: 0,
      summary: { total: "0" },
    });
    expect(await listFleet()).toEqual([]);
    expect(calls).toHaveLength(1);
  });
  it.each([
    [401, "unauthenticated", 401],
    [403, "permission_denied", 403],
    [404, "not_found", 404],
    [501, "unimplemented", 501],
  ] as const)(
    "propagates refusal %i without replay",
    async (status, code, expected) => {
      const calls = install({ code, message: "refused" }, status);
      await expect(listFleet()).rejects.toMatchObject({
        name: "HubError",
        status: expected,
      });
      expect(calls).toHaveLength(1);
    },
  );
  it("refuses malformed rows instead of silently dropping them", async () => {
    const calls = install({
      ...payload,
      installations: [{ ...row, installationId: "" }],
    });
    await expect(listFleet()).rejects.toMatchObject({
      name: "HubError",
      status: 502,
    });
    expect(calls).toHaveLength(1);
  });
  it("forwards cancellation without REST replay", async () => {
    vi.stubGlobal("window", { location: { origin: "http://hub.local" } });
    const controller = new AbortController();
    let request: Request | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        request = input;
        return new Promise<Response>((_, reject) => {
          input.signal.addEventListener(
            "abort",
            () => reject(input.signal.reason),
            { once: true },
          );
        });
      }),
    );
    const result = listFleet(controller.signal);
    await vi.waitFor(() => expect(request).toBeDefined());
    controller.abort();
    await expect(result).rejects.toMatchObject({
      name: "HubError",
      status: 499,
    });
    expect(request?.signal.aborted).toBe(true);
    expect(fetch).toHaveBeenCalledTimes(1);
  });
});

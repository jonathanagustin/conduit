import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { runAssistantAction } from "../../console/operator/assistant-rail.js";
import * as packagingContext from "../../console/operator/packaging-action-context.js";

const run = {
  configured: true,
  runId: "selected",
  targetPlanRef: "env-a/api@namespace/rid",
  phase: "review",
  sourceDigest: "sha256:source",
  proposalRevision: "reviewed",
  templateVersion: "v1",
  toolVersions: "tools",
};
const row = {
  environment: "env-a",
  installationId: "api@namespace/rid",
  product: "acme:api",
  desiredVersion: "1",
};
const page = () => ({
  installations: [{ ...row }],
  summary: { total: 1 },
  pageSize: 1,
  limit: 1,
});
const state = () => ({
  screen: "s08-staged-run",
  assistantMode: "release_plan",
  packagingRunId: run.runId,
  operatorName: "operator",
  spaceId: "",
  assistantRemoved: [] as string[],
  hub: {
    packaging: [
      { runId: "unrelated", targetPlanRef: "wrong/first" },
      { ...run },
    ],
    installations: null,
    indexPages: { packaging: { shown: 20, total: 321 } },
  },
});
const response = (body: unknown, ok = true) => ({ ok, json: async () => body });
// Project these existing semantic fixtures onto actual generated-client Requests.
// Only the fixture callback sees a query URL; production never sends REST Fleet.
function installFetch(mock: (url: string, options?: any) => Promise<any>) {
  vi.stubGlobal("window", { location: { origin: "http://hub.local" } });
  vi.stubGlobal(
    "fetch",
    async (input: RequestInfo | URL, options?: RequestInit) => {
      if (!(input instanceof Request)) {
        expect(String(input)).not.toMatch(/^\/v1\/fleet\/installations/);
        return mock(String(input), options);
      }
      expect(input.url).toBe(
        "http://hub.local/conduit.fleet.v1.FleetReadService/ListFleetInstallations",
      );
      expect(input.method).toBe("POST");
      expect(input.credentials).toBe("same-origin");
      const body = (await input.clone().json()) as Record<string, unknown>;
      const query = new URLSearchParams();
      const keys = ["environment", "installationId", "limit", "spaceId"];
      for (const key of [
        ...keys,
        ...Object.keys(body).filter((key) => !keys.includes(key)),
      ])
        if (body[key] !== undefined) {
          const value = body[key];
          expect(["string", "number"]).toContain(typeof value);
          query.set(
            key,
            typeof value === "string" ? value : JSON.stringify(value),
          );
        }
      const result = await mock(`/v1/fleet/installations?${query}`, {
        method: "POST",
        signal: input.signal,
      });
      if (!result.ok)
        return new Response(
          JSON.stringify({
            code: "permission_denied",
            message: "Fleet fixture refused",
          }),
          { status: 403, headers: { "content-type": "application/json" } },
        );
      const value = await result.json();
      return new Response(
        JSON.stringify({
          ...value,
          generatedAt: "2026-11-20T01:00:00Z",
          facets: {},
          summary: { ...value.summary, total: String(value.summary.total) },
        }),
        { headers: { "content-type": "application/json" } },
      );
    },
  );
}
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("fresh packaging action target", () => {
  for (const [spaceId, allowed] of [
    [`space_${"a".repeat(32)}`, true],
    ["unavailable-selection", false],
  ] as const) {
    it(`preserves selected Space ${spaceId} without an unscoped retry`, async () => {
      const original = state();
      original.spaceId = spaceId;
      const fetch = vi.fn(async (url: string) =>
        url.startsWith("/v1/packaging-runs/")
          ? response(run)
          : url.startsWith("/v1/fleet/installations?")
            ? response(page(), allowed)
            : response({ ok: true }),
      );
      installFetch(fetch);
      const result = await runAssistantAction(
        original,
        "propose-cancel",
        "PackagingRun:selected",
      );
      // Packaging is explicitly global; its installation lookup is scoped.
      expect(fetch.mock.calls[0][0]).toBe("/v1/packaging-runs/selected");
      const query = new URL(fetch.mock.calls[1][0], "http://hub.local")
        .searchParams;
      expect(Object.fromEntries(query)).toEqual({
        environment: row.environment,
        installationId: row.installationId,
        limit: "1",
        spaceId,
      });
      expect(result.ok).toBe(allowed);
      expect(fetch).toHaveBeenCalledTimes(allowed ? 3 : 2);
    });
  }
  it("rechecks selection at the final POST boundary after the resolver-return microtask gap", async () => {
    const original = state();
    const resolve = packagingContext.resolvePackagingActionContext;
    vi.spyOn(
      packagingContext,
      "resolvePackagingActionContext",
    ).mockImplementation(async (s, id) => {
      const result = await resolve(s, id);
      queueMicrotask(() => {
        s.packagingRunId = "unrelated";
      });
      return result;
    });
    const fetch = vi.fn(async (url: string) =>
      response(
        url.startsWith("/v1/packaging-runs/")
          ? run
          : url.startsWith("/v1/fleet/installations?")
            ? page()
            : { ok: true },
      ),
    );
    installFetch(fetch);
    const result = await runAssistantAction(
      original,
      "propose-cancel",
      "PackagingRun:selected",
    );
    expect(result.ok).toBe(false);
    expect(fetch).toHaveBeenCalledTimes(2);
  });
  it("uses exact authorized reads independently of visible index rows and counts", async () => {
    const original = state();
    const before = structuredClone(original.hub);
    const fetch = vi.fn(async (url: string, _options?: { body?: string }) =>
      response(
        url.startsWith("/v1/packaging-runs/")
          ? run
          : url.startsWith("/v1/fleet/installations?")
            ? page()
            : { ok: true },
      ),
    );
    installFetch(fetch);
    await runAssistantAction(
      original,
      "propose-rerun",
      "PackagingRun:selected",
    );
    expect(fetch.mock.calls.map((call) => call[0])).toEqual([
      "/v1/packaging-runs/selected",
      "/v1/fleet/installations?environment=env-a&installationId=api%40namespace%2Frid&limit=1",
      "/v1/assistant/tools",
    ]);
    expect(JSON.parse(fetch.mock.calls[2][1]!.body!).arguments).toMatchObject({
      environment: row.environment,
      installationId: row.installationId,
      productId: row.product,
      targetEntityId: run.runId,
    });
    expect(original.hub).toEqual(before);
  });
  for (const [name, change] of [
    [
      "missing target",
      (value: ReturnType<typeof page>) => {
        value.installations = [];
        value.summary.total = 0;
        value.pageSize = 0;
      },
    ],
    [
      "wrong environment",
      (value: ReturnType<typeof page>) => {
        value.installations[0].environment = "other";
      },
    ],
    [
      "substring installation",
      (value: ReturnType<typeof page>) => {
        value.installations[0].installationId += "-extra";
      },
    ],
    [
      "ambiguous page",
      (value: ReturnType<typeof page>) => {
        value.installations.push({ ...row, product: "unrelated" });
        value.summary.total = 2;
        value.pageSize = 2;
      },
    ],
    [
      "observed-only row",
      (value: ReturnType<typeof page>) => {
        value.installations[0].desiredVersion = "";
      },
    ],
    [
      "missing Product",
      (value: ReturnType<typeof page>) => {
        value.installations[0].product = "";
      },
    ],
    [
      "partial lookup",
      (value: ReturnType<typeof page>) => {
        value.summary.total = 21;
      },
    ],
  ] as const) {
    it(`refuses ${name} before the tool POST`, async () => {
      const value = page();
      change(value);
      const fetch = vi.fn(async (url: string) =>
        response(url.startsWith("/v1/packaging-runs/") ? run : value),
      );
      installFetch(fetch);
      expect(
        (
          await runAssistantAction(
            state(),
            "propose-cancel",
            "PackagingRun:selected",
          )
        ).ok,
      ).toBe(false);
      expect(fetch.mock.calls).toHaveLength(2);
    });
  }
  it("refuses a forbidden target lookup", async () => {
    const fetch = vi.fn(async (url: string) =>
      url.startsWith("/v1/packaging-runs/")
        ? response(run)
        : response({}, false),
    );
    installFetch(fetch);
    expect(
      (
        await runAssistantAction(
          state(),
          "propose-cancel",
          "PackagingRun:selected",
        )
      ).ok,
    ).toBe(false);
    expect(fetch.mock.calls).toHaveLength(2);
  });
  for (const [name, patch] of [
    ["changed target", { targetPlanRef: "different/installation" }],
    ["unconfigured worker", { configured: false }],
    ["different run", { runId: "different" }],
  ] as const)
    it(`refuses a fresh run read with ${name}`, async () => {
      const fetch = vi.fn(async () => response({ ...run, ...patch }));
      installFetch(fetch);
      expect(
        (
          await runAssistantAction(
            state(),
            "propose-cancel",
            "PackagingRun:selected",
          )
        ).ok,
      ).toBe(false);
      expect(fetch).toHaveBeenCalledTimes(1);
    });
  it("a newer action supersedes an older unresolved lookup", async () => {
    const original = state();
    let release!: () => void;
    let lookups = 0;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    const fetch = vi.fn(async (url: string) => {
      if (url.startsWith("/v1/packaging-runs/")) return response(run);
      if (url.startsWith("/v1/fleet/installations?")) {
        if (++lookups === 1) await gate;
        return response(page());
      }
      return response({ ok: true });
    });
    installFetch(fetch);
    const old = runAssistantAction(
      original,
      "propose-rerun",
      "PackagingRun:selected",
    );
    await vi.waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));
    expect(
      (
        await runAssistantAction(
          original,
          "propose-cancel",
          "PackagingRun:selected",
        )
      ).ok,
    ).toBe(true);
    release();
    expect((await old).ok).toBe(false);
    expect(fetch).toHaveBeenCalledTimes(5);
  });
  for (const change of [
    "selection",
    "screen",
    "identity",
    "space",
    "refresh",
    "binding",
    "chip-removal",
  ] as const) {
    it(`refuses stale asynchronous ${change}`, async () => {
      const original = state();
      let release!: () => void;
      const gate = new Promise<void>((resolve) => {
        release = resolve;
      });
      const fetch = vi.fn(async (url: string) => {
        if (url.startsWith("/v1/packaging-runs/")) return response(run);
        await gate;
        return response(page());
      });
      installFetch(fetch);
      const result = runAssistantAction(
        original,
        "propose-cancel",
        "PackagingRun:selected",
      );
      await vi.waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));
      if (change === "selection") original.packagingRunId = "unrelated";
      if (change === "screen") original.screen = "area-fleet";
      if (change === "identity") original.operatorName = "other";
      if (change === "space") original.spaceId = `space_${"b".repeat(32)}`;
      if (change === "chip-removal")
        original.assistantRemoved = ["PackagingRun:selected"];
      if (change === "refresh") original.hub = structuredClone(original.hub);
      if (change === "binding")
        original.hub.packaging[1].targetPlanRef = "different/installation";
      release();
      expect((await result).ok).toBe(false);
      expect(fetch).toHaveBeenCalledTimes(2);
    });
  }
});

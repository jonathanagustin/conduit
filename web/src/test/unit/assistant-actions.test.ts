import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import {
  assistantChrome,
  resultCard,
  runAssistantAction,
} from "../../console/operator/assistant-rail.js";

const fixture = () => ({
  screen: "s08-staged-run",
  assistantMode: "ontology",
  hub: {
    packaging: [{ runId: "run-a", targetPlanRef: "env-a/api" }],
    environments: [{ environment: { id: "unrelated" } }],
    changes: [
      {
        environment: "unrelated",
        changes: [
          {
            installationId: "wrong",
            referencedResources: { product: { name: "wrong" } },
          },
        ],
      },
    ],
    installations: [
      { environment: "other", installationId: "api", product: "wrong" },
      { environment: "env-a", installationId: "api", product: "acme:api" },
    ],
  },
});
afterEach(() => vi.unstubAllGlobals());
describe("assistant supported actions", () => {
  it("offers real packaging controls and feedback, without inert memory writes", () => {
    const html = assistantChrome(fixture());
    expect(html).toContain("Propose rerun");
    expect(html).toContain("Propose cancel");
    expect(html).toContain("Add eval case");
    expect(html).not.toContain("Propose memory");
  });
  it("binds a proposal to the selected run and matching installation", async () => {
    vi.stubGlobal("window", { location: { origin: "http://hub.local" } });
    const fetch = vi.fn(
      async (input: RequestInfo | URL, _options?: RequestInit) => {
        if (input instanceof Request) {
          expect(input.url).toBe(
            "http://hub.local/conduit.fleet.v1.FleetReadService/ListFleetInstallations",
          );
          expect(input.method).toBe("POST");
          expect(input.credentials).toBe("same-origin");
          expect(await input.json()).toEqual({
            environment: "env-a",
            installationId: "api",
            limit: 1,
          });
          return new Response(
            JSON.stringify({
              installations: [
                {
                  environment: "env-a",
                  installationId: "api",
                  product: "acme:api",
                  desiredVersion: "1",
                },
              ],
              summary: { total: "1" },
              facets: {},
              generatedAt: "2026-11-20T01:00:00Z",
              pageSize: 1,
              limit: 1,
            }),
            { headers: { "content-type": "application/json" } },
          );
        }
        expect(String(input)).not.toMatch(/^\/v1\/fleet\/installations/);
        return {
          ok: true,
          json: async () =>
            String(input).startsWith("/v1/packaging-runs/")
              ? { ...fixture().hub.packaging[0], configured: true }
              : { ok: true },
        };
      },
    );
    vi.stubGlobal("fetch", fetch);
    await runAssistantAction(fixture(), "propose-cancel", "PackagingRun:run-a");
    expect(fetch).toHaveBeenCalledTimes(3);
    expect(fetch.mock.calls[2][0]).toBe("/v1/assistant/tools");
    expect(
      JSON.parse(fetch.mock.calls[2][1]!.body as string).arguments,
    ).toEqual({
      actionId: "cancel_packaging_run",
      targetEntityId: "run-a",
      targetObjectType: "PackagingRun",
      environment: "env-a",
      installationId: "api",
      productId: "acme:api",
    });
  });
  it("refuses missing coordinates and stale unsupported actions without sending a request", async () => {
    const fetch = vi.fn(
      async (_url: string, _options?: { method?: string }) => ({ ok: false }),
    );
    vi.stubGlobal("fetch", fetch);
    const state = fixture();
    state.hub.installations = [];
    expect(
      (await runAssistantAction(state, "propose-cancel", "PackagingRun:run-a"))
        .ok,
    ).toBe(false);
    expect(
      (
        await runAssistantAction(
          fixture(),
          "write-memory",
          "PackagingRun:run-a",
        )
      ).ok,
    ).toBe(false);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(fetch.mock.calls.every((call) => call[1]?.method !== "POST")).toBe(
      true,
    );
  });
  it("distinguishes a cancel acknowledgement from observed cancellation", () => {
    const accepted = resultCard({
      tool: "governed_write",
      execution: { state: "SUCCEEDED", outcome: "CANCEL_REQUEST_ACCEPTED" },
    });
    expect(accepted).toContain("termination has not been observed");
    const observed = resultCard({
      tool: "governed_write",
      execution: { state: "SUCCEEDED", outcome: "CANCELLATION_OBSERVED" },
    });
    expect(observed).toContain("Run cancellation observed.");
    expect(observed).not.toContain("termination has not been observed");
    expect(
      resultCard({ execution: { state: "FAILED", error: "<script>" } }),
    ).not.toContain("<script>");
  });
});

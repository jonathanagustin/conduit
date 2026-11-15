import { describe, expect, it } from "vite-plus/test";
import {
  fetchOverview,
  overviewTransport,
  HubError,
} from "../../api/overview.js";
import { HubError as LegacyHubError } from "../../console/data/hub.js";
import {
  overviewPanelValue,
  overviewSummaryMarkup,
} from "../../console/operator/overview-summary.js";
const payload = {
  generatedAt: "2026-11-15T12:00:00Z",
  staleAfterSeconds: "300",
  awaitingApprovals: {
    status: "CURRENT",
    count: "9007199254740993",
    items: [],
  },
  failedPlans: { status: "UNAVAILABLE", count: "0", items: [] },
};
describe("generated overview client", () => {
  it("reads the exact RPC with same-origin identity and preserves int64 precision", async () => {
    const calls: Request[] = [];
    const transport = overviewTransport(
      "http://hub.local",
      async (input, init) => {
        const request =
          input instanceof Request ? input : new Request(input, init);
        calls.push(request);
        expect(await request.clone().json()).toEqual({});
        return new Response(JSON.stringify(payload), {
          headers: { "content-type": "application/json" },
        });
      },
    );
    const view = await fetchOverview(transport);
    expect(calls).toHaveLength(1);
    expect(calls[0]?.method).toBe("POST");
    expect(calls[0]?.url).toBe(
      "http://hub.local/conduit.reporting.v1.ReportingService/GetOverview",
    );
    expect(calls[0]?.credentials).toBe("same-origin");
    expect(Number(calls[0]?.headers.get("Connect-Timeout-Ms"))).toBeGreaterThan(
      0,
    );
    expect(view.awaitingApprovals?.count).toBe(BigInt("9007199254740993"));
    const html = overviewSummaryMarkup(view);
    expect(html).toContain("9007199254740993");
    expect(html).not.toContain("9007199254740992");
    expect(overviewPanelValue(view.failedPlans)).toBe("Unavailable");
    expect(overviewPanelValue({ status: "CURRENT", count: BigInt(0) })).toBe(
      "0",
    );
    expect(overviewPanelValue({ status: "unexpected", count: BigInt(0) })).toBe(
      "Unavailable",
    );
    expect(overviewPanelValue({ status: "CURRENT", count: BigInt(-1) })).toBe(
      "Unavailable",
    );
    expect(() => overviewSummaryMarkup({})).toThrow("timestamp");
  });
  it.each([
    [401, "unauthenticated", 401],
    [403, "permission_denied", 403],
    [400, "invalid_argument", 422],
    [503, "unavailable", 503],
  ])(
    "preserves Hub error semantics for HTTP %i",
    async (status, code, expected) => {
      const transport = overviewTransport(
        "http://hub.local",
        async () =>
          new Response(JSON.stringify({ code, message: "request refused" }), {
            status,
            headers: { "content-type": "application/json" },
          }),
      );
      expect(HubError).toBe(LegacyHubError);
      await expect(fetchOverview(transport)).rejects.toMatchObject({
        name: "HubError",
        status: expected,
        message: "request refused",
      });
    },
  );
  it("propagates cancellation to the actual fetch", async () => {
    const controller = new AbortController();
    let started!: () => void;
    const ready = new Promise<void>((resolve) => {
      started = resolve;
    });
    let requestSignal: AbortSignal | undefined;
    const transport = overviewTransport(
      "http://hub.local",
      async (input, init) => {
        const request =
          input instanceof Request ? input : new Request(input, init);
        requestSignal = request.signal;
        started();
        return await new Promise<Response>((_, reject) =>
          request.signal.addEventListener(
            "abort",
            () => reject(new DOMException("Aborted", "AbortError")),
            { once: true },
          ),
        );
      },
    );
    const pending = fetchOverview(transport, controller.signal);
    const rejected = expect(pending).rejects.toMatchObject({
      name: "HubError",
      status: 499,
    });
    await ready;
    controller.abort();
    await rejected;
    expect(requestSignal?.aborted).toBe(true);
  });
});

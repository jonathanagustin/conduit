import { describe, expect, it, vi } from "vite-plus/test";
import {
  adminStatusHTML,
  createAdminStatus,
} from "../../console/operator/admin-status.js";

const snapshot = () => ({
  generatedAt: "2026-11-14T12:00:00Z",
  components: [{ label: "Hub", status: "AVAILABLE", detail: "Request served" }],
  counters: [
    {
      label: "Pending reviews",
      status: "AVAILABLE",
      value: 0,
      detail: "Stored requests",
      link: "rm07-change-requests",
    },
    {
      label: "Packaging runs",
      status: "UNAVAILABLE",
      value: null,
      detail: "Reader unavailable",
      link: "area-packaging",
    },
  ],
  storage: [
    {
      label: "Registry",
      status: "UNAVAILABLE",
      usedBytes: null,
      capacityBytes: null,
      usedPercent: null,
      warningPercent: 80,
      criticalPercent: 95,
      detail: "No usage telemetry",
    },
  ],
  audit: {
    status: "UNAVAILABLE",
    chainLength: null,
    verifiedAt: null,
    detail: "No stored-chain verifier",
  },
});
const root = () =>
  ({
    innerHTML: "",
    isConnected: true,
    onclick: null,
  }) as unknown as HTMLElement;

describe("read-only administrator status", () => {
  it("shows real zero counts separately from unavailable measurements", () => {
    const html = adminStatusHTML(snapshot());
    expect(html).toContain("<strong>0</strong>");
    expect(html).toContain("<strong>Unavailable</strong>");
    expect(html).toContain("Length unavailable");
    expect(html).toContain("Verification time unavailable");
    expect(html).toContain("Warning 80% · Critical 95%");
    expect(html).not.toContain("null entries");
    expect(html).not.toContain("0 B / 0 B");
  });
  it("escapes backend text and permits only owning-view links", () => {
    const data = snapshot();
    data.counters[0].label = '<img src=x onerror="alert(1)">';
    data.counters[0].link = "javascript:alert(1)";
    const html = adminStatusHTML(data);
    expect(html).not.toContain("<img");
    expect(html).toContain("&lt;img");
    expect(html).not.toContain("javascript:");
    expect(html).toContain('data-admin-open="area-packaging"');
  });
  it("uses a same-origin GET with no write body", async () => {
    const fetchImpl = vi.fn<typeof fetch>(
      async () => new Response(JSON.stringify(snapshot()), { status: 200 }),
    );
    const element = root();
    await createAdminStatus({ fetchImpl }).render(element);
    expect(fetchImpl).toHaveBeenCalledOnce();
    expect(fetchImpl.mock.calls[0][0]).toBe("/v1/admin/status");
    const request = fetchImpl.mock.calls[0][1];
    expect(request?.method).toBe("GET");
    expect(request?.credentials).toBe("same-origin");
    expect(request?.body).toBeUndefined();
    expect(element.innerHTML).toContain("Platform status");
  });
  it.each([401, 403])(
    "shows an explicit administrator refusal for %s without example counts",
    async (status) => {
      const fetchImpl = vi.fn<typeof fetch>(
        async () => new Response("private error", { status }),
      );
      const element = root();
      await createAdminStatus({ fetchImpl }).render(element);
      expect(element.innerHTML).toContain("Administrator access required");
      expect(element.innerHTML).not.toContain("<strong>0");
      expect(element.innerHTML).not.toContain("private error");
    },
  );
  it("keeps backend failures unavailable and hides error bodies", async () => {
    const fetchImpl = vi.fn<typeof fetch>(
      async () => new Response("private credential", { status: 503 }),
    );
    const element = root();
    await createAdminStatus({ fetchImpl }).render(element);
    expect(element.innerHTML).toContain("Platform status is unavailable");
    expect(element.innerHTML).not.toContain("private credential");
  });
  it("does not let an old response replace a newer render", async () => {
    let finish: (response: Response) => void = () => {};
    const old = new Promise<Response>((resolve) => {
      finish = resolve;
    });
    const fetchImpl = vi
      .fn<typeof fetch>()
      .mockImplementationOnce(() => old)
      .mockResolvedValueOnce(new Response("", { status: 403 }));
    const view = createAdminStatus({ fetchImpl });
    const element = root();
    const first = view.render(element);
    await view.render(element);
    finish(new Response(JSON.stringify(snapshot()), { status: 200 }));
    await first;
    expect(element.innerHTML).toContain("Administrator access required");
  });
});

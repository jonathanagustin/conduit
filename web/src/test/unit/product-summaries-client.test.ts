import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import {
  fetchProductSummaries,
  productSummariesTransport,
} from "../../api/product-summaries.js";
import { listProducts } from "../../console/data/hub.js";

const origin = "http://hub.local";
const procedure =
  origin + "/conduit.catalog.v1.ProductReadService/ListProductSummaries";
const reply = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
const page = () => ({
  products: [
    {
      product: "org:<ledger>",
      releaseCount: "9007199254740993",
      latestVersion: "10.0.0",
    },
  ],
  total: "9223372036854775807",
  pageSize: 1,
  limit: 20,
  nextCursor: "opaque/+==",
  previousCursor: "previous/%",
  hasPrevious: true,
});
function install(respond: (request: Request) => Response | Promise<Response>) {
  vi.stubGlobal("window", { location: { origin } });
  const calls: Request[] = [];
  vi.stubGlobal("fetch", async (input: RequestInfo | URL) => {
    expect(input).toBeInstanceOf(Request);
    const request = input as Request;
    calls.push(request);
    expect(request.method).toBe("POST");
    expect(request.credentials).toBe("same-origin");
    expect(request.url).toBe(procedure);
    return respond(request);
  });
  return calls;
}
afterEach(() => vi.unstubAllGlobals());

describe("generated native Product summary client", () => {
  it("preserves exact counts, opaque cursors and query inputs", async () => {
    const calls = install(async (request) => {
      expect(await request.json()).toEqual({
        limit: 20,
        cursor: "opaque/+==",
        search: " <ledger> ",
      });
      return reply(page());
    });
    const result = await fetchProductSummaries(
      productSummariesTransport(origin),
      undefined,
      { limit: 20, cursor: "opaque/+==", search: " <ledger> " },
    );
    expect(result.total).toBe(BigInt("9223372036854775807"));
    expect(result.products[0]?.releaseCount).toBe(BigInt("9007199254740993"));
    expect(result.products[0]?.latestVersion).toBe("10.0.0");
    expect(result.nextCursor).toBe("opaque/+==");
    expect(result.previousCursor).toBe("previous/%");
    expect(result.hasPrevious).toBe(true);
    expect(calls).toHaveLength(1);
  });

  it("keeps the legacy name DTO and first-page limit", async () => {
    const calls = install(async (request) => {
      expect(await request.json()).toEqual({ limit: 20 });
      return reply(page());
    });
    expect(await listProducts()).toEqual([{ name: "org:<ledger>" }]);
    expect(calls).toHaveLength(1);
  });

  it("retains an empty authorized catalog", async () => {
    const calls = install(() =>
      reply({ products: [], total: "0", pageSize: 0, limit: 20 }),
    );
    expect(await listProducts()).toEqual([]);
    expect(calls).toHaveLength(1);
  });

  it.each([
    { total: "0" },
    { pageSize: 0 },
    { limit: 0 },
    { limit: 201 },
    { products: [{ product: "", releaseCount: "1" }] },
    { products: [{ product: "org:bad", releaseCount: "-1" }] },
  ])("rejects invalid page evidence %j without replay", async (invalid) => {
    const calls = install(() => reply({ ...page(), ...invalid }));
    await expect(listProducts()).rejects.toMatchObject({
      name: "HubError",
      status: 502,
    });
    expect(calls).toHaveLength(1);
  });

  it.each([
    [401, "unauthenticated", 401],
    [403, "permission_denied", 403],
    [400, "invalid_argument", 422],
    [501, "unimplemented", 501],
  ] as const)(
    "propagates refusal %i without REST replay",
    async (status, code, expected) => {
      const calls = install(() => reply({ code, message: "refused" }, status));
      await expect(listProducts()).rejects.toMatchObject({
        name: "HubError",
        status: expected,
      });
      expect(calls).toHaveLength(1);
    },
  );

  it("cancels the actual generated request without falling back", async () => {
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
    const pending = expect(
      listProducts(controller.signal),
    ).rejects.toMatchObject({ status: 499 });
    await started;
    controller.abort();
    await pending;
    expect(calls).toHaveLength(1);
    expect(calls[0]?.signal.aborted).toBe(true);
  });
});

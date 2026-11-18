import { describe, expect, it, vi } from "vite-plus/test";
import {
  fetchSessionPage,
  loadSessionIndex,
  sessionIndexMarkup,
} from "../../console/operator/planning-session-index.js";
const row = (id = "ps_alice") => ({
  id,
  status: "ready",
  profile: { name: "onboard" },
  repoUrl: "https://example.test/repo",
});
const page = (items = [row()]) => ({
  items,
  valueTier: "CONDUIT_NATIVE",
  total: items.length,
  pageSize: items.length,
  limit: 100,
  hasPrevious: false,
});
const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
const asFetch = (value: unknown) => value as typeof fetch;
const asWindow = (value: unknown) => value as typeof globalThis;

describe("owner-scoped planning inventory", () => {
  it("uses only the native read and opaque cursor, with no caller-selectable owner", async () => {
    const fetchImpl = vi.fn(async () => json(page()));
    expect(await fetchSessionPage(asFetch(fetchImpl), "cHNfYWxpY2U")).toEqual(
      page(),
    );
    expect(fetchImpl.mock.calls).toEqual([
      ["/v1/planning-sessions?limit=100&cursor=cHNfYWxpY2U"],
    ]);
  });
  it("rejects unavailable and malformed inventories rather than fabricating empty success", async () => {
    await expect(
      fetchSessionPage(
        asFetch(async () => json({ error: "unavailable" }, 502)),
      ),
    ).rejects.toThrow("unavailable");
    for (const body of [
      { items: [] },
      { ...page(), pageSize: 9 },
      { ...page(), total: -1 },
      { ...page(), nextCursor: "bad&owner=bob" },
      page([row(), row()]),
      { ...page(), items: [{ ...row(), profile: null }] },
    ]) {
      await expect(
        fetchSessionPage(asFetch(async () => json(body))),
      ).rejects.toThrow("invalid planning");
    }
  });
  it("escapes literal metadata and links through the existing session deep link", () => {
    const html = sessionIndexMarkup({
      sessions: {
        ...page([
          {
            ...row(),
            repoUrl: "<script>literal</script>",
            status: '<img onerror="bad">',
          },
        ]),
        status: "ready",
      },
    });
    expect(html).toContain('href="#s01-intake/ps_alice"');
    expect(html).toContain("&lt;script&gt;literal&lt;/script&gt;");
    expect(html).not.toContain("<script>");
    expect(html).toContain("1 shown · 1 available");
    expect(html).toContain("does not cancel or approve a run");
  });
  it("discards an old-owner response even before another inventory read starts", async () => {
    let resolve!: (response: Response) => void;
    const response = new Promise<Response>((done) => {
      resolve = done;
    });
    const store: Record<string, any> = {
      reviewOperatorActor: "alice",
      planning: { message: "draft" },
    };
    const request = loadSessionIndex(
      store,
      asFetch(() => response),
      asWindow({}),
    );
    store.reviewOperatorActor = "bob";
    resolve(json(page()));
    await request;
    expect(store.planning.sessions.status).toBe("loading");
    expect(sessionIndexMarkup(store.planning)).not.toContain("ps_alice");
    expect(store.planning.message).toBe("draft");
  });
  it("discards superseded reads and preserves composer state during refresh", async () => {
    let resolve!: (response: Response) => void;
    const response = new Promise<Response>((done) => {
      resolve = done;
    });
    const store: Record<string, any> = {
      reviewOperatorActor: "alice",
      planning: { message: "draft" },
    };
    const first = loadSessionIndex(
      store,
      asFetch(() => response),
      asWindow({}),
    );
    await loadSessionIndex(
      store,
      asFetch(async () => json(page([row("ps_new")]))),
      asWindow({}),
    );
    resolve(json(page()));
    await first;
    expect(
      store.planning.sessions.items.map((item: { id: string }) => item.id),
    ).toEqual(["ps_new"]);
    expect(store.planning.message).toBe("draft");
    await loadSessionIndex(
      store,
      asFetch(async () => json({ error: "denied" }, 403)),
      asWindow({}),
    );
    expect(sessionIndexMarkup(store.planning)).toContain("denied");
    expect(sessionIndexMarkup(store.planning)).not.toContain("ps_new");
    await loadSessionIndex(
      store,
      asFetch(async () => json(page([]))),
      asWindow({}),
    );
    expect(sessionIndexMarkup(store.planning)).toContain(
      "No planning sessions are available to you",
    );
  });
});

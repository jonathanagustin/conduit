import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { fetchOwnAIUsage, parseOwnAIUsage } from "../../api/ai-usage.js";
import { HubError } from "../../api/hub-error.js";
import { createAIUsagePanel } from "../../console/operator/ai-usage.js";
import { createSettings } from "../../console/operator/settings.js";

function payload(owner = "alice") {
  return {
    owner,
    days: 1,
    budget: {
      day: "2026-11-18",
      resetsAt: "2026-11-19T00:00:00Z",
      used: { calls: 10, tokens: 0 },
      dailyCallBudget: 10 as number | null,
      dailyTokenBudget: null as number | null,
      remainingCalls: 0 as number | null,
      remainingTokens: null as number | null,
    },
    usage: [] as Record<string, unknown>[],
  };
}

const escape = (value: unknown) =>
  String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll('"', "&quot;");

afterEach(() => vi.unstubAllGlobals());

describe("own AI usage contract", () => {
  it("keeps an exhausted zero allowance distinct from unlimited", () => {
    const result = parseOwnAIUsage(payload());
    expect(result.remainingCalls).toBe(0);
    expect(result.tokenLimit).toBeNull();
    expect(result.remainingTokens).toBeNull();
  });

  it.each([-1, 0.5, Number.MAX_SAFE_INTEGER + 1, undefined, "10"])(
    "refuses an unreadable count rather than reporting zero: %j",
    (calls) => {
      const p = payload();
      expect(() =>
        parseOwnAIUsage({
          ...p,
          budget: { ...p.budget, used: { calls, tokens: 0 } },
        }),
      ).toThrow(HubError);
    },
  );

  it("refuses absent allowance metadata and cross-owner history", () => {
    const p = payload();
    expect(() =>
      parseOwnAIUsage({
        ...p,
        budget: { ...p.budget, remainingCalls: undefined },
      }),
    ).toThrow(HubError);
    p.usage = [{ owner: "bob", day: "2026-11-18" }];
    expect(() => parseOwnAIUsage(p)).toThrow(HubError);
  });

  it("records missing provider token counts without estimating them", () => {
    const p = payload();
    p.usage = [
      {
        owner: "alice",
        day: p.budget.day,
        budgetedCalls: 1,
        attempts: 1,
        callsWithTokens: 0,
        promptTokens: null,
        completionTokens: null,
      },
    ];
    expect(parseOwnAIUsage(p)).toMatchObject({
      missingTokenCounts: true,
      hasTokenCounts: false,
    });
  });

  it("uses only the same-origin self route and never supplies a credential or owner selector", async () => {
    const fetch = vi.fn(
      async () => new Response(JSON.stringify(payload()), { status: 200 }),
    );
    vi.stubGlobal("fetch", fetch);
    await fetchOwnAIUsage();
    expect(fetch).toHaveBeenCalledOnce();
    expect(fetch.mock.calls[0]).toEqual([
      "/v1/ai/usage?days=1",
      expect.objectContaining({
        method: "GET",
        credentials: "same-origin",
        cache: "no-store",
        headers: { accept: "application/json" },
      }),
    ]);
  });
});

describe("identity-scoped usage panel", () => {
  it("keeps repeated load actions to one in-flight read", async () => {
    let finish!: (value: ReturnType<typeof parseOwnAIUsage>) => void;
    const pending = new Promise<ReturnType<typeof parseOwnAIUsage>>(
      (resolve) => {
        finish = resolve;
      },
    );
    const read = vi.fn(() => pending);
    const panel = createAIUsagePanel({
      identityKey: () => "alice",
      owner: () => "alice",
      render: vi.fn(),
      escape,
      read,
    });
    const first = panel.load();
    const second = panel.load();
    expect(read).toHaveBeenCalledOnce();
    finish(parseOwnAIUsage(payload()));
    await Promise.all([first, second]);
  });
  it("reads the authenticated actor's budget in a delegated session", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () => new Response(JSON.stringify(payload()), { status: 200 }),
      ),
    );
    const state = {
      reviewOperatorActor: "alice",
      reviewOperatorName: "bob",
      reviewOperatorIdentities: ["alice", "bob"],
    };
    const settings = createSettings({ state, render: vi.fn() });
    settings.extendState();
    const actions: Record<string, () => Promise<void>> = {};
    settings.bind(actions);
    await actions.refreshAIUsage();
    const root = { innerHTML: "" };
    settings.renderers["adm01-user-prefs"](root);
    expect(root.innerHTML).toContain("Hub snapshot for account alice");
    expect(root.innerHTML).toContain("0 remaining of 10");
  });
  it("loads explicitly, exposes exhausted/unlimited states, and does not turn unknown tokens into zero", async () => {
    const p = payload();
    p.usage = [
      {
        owner: "alice",
        day: p.budget.day,
        budgetedCalls: 1,
        attempts: 1,
        callsWithTokens: 0,
        promptTokens: null,
        completionTokens: null,
      },
    ];
    const read = vi.fn(async () => parseOwnAIUsage(p));
    const panel = createAIUsagePanel({
      identityKey: () => "alice",
      owner: () => "alice",
      render: vi.fn(),
      escape,
      read,
    });
    expect(panel.markup()).toContain("Load AI usage");
    expect(read).not.toHaveBeenCalled();
    await panel.load();
    expect(panel.markup()).toContain("0 remaining of 10");
    expect(panel.markup()).toContain("Unlimited");
    expect(panel.markup()).toContain("Not reported");
    expect(panel.markup()).not.toContain("0 reported");
  });

  it("discards a delayed previous-identity response even after a new owner's read finishes", async () => {
    let owner = "alice";
    let release!: (value: ReturnType<typeof parseOwnAIUsage>) => void;
    const pending = new Promise<ReturnType<typeof parseOwnAIUsage>>(
      (resolve) => {
        release = resolve;
      },
    );
    const read = vi
      .fn()
      .mockReturnValueOnce(pending)
      .mockResolvedValueOnce(parseOwnAIUsage(payload("bob")));
    const panel = createAIUsagePanel({
      identityKey: () => owner,
      owner: () => owner,
      render: vi.fn(),
      escape,
      read,
    });
    const old = panel.load();
    owner = "bob";
    expect(panel.markup()).toContain("Load AI usage");
    await panel.load();
    release(parseOwnAIUsage(payload("alice")));
    await old;
    expect(panel.markup()).toContain("bob");
    expect(panel.markup()).not.toContain("alice");
  });

  it("does not display a wrong owner, hides server error text, and can retry", async () => {
    const read = vi
      .fn()
      .mockResolvedValueOnce(parseOwnAIUsage(payload("bob")))
      .mockRejectedValueOnce(new HubError(503, "private server diagnostic"))
      .mockResolvedValueOnce(parseOwnAIUsage(payload()));
    const panel = createAIUsagePanel({
      identityKey: () => "alice",
      owner: () => "alice",
      render: vi.fn(),
      escape,
      read,
    });
    await panel.load();
    expect(panel.markup()).not.toContain("bob");
    expect(panel.markup()).toContain("Retry AI usage");
    await panel.load();
    expect(panel.markup()).not.toContain("private server diagnostic");
    await panel.load();
    expect(panel.markup()).toContain("Refresh AI usage");
  });

  it("escapes the Hub's owner label", async () => {
    const owner = '<img src=x onerror="bad">';
    const panel = createAIUsagePanel({
      identityKey: () => owner,
      owner: () => owner,
      render: vi.fn(),
      escape,
      read: async () => parseOwnAIUsage(payload(owner)),
    });
    await panel.load();
    expect(panel.markup()).toContain("&lt;img");
    expect(panel.markup()).not.toContain("<img");
  });
});

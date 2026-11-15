// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  CHAIN_VERSION,
  checkChainLinks,
  filterEntries,
  genesisDigest,
  ledgerMarkup,
} from "../../console/operator/audit-ledger.js";

const GENESIS =
  "7afa15678bef67a658e82f9ad7ff54f9489f7bcf39d5d8a6f39ad93fc0f18ef3";

type Decision = "ALLOW" | "DENY";

function entry(
  sequence: number,
  previousDigest: string,
  decision: Decision = "ALLOW",
  extra = {},
) {
  return {
    sequence,
    id: `evt-${sequence}`,
    actor: `user-${sequence}`,
    action: "release.promote",
    decision,
    previousDigest,
    digest: `${String(sequence).padStart(2, "0")}`.repeat(32),
    createdAt: `2026-01-0${sequence}T00:00:00Z`,
    ...extra,
  };
}

function goodChain() {
  const e1 = entry(1, GENESIS);
  const e2 = entry(2, e1.digest, "DENY");
  const e3 = entry(3, e2.digest);
  return [e1, e2, e3];
}

describe("genesisDigest", () => {
  it("matches the known checkpoint", async () => {
    expect(CHAIN_VERSION).toBe("conduit.authorization-audit-chain/v1");
    expect(await genesisDigest()).toBe(GENESIS);
  });
});

describe("checkChainLinks", () => {
  it("passes a good chain", () => {
    expect(checkChainLinks(goodChain(), GENESIS)).toEqual({
      ok: true,
      checked: 3,
      breaks: [],
    });
  });

  it("reports a broken link", () => {
    const chain = goodChain();
    chain[2] = { ...chain[2], previousDigest: "ff".repeat(32) };
    const result = checkChainLinks(chain, GENESIS);
    expect(result.ok).toBe(false);
    expect(result.breaks).toHaveLength(1);
    expect(result.breaks[0].sequence).toBe(3);
    expect(result.breaks[0].reason).toMatch(/previous hash/i);
  });

  it("reports a sequence gap", () => {
    const chain = goodChain();
    chain[2] = { ...chain[2], sequence: 5 };
    const result = checkChainLinks(chain, GENESIS);
    expect(result.ok).toBe(false);
    expect(result.breaks[0]).toEqual({
      sequence: 5,
      reason: "Sequence jumps from 2 to 5.",
    });
  });

  it("reports a first entry that misses the genesis checkpoint", () => {
    const chain = goodChain();
    chain[0] = { ...chain[0], previousDigest: "00".repeat(32) };
    const result = checkChainLinks(chain, GENESIS);
    expect(result.ok).toBe(false);
    expect(result.breaks[0].sequence).toBe(1);
  });
});

describe("filterEntries", () => {
  const chain = goodChain();
  it("filters by decision", () => {
    expect(
      filterEntries(chain, { decision: "deny" }).map((e) => e.sequence),
    ).toEqual([2]);
    expect(filterEntries(chain, { decision: "allow" })).toHaveLength(2);
    expect(filterEntries(chain, { decision: "all" })).toHaveLength(3);
  });
  it("matches actor, action and id case-insensitively", () => {
    expect(filterEntries(chain, { query: "USER-3" })).toHaveLength(1);
    expect(filterEntries(chain, { query: "RELEASE.PROMOTE" })).toHaveLength(3);
    expect(filterEntries(chain, { query: "evt-2" })).toHaveLength(1);
    expect(filterEntries(chain, { query: "nothing" })).toHaveLength(0);
  });
});

describe("ledgerMarkup", () => {
  it("escapes values", () => {
    const chain = [entry(1, GENESIS, "ALLOW", { actor: "<script>x</script>" })];
    const html = ledgerMarkup(chain);
    expect(html).not.toContain("<script>");
    expect(html).toContain("&lt;script&gt;");
    const filtered = ledgerMarkup(chain, { query: '"><b>' });
    expect(filtered).toContain('value="&quot;&gt;&lt;b&gt;"');
  });

  it("renders newest first with shortened hashes", () => {
    const html = ledgerMarkup(goodChain(), {});
    expect(html.indexOf("user-3")).toBeLessThan(html.indexOf("user-1"));
    expect(html).toContain('class="so-table"');
    expect(html).toContain("kit-badge-err");
    expect(html).toContain(`title="${"03".repeat(32)}"`);
    expect(html).toContain(">030303030303<");
    expect(html).toContain("data-ledger-check");
    expect(html).toContain("projection consistency only");
    expect(html).toContain("independent checkpoints are separate");
  });

  it("marks the active decision chip", () => {
    const html = ledgerMarkup(goodChain(), { decision: "deny" });
    expect(html).toMatch(/kit-tab is-active" data-ledger-decision="deny"/);
    expect(html).not.toMatch(/kit-tab is-active" data-ledger-decision="all"/);
  });

  it("paginates", () => {
    const chain = [entry(1, GENESIS)];
    for (let i = 2; i <= 5; i += 1) chain.push(entry(i, chain[i - 2].digest));
    const first = ledgerMarkup(chain, { pageSize: 2, page: 0 });
    expect(first).toContain("Showing 1–2 of 5");
    expect(first).toContain("user-5");
    expect(first).not.toContain("user-3");
    expect(first).toMatch(/data-ledger-page="-1" disabled/);
    const last = ledgerMarkup(chain, { pageSize: 2, page: 2 });
    expect(last).toContain("Showing 5–5 of 5");
    expect(last).toMatch(/data-ledger-page="3" disabled/);
  });

  it("shows the empty state", () => {
    expect(ledgerMarkup(goodChain(), { query: "zzz" })).toContain(
      "No entry matches.",
    );
  });

  it("shows the ok and error results", () => {
    const chain = goodChain();
    const ok = ledgerMarkup(chain, { check: checkChainLinks(chain, GENESIS) });
    expect(ok).toContain("kit-bar-ok");
    expect(ok).toContain(
      "All 3 links match, from the genesis checkpoint to entry 3.",
    );

    const bad = [...chain];
    bad[1] = { ...bad[1], previousDigest: "ee".repeat(32) };
    const err = ledgerMarkup(bad, { check: checkChainLinks(bad, GENESIS) });
    expect(err).toContain("kit-bar-err");
    expect(err).toContain("Entry 2");
  });
});

describe("model exchange on the ledger", () => {
  it("shows prompt, response, skill hash and tool names when present", () => {
    const chain = [
      entry(1, GENESIS, "ALLOW", {
        context: {
          promptSha256: "abcd".repeat(16),
          responseSha256: "ef01".repeat(16),
          skillSha256: "2345".repeat(16),
          toolNames: ["inspect_image", "validate_manifests"],
        },
      }),
    ];
    const html = ledgerMarkup(chain);
    expect(html).toContain("Model exchange");
    expect(html).toContain("prompt");
    expect(html).toContain("response");
    expect(html).toContain("skill");
    expect(html).toContain("tools inspect_image, validate_manifests");
    expect(html).toContain('title="' + "abcd".repeat(16) + '"');
  });

  it("leaves the exchange column empty when those fields are absent", () => {
    const html = ledgerMarkup(goodChain(), {});
    expect(html).toContain("Model exchange");
    expect(html).toContain("—");
    expect(html).not.toContain("prompt ");
  });

  it("matches a search against an exchange hash", () => {
    const chain = [
      entry(1, GENESIS, "ALLOW", { context: { promptSha256: "c66fd1e564ff" } }),
      entry(2, "11".repeat(32)),
    ];
    expect(
      filterEntries(chain, { query: "c66fd1e564ff" }).map((e) => e.sequence),
    ).toEqual([1]);
  });
});

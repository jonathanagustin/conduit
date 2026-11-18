import { readFileSync } from "node:fs";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vite-plus/test";
import { parseGrounding } from "../../console/assistant-grounding";
import { AssistantSources } from "../../console/AssistantSources";
import {
  applyTurnDocument,
  newTurn,
  reduceTurn,
  messagesFromThread,
  toThreadMessage,
} from "../../console/assistant-turn";

const fixture = () =>
  JSON.parse(
    readFileSync(
      new URL(
        "../../../../internal/assistant/domain/testdata/grounding-v1.json",
        import.meta.url,
      ),
      "utf8",
    ),
  ) as Record<string, unknown>;

describe("retrieved assistant source evidence", () => {
  it("renders the native wire fixture as escaped excerpts with real source identities and receipts", () => {
    const evidence = parseGrounding(fixture());
    expect(evidence?.state).toBe("available");
    expect(evidence?.sources).toHaveLength(2);
    if (!evidence) throw new Error("missing source evidence");
    const html = renderToStaticMarkup(
      createElement(AssistantSources, { evidence }),
    );
    expect(html).toContain("Sources retrieved (2)");
    expect(html).toContain("fixture/operator-reference.md");
    expect(html).toContain("Native operator reference");
    expect(html).toContain("Authorized ontology snapshot");
    expect(html).toContain("&lt;script&gt;");
    expect(html).toContain("&lt;img src=x onerror=alert(1)&gt;");
    expect(html).not.toContain("<script>");
    expect(html).not.toContain("<img");
    expect(html).not.toContain("href=");
    expect(html).toContain("aud_fixture_doc");
    expect(html).toContain("Excerpt SHA-256");
    expect(html).toContain(evidence.sources[0]?.excerptSha256);
  });

  it("distinguishes old, empty, bounded and unavailable source metadata", () => {
    expect(parseGrounding(undefined)).toBeNull();
    expect(parseGrounding(null)).toBeNull();
    const empty = parseGrounding({
      schemaVersion: "conduit.assistant-grounding/v1",
      sources: [],
      truncated: false,
    });
    expect(empty).toEqual({
      state: "available",
      sources: [],
      truncated: false,
    });
    if (!empty) throw new Error("missing observed empty set");
    expect(
      renderToStaticMarkup(
        createElement(AssistantSources, { evidence: empty }),
      ),
    ).toContain("No source excerpts retrieved");
    const bounded = parseGrounding({ ...fixture(), truncated: true });
    if (!bounded) throw new Error("missing bounded sources");
    expect(
      renderToStaticMarkup(
        createElement(AssistantSources, { evidence: bounded }),
      ),
    ).toContain("Additional source excerpts are not shown");
    for (const change of [
      { kind: "external" },
      { valueTier: "APOLLO" },
      { auditEventId: "" },
      { retrievedAt: "invalid" },
      { endByte: -1 },
      { excerptSha256: "not-a-hash" },
      { quote: "x".repeat(513) },
    ]) {
      const raw = fixture();
      const sources = raw["sources"] as Record<string, unknown>[];
      raw["sources"] = [{ ...sources[0], ...change }];
      const unavailable = parseGrounding(raw);
      expect(unavailable?.state).toBe("unavailable");
      if (!unavailable) throw new Error("invalid metadata became absent");
      const html = renderToStaticMarkup(
        createElement(AssistantSources, { evidence: unavailable }),
      );
      expect(html).toContain("Source metadata unavailable");
      expect(html).not.toContain("No source excerpts retrieved");
    }
    expect(
      parseGrounding({ ...fixture(), schemaVersion: "future" })?.state,
    ).toBe("unavailable");
    const raw = fixture();
    raw["sources"] = Array(9).fill((raw["sources"] as unknown[])[0]);
    expect(parseGrounding(raw)?.state).toBe("unavailable");
  });

  it("preserves JSON, streamed failure and thread source metadata on their own messages", () => {
    const grounding = fixture();
    const turn = applyTurnDocument(newTurn(), { text: "answer", grounding });
    expect(turn.grounding?.sources).toHaveLength(2);
    const partial = reduceTurn(newTurn(), {
      type: "CUSTOM",
      name: "conduit.assistant.turn-result",
      value: { grounding },
    });
    const failed = reduceTurn(partial, {
      type: "RUN_ERROR",
      code: "ROUND_CAP",
      message: "stopped",
    });
    expect(failed.status).toBe("error");
    expect(failed.grounding).toEqual(turn.grounding);
    const messages = messagesFromThread({
      id: "ath_fixture",
      messages: [
        { role: "assistant", text: "answer", grounding },
        { role: "assistant", text: "old answer" },
      ],
    });
    const first = messages[0],
      old = messages[1];
    if (first?.role !== "assistant" || old?.role !== "assistant")
      throw new Error("missing stored answers");
    expect(toThreadMessage(first).metadata?.custom.conduit.grounding).toEqual(
      turn.grounding,
    );
    expect(old.turn.grounding).toBeNull();
    expect(
      applyTurnDocument(newTurn(), { text: "no metadata" }).grounding,
    ).toBeNull();
    expect(
      reduceTurn(newTurn(), {
        type: "CUSTOM",
        name: "untrusted-other-event",
        value: { grounding },
      }).grounding,
    ).toBeNull();
  });
});

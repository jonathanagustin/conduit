import { describe, expect, it } from "vite-plus/test";
import { createLiveOperations } from "../../console/operator/live-operations.js";
const event = (sequence: number, streamID = "s_one") => ({
  streamID,
  sequence,
  occurredAt: "2026-11-15T12:00:00Z",
  kind: "approval.changed",
  resource: { type: "plan", id: "pln_one" },
  data: { status: "APPROVED" },
});
const status = { state: "live", message: "Live updates connected." };
describe("session operations feed", () => {
  it("retains only the latest 100 in delivery order, ignores replay duplicates, and clears across epochs", () => {
    const feed = createLiveOperations();
    feed.accept(Array.from({ length: 105 }, (_, index) => event(index + 1)));
    feed.accept([event(105), event(2)]);
    const markup = feed.markup(status);
    expect(markup.match(/data-operation-sequence=/g)).toHaveLength(100);
    expect(markup).toContain("5 earlier session updates omitted");
    expect(markup.indexOf('data-operation-sequence="105"')).toBeLessThan(
      markup.indexOf('data-operation-sequence="6"'),
    );
    expect(markup).not.toContain('data-operation-sequence="5"');
    feed.accept([event(1, "s_two")]);
    expect(feed.markup(status)).not.toContain('data-operation-sequence="105"');
    feed.reset();
    expect(feed.markup(status)).not.toContain("data-operation-sequence");
  });
  it("escapes text and never displays opaque cursors or makes actions", () => {
    const feed = createLiveOperations();
    feed.accept([
      {
        ...event(1),
        cursor: "SECRET_CURSOR",
        resource: { type: "plan", id: '<img src=x onerror="bad()">' },
        data: { status: "<script>bad()</script>" },
      },
    ]);
    const markup = feed.markup({
      state: "offline",
      message: "Live updates unavailable; using polling.",
    });
    expect(markup).toContain("&lt;img");
    expect(markup).not.toContain("<img");
    expect(markup).not.toContain("<script");
    expect(markup).not.toContain("SECRET_CURSOR");
    expect(markup).not.toMatch(/<button|<form|href=/);
    expect(markup).toContain("not a complete event history");
    expect(markup).toContain('data-operations-state="offline"');
  });
});

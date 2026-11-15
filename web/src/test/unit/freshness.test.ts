// @vitest-environment node
import { describe, expect, it } from "vite-plus/test";

import {
  freshness,
  freshnessPill,
  reportTime,
} from "../../console/operator/freshness.js";

const now = Date.parse("2026-11-14T20:00:00Z");

describe("report freshness", () => {
  it("skips unset and zero times", () => {
    expect(reportTime("", "0001-01-01T00:00:00Z", "2026-11-14T19:00:00Z")).toBe(
      "2026-11-14T19:00:00Z",
    );
    expect(reportTime(undefined, "0001-01-01T00:00:00Z")).toBe("");
  });

  it("buckets the age", () => {
    expect(freshness("", now)).toEqual({ state: "none", label: "No report" });
    expect(freshness("2026-11-14T19:59:40Z", now).label).toBe("just now");
    expect(freshness("2026-11-14T19:55:00Z", now)).toEqual({
      state: "fresh",
      label: "5m ago",
    });
    expect(freshness("2026-11-14T17:00:00Z", now)).toEqual({
      state: "recent",
      label: "3h ago",
    });
    expect(freshness("2026-11-12T20:00:00Z", now)).toEqual({
      state: "stale",
      label: "Stale · 2d ago",
    });
    expect(freshness("not a time", now).state).toBe("unknown");
  });

  it("follows the Hub's own stale verdict", () => {
    expect(freshness("2026-11-14T19:55:00Z", now, true)).toEqual({
      state: "stale",
      label: "Stale · 5m ago",
    });
    expect(freshness("0001-01-01T00:00:00Z", now, true).state).toBe("none");
  });

  it("puts the exact time in the title and escapes it", () => {
    const html = freshnessPill("2026-11-14T19:55:00Z", { now });
    expect(html).toContain('title="2026-11-14T19:55:00Z"');
    expect(html).toContain("kit-fresh-fresh");
    expect(freshnessPill("<x>", { now })).toContain("&lt;x&gt;");
  });
});

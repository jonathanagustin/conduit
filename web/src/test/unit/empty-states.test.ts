// @vitest-environment node
import { describe, expect, it } from "vite-plus/test";

import {
  EMPTY,
  emptyState,
  hubSetupMarkup,
} from "../../console/operator/empty-states.js";

describe("empty states", () => {
  it("keeps the Hub's message and adds a title and guidance", () => {
    const html = emptyState("recall", "No recall is recorded.");
    expect(html).toContain('data-state="empty"');
    expect(html).toContain("No recall is recorded.");
    expect(html).toContain(EMPTY.recall.title);
    expect(html).toContain("switchScreen('rm-waves')");
    expect(html).toContain("refreshHub()");
  });

  it("escapes the message", () => {
    expect(emptyState("audit", "<b>x</b>")).toContain("&lt;b&gt;x&lt;/b&gt;");
  });

  it("falls back for an unknown view", () => {
    expect(emptyState("", "Nothing.")).toContain("Nothing recorded yet");
  });
});

describe("hub setup checklist", () => {
  const bare = { products: [], environments: [], packaging: [], changes: [] };

  it("shows on a Hub with nothing in it", () => {
    const html = hubSetupMarkup(bare);
    expect(html).toContain("data-hub-setup");
    expect(html).toContain("0 of 4 done");
  });

  it("counts a step done from the Hub's own records", () => {
    expect(hubSetupMarkup({ ...bare, changes: [{}] })).toContain("1 of 4 done");
  });

  it("goes away once anything is published, enrolled or packaged", () => {
    expect(hubSetupMarkup({ ...bare, environments: [{}] })).toBe("");
    expect(hubSetupMarkup({ ...bare, products: [{}] })).toBe("");
  });

  it("does not treat an unreachable Hub as empty", () => {
    expect(hubSetupMarkup(undefined)).toBe("");
    expect(
      hubSetupMarkup({ ...bare, note: "Hub did not return environments." }),
    ).toBe("");
  });
});

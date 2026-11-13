import { describe, expect, it } from "vitest";

import {
  conversationHits,
  conversationMarkdown,
  loadSettings,
  saveSettings,
  threadRows,
  type KeyValueStore,
} from "../../embed/console-prefs.js";

function memoryStore(): KeyValueStore & { values: Map<string, string> } {
  const values = new Map<string, string>();
  return {
    values,
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => {
      values.set(key, value);
    },
  };
}

describe("console settings", () => {
  it("round-trips the fields the settings panel stores", () => {
    const store = memoryStore();
    saveSettings(store, {
      model: "codex-fast",
      effort: "high",
      systemPrompt: "Stay inside the workspace.",
      temperature: 0.4,
    });
    expect(loadSettings(store)).toEqual({
      model: "codex-fast",
      effort: "high",
      systemPrompt: "Stay inside the workspace.",
      temperature: 0.4,
    });
  });

  it("drops a malformed settings document", () => {
    const store = memoryStore();
    store.setItem("conduit-console-settings", "{");
    expect(loadSettings(store).model).toBe("");
  });

  it("groups threads from the live list and searches their titles", () => {
    const now = new Date();
    const older = new Date(now);
    older.setDate(older.getDate() - 10);
    const rows = threadRows(
      [
        { id: "today", title: "Packaging blockers", lastMessageAt: now },
        { id: "older", title: "Spoke boundary", lastMessageAt: older },
      ],
      now,
    );
    expect(rows.find((row) => row.id === "today")?.group).toBe("Today");
    expect(rows.find((row) => row.id === "older")?.group).toBe("Earlier");
    expect(
      rows.filter((row) => row.title.toLowerCase().includes("spoke")),
    ).toHaveLength(1);
  });

  it("finds a match inside the open conversation", () => {
    const hits = conversationHits(
      [{ id: "m1", text: "Keep the Release coordinate immutable." }],
      "release",
    );
    expect(hits).toHaveLength(1);
    expect(hits[0]?.match.toLowerCase()).toBe("release");
  });

  it("exports the open turns as markdown", () => {
    const markdown = conversationMarkdown("Packaging", [
      { role: "user", text: "List the blockers." },
      { role: "assistant", text: "None verified." },
    ]);
    expect(markdown).toContain("# Packaging");
    expect(markdown).toContain("**user**");
    expect(markdown).toContain("List the blockers.");
  });
});

import { afterEach, describe, expect, it } from "vite-plus/test";

import {
  TIMEZONE_KEY,
  applyTimezone,
  createSettingsDialog,
  DENSITY_KEY,
  formatConsoleTimestamp,
  settingsMarkup,
  storedDensity,
  storedTheme,
  storedTimezone,
  THEME_KEY,
} from "../../console/operator/settings-dialog.js";

const store = new Map<string, string>();
(globalThis as { localStorage?: unknown }).localStorage = {
  getItem: (key: string) => (store.has(key) ? store.get(key) : null),
  setItem: (key: string, value: string) => store.set(key, String(value)),
};

afterEach(() => store.clear());

describe("settings dialog", () => {
  it("falls back to dark and standard for unknown stored values", () => {
    store.set(THEME_KEY, "neon");
    store.set(DENSITY_KEY, "tiny");
    expect(storedTheme()).toBe("dark");
    expect(storedDensity()).toBe("standard");
  });

  it("marks the current choices and shows identity read-only", () => {
    const html = settingsMarkup({
      theme: "light",
      density: "compact",
      dockWidth: "400",
      operatorName: "<ops>",
    });
    expect(html).toContain('role="dialog"');
    expect(html).toMatch(
      /aria-checked="true"[^>]*data-setting="theme" data-value="light"/,
    );
    expect(html).toMatch(
      /aria-checked="true"[^>]*data-setting="density" data-value="compact"/,
    );
    expect(html).toContain("&lt;ops&gt;");
    expect(html).not.toMatch(/data-operator|switch operator/i);
  });

  it("toggles on Ctrl+, and closes on Escape", () => {
    const dialog = createSettingsDialog({
      getRoot: () => null,
      getOperatorName: () => "ops",
    });
    const key = (init: Partial<KeyboardEvent>) =>
      ({
        preventDefault() {},
        metaKey: false,
        ctrlKey: false,
        ...init,
      }) as KeyboardEvent;
    expect(dialog.onKey(key({ ctrlKey: true, key: "," }))).toBe(true);
    expect(dialog.isOpen()).toBe(true);
    expect(dialog.onKey(key({ key: "Escape" }))).toBe(true);
    expect(dialog.isOpen()).toBe(false);
    expect(dialog.onKey(key({ key: "Escape" }))).toBe(false);
  });

  it("handles timezone stored preferences, applyTimezone, and formatConsoleTimestamp", () => {
    store.set(TIMEZONE_KEY, "invalid");
    expect(storedTimezone()).toBe("utc");
    store.set(TIMEZONE_KEY, "local");
    expect(storedTimezone()).toBe("local");

    const mockRoot = {
      attrs: {} as Record<string, string>,
      setAttribute(k: string, v: string) {
        this.attrs[k] = v;
      },
    };
    applyTimezone(mockRoot as any, "local");
    expect(mockRoot.attrs["data-timezone"]).toBe("local");

    // Invalid timestamp returns empty string
    expect(formatConsoleTimestamp("")).toBe("");
    expect(formatConsoleTimestamp(null)).toBe("");
    expect(formatConsoleTimestamp("not-a-date")).toBe("");

    // Known UTC date
    const ts = "2026-11-15T20:34:20Z";
    expect(formatConsoleTimestamp(ts, "utc")).toBe("2026-11-15 20:34:20 UTC");

    // Default timezone uses stored preference
    store.set(TIMEZONE_KEY, "utc");
    expect(formatConsoleTimestamp(ts)).toBe("2026-11-15 20:34:20 UTC");

    // Local timestamp ends in Local
    const localFormatted = formatConsoleTimestamp(ts, "local");
    expect(localFormatted).toMatch(
      /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} Local$/,
    );

    // Settings markup includes timestamps section
    const html = settingsMarkup({
      theme: "dark",
      density: "standard",
      timezone: "utc",
      dockWidth: "368",
      operatorName: "ops",
    });
    expect(html).toContain('role="radiogroup" aria-label="Timestamps"');
    expect(html).toMatch(
      /aria-checked="true"[^>]*data-setting="timezone" data-value="utc"/,
    );
  });
});

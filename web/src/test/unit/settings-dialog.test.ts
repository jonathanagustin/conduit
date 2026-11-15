import { afterEach, describe, expect, it } from "vite-plus/test";

import {
  createSettingsDialog,
  DENSITY_KEY,
  settingsMarkup,
  storedDensity,
  storedTheme,
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
});

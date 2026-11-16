import { describe, expect, it } from "vite-plus/test";
import { createEngineerFocus } from "../../console/operator/engineer-workspace.js";
import { assistantChrome } from "../../console/operator/assistant-rail.js";

function fixture() {
  const element = (inert = false) => ({
    inert,
    attributes: new Map<string, string>(),
    setAttribute(name: string, value: string) {
      this.attributes.set(name, value);
    },
    removeAttribute(name: string) {
      this.attributes.delete(name);
    },
  });
  const nav = element(true),
    workspace = element();
  const root = {
    dataset: {} as Record<string, string>,
    querySelector(selector: string) {
      return selector === "#left-nav"
        ? nav
        : selector === "#main-workspace"
          ? { parentElement: workspace }
          : null;
    },
  };
  return { root, nav, workspace, focus: createEngineerFocus(root) };
}

describe("engineer conversation focus", () => {
  it("makes only the background inert and restores its prior state", () => {
    const { root, nav, workspace, focus } = fixture();
    focus.set(true);
    expect(root.dataset.engineerFocus).toBe("true");
    expect(nav.inert).toBe(true);
    expect(workspace.inert).toBe(true);
    focus.set(true);
    focus.set(false);
    expect(nav.inert).toBe(true);
    expect(workspace.inert).toBe(false);
    expect(nav.attributes.has("data-engineer-background")).toBe(false);
    expect(workspace.attributes.has("data-engineer-background")).toBe(false);
  });
  it("cleans up focus without retaining hidden background state", () => {
    const { root, workspace, focus } = fixture();
    focus.set(true);
    focus.dispose();
    expect(root.dataset.engineerFocus).toBeUndefined();
    expect(workspace.inert).toBe(false);
  });
  it("offers focus and native Studio navigation without a second composer", () => {
    expect(assistantChrome({})).toContain(
      'aria-label="Open engineer workspace"',
    );
    const focused = assistantChrome({ engineerFocus: true });
    expect(focused).toContain('aria-label="Return to workbench"');
    expect(focused).toContain('aria-label="Workspace studio"');
    expect(focused).toContain("s09-source");
    expect(focused).toContain("s10-diff");
    expect(focused).toContain("s11-preview");
    expect(focused).not.toContain("textarea");
  });
});

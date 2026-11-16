import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { createSettings } from "../../console/operator/settings.js";

function harness() {
  const state: any = {};
  const render = vi.fn();
  const settings = createSettings({ state, render });
  settings.extendState();
  const actions: any = {};
  settings.bind(actions);
  const root = { innerHTML: "" };
  const paint = () => {
    settings.renderers["adm01-user-prefs"](root);
    return root.innerHTML;
  };
  return { state, actions, paint, render };
}

afterEach(() => vi.unstubAllGlobals());

describe("explicit personal-token examples", () => {
  it("defaults to unavailable without sample credentials or simulated mutation controls", () => {
    const h = harness();
    expect(h.paint()).toContain("This Hub does not issue personal tokens.");
    expect(h.paint()).toContain("Show token example data");
    expect(h.paint()).toContain("Unavailable");
    expect(h.paint()).not.toContain("Workstation");
    expect(h.paint()).not.toContain("Create example token");
    expect(h.paint()).not.toContain("Revoke example");
    expect(h.state.prefTokens).toEqual([]);
  });

  it("requires explicit opt-in, labels the simulation and never calls the Hub", () => {
    const fetch = vi.fn(() => {
      throw new Error("unexpected credential request");
    });
    vi.stubGlobal("fetch", fetch);
    const h = harness();
    h.actions.setTokenExampleMode(true);
    expect(h.paint()).toContain(
      "Example data. Changes here stay in this browser view",
    );
    expect(h.paint()).toContain("Workstation");
    expect(h.paint()).toContain("Exit token example mode");
    h.state.prefTokenDraft.name = '<img src=x onerror="alert(1)">';
    h.actions.createExampleToken();
    expect(h.paint()).toContain("&lt;img");
    expect(h.paint()).not.toContain("<img");
    h.actions.revokeExampleToken(h.state.prefTokens[0].id);
    expect(h.state.prefTokens[0].status).toBe("Revoked");
    expect(fetch).not.toHaveBeenCalled();
  });

  it("discards edits on exit and reentry; a new console starts opted out", () => {
    const h = harness();
    h.actions.setTokenExampleMode(true);
    h.state.prefTokenDraft.name = "discard me";
    h.actions.createExampleToken();
    h.state.prefTokenDraft.name = "unsaved draft";
    h.actions.setTokenExampleMode(false);
    expect(h.state.prefTokens).toEqual([]);
    expect(h.state.prefTokenDraft.name).toBe("");
    expect(h.paint()).not.toContain("discard me");
    h.actions.setTokenExampleMode(true);
    expect(h.state.prefTokens).toHaveLength(1);
    expect(h.paint()).not.toContain("discard me");
    expect(harness().paint()).not.toContain("Workstation");
  });

  it.each([false, "true", 1, null, undefined])(
    "rejects non-boolean opt-in %s",
    (enabled) => {
      const h = harness();
      h.actions.setTokenExampleMode(enabled);
      expect(h.state.prefTokenExamples).toBe(false);
      expect(h.paint()).not.toContain("Workstation");
    },
  );

  it("refuses direct simulated create/revoke handlers while opted out", () => {
    const h = harness();
    h.state.prefTokenDraft.name = "hidden";
    h.actions.createExampleToken();
    expect(h.state.prefTokens).toEqual([]);
    h.state.prefTokens = [{ id: "old", status: "Active" }];
    h.actions.revokeExampleToken("old");
    expect(h.state.prefTokens[0].status).toBe("Active");
    expect(h.render).not.toHaveBeenCalled();
  });
});

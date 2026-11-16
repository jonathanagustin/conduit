import { describe, expect, it, vi } from "vite-plus/test";
import { createOperationalExamples } from "../../console/operator/operational-examples.js";

const prototypes = [
  "s03-saved-views",
  "s04-scope-plan",
  "s05-backlog",
  "s06-active-work",
  "s13-fleet-overview",
  "rm01-env-page",
  "rm02-env-activity",
];
function harness(screen = "s13-fleet-overview") {
  let current = screen;
  let actor = "alice";
  const events: Record<string, () => void> = {};
  const body = { innerHTML: "" };
  const root: any = {
    innerHTML: "",
    querySelector: (selector: string) =>
      selector === "[data-example-body]"
        ? body
        : {
            addEventListener: (_name: string, callback: () => void) => {
              events[selector] = callback;
            },
          },
  };
  const render = vi.fn();
  const mode = createOperationalExamples({
    currentScreen: () => current,
    identity: () => actor,
    render,
  });
  const renderer = vi.fn((node: any) => {
    node.innerHTML = "sample operations";
  });
  return {
    root,
    body,
    events,
    mode,
    renderer,
    render,
    move: (next: string) => {
      current = next;
      mode.sync(next);
    },
    identity: (next: string) => {
      actor = next;
    },
    paint: () => mode.paint(root, current, renderer),
  };
}

describe("operational example dispatch", () => {
  it.each(prototypes)(
    "never renders %s fixtures until explicit opt-in",
    (screen) => {
      const h = harness(screen);
      expect(h.paint()).toBe(true);
      expect(h.renderer).not.toHaveBeenCalled();
      expect(h.root.innerHTML).toContain("Show example data");
      h.events["[data-example-enable]"]();
      h.paint();
      expect(h.renderer).toHaveBeenCalledWith(h.body);
      expect(h.body.innerHTML).toBe("sample operations");
      expect(h.root.innerHTML).toContain("Exit example mode");
      expect(h.root.innerHTML).toContain("Example data");
      h.events["[data-example-exit]"]();
      h.renderer.mockClear();
      h.paint();
      expect(h.renderer).not.toHaveBeenCalled();
    },
  );
  it.each([
    "rm03-plan-detail",
    "s16-approvals-board",
    "rel-dependencies",
    "rm09-fleet-scheduler",
    "adm01-user-prefs",
    "s19-ontology",
    "s09-source",
    "rm11-debug-terminal",
  ])("leaves native %s to its existing renderer", (screen) => {
    const h = harness(screen);
    expect(h.paint()).toBe(false);
    expect(h.root.innerHTML).toBe("");
    expect(h.events).toEqual({});
  });
  it("requires another explicit choice after identity changes, navigation and reload", () => {
    const h = harness();
    h.paint();
    h.events["[data-example-enable]"]();
    expect(h.mode.enabled("s13-fleet-overview")).toBe(true);
    h.identity("delegate");
    h.renderer.mockClear();
    h.paint();
    expect(h.renderer).not.toHaveBeenCalled();
    h.events["[data-example-enable]"]();
    h.move("s05-backlog");
    h.paint();
    h.move("s13-fleet-overview");
    expect(h.mode.enabled("s13-fleet-overview")).toBe(false);
    expect(harness().mode.enabled("s13-fleet-overview")).toBe(false);
  });
  it("does not allow a detached old view to opt in after navigation", () => {
    const h = harness();
    h.paint();
    const enable = h.events["[data-example-enable]"];
    h.move("s05-backlog");
    enable();
    expect(h.mode.enabled("s13-fleet-overview")).toBe(false);
    expect(h.render).not.toHaveBeenCalled();
  });
});

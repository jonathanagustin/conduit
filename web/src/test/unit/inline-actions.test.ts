// @vitest-environment node
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it, vi } from "vite-plus/test";

import {
  parseAction,
  runAction,
} from "../../console/operator/inline-actions.js";

function run(
  source: string,
  globals: Record<string, unknown>,
  self: unknown = {},
  event: unknown = {},
) {
  return runAction(parseAction(source), { self, event, globals, document: {} });
}

describe("inline actions", () => {
  it("calls window functions with literal and element arguments", () => {
    const switchScreen = vi.fn();
    const search = vi.fn();
    run("switchScreen('rm-channels')", { switchScreen });
    run(
      "handleGlobalSearch(this.value)",
      { handleGlobalSearch: search },
      { value: "plan" },
    );
    expect(switchScreen).toHaveBeenCalledWith("rm-channels");
    expect(search).toHaveBeenCalledWith("plan");
  });

  it("assigns into state and runs statements in order", () => {
    const state: Record<string, unknown> = { follow: false, tab: "a" };
    const render = vi.fn(() => expect(state.tab).toBe("b"));
    run("state.follow=!state.follow; state['tab']='b'; renderMainWorkspace()", {
      state,
      renderMainWorkspace: render,
    });
    expect(state.follow).toBe(true);
    expect(render).toHaveBeenCalledOnce();
  });

  it("handles ternaries, comparisons, and if / else chains", () => {
    const state: Record<string, unknown> = { filter: "W1" };
    run("state.filter=(state.filter==='W1'?'ALL':'W1')", { state });
    expect(state.filter).toBe("ALL");
    const go = vi.fn();
    run(
      "if('config'==='activity'){go('a');}else if('config'==='config'){go('c');}else{go('z');}",
      { go },
    );
    expect(go).toHaveBeenCalledWith("c");
  });

  it("uses event members and checks the event target", () => {
    const stop = vi.fn();
    const close = vi.fn();
    const self = {};
    run("event.stopPropagation(); close()", { close }, self, {
      stopPropagation: stop,
    });
    expect(stop).toHaveBeenCalledOnce();
    run("if(event.target===this){close();}", { close }, self, { target: {} });
    expect(close).toHaveBeenCalledOnce();
  });

  it("refuses prototype keys and anything that is not the language", () => {
    expect(() => run("state.constructor", { state: {} })).toThrow();
    expect(() => run("state['__proto__'].x=1", { state: {} })).toThrow();
    expect(() => parseAction("items.find(x=>x.id)")).toThrow(SyntaxError);
    expect(() => parseAction("a + b")).toThrow(SyntaxError);
  });
});

describe("operator views", () => {
  it("write no inline event handlers, which the Hub's CSP blocks", () => {
    const dir = join(import.meta.dirname, "../../console/operator");
    const offenders: string[] = [];
    for (const file of readdirSync(dir).filter((name) =>
      name.endsWith(".js"),
    )) {
      const text = readFileSync(join(dir, file), "utf8");
      for (const match of text.matchAll(/(^|[\s"'`>])on[a-z]{3,12}="/gm)) {
        offenders.push(`${file}: ${match[0].trim()}`);
      }
    }
    expect(offenders).toEqual([]);
  });
});

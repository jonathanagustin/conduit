import { readFileSync } from "node:fs";
import { describe, expect, it } from "vite-plus/test";

import {
  SCREEN_REGISTRY,
  classifyRenderers,
  screenBadge,
  truncationNotices,
  unclassifiedScreens,
} from "../../console/operator/screen-registry.js";

const operator = new URL("../../console/operator/", import.meta.url);
const source = (name: string) => readFileSync(new URL(name, operator), "utf8");

/** The body of every object literal that follows `opener` in `text`. */
function literals(text: string, opener: string): string[] {
  const bodies: string[] = [];
  let from = 0;
  for (;;) {
    const start = text.indexOf(opener, from);
    if (start < 0) return bodies;
    let depth = 0;
    let index = start + opener.length - 1;
    for (; index < text.length; index += 1) {
      if (text[index] === "{") depth += 1;
      if (text[index] === "}" && --depth === 0) break;
    }
    bodies.push(text.slice(start + opener.length, index));
    from = index;
  }
}

/** Property keys written at the top level of a renderer table. */
function tableKeys(body: string): string[] {
  const keys: string[] = [];
  let depth = 0;
  for (const line of body.split("\n")) {
    const key = depth === 0 ? /^\s*"([a-z0-9-]+)":/.exec(line) : null;
    if (key) keys.push(key[1]!);
    for (const character of line) {
      if ("{([".includes(character)) depth += 1;
      if ("})]".includes(character)) depth -= 1;
    }
  }
  return keys;
}

function engineRendererKeys(): string[] {
  const engine = source("engine.js");
  const tables = literals(engine, "classifyRenderers({");
  // The screen table and the area-tab table. A third table must be routed
  // through classifyRenderers as well, and then this count is updated.
  expect(tables).toHaveLength(2);
  const spread = ["release-gates.js", "ontology-graph.js", "settings.js"];
  for (const name of spread) expect(engine.includes(`./${name}`)).toBe(true);
  expect(
    tables[0]!.match(/\.\.\.\w+\.renderers/g)?.map((item) => item.slice(3)),
  ).toEqual([
    "releaseGates.renderers",
    "ontologyGraph.renderers",
    "settings.renderers",
  ]);
  return [
    ...tables.flatMap(tableKeys),
    ...spread.flatMap((name) =>
      literals(source(name), "renderers: {").flatMap(tableKeys),
    ),
  ];
}

describe("screen data classification", () => {
  it("classifies every renderer key the engine can paint", () => {
    const keys = engineRendererKeys();
    expect(keys.length).toBeGreaterThanOrEqual(79);
    expect(new Set(keys).size).toBeGreaterThan(45);
    expect(unclassifiedScreens(keys)).toEqual([]);
  });

  it("has no classification for a screen without a renderer", () => {
    const keys = new Set(engineRendererKeys());
    expect(
      Object.keys(SCREEN_REGISTRY).filter((screen) => !keys.has(screen)),
    ).toEqual([]);
  });

  it("refuses a renderer table with an unclassified key", () => {
    const table = {
      "s16-approvals-board": () => {},
      "zz-new-screen": () => {},
    };
    expect(unclassifiedScreens(Object.keys(table))).toEqual(["zz-new-screen"]);
    expect(() => classifyRenderers(table)).toThrow(/zz-new-screen/);
    expect(
      classifyRenderers({
        "s16-approvals-board": table["s16-approvals-board"],
      }),
    ).toBeTruthy();
  });

  it.each([
    "rm03-plan-detail",
    "s15-plan-detail",
    "s16-approvals-board",
    "rm04-constraints",
    "rm05-release-rollout",
    "rel-dependencies",
    "rm09-fleet-scheduler",
    "s10-diff",
  ])("labels the Hub-backed view %s live", (screen) => {
    expect(screenBadge(screen)).toMatchObject({ mode: "live", text: "LIVE" });
  });

  it("keeps prototypes, self-labelled and evidence-gated views honest", () => {
    expect(screenBadge("s13-fleet-overview").text).toBe("EXAMPLE DATA");
    expect(screenBadge("s13-fleet-overview", { unavailable: true }).text).toBe(
      "UNAVAILABLE",
    );
    expect(screenBadge("adm02-org-control-panel").hidden).toBe(true);
    expect(screenBadge("pt01-proposal-trace").mode).toBe("example");
    expect(
      screenBadge("pt01-proposal-trace", {
        hub: { packaging: [{ runId: "run-1" }] },
        unavailable: true,
      }).mode,
    ).toBe("live");
    expect(screenBadge("og01-ontology-graph", { graphLive: true }).mode).toBe(
      "live",
    );
    expect(screenBadge("not-a-screen").mode).toBe("example");
    expect(screenBadge("s16-approvals-board", { failed: true })).toMatchObject({
      mode: "unavailable",
      title: "This view failed to render",
    });
  });

  it("states a truncated index without inventing a total", () => {
    const hub = {
      truncated: {
        plans: { shown: 100, total: 240 },
        changes: { shown: 500, total: null },
      },
    };
    expect(truncationNotices(hub, "s16-approvals-board")).toEqual([
      "Showing the first 100 of 240 plans. The rest are not loaded in this view.",
    ]);
    expect(truncationNotices(hub, "rm07-change-requests")).toEqual([
      "Showing the first 500 change requests; the Hub has more that are not loaded in this view.",
    ]);
    expect(truncationNotices(hub, "rm05-release-rollout")).toEqual([]);
    expect(truncationNotices({}, "s16-approvals-board")).toEqual([]);
  });
});

import { areaObject } from "../../console/operator/kit.js";
import { describe, expect, it } from "vite-plus/test";

import {
  connectivityFromSpoke,
  diffRowsFromChanges,
  lineageFromHub,
  movePuck,
  sitesFromEnvironments,
  viewState,
  wavesFromRecords,
} from "../../console/operator/kit.js";

describe("v3 kit records", () => {
  it("ignores a promotion that has no wave window", () => {
    expect(
      wavesFromRecords([
        { id: "promo-1", fromChannel: "DEV", toChannel: "STAGING" },
      ]),
    ).toEqual({ pucks: [], dependencies: [] });
  });

  it("keeps a recorded wave window and clamps a drag", () => {
    const waves = wavesFromRecords([
      {
        id: "wave-1",
        lane: "dev",
        label: "sample",
        startHourUtc: 2,
        durationHours: 2,
        windowStart: 0,
        windowEnd: 8,
        dependsOn: "wave-0",
      },
    ]);
    expect(waves.pucks[0].startHourUtc).toBe(2);
    expect(waves.dependencies).toEqual([["wave-0", "wave-1"]]);
    const moved = movePuck(waves.pucks, "wave-1", 20);
    expect(moved[0].startHourUtc).toBe(6);
  });

  it("does not invent connectivity for a blank spoke", () => {
    expect(connectivityFromSpoke("")).toBe("");
    expect(connectivityFromSpoke("offline")).toBe("Disconnected");
    const sites = sitesFromEnvironments([
      { environment: { id: "dev" }, spoke: { state: "" } },
    ]);
    expect(sites[0].connectivity).toBe("");
    expect(sites[0].health).toBe("not reported");
  });

  it("links only recorded channels", () => {
    const graph = lineageFromHub({
      promotions: [
        { fromChannel: "DEV", toChannel: "STAGING", status: "APPROVED" },
      ],
      packaging: [{ runId: "sample-app-1", phase: "review" }],
    });
    expect(graph.edges).toEqual([["channel:DEV", "channel:STAGING"]]);
    expect(graph.nodes.map((node) => node.id)).toContain("run:sample-app-1");
  });

  it("reads a stored field diff and leaves a missing diff empty", () => {
    expect(
      diffRowsFromChanges([
        {
          environment: "dev",
          author: { username: "local-developer" },
          changes: [{ diff: { memory: { before: "256Mi", after: "512Mi" } } }],
        },
      ]),
    ).toMatchObject([
      {
        path: "memory",
        before: "256Mi",
        after: "512Mi",
        actor: "local-developer",
      },
    ]);
    expect(diffRowsFromChanges([{ id: "crq", changes: [{}] }])).toEqual([]);
  });

  it("states an empty view without a fabricated cause", () => {
    const html = viewState({
      state: "empty",
      title: "Waves",
      detail: "No staged wave is recorded.",
    });
    expect(html).toContain("No staged wave is recorded.");
    expect(html).not.toContain("quorum");
  });
});

describe("packaging area selection", () => {
  const hub = {
    packaging: [
      { runId: "first", phase: "succeeded", outcome: "PASS" },
      { runId: "selected", phase: "review", outcome: "PASS" },
    ],
  };
  it("binds the shared area object to the selected visible run", () => {
    expect(
      areaObject(hub, "area-packaging", { packagingRunId: "selected" }),
    ).toMatchObject({ name: "selected", status: "review", tone: "warn" });
    expect(
      areaObject(hub, "area-packaging", { packagingRunId: "hidden" }),
    ).toBeNull();
    expect(areaObject(hub, "area-packaging")).toMatchObject({ name: "first" });
    expect(
      areaObject(
        {
          packaging: [
            {
              runId: "off",
              configured: false,
              phase: "succeeded",
              outcome: "PASS",
            },
          ],
        },
        "area-packaging",
      ),
    ).toMatchObject({ status: "blocked", tone: "warn" });
  });
});

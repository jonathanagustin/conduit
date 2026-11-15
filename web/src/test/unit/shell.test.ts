import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import {
  DOCK_DEFAULT,
  DOCK_MAX,
  DOCK_MIN,
  DOCK_SURFACES,
  allLive,
  clampDockWidth,
  dockSurfaceCounts,
  kpisFromHub,
  readPref,
  siteSummary,
  writePref,
} from "../../console/operator/shell.js";

const installed = (environment: string, version = "1", observed = version) => ({
  environment,
  desiredVersion: version,
  observedVersion: observed,
  desiredReleaseDigest: "sha256:a",
  observedReleaseDigest: "sha256:a",
  desiredConfigDigest: "sha256:b",
  observedConfigDigest: "sha256:b",
  observedAt: "2026-11-15T12:00:00Z",
  stale: false,
  lifecycle: "INSTALLED",
  configStatus: "APPLIED",
  rolloutStatus: "UP_TO_DATE",
  health: "HEALTHY",
});
const full = {
  environments: [
    {
      environment: { id: "e1" },
      reported: { agentId: "a1" },
      spoke: { state: "live", agentId: "a1" },
    },
    {
      environment: { id: "e2" },
      reported: { agentId: "a1" },
      spoke: { state: "live", agentId: "a1" },
    },
    {
      environment: { id: "e3", maintenance: true },
      spoke: { state: "unreachable", agentId: "" },
    },
  ],
  installations: [
    installed("e1"),
    installed("e1"),
    installed("e2", "2"),
    installed("e2", "2", "1"),
  ],
  packaging: [
    { phase: "running" },
    { phase: "blocked" },
    { phase: "succeeded" },
    { phase: "failed" },
    { phase: "cancelled" },
    { phase: "complete" },
  ],
  plans: [{ status: "pending_approval" }, { status: "applied" }],
  changes: [{ status: "pending" }, { status: "merged" }],
};

const byId = (k: any[], id: string) => k.find((x) => x.id === id);

describe("kpisFromHub", () => {
  it("returns five kpis from full data", () => {
    const k = kpisFromHub(full);
    expect(k.map((x: any) => x.id)).toEqual([
      "convergence",
      "environments",
      "packaging",
      "spokes",
      "changes",
    ]);
    expect(byId(k, "convergence").value).toBe("1");
    expect(byId(k, "convergence").sub).toBe("1 of 3 environments in sync");
    expect(byId(k, "environments").value).toBe("3");
    expect(byId(k, "environments").sub).toBe("authorized rows");
    expect(byId(k, "packaging").value).toBe("1");
    expect(byId(k, "packaging").sub).toBe("1 blocked");
    expect(byId(k, "spokes").value).toBe("1");
    expect(byId(k, "changes").value).toBe("1");
    expect(allLive(k)).toBe(true);
    for (const x of k) expect(["ok", "info", "warn", "bad"]).toContain(x.tone);
  });

  it("requires complete fresh evidence for every installation and never counts an empty environment", () => {
    for (const override of [
      { observedAt: "" },
      { stale: true },
      { configStatus: "DRIFTED" },
      { observedConfigDigest: "sha256:wrong" },
      { health: "UNKNOWN" },
      { observedReleaseDigest: "" },
    ]) {
      expect(
        byId(
          kpisFromHub({
            ...full,
            installations: [{ ...installed("e1"), ...override }],
          }),
          "convergence",
        ).value,
      ).toBe("0");
    }
    expect(
      byId(kpisFromHub({ ...full, installations: [] }), "convergence").value,
    ).toBe("0");
  });
  it("distinguishes failed reads from empty lists and never infers online from a historical report", () => {
    for (const [note, id] of [
      ["note", "environments"],
      ["installationNote", "convergence"],
      ["packagingNote", "packaging"],
      ["changeNote", "changes"],
    ]) {
      expect(byId(kpisFromHub({ ...full, [note!]: "denied" }), id!).value).toBe(
        "—",
      );
    }
    expect(
      byId(
        kpisFromHub({
          ...full,
          environments: [{ reported: { agentId: "a1" } }],
        }),
        "spokes",
      ).value,
    ).toBe("—");
    expect(
      byId(
        kpisFromHub({
          ...full,
          environments: [{ spoke: { state: "stale", agentId: "a1" } }],
        }),
        "spokes",
      ).value,
    ).toBe("0");
  });

  it("does not count unknown run phases as in-flight or a zero", () => {
    expect(
      byId(
        kpisFromHub({ ...full, packaging: [{ phase: "unrecognized" }] }),
        "packaging",
      ).value,
    ).toBe("—");
    expect(
      byId(
        kpisFromHub({ ...full, packaging: [{ phase: "complete" }] }),
        "packaging",
      ).value,
    ).toBe("0");
  });

  it("marks missing lists as not live", () => {
    const k = kpisFromHub({});
    expect(k).toHaveLength(5);
    for (const x of k) {
      expect(x.value).toBe("—");
      expect(x.live).toBe(false);
    }
    expect(allLive(k)).toBe(false);
    expect(kpisFromHub(null)).toHaveLength(5);
    const partial = kpisFromHub({ ...full, changes: null });
    expect(byId(partial, "changes").live).toBe(false);
    expect(allLive(partial)).toBe(false);
  });
});

describe("siteSummary", () => {
  it("counts sites and distinct spokes", () => {
    expect(siteSummary(full)).toEqual({ sites: 3, spokes: 1 });
  });
  it("returns null without rows", () => {
    expect(siteSummary({ environments: [] })).toBeNull();
    expect(siteSummary({})).toBeNull();
    expect(siteSummary(undefined)).toBeNull();
  });
});

describe("clampDockWidth", () => {
  it("clamps edges", () => {
    expect(DOCK_DEFAULT).toBe(368);
    expect(clampDockWidth(0)).toBe(DOCK_MIN);
    expect(clampDockWidth(280)).toBe(280);
    expect(clampDockWidth(500)).toBe(500);
    expect(clampDockWidth(640)).toBe(DOCK_MAX);
    expect(clampDockWidth(9999)).toBe(640);
    expect(clampDockWidth(NaN)).toBe(368);
    expect(clampDockWidth(undefined as any)).toBe(368);
  });
});

describe("prefs", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("works without localStorage", () => {
    vi.stubGlobal("localStorage", undefined);
    expect(readPref("k", "fb")).toBe("fb");
    expect(writePref("k", "v")).toBe(false);
  });

  it("round trips and survives throwing storage", () => {
    const store: Record<string, string> = {};
    vi.stubGlobal("localStorage", {
      getItem: (k: string) => (k in store ? store[k] : null),
      setItem: (k: string, v: string) => {
        store[k] = v;
      },
    });
    expect(readPref("k", "fb")).toBe("fb");
    expect(writePref("k", "v")).toBe(true);
    expect(readPref("k", "fb")).toBe("v");
    vi.stubGlobal("localStorage", {
      getItem: () => {
        throw new Error("x");
      },
      setItem: () => {
        throw new Error("x");
      },
    });
    expect(readPref("k", "fb")).toBe("fb");
    expect(writePref("k", "v")).toBe(false);
  });
});

describe("DOCK_SURFACES and dockSurfaceCounts", () => {
  it("includes 16x16 crisp svg icons and glyphs on all dock surfaces", () => {
    expect(DOCK_SURFACES.map((s) => s.id)).toEqual([
      "inspector",
      "assistant",
      "outline",
      "windows",
      "live",
    ]);
    for (const surface of DOCK_SURFACES) {
      expect(surface.glyph).toBeTruthy();
      expect(surface.icon).toContain('<svg width="16" height="16"');
    }
  });

  it("calculates dockSurfaceCounts accurately from hub data", () => {
    const hub = {
      environments: [
        { environment: { id: "e1", maintenance: { active: true } } },
        { environment: { id: "e2", maintenance: {} } },
        { environment: { id: "e3" } },
      ],
      audit: [
        { sequence: 1, action: "action-1" },
        { sequence: 2, action: "action-2" },
      ],
    };
    const counts = dockSurfaceCounts(hub);
    expect(counts).toEqual({
      inspector: 0,
      assistant: 0,
      outline: 0,
      windows: 1,
      live: 2,
    });

    expect(dockSurfaceCounts(null)).toEqual({
      inspector: 0,
      assistant: 0,
      outline: 0,
      windows: 0,
      live: 0,
    });
  });
});

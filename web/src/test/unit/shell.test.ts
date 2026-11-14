import { afterEach, describe, expect, it, vi } from "vitest";
import {
  DOCK_DEFAULT,
  DOCK_MAX,
  DOCK_MIN,
  allLive,
  clampDockWidth,
  kpisFromHub,
  readPref,
  siteSummary,
  writePref,
} from "../../console/operator/shell.js";

const full = {
  environments: [
    { environment: { id: "e1" }, reported: { agentId: "a1" } },
    { environment: { id: "e2" }, reported: { agentId: "a1" } },
    { environment: { id: "e3", maintenance: true } },
  ],
  installations: [
    { desiredVersion: "1", observedVersion: "1" },
    { desiredVersion: "1", observedVersion: "1" },
    { desiredVersion: "2", observedVersion: "2" },
    { desiredVersion: "2", observedVersion: "1" },
  ],
  packaging: [
    { phase: "running" },
    { phase: "blocked" },
    { phase: "succeeded" },
    { phase: "failed" },
    { phase: "cancelled" },
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
      "plans",
      "changes",
    ]);
    expect(byId(k, "convergence").value).toBe("75%");
    expect(byId(k, "environments").value).toBe("3");
    expect(byId(k, "environments").sub).toBe("2 reporting");
    expect(byId(k, "packaging").value).toBe("1");
    expect(byId(k, "packaging").sub).toBe("1 blocked");
    expect(byId(k, "plans").value).toBe("2");
    expect(byId(k, "plans").sub).toBe("1 awaiting approval");
    expect(byId(k, "changes").value).toBe("1");
    expect(allLive(k)).toBe(true);
    for (const x of k) expect(["ok", "info", "warn", "bad"]).toContain(x.tone);
  });

  it("counts drift", () => {
    expect(byId(kpisFromHub(full), "changes").sub).toBe("1 drift");
    const none = {
      ...full,
      installations: [{ desiredVersion: "1", observedVersion: "1" }],
    };
    expect(byId(kpisFromHub(none), "changes").sub).toBe("0 drift");
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
    const partial = kpisFromHub({ ...full, plans: null });
    expect(byId(partial, "plans").live).toBe(false);
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

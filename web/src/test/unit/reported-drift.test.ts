import { describe, expect, it } from "vite-plus/test";

import { reportedDrift } from "../../console/operator/live-hub.js";

describe("reported drift", () => {
  it("keeps a missing report unknown", () => {
    expect(reportedDrift({})).toBe("unknown");
    expect(
      reportedDrift({
        desiredVersion: "1.2.0",
        desiredReleaseDigest: "sha256:desired",
        rolloutStatus: "OUT_OF_DATE",
        configStatus: "INDETERMINATE",
        health: "UNKNOWN",
        stale: true,
      }),
    ).toBe("unknown");
  });

  it("marks drift only from a reported difference", () => {
    expect(
      reportedDrift({
        observedAt: "2026-11-14T00:00:00Z",
        observedVersion: "1.0.0",
        configStatus: "DRIFTED",
      }),
    ).toBe("drifted");
    expect(
      reportedDrift({
        observedAt: "2026-11-14T00:00:00Z",
        desiredReleaseDigest: "sha256:desired",
        observedReleaseDigest: "sha256:observed",
        configStatus: "APPLIED",
      }),
    ).toBe("drifted");
    expect(
      reportedDrift({
        observedAt: "2026-11-14T00:00:00Z",
        desiredVersion: "1.2.0",
        observedVersion: "1.0.0",
        configStatus: "APPLIED",
      }),
    ).toBe("drifted");
  });

  it("keeps a matching stale report stale and a fresh match in sync", () => {
    const matched = {
      observedAt: "2026-11-14T00:00:00Z",
      desiredVersion: "1.2.0",
      observedVersion: "1.2.0",
      desiredReleaseDigest: "sha256:same",
      observedReleaseDigest: "sha256:same",
      configStatus: "APPLIED",
    };
    expect(reportedDrift({ ...matched, stale: true })).toBe("stale");
    expect(reportedDrift(matched)).toBe("in sync");
    expect(
      reportedDrift({
        observedAt: "2026-11-14T00:00:00Z",
        configStatus: "INDETERMINATE",
      }),
    ).toBe("unknown");
  });
});

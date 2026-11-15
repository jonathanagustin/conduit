import { describe, expect, it } from "vitest";
import {
  convergenceState,
  convergenceMarkup,
} from "../../console/operator/release-convergence.js";
const report = {
  environment: "prod",
  installationId: "api",
  product: "service",
  desiredVersion: "1.0.0",
  observedVersion: "1.0.0",
  desiredReleaseDigest: "sha256:release",
  observedReleaseDigest: "sha256:release",
  desiredConfigDigest: "sha256:config",
  observedConfigDigest: "sha256:config",
  observedAt: "2026-11-15T10:00:00Z",
  stale: false,
  lifecycle: "INSTALLED",
  configStatus: "APPLIED",
  rolloutStatus: "UP_TO_DATE",
  health: "HEALTHY",
};
describe("reported release convergence", () => {
  it("requires all reported evidence, not version equality alone", () => {
    expect(convergenceState(report)).toBe("converged");
    expect(convergenceState({ ...report, stale: undefined })).toBe("unknown");
    expect(convergenceState({ ...report, observedAt: "invalid" })).toBe(
      "unknown",
    );
    expect(convergenceState({ ...report, stale: true })).toBe("stale");
    expect(
      convergenceState({ ...report, observedAt: "0001-01-01T00:00:00Z" }),
    ).toBe("unreported");
    expect(convergenceState({ ...report, desiredConfigDigest: "" })).toBe(
      "unknown",
    );
    expect(
      convergenceState({ ...report, observedReleaseDigest: "different" }),
    ).toBe("diverged");
    expect(
      convergenceState({ ...report, observedConfigDigest: "different" }),
    ).toBe("diverged");
    expect(convergenceState({ ...report, health: "UNKNOWN" })).toBe("pending");
    expect(convergenceState({ ...report, configStatus: "INDETERMINATE" })).toBe(
      "pending",
    );
    expect(convergenceState({ ...report, lifecycle: "UNINSTALLED" })).toBe(
      "pending",
    );
    expect(convergenceState({ ...report, rolloutStatus: "OUT_OF_DATE" })).toBe(
      "pending",
    );
  });
  it("escapes records and keeps absent reports explicit", () => {
    expect(convergenceMarkup([{ ...report, product: "<script>" }])).toContain(
      "&lt;script&gt;",
    );
    expect(convergenceMarkup([])).toContain(
      "No convergence result can be inferred",
    );
    expect(convergenceMarkup([], "<error>")).toContain("&lt;error&gt;");
  });
});

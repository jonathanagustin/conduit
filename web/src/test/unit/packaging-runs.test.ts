import { describe, expect, it } from "vitest";

import {
  gateResults,
  runCounts,
  runState,
  runsMarkup,
} from "../../console/operator/packaging-runs.js";

describe("packaging runs", () => {
  it("groups runs by state", () => {
    expect(runState({ phase: "succeeded", outcome: "PASS" })).toBe("succeeded");
    expect(runState({ phase: "blocked", outcome: "TIMED_OUT" })).toBe("blocked");
    expect(runState({ phase: "failed", outcome: "FAIL_REQUIRED" })).toBe("failed");
    expect(runState({ phase: "validate", outcome: "" })).toBe("running");
    expect(
      runCounts([
        { phase: "succeeded", outcome: "PASS" },
        { phase: "blocked", outcome: "WAITING" },
      ]),
    ).toEqual({ succeeded: 1, running: 0, blocked: 1, failed: 0 });
  });

  it("keeps the latest outcome per step in first-seen order", () => {
    expect(
      gateResults([
        { step: "assess", outcome: "PASS" },
        { step: "review", outcome: "WAITING" },
        { step: "review", outcome: "TIMED_OUT" },
      ]),
    ).toEqual([
      { step: "assess", outcome: "PASS" },
      { step: "review", outcome: "TIMED_OUT" },
    ]);
  });

  it("renders rows, gates, and an empty note, escaping values", () => {
    const html = runsMarkup(
      [{ runId: "<r1>", phase: "blocked", outcome: "TIMED_OUT", sourceDigest: "sha256:abcdef0123456789" }],
      { "<r1>": [{ step: "assess", outcome: "PASS" }] },
    );
    expect(html).toContain("&lt;r1&gt;");
    expect(html).not.toContain("<r1>");
    expect(html).toContain("abcdef012345");
    expect(html).toContain("kit-badge-ok");
    expect(runsMarkup([], {}, { note: "Hub did not return packaging runs." })).toContain(
      "Hub did not return packaging runs.",
    );
  });
});

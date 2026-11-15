import { describe, expect, it } from "vite-plus/test";

import {
  gateResults,
  gateStepper,
  runCounts,
  runState,
  runsMarkup,
} from "../../console/operator/packaging-runs.js";

describe("packaging runs", () => {
  it("groups runs by state", () => {
    expect(runState({ phase: "succeeded", outcome: "PASS" })).toBe("succeeded");
    expect(runState({ phase: "blocked", outcome: "TIMED_OUT" })).toBe(
      "blocked",
    );
    expect(runState({ phase: "failed", outcome: "FAIL_REQUIRED" })).toBe(
      "failed",
    );
    expect(runState({ phase: "validate", outcome: "" })).toBe("running");
    expect(runState({ phase: "review", outcome: "" })).toBe("review");
    expect(runState({ phase: "review", outcome: "PASS" })).toBe("review");
    expect(runState({ phase: "blocked", outcome: "PASS" })).toBe("blocked");
    expect(runState({ configured: false, outcome: "PASS" })).toBe("blocked");
    expect(runState({ phase: "review", outcome: "WAITING" })).toBe("review");
    expect(runState({ phase: "review", outcome: "TIMED_OUT" })).toBe("failed");
    expect(
      runCounts([
        { phase: "succeeded", outcome: "PASS" },
        { phase: "blocked", outcome: "WAITING" },
        { phase: "review", outcome: "" },
      ]),
    ).toEqual({ succeeded: 1, review: 1, running: 0, blocked: 1, failed: 0 });
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
      [
        {
          runId: "<r1>",
          phase: "blocked",
          outcome: "TIMED_OUT",
          sourceDigest: "sha256:abcdef0123456789",
        },
      ],
      { "<r1>": [{ step: "assess", outcome: "PASS" }] },
    );
    expect(html).toContain("&lt;r1&gt;");
    expect(html).not.toContain("<r1>");
    expect(html).toContain("abcdef012345");
    expect(html).toContain('class="kit-gate is-done"');
    expect(html).toContain("assess: PASS");
    expect(
      runsMarkup([], {}, { note: "Hub did not return packaging runs." }),
    ).toContain("Hub did not return packaging runs.");
  });

  it("draws a connected gate stepper from step outcomes", () => {
    const html = gateStepper([
      { step: "assess", outcome: "PASS" },
      { step: "propose", outcome: "WAITING" },
      { step: "<v>", outcome: "FAIL_REQUIRED" },
    ]);
    expect(html).toContain('aria-label="Gate results"');
    expect(html).toContain("kit-gate is-done");
    expect(html).toContain("kit-gate is-active is-after-done");
    expect(html).toContain("kit-gate is-blocked");
    expect(html).toContain("&lt;v&gt;: FAIL_REQUIRED");
    expect(html).toContain(": did not pass (FAIL_REQUIRED)");
    expect(gateStepper([])).toBe("");
  });
});

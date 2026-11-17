// @vitest-environment node
import { describe, expect, it } from "vite-plus/test";

import {
  RECORDED_DEMO_LABEL,
  isRecordedDemo,
  recordedDemoMarkup,
} from "../../console/operator/planning-flow.js";

// #817: a recorded demonstration is labelled as one
// ---------------------------------------------------------------------------

describe("recorded demonstration", () => {
  it("is recognised only by recordedDemo: true", () => {
    expect(isRecordedDemo({ id: "gen_1", recordedDemo: true })).toBe(true);
    for (const result of [
      { id: "gen_1" },
      { id: "gen_1", recordedDemo: false },
      { id: "gen_1", recordedDemo: "true" },
      null,
      undefined,
    ])
      expect(isRecordedDemo(result)).toBe(false);
  });

  it("labels the result as not model output", () => {
    const html = recordedDemoMarkup({ recordedDemo: true });
    expect(html).toContain("data-plan-generation-demo");
    expect(html).toContain(RECORDED_DEMO_LABEL);
    expect(RECORDED_DEMO_LABEL).toMatch(/not model output/);
    expect(recordedDemoMarkup({ recordedDemo: false })).toBe("");
    expect(recordedDemoMarkup({})).toBe("");
  });
});

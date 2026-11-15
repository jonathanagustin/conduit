import { expect, it } from "vite-plus/test";
import {
  selectedPlan,
  planInspectorMarkup,
  timelineMarkup,
} from "../../console/operator/release-plan-inspector.js";
it("keeps explicit plan identity and records separate from execution claims", () => {
  const plans = [
    {
      id: "plan-a",
      status: "FAILED",
      failure: "<rejected>",
      plan: {
        environment: "prod",
        steps: [
          {
            position: 1,
            action: "apply",
            installationId: "api",
            product: "service",
            version: "1",
          },
        ],
      },
    },
  ];
  expect(selectedPlan(plans, "missing")).toBeNull();
  expect(planInspectorMarkup(plans, "missing")).toContain("No selected plan");
  const html = planInspectorMarkup(plans);
  expect(html).toContain("&lt;rejected&gt;");
  expect(html).toContain("Instructions, not per-step execution receipts");
  expect(html).toContain("FAILED");
  expect(html).not.toContain("OOMKilled");
});
it("shows only complete audit rows as Hub-verified chain links", () => {
  const audit = {
    at: "2026-11-15",
    kind: "audit",
    summary: "allowed",
    verified: true,
    digest: "sha256:link",
    sequence: 4,
  };
  expect(timelineMarkup([audit])).toContain("Hub-verified chain link 4");
  for (const patch of [
    { kind: "plan" },
    { verified: false },
    { digest: "" },
    { sequence: 0 },
    { sequence: 1.5 },
  ]) {
    expect(timelineMarkup([{ ...audit, ...patch }])).not.toContain(
      "Hub-verified chain link",
    );
  }
  expect(timelineMarkup([{ ...audit, summary: "<script>" }])).toContain(
    "&lt;script&gt;",
  );
  expect(timelineMarkup([], "<offline>")).toContain("&lt;offline&gt;");
  expect(timelineMarkup(Array.from({ length: 513 }, () => audit))).toContain(
    "Only the first 512",
  );
});

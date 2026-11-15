import { describe, it, expect } from "vitest";
import {
  spokesMarkup,
  workloadMarkup,
} from "../../console/operator/fleet-spokes.mjs";
describe("reported spoke workloads", () => {
  it("keeps missing reports unknown and escapes recorded fields", () => {
    const html = workloadMarkup({
      environment: { id: "<east>" },
      reported: {
        observedAt: "0001-01-01T00:00:00Z",
        entities: [{ product: "<script>", version: "1" }],
      },
    });
    expect(html).toContain("&lt;east&gt;");
    expect(html).toContain("&lt;script&gt;");
    expect(html).toContain("UNKNOWN");
    expect(html).toContain("observed unavailable");
    expect(html).not.toContain("<script>");
    expect(workloadMarkup({ environment: { id: "empty" } })).toContain(
      "No workload report",
    );
  });
  it("shows Hub liveness without converting missing data to success", () => {
    const html = spokesMarkup({
      environments: [
        {
          environment: { id: "east" },
          spoke: { state: "stale", agentId: "agent-e" },
        },
        { environment: { id: "west" } },
      ],
    });
    expect(html).toContain("Spoke: stale");
    expect(html).toContain("Spoke: unknown");
    expect(html).toContain('data-spoke-environment="west"');
    expect(html).not.toContain("94% Synced");
    expect(html).toContain("No site pending-sync status");
  });
  it("bounds workload rows without claiming the displayed subset is a cluster total", () => {
    const html = workloadMarkup({
      environment: { id: "east" },
      reported: {
        entities: Array.from({ length: 257 }, (_, i) => ({
          installationId: `i-${i}`,
        })),
      },
    });
    expect(html).toContain("Showing the first 256");
    expect(html).toContain("i-255");
    expect(html).not.toContain("i-256");
  });
});

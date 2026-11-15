// @vitest-environment node
import { describe, expect, it } from "vite-plus/test";

import {
  decisions,
  overviewMarkup,
  runsNeedingAttention,
} from "../../console/operator/command-center.js";

const now = Date.parse("2026-11-14T20:00:00Z");
const hub = {
  changes: [
    { id: "cr-1", title: "Raise memory", status: "PENDING_APPROVALS" },
    { id: "cr-2", title: "Done", status: "APPROVED" },
  ],
  plans: [
    { id: "pln-1", status: "AWAITING_APPROVAL", plan: { environment: "dev" } },
  ],
  packaging: [
    { runId: "run-ok", phase: "succeeded", outcome: "PASS" },
    { runId: "run-stuck", phase: "blocked", outcome: "FAIL_REQUIRED" },
  ],
  packagingEventsByRun: { "run-stuck": [{ step: "assess", outcome: "PASS" }] },
  environments: [
    {
      environment: { id: "dev" },
      spoke: { state: "connected", observedAt: "2026-11-14T19:58:00Z" },
    },
  ],
  installations: [
    { environment: "dev", desiredVersion: "1.1", observedVersion: "1.0" },
    { environment: "dev", desiredVersion: "1.1", observedVersion: "1.1" },
  ],
  audit: [{ sequence: 1, actor: "operator", action: "plan:approve" }],
};

describe("command center overview", () => {
  it("keeps only what needs a decision or attention", () => {
    const { changes, plans } = decisions(hub);
    expect(changes.map((c) => c.id)).toEqual(["cr-1"]);
    expect(plans.map((p) => p.id)).toEqual(["pln-1"]);
    expect(runsNeedingAttention(hub).map((r) => r.runId)).toEqual([
      "run-stuck",
    ]);
  });

  it("renders the Hub's records, not example data", () => {
    const html = overviewMarkup(hub, { now });
    expect(html).toContain("Raise memory");
    expect(html).not.toContain("cr-2");
    expect(html).toContain("run-stuck");
    expect(html).not.toContain("run-ok");
    expect(html).toContain("1 of 2");
    expect(html).toContain("2m ago");
    expect(html).toContain("plan:approve");
  });

  it("passes the Hub's own error notes through", () => {
    const html = overviewMarkup(
      { ...hub, changeNote: "Hub did not return change requests." },
      { now },
    );
    expect(html).toContain("Hub did not return change requests.");
  });

  it("says so before the Hub has answered", () => {
    expect(overviewMarkup(undefined)).toContain("Reading the Hub");
  });
});

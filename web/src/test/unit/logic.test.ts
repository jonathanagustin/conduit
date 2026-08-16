import { describe, expect, it } from "vitest";

import { filterFleet, matchesSearch } from "../../features/fleet/filter.js";
import { orderInbox, outstandingRequirements } from "../../features/changes/order.js";
import { countOf, relative } from "../../components/Timestamp.js";
import { latestForChannel, orderChannels } from "../../features/promotions/ReleaseChannelGraph.js";
import { statusIntent } from "../../api/imports.js";
import { findingScore, orderFindingsByHubScore } from "../../api/catalog.js";
import type { VulnerabilityFinding } from "../../api/catalog.js";
import { pipelineStatus, pipelineStatusIntent } from "../../api/exports.js";
import { HEALTH_INTENT, ROLLOUT_INTENT, CONFIG_INTENT, LIFECYCLE_INTENT } from "../../features/fleet/statusTags.js";
import { PLAN_INTENT } from "../../features/plans/PlansPage.js";
import { installation } from "../fixtures.js";
import type { ChangeRequest } from "../../api/resources.js";

/** Unit tests over the console's own logic -- the parts that are not the Hub's
 * behaviour and not Blueprint's. These exist to be mutated: the browser tests
 * prove the screens work, but they are far too slow to run once per mutant. */

describe("fleet search", () => {
  // The three searchable fields deliberately share no substring. The first
  // version of these fixtures used installationId "payments" with product
  // "app.example:payments", so a mutation that searched the wrong field still
  // matched through the other one and survived. Overlapping fixtures cannot
  // tell you which field a match came from.
  const payments = installation({
    installationId: "alpha",
    environment: "prod-use1",
    product: "app.example:billing",
  });
  const ledger = installation({
    installationId: "bravo",
    environment: "prod-euw1",
    product: "app.example:ledger",
  });

  it("matches on the installation identifier", () => {
    expect(matchesSearch(payments, "alpha")).toBe(true);
    expect(matchesSearch(ledger, "alpha")).toBe(false);
  });

  it("matches on the Environment", () => {
    expect(matchesSearch(ledger, "euw1")).toBe(true);
    expect(matchesSearch(payments, "euw1")).toBe(false);
  });

  it("matches on the Product", () => {
    expect(matchesSearch(ledger, "ledger")).toBe(true);
    expect(matchesSearch(payments, "ledger")).toBe(false);
  });

  it("ignores case on each field independently", () => {
    // One assertion per field, or an uppercased comparison on one field is
    // masked by another field still matching.
    expect(matchesSearch(payments, "ALPHA")).toBe(true);
    expect(matchesSearch(payments, "PROD-USE1")).toBe(true);
    expect(matchesSearch(payments, "APP.EXAMPLE:BILLING")).toBe(true);
    expect(matchesSearch(installation({ installationId: "ALPHA", environment: "e", product: "p" }), "alpha")).toBe(true);
  });

  it("treats an empty or whitespace-only search as no filter", () => {
    expect(matchesSearch(payments, "")).toBe(true);
    expect(matchesSearch(payments, "   ")).toBe(true);
    expect(filterFleet([payments, ledger], "  ")).toHaveLength(2);
  });

  it("matches a substring rather than requiring the whole field", () => {
    expect(matchesSearch(payments, "lph")).toBe(true);
  });

  it("returns nothing when nothing matches", () => {
    expect(filterFleet([payments, ledger], "absent")).toHaveLength(0);
  });
});

function request(overrides: Partial<ChangeRequest> = {}): ChangeRequest {
  return {
    id: "crq_1",
    planId: "pln_1",
    environment: "prod-use1",
    title: "Change",
    author: { username: "author" },
    status: "PENDING_APPROVALS",
    requirementsSatisfied: false,
    changes: [],
    evaluations: [],
    requirements: [],
    createdAt: "2026-08-07T12:00:00Z",
    updatedAt: "2026-08-07T12:00:00Z",
    revision: 1,
    ...overrides,
  };
}

describe("inbox ordering", () => {
  it("puts requests awaiting approval before settled ones", () => {
    const ordered = orderInbox([
      request({ id: "settled", status: "APPROVED", updatedAt: "2026-08-07T13:00:00Z" }),
      request({ id: "waiting", status: "PENDING_APPROVALS" }),
    ]);
    expect(ordered.map((entry) => entry.id)).toEqual(["waiting", "settled"]);
  });

  it("orders within a group by recency, newest first", () => {
    const ordered = orderInbox([
      request({ id: "older", status: "APPROVED", updatedAt: "2026-08-07T10:00:00Z" }),
      request({ id: "newer", status: "APPROVED", updatedAt: "2026-08-07T14:00:00Z" }),
    ]);
    expect(ordered.map((entry) => entry.id)).toEqual(["newer", "older"]);
  });

  it("does not mutate the caller's array", () => {
    const input = [request({ id: "a", status: "APPROVED" }), request({ id: "b" })];
    orderInbox(input);
    expect(input.map((entry) => entry.id)).toEqual(["a", "b"]);
  });
});

describe("outstanding requirements", () => {
  it("counts only requirements nobody has satisfied", () => {
    expect(
      outstandingRequirements(
        request({
          requirements: [
            { id: "one", satisfiedBy: [] },
            { id: "two", satisfiedBy: ["reviewer"] },
            { id: "three" },
          ],
        }),
      ),
    ).toBe(2);
  });

  it("is zero when every requirement is satisfied", () => {
    expect(
      outstandingRequirements(
        request({ requirements: [{ id: "one", satisfiedBy: ["reviewer"] }] }),
      ),
    ).toBe(0);
  });
});

describe("countOf", () => {
  it("agrees with its noun", () => {
    expect(countOf(1, "installation")).toBe("1 installation");
    expect(countOf(0, "installation")).toBe("0 installations");
    expect(countOf(2, "installation")).toBe("2 installations");
  });

  it("takes an irregular plural when given one", () => {
    expect(countOf(2, "Entity", "Entities")).toBe("2 Entities");
    expect(countOf(1, "Entity", "Entities")).toBe("1 Entity");
  });
});

describe("release channel ordering", () => {
  const promotion = (fromChannel: string, toChannel: string) => ({
    id: `${fromChannel}-${toChannel}`,
    product: "app.example:payments",
    version: "1.0.0",
    fromChannel,
    toChannel,
    status: "PASSED",
  });

  it("recovers the run by following transitions, not by sorting", () => {
    // Alphabetical order would give CANARY, RELEASE, STABLE -- which is not the
    // pipeline. The Hub publishes transitions and the order has to be walked.
    expect(
      orderChannels([promotion("RELEASE", "CANARY"), promotion("CANARY", "STABLE")]),
    ).toEqual(["RELEASE", "CANARY", "STABLE"]);
  });

  it("does not depend on the order the promotions arrive in", () => {
    expect(
      orderChannels([promotion("CANARY", "STABLE"), promotion("RELEASE", "CANARY")]),
    ).toEqual(["RELEASE", "CANARY", "STABLE"]);
  });

  it("terminates on a cycle instead of walking forever", () => {
    // A misconfigured Product can promote in a loop. The walk is bounded, and
    // showing the channels beats hanging the page.
    const ordered = orderChannels([promotion("A", "B"), promotion("B", "A")]);
    expect(ordered.length).toBeLessThanOrEqual(2);
    expect(ordered).toContain("A");
    expect(ordered).toContain("B");
  });

  it("returns nothing for no promotions", () => {
    expect(orderChannels([])).toEqual([]);
  });

  it("keeps a single transition in order", () => {
    expect(orderChannels([promotion("RELEASE", "STAGING")])).toEqual(["RELEASE", "STAGING"]);
  });
});

describe("relative time", () => {
  // A fixed "now" so the arithmetic is deterministic. Reading Date.now() inside
  // the test would make the boundaries drift with the clock.
  const now = Date.parse("2026-08-07T12:00:00Z");
  const ago = (seconds: number) => new Date(now - seconds * 1000);

  it("counts seconds, then minutes, then hours, then days", () => {
    expect(relative(ago(5), now)).toBe("5 seconds ago");
    expect(relative(ago(120), now)).toBe("2 minutes ago");
    expect(relative(ago(7200), now)).toBe("2 hours ago");
    expect(relative(ago(172_800), now)).toBe("2 days ago");
  });

  it("switches unit at each boundary rather than one either side of it", () => {
    expect(relative(ago(59), now)).toBe("59 seconds ago");
    expect(relative(ago(60), now)).toBe("1 minute ago");
    expect(relative(ago(3599), now)).toBe("60 minutes ago");
    expect(relative(ago(3600), now)).toBe("1 hour ago");
    expect(relative(ago(86_399), now)).toBe("24 hours ago");
    expect(relative(ago(86_400), now)).toBe("1 day ago");
  });

  it("agrees with its unit in the singular", () => {
    expect(relative(ago(1), now)).toBe("1 second ago");
    expect(relative(ago(60), now)).toBe("1 minute ago");
  });

  it("says 'in' for a timestamp ahead of the clock", () => {
    // Hub and browser clocks disagree; "-30 seconds ago" would be nonsense.
    expect(relative(new Date(now + 30_000), now)).toBe("in 30 seconds");
    expect(relative(new Date(now + 7_200_000), now)).toBe("in 2 hours");
  });

  it("treats the present as zero seconds rather than as future", () => {
    expect(relative(new Date(now), now)).toBe("0 seconds ago");
  });
});

describe("latest promotion per channel", () => {
  const record = (id: string, fromChannel: string, evaluationStartedAt?: string) => ({
    id,
    product: "app.example:payments",
    version: "1.0.0",
    fromChannel,
    toChannel: "STAGING",
    status: "BLOCKED",
    ...(evaluationStartedAt === undefined ? {} : { evaluationStartedAt }),
  });

  it("takes the most recent evaluation, not the first in the list", () => {
    // Promotion records are immutable evidence and a boundary accumulates them.
    // Picking the first would show a blocking state that has since been resolved.
    const chosen = latestForChannel(
      [
        record("old", "RELEASE", "2026-08-01T00:00:00Z"),
        record("new", "RELEASE", "2026-08-07T00:00:00Z"),
      ],
      "RELEASE",
    );
    expect(chosen?.id).toBe("new");
  });

  it("does not depend on the order the records arrive in", () => {
    const chosen = latestForChannel(
      [
        record("new", "RELEASE", "2026-08-07T00:00:00Z"),
        record("old", "RELEASE", "2026-08-01T00:00:00Z"),
      ],
      "RELEASE",
    );
    expect(chosen?.id).toBe("new");
  });

  it("prefers a record that carries a timestamp over one that does not", () => {
    expect(
      latestForChannel([record("undated", "RELEASE"), record("dated", "RELEASE", "2026-08-01T00:00:00Z")], "RELEASE")?.id,
    ).toBe("dated");
  });

  it("ignores other channels", () => {
    expect(
      latestForChannel([record("other", "STAGING", "2026-08-09T00:00:00Z")], "RELEASE"),
    ).toBeUndefined();
  });
});

describe("bundle import status intent", () => {
  // The vocabulary is the Hub's, from controlplane.bundles.go. An earlier
  // version guessed "IMPORTED"/"BLOCKED" and coloured the real ADMITTED and
  // PENDING_APPROVAL states neutral grey -- a passing e2e suite over invented
  // fixtures never caught it, which is why this pins the real values.
  it("colours the done-and-good states success", () => {
    expect(statusIntent("ADMITTED")).toBe("success"); // import
    expect(statusIntent("SUCCEEDED")).toBe("success"); // task
  });
  it("colours the failed states danger", () => {
    expect(statusIntent("ERROR")).toBe("danger"); // import
    expect(statusIntent("FAILED")).toBe("danger"); // task
  });
  it("colours the awaiting-a-person state warning", () => {
    expect(statusIntent("PENDING_APPROVAL")).toBe("warning");
  });
  it("leaves an in-progress, abandoned, or unknown status neutral", () => {
    expect(statusIntent("PENDING")).toBe("none");
    expect(statusIntent("SKIPPED")).toBe("none");
    expect(statusIntent("SOMETHING_NEW")).toBe("none");
  });
  it("is case-insensitive so a lower-case status is not silently uncoloured", () => {
    expect(statusIntent("admitted")).toBe("success");
  });
});

describe("export pipeline status", () => {
  // GET /v1/export-pipelines returns archived and deleted pipelines, and the
  // Hub reads an empty status as ACTIVE. A screen that dropped either would
  // show a retired pipeline as if it still exported.
  const pipeline = (status?: string) => ({
    id: "p",
    displayName: "P",
    buildStrategy: "SNAPSHOT",
    selections: [],
    ...(status === undefined ? {} : { status }),
  });

  it("reads an absent or empty status as ACTIVE, like the Hub does", () => {
    expect(pipelineStatus(pipeline(undefined))).toBe("ACTIVE");
    expect(pipelineStatus(pipeline(""))).toBe("ACTIVE");
    expect(pipelineStatus(pipeline("ARCHIVED"))).toBe("ARCHIVED");
  });

  it("colours active success, archived warning, deleted danger", () => {
    expect(pipelineStatusIntent("ACTIVE")).toBe("success");
    expect(pipelineStatusIntent("ARCHIVED")).toBe("warning");
    expect(pipelineStatusIntent("DELETED")).toBe("danger");
  });

  it("leaves an unknown status neutral rather than colouring it on a guess", () => {
    expect(pipelineStatusIntent("SOMETHING_NEW")).toBe("none");
  });
});

describe("status colour vocabularies match the Hub", () => {
  // The map keys must be exactly the values the Hub emits (from
  // internal/environments/domain/reporting.go and internal/controlplane/
  // lifecycle.go). A missing real value renders as an unstyled tag; an invented
  // key is a dead branch. Both are the drift this pins -- and because these are
  // the Go const sets, adding a value on either side without the other fails.
  const keys = (map: Record<string, unknown>) => Object.keys(map).sort();

  it("health covers every reporting.go Health value", () => {
    expect(keys(HEALTH_INTENT)).toEqual(
      ["ERROR", "HEALTHY", "INDETERMINATE", "REPAIRING", "TERMINAL", "UNHEALTHY", "UNKNOWN", "WARNING"].sort(),
    );
  });
  it("rollout covers every RolloutStatus value and invents none", () => {
    expect(keys(ROLLOUT_INTENT)).toEqual(
      ["UP_TO_DATE", "IN_GRACE_PERIOD", "BLOCKED", "RECALLED", "OUT_OF_DATE"].sort(),
    );
  });
  it("config covers every ConfigStatus value and invents none", () => {
    expect(keys(CONFIG_INTENT)).toEqual(["APPLIED", "DRIFTED", "UNAVAILABLE", "INDETERMINATE"].sort());
  });
  it("lifecycle covers every Lifecycle value and invents none", () => {
    expect(keys(LIFECYCLE_INTENT)).toEqual(["INSTALLED", "UNINSTALLED", "UNKNOWN"].sort());
  });
  it("plan covers every PlanStatus value and invents none", () => {
    expect(keys(PLAN_INTENT)).toEqual(
      ["AWAITING_APPROVAL", "APPROVED", "LEASED", "RUNNING", "SUCCEEDED", "FAILED", "CANCELLED", "RECORDED"].sort(),
    );
  });
});

describe("findingScore mirrors the Hub", () => {
  // These five numbers are duplicated from findingScore in
  // internal/controlplane/risk.go. Pinning each one here is what makes the
  // duplication safe: a drift in either direction fails this test rather than
  // silently ordering the console differently from the Hub.
  it("prefers CVSS whenever it is above zero", () => {
    expect(findingScore({ id: "a", cvss: 6.6, severity: "CRITICAL" })).toBe(6.6);
  });
  it("caps CVSS at the Hub's maximum", () => {
    expect(findingScore({ id: "a", cvss: 99 })).toBe(10);
  });
  it("falls back to the severity tiers the Hub uses", () => {
    expect(findingScore({ id: "a", severity: "CRITICAL" })).toBe(9);
    expect(findingScore({ id: "a", severity: "HIGH" })).toBe(7);
    expect(findingScore({ id: "a", severity: "MEDIUM" })).toBe(4);
    expect(findingScore({ id: "a", severity: "MODERATE" })).toBe(4);
    expect(findingScore({ id: "a", severity: "LOW" })).toBe(0.1);
  });
  it("treats the Hub's informational aliases as zero", () => {
    for (const severity of ["NONE", "INFORMATIONAL", "INFO"]) {
      expect(findingScore({ id: "a", severity })).toBe(0);
    }
  });
  // risk.go: "a finding whose severity nobody could parse must not make a
  // target look cleaner than it is."
  it("scores an unparseable severity at the HIGH tier, not at zero", () => {
    expect(findingScore({ id: "a", severity: "banana" })).toBe(7);
    expect(findingScore({ id: "a" })).toBe(7);
  });
});

describe("orderFindingsByHubScore", () => {
  it("orders worst-first by the Hub's score, not by the raw CVSS column", () => {
    // The case the first revision got wrong: HIGH with no CVSS scores 7.0 at
    // the Hub and must outrank a reported 4.2, even though its CVSS cell is
    // empty.
    const ordered = orderFindingsByHubScore([
      { id: "CVE-3", cvss: 4.2, severity: "CRITICAL" },
      { id: "CVE-1", cvss: 9.8, severity: "LOW" },
      { id: "CVE-2", severity: "HIGH" },
    ]);
    expect(ordered.map((finding: VulnerabilityFinding) => finding.id)).toEqual([
      "CVE-1",
      "CVE-2",
      "CVE-3",
    ]);
  });

  it("puts an informational finding below a scored one", () => {
    const ordered = orderFindingsByHubScore([
      { id: "CVE-info", severity: "INFO" },
      { id: "CVE-low", cvss: 0.1 },
    ]);
    expect(ordered.map((finding: VulnerabilityFinding) => finding.id)).toEqual([
      "CVE-low",
      "CVE-info",
    ]);
  });

  it("breaks ties on CVE id so the list is stable", () => {
    const ordered = orderFindingsByHubScore([
      { id: "CVE-b", cvss: 7 },
      { id: "CVE-a", severity: "HIGH" },
    ]);
    expect(ordered.map((finding: VulnerabilityFinding) => finding.id)).toEqual([
      "CVE-a",
      "CVE-b",
    ]);
  });

  it("does not mutate its argument", () => {
    const findings = [
      { id: "CVE-2", cvss: 1 },
      { id: "CVE-1", cvss: 9 },
    ];
    orderFindingsByHubScore(findings);
    expect(findings.map((finding: VulnerabilityFinding) => finding.id)).toEqual(["CVE-2", "CVE-1"]);
  });
});

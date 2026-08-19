import { describe, expect, it } from "vitest";

import { orderInbox, outstandingRequirements } from "../../features/changes/order.js";
import {
  CANCELLABLE_STATUSES,
  REOPENABLE_STATUSES,
  canCancel,
  canReopen,
  carriesRationale,
} from "../../features/changes/review.js";
import { countOf, relative } from "../../components/Timestamp.js";
import {
  installationSource,
  installationSourceLabel,
  parseInstallationRID,
} from "../../api/fleet.js";
import { hasInstant } from "../../api/types.js";
import { parseInline, parseMarkdown } from "../../components/Markdown.js";
import { latestForChannel, orderChannels } from "../../components/ReleaseChannelGraph.js";
import {
  boundFacetValues,
  facetBarValue,
  facetsAreEmpty,
  largestFacetCount,
  orderFacetValues,
} from "../../components/facets.js";
import { fleetFacetGroups } from "../../features/fleet/facets.js";
import { statusIntent } from "../../api/imports.js";
import { findingScore, orderFindingsByHubScore, severityTier } from "../../api/catalog.js";
import type { ActiveFindingRow, VulnerabilityFinding } from "../../api/catalog.js";
import {
  filterInventoryByTier,
  inventoryTiers,
  orderInventoryRows,
} from "../../features/risk/inventory.js";
import { pipelineStatus, pipelineStatusIntent } from "../../api/exports.js";
import {
  NOTIFICATION_CHANNELS,
  NOTIFICATION_EVENT_TYPES,
  TEAM_CONTACT_KINDS,
  contactDeliverable,
  inboxPage,
} from "../../api/notifications.js";
import {
  deliversSomething,
  readChannel,
  toggleType,
  unknownTypes,
} from "../../features/notifications/NotificationsPage.js";
import { HEALTH_INTENT, ROLLOUT_INTENT, CONFIG_INTENT, LIFECYCLE_INTENT } from "../../components/statusTags.js";
import { HEALTH } from "../../api/types.js";
import { PLAN_INTENT } from "../../components/PlanStatusTag.js";
import {
  PLAN_TERMINAL_STATUSES,
  planPollInterval,
  reviewBody,
  reviewInvalidationKeys,
  type ChangeRequest,
} from "../../api/resources.js";

/** Unit tests over the console's own logic -- the parts that are not the Hub's
 * behaviour and not Blueprint's. These exist to be mutated: the browser tests
 * prove the screens work, but they are far too slow to run once per mutant. */

// Fleet search is deliberately absent from this file. It used to live here as
// unit tests over a local matchesSearch/filterFleet, written when the Hub was
// believed to have no free-text parameter. It has one: FleetPage puts `search`
// in the query string, the Hub reads it in http.go and matches installation,
// Environment and Product in reporting/domain/fleet.go, and the console never
// filtered a row itself. Filtering locally would narrow the page the Hub
// returned rather than the fleet, so the right console-side behaviour is to
// send the parameter and render what comes back -- which the browser tests
// cover, because it is a request, not a computation.

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
  // the test would make the boundaries drift with the clock. Midday UTC so a
  // day either side stays inside its own UTC date.
  const now = Date.parse("2026-08-07T12:00:00Z");
  const ago = (seconds: number) => new Date(now - seconds * 1000);
  const days = (count: number) => ago(count * 86_400);

  // Every expectation below is the captured Foundry ladder, not a preference:
  // `apollo-control-panel` 6.533.15 truncates each unit, names yesterday and
  // tomorrow before counting hours, and counts whole calendar days beyond that.
  // See docs/apollo/FRONTEND-CAS-CAPTURE.md.

  it("counts seconds, then minutes, then hours, then days", () => {
    expect(relative(ago(5), now)).toBe("5 seconds ago");
    expect(relative(ago(120), now)).toBe("2 minutes ago");
    expect(relative(ago(7200), now)).toBe("2 hours ago");
    expect(relative(days(2), now)).toBe("2 days ago");
  });

  it("switches unit at each boundary rather than one either side of it", () => {
    expect(relative(ago(59), now)).toBe("59 seconds ago");
    expect(relative(ago(60), now)).toBe("1 minute ago");
    // Truncated, not rounded: 59m59s is not an hour, and reading it as one made
    // two screens disagree about whether a deadline had passed.
    expect(relative(ago(3599), now)).toBe("59 minutes ago");
    expect(relative(ago(3600), now)).toBe("1 hour ago");
    expect(relative(ago(43_199), now)).toBe("11 hours ago");
    expect(relative(ago(43_200), now)).toBe("12 hours ago");
  });

  it("names yesterday and tomorrow ahead of counting hours or days", () => {
    // 12 hours back from midday is still today; 13 is not.
    expect(relative(ago(12 * 3600), now)).toBe("12 hours ago");
    expect(relative(ago(13 * 3600), now)).toBe("Yesterday at 23:00 UTC");
    expect(relative(days(1), now)).toBe("Yesterday at 12:00 UTC");
    expect(relative(new Date(now + 13 * 3_600_000), now)).toBe("Tomorrow at 01:00 UTC");
  });

  it("keeps the day count above the hour count once a whole day has passed", () => {
    expect(relative(days(2), now)).toBe("2 days ago");
    expect(relative(days(20), now)).toBe("20 days ago");
  });

  it("climbs to weeks, months and years the way the captured ladder does", () => {
    // 21 days is 3 weeks; 60 days is 2 months; 730 days is 2 years. The spans
    // are Foundry's coarse constants -- a month is 30 days, a year is 365.
    expect(relative(days(21), now)).toBe("3 weeks ago");
    expect(relative(days(59), now)).toBe("8 weeks ago");
    expect(relative(days(60), now)).toBe("2 months ago");
    expect(relative(days(97), now)).toBe("3 months ago");
    expect(relative(days(729), now)).toBe("24 months ago");
    expect(relative(days(730), now)).toBe("2 years ago");
  });

  it("agrees with its unit in the singular", () => {
    expect(relative(ago(1), now)).toBe("1 second ago");
    expect(relative(ago(60), now)).toBe("1 minute ago");
    expect(relative(days(7), now)).toBe("7 days ago");
  });

  it("says 'in' for a timestamp ahead of the clock", () => {
    // Hub and browser clocks disagree; "-30 seconds ago" would be nonsense.
    expect(relative(new Date(now + 30_000), now)).toBe("in 30 seconds");
    expect(relative(new Date(now + 7_200_000), now)).toBe("in 2 hours");
    expect(relative(new Date(now + 40 * 86_400_000), now)).toBe("in 5 weeks");
  });

  it("says 'now' rather than putting a zero on the screen", () => {
    expect(relative(new Date(now), now)).toBe("now");
    expect(relative(new Date(now - 999), now)).toBe("now");
    expect(relative(new Date(now + 999), now)).toBe("now");
    expect(relative(new Date(now - 1000), now)).toBe("1 second ago");
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

describe("review bodies match the Hub's decoder", () => {
  // JSON.stringify is what postJSON puts on the wire. requireEmptyBody in
  // http.go reads the first byte and 400s if any is present -- it does not
  // parse JSON -- so "{}" from an empty object is a refusal, not an empty
  // body. The browser stub JSON.parsed that document back to {} and the
  // reopen test stayed green.
  it("does not serialise reopen as a JSON document", () => {
    expect(JSON.stringify(reviewBody("reopen", "reopening"))).toBeUndefined();
  });

  it("sends approve and reject a rationale, and cancel a reason", () => {
    expect(reviewBody("approve", "ok")).toEqual({ rationale: "ok" });
    expect(reviewBody("reject", "no")).toEqual({ rationale: "no" });
    expect(reviewBody("cancel", "superseded")).toEqual({ reason: "superseded" });
  });

  // The screen asked for a rationale before every action and promised it was
  // recorded. Reopen has nowhere to put one, so for that action the promise was
  // false and the text went nowhere.
  it("knows reopen carries no rationale and the other three do", () => {
    expect(carriesRationale("reopen")).toBe(false);
    expect(carriesRationale("approve")).toBe(true);
    expect(carriesRationale("reject")).toBe(true);
    expect(carriesRationale("cancel")).toBe(true);
  });

  // The Plans index refreshed after an approval and the Plan detail page did
  // not, so the two screens disagreed about the Plan just acted on. The key is
  // a prefix because this hook holds the Change Request's id, not the Plan's.
  it("makes the open Plan detail stale, not only the Plans index", () => {
    expect(reviewInvalidationKeys("crq_1")).toContainEqual(["plan"]);
    expect(reviewInvalidationKeys("crq_1")).toContainEqual(["plans"]);
    expect(reviewInvalidationKeys("crq_1")).toContainEqual(["change-request", "crq_1"]);
    expect(reviewInvalidationKeys("crq_1")).toContainEqual(["change-requests"]);
  });
});

describe("author-only review actions match the Hub's gates", () => {
  // The Hub admits Cancel and Reopen from the author alone, and only from the
  // statuses each transition can leave. Rendering them to every viewer made two
  // permanently non-functional controls -- which first demanded a rationale.
  it("offers neither action to a viewer who is not the author", () => {
    for (const status of ["PENDING_APPROVALS", "APPROVED", "REJECTED", "CANCELLED", "ERROR"]) {
      const settled = request({ status });
      expect(canCancel(settled, "reviewer")).toBe(false);
      expect(canReopen(settled, "reviewer")).toBe(false);
    }
  });

  it("offers neither action while the viewer is unknown", () => {
    // The session is a second fetch, so "" is what the page holds before it
    // lands. An empty username matching an empty author would offer both
    // actions to whoever the Hub is about to refuse.
    expect(canCancel(request({ status: "PENDING_APPROVALS" }), "")).toBe(false);
    expect(canReopen(request({ status: "CANCELLED", author: { username: "" } }), "")).toBe(false);
  });

  it("lets the author cancel only what the cancel route admits", () => {
    // http.go POST /v1/change-requests/{id}/cancel: PENDING_APPROVALS, APPROVED.
    expect(canCancel(request({ status: "PENDING_APPROVALS" }), "author")).toBe(true);
    expect(canCancel(request({ status: "APPROVED" }), "author")).toBe(true);
    expect(canCancel(request({ status: "REJECTED" }), "author")).toBe(false);
    expect(canCancel(request({ status: "CANCELLED" }), "author")).toBe(false);
    expect(canCancel(request({ status: "ERROR" }), "author")).toBe(false);
  });

  it("lets the author reopen only what Reopen admits", () => {
    // governance.go Reopen: REJECTED, CANCELLED, ERROR. ERROR is in that set
    // deliberately -- nobody vetoed an errored request, and the id is derived
    // from the target, so refusing it would burn that target permanently.
    expect(canReopen(request({ status: "REJECTED" }), "author")).toBe(true);
    expect(canReopen(request({ status: "CANCELLED" }), "author")).toBe(true);
    expect(canReopen(request({ status: "ERROR" }), "author")).toBe(true);
    expect(canReopen(request({ status: "PENDING_APPROVALS" }), "author")).toBe(false);
    expect(canReopen(request({ status: "APPROVED" }), "author")).toBe(false);
  });

  it("holds the two status sets apart", () => {
    // Sharing one set is the plausible simplification, and it would offer the
    // author Cancel on a cancelled request and Reopen on a pending one.
    expect(CANCELLABLE_STATUSES.some((status) => REOPENABLE_STATUSES.includes(status))).toBe(false);
  });
});

describe("plan detail polling", () => {
  // The Agent drives APPROVED -> LEASED -> RUNNING -> SUCCEEDED/FAILED with no
  // help from the console (internal/controlplane/lifecycle.go), and
  // AWAITING_APPROVAL moves the moment another reviewer approves. Polling only
  // RUNNING and LEASED therefore froze a Plan detail page opened at APPROVED
  // for the entire rollout, while the Plans index beside it kept moving.
  it("polls a Plan the Hub can still move, APPROVED included", () => {
    expect(planPollInterval("APPROVED")).toBe(5_000);
    expect(planPollInterval("AWAITING_APPROVAL")).toBe(5_000);
    expect(planPollInterval("LEASED")).toBe(5_000);
    expect(planPollInterval("RUNNING")).toBe(5_000);
  });

  it("stops at a status no transition leaves", () => {
    expect(planPollInterval("SUCCEEDED")).toBe(false);
    expect(planPollInterval("FAILED")).toBe(false);
    expect(planPollInterval("CANCELLED")).toBe(false);
    expect(planPollInterval("RECORDED")).toBe(false);
  });

  // A status the Hub never emits would be a terminal entry that silences
  // nothing, and the mistake is invisible in the assertions above because they
  // spell the same strings. PLAN_INTENT is held to the Go consts by
  // scripts/verify-console-contract.mjs, so comparing against it borrows that
  // gate's authority.
  it("names only statuses the Hub actually emits, and not all of them", () => {
    for (const status of PLAN_TERMINAL_STATUSES) {
      expect(Object.keys(PLAN_INTENT)).toContain(status);
    }
    expect(PLAN_TERMINAL_STATUSES.size).toBeLessThan(Object.keys(PLAN_INTENT).length);
  });

  it("does not poll before a Plan has loaded", () => {
    // React Query owns retrying the first fetch; polling here would re-ask for
    // a Plan id the Hub refuses every five seconds for as long as the tab is
    // open.
    expect(planPollInterval(undefined)).toBe(false);
  });
});

describe("notification inbox paging", () => {
  const page = (entries: unknown[] | null, limit: number) => ({
    owner: { kind: "USER", id: "operator" },
    channel: "IN_APP",
    limit,
    entries: entries as never,
  });

  // Go encodes an empty slice as null, so the state every new operator meets
  // first arrives as {"entries":null}. Reading it as an array is the difference
  // between an empty inbox and a screen that threw.
  it("reads the null Go sends for an empty inbox as no entries", () => {
    expect(inboxPage(page(null, 100)).entries).toEqual([]);
    expect(inboxPage(page(null, 100)).bounded).toBe(false);
  });

  // The Hub reports the limit it APPLIED, not the one asked for, precisely so a
  // caller can tell a truncated page from the end of the inbox. Losing that
  // distinction is how a console silently stops showing an operator half of
  // what happened to them.
  it("distinguishes a page that filled the limit from the end of the inbox", () => {
    expect(inboxPage(page([1, 2], 2)).bounded).toBe(true);
    expect(inboxPage(page([1], 2)).bounded).toBe(false);
  });

  // A limit of zero is not a bound the Hub applies -- application.Bound turns
  // every non-positive request into MaximumLimit -- so treating one as "this
  // page is full" would put a truncation notice on an empty inbox.
  it("does not call an empty inbox bounded when the Hub reports no limit", () => {
    expect(inboxPage(page([], 0)).bounded).toBe(false);
  });
});

describe("notification channel selection", () => {
  it("defaults to the channel whose delivery is the inbox row itself", () => {
    // domain.NeedsTransport: an IN_APP entry is delivered by being in the
    // inbox, so it is the one channel with content in a deployment that has
    // configured no transport at all.
    expect(readChannel(null)).toBe("IN_APP");
    expect(readChannel("")).toBe("IN_APP");
  });

  it("carries a channel the route accepts", () => {
    expect(readChannel("EMAIL")).toBe("EMAIL");
    expect(readChannel("IN_APP")).toBe("IN_APP");
  });

  // requestedChannel refuses a channel it does not know with a 400 rather than
  // defaulting, so forwarding one would turn a hand-edited link into what reads
  // as a broken console.
  it("falls back rather than forwarding a channel the Hub would refuse", () => {
    expect(readChannel("SLACK")).toBe("IN_APP");
    expect(readChannel("in_app")).toBe("IN_APP");
  });

  it("offers only channels the Hub has", () => {
    expect([...NOTIFICATION_CHANNELS]).toEqual(["EMAIL", "IN_APP"]);
  });
});

describe("notification preference edits", () => {
  it("keeps the enabled types sorted so one edit produces one document", () => {
    expect(toggleType(["PLAN_FAILURES"], "BLUE_GREEN_UPGRADE_FAILED", true)).toEqual([
      "BLUE_GREEN_UPGRADE_FAILED",
      "PLAN_FAILURES",
    ]);
    expect(toggleType(["PLAN_FAILURES"], "PLAN_FAILURES", true)).toEqual(["PLAN_FAILURES"]);
    expect(toggleType(["PLAN_FAILURES", "ROLE_CHANGE"], "PLAN_FAILURES", false)).toEqual([
      "ROLE_CHANGE",
    ]);
  });

  // domain.Preference.Validate refuses a preference that enables no types and
  // has not opted into future ones, because it could never deliver anything.
  // The screen refuses it first rather than offering a button whose only
  // outcome is a 422.
  it("refuses an edit the Hub would refuse", () => {
    expect(deliversSomething([], false)).toBe(false);
    expect(deliversSomething([], true)).toBe(true);
    expect(deliversSomething(["PLAN_FAILURES"], false)).toBe(true);
  });

  // The vocabulary is open and the Hub's is wider than any one bundle's, so a
  // stored preference may name a type this console has no checkbox for. It is
  // carried through a save, and reported, because silently dropping it would
  // unsubscribe an operator as a side effect of opening a settings page.
  it("names the enabled types this build has no checkbox for", () => {
    expect(unknownTypes(["PLAN_FAILURES"])).toEqual([]);
    expect(unknownTypes(["SOMETHING_ADDED_LATER", "PLAN_FAILURES"])).toEqual([
      "SOMETHING_ADDED_LATER",
    ]);
  });

  // Held to the Go consts by scripts/verify-console-contract.mjs; this asserts
  // the console's own use of that list is total, so a type added there cannot
  // arrive without a checkbox.
  it("has a checkbox for every type it claims to know", () => {
    for (const type of NOTIFICATION_EVENT_TYPES) {
      expect(unknownTypes([type])).toEqual([]);
    }
  });
});

/** The facet rail's arithmetic. The browser tests prove a rail renders and a
 * click reaches the URL; these are the parts a rendering test cannot see --
 * that the order is the one claimed, that a bound says what it left out, and
 * that a bar is a proportion of the right number. */
describe("facet rail arithmetic", () => {
  it("orders a dimension by count, largest first", () => {
    expect(
      orderFacetValues({ UNHEALTHY: 3, HEALTHY: 52, UNKNOWN: 7 }, undefined).map(
        (value) => value.value,
      ),
    ).toEqual(["HEALTHY", "UNKNOWN", "UNHEALTHY"]);
  });

  it("breaks equal counts on the declared vocabulary, then alphabetically", () => {
    // HEALTHY precedes UNHEALTHY in HEALTH, so an equal count must not reorder
    // them between renders. QUARANTINED is not in the vocabulary at all and
    // sorts after the declared values rather than vanishing.
    expect(
      orderFacetValues({ UNHEALTHY: 4, QUARANTINED: 4, HEALTHY: 4 }, undefined, HEALTH).map(
        (value) => value.value,
      ),
    ).toEqual(["HEALTHY", "UNHEALTHY", "QUARANTINED"]);
  });

  it("drops values the Hub counted at zero", () => {
    expect(orderFacetValues({ HEALTHY: 2, TERMINAL: 0 }, undefined).map((v) => v.value)).toEqual([
      "HEALTHY",
    ]);
  });

  // The facets exclude their own dimension, so a selection that matches nothing
  // still has to be visible: it is the only control that can undo itself.
  it("keeps a selected value present even with no count", () => {
    const values = orderFacetValues({ HEALTHY: 2 }, "TERMINAL", HEALTH);
    expect(values.map((value) => value.value)).toEqual(["HEALTHY", "TERMINAL"]);
    expect(values.find((value) => value.value === "TERMINAL")).toEqual({
      value: "TERMINAL",
      count: 0,
      selected: true,
    });
    expect(values.find((value) => value.value === "HEALTHY")?.selected).toBe(false);
  });

  it("bounds a long dimension and hands back what it withheld", () => {
    const values = orderFacetValues(
      Object.fromEntries(Array.from({ length: 12 }, (_, index) => [`env-${index}`, 12 - index])),
      undefined,
    );
    const bounded = boundFacetValues(values, 8);
    expect(bounded.shown).toHaveLength(8);
    // Returned, not discarded: the control that reveals them is labelled with
    // this list's length, so a silent truncation has nowhere to hide.
    expect(bounded.overflow.map((value) => value.value)).toEqual([
      "env-8",
      "env-9",
      "env-10",
      "env-11",
    ]);
    // The bound keeps the largest, which is the half a reader wants first.
    expect(bounded.shown[0]?.value).toBe("env-0");
  });

  it("leaves a dimension inside the bound alone and withholds nothing", () => {
    const values = orderFacetValues({ a: 3, b: 2 }, undefined);
    expect(boundFacetValues(values, 8)).toEqual({ shown: values, overflow: [] });
  });

  it("keeps a selected value visible even from below the bound", () => {
    const counts = Object.fromEntries(
      Array.from({ length: 12 }, (_, index) => [`env-${index}`, 12 - index]),
    );
    const bounded = boundFacetValues(orderFacetValues(counts, "env-11"), 8);
    expect(bounded.shown.map((value) => value.value)).toContain("env-11");
    // Eight from the head plus the selected one, and the withheld list is the
    // values actually withheld rather than the difference from the limit.
    expect(bounded.shown).toHaveLength(9);
    expect(bounded.overflow.map((value) => value.value)).toEqual(["env-8", "env-9", "env-10"]);
  });

  it("shows every value when the bound is lifted", () => {
    const values = orderFacetValues({ a: 3, b: 2, c: 1 }, undefined);
    expect(boundFacetValues(values, 0)).toEqual({ shown: values, overflow: [] });
  });

  // The denominator is the group's largest count, not its total: the bars
  // compare the values of one dimension against each other, and against a total
  // a dimension with many values renders every bar as an identical sliver.
  it("measures a bar against the largest count in its group", () => {
    const values = orderFacetValues({ HEALTHY: 50, UNHEALTHY: 25, UNKNOWN: 5 }, undefined);
    expect(largestFacetCount(values)).toBe(50);
    expect(facetBarValue(50, 50)).toBe(1);
    expect(facetBarValue(25, 50)).toBe(0.5);
    // 25/80 is what a total-based denominator would give. Naming it here makes
    // the wrong denominator a failing number rather than a plausible one.
    expect(facetBarValue(25, 50)).not.toBe(25 / 80);
  });

  it("draws nothing for a zero count and never divides by an empty group", () => {
    expect(largestFacetCount([])).toBe(0);
    expect(facetBarValue(0, 10)).toBe(0);
    expect(facetBarValue(4, 0)).toBe(0);
  });

  it("calls a rail empty only when every dimension is", () => {
    expect(facetsAreEmpty([{ key: "health", heading: "Health", values: [] }])).toBe(true);
    expect(
      facetsAreEmpty([
        { key: "health", heading: "Health", values: [] },
        { key: "product", heading: "Product", values: orderFacetValues({ a: 1 }, undefined) },
      ]),
    ).toBe(false);
  });
});

/** The fleet's own mapping onto that rail. What has to be right is which query
 * parameter each dimension writes: a browser test proving one facet click
 * filters would not prove the other five write the parameter they claim. */
describe("fleet facet groups", () => {
  const facets = {
    environment: { "prod-use1": 3, "dev-use2": 1 },
    product: { "app.example:payments": 4 },
    health: { HEALTHY: 3, UNHEALTHY: 1 },
    rolloutStatus: { UP_TO_DATE: 4 },
    configStatus: { APPLIED: 4 },
    lifecycle: { INSTALLED: 4 },
  };

  it("writes each dimension under the query parameter that selects it", () => {
    expect(fleetFacetGroups(facets, {}).map((group) => group.key)).toEqual([
      "environment",
      "product",
      "health",
      "rolloutStatus",
      "configStatus",
      "lifecycle",
    ]);
  });

  it("counts each dimension from its own facet and no other", () => {
    const groups = fleetFacetGroups(facets, {});
    const values = (key: string) =>
      groups.find((group) => group.key === key)?.values.map((v) => [v.value, v.count]);
    expect(values("health")).toEqual([
      ["HEALTHY", 3],
      ["UNHEALTHY", 1],
    ]);
    expect(values("environment")).toEqual([
      ["prod-use1", 3],
      ["dev-use2", 1],
    ]);
  });

  it("marks only the selected value of the dimension the URL selects", () => {
    const groups = fleetFacetGroups(facets, { health: "UNHEALTHY" });
    const selected = groups.flatMap((group) =>
      group.values.filter((value) => value.selected).map((value) => `${group.key}=${value.value}`),
    );
    expect(selected).toEqual(["health=UNHEALTHY"]);
  });
});

// The Environment README renderer. The parser is the injection boundary: an
// operator's Markdown becomes a data model here and only that model reaches
// React, so these tests pin the two properties that matter -- the supported
// subset is recognised, and an unsafe construct degrades rather than escaping.
describe("Environment README Markdown", () => {
  it("splits paragraphs, headings, lists and fenced code into blocks", () => {
    const blocks = parseMarkdown(
      "# Title\n\nA paragraph line\nand its continuation.\n\n- one\n- two\n\n```\ncode line\n```\n",
    );
    expect(blocks.map((block) => block.kind)).toEqual([
      "heading",
      "paragraph",
      "list",
      "code",
    ]);
    const [heading, paragraph, list, code] = blocks;
    // ATX depth clamps to h3-h5 so a README never emits an h1/h2 that competes
    // with the page's own object header.
    if (heading?.kind !== "heading") {
      throw new Error("expected a heading block");
    }
    expect(heading.level).toBe(3);
    // Continuation lines join into one paragraph rather than becoming two.
    if (paragraph?.kind !== "paragraph") {
      throw new Error("expected a paragraph block");
    }
    expect(paragraph.spans[0]).toEqual({
      kind: "text",
      text: "A paragraph line and its continuation.",
    });
    if (list?.kind !== "list") {
      throw new Error("expected a list block");
    }
    expect(list.items.length).toBe(2);
    if (code?.kind !== "code") {
      throw new Error("expected a code block");
    }
    expect(code.text).toBe("code line");
  });

  it("clamps deeper headings to level five", () => {
    const blocks = parseMarkdown("## Two\n\n#### Four");
    expect(blocks.map((block) => (block.kind === "heading" ? block.level : 0))).toEqual([4, 5]);
  });

  it("recognises strong, emphasis and code inline spans", () => {
    expect(parseInline("plain **bold** and *soft* and `mono`")).toEqual([
      { kind: "text", text: "plain " },
      { kind: "strong", text: "bold" },
      { kind: "text", text: " and " },
      { kind: "emphasis", text: "soft" },
      { kind: "text", text: " and " },
      { kind: "code", text: "mono" },
    ]);
  });

  it("keeps a safe link but strips a javascript: scheme to plain text", () => {
    expect(parseInline("see [the docs](https://example.test/x)")).toEqual([
      { kind: "text", text: "see " },
      { kind: "link", text: "the docs", href: "https://example.test/x" },
    ]);
    // An unsafe scheme must never become a live href; it degrades to the label
    // text so the README cannot smuggle script through a link.
    expect(parseInline("[click](javascript:evil)")).toEqual([{ kind: "text", text: "click" }]);
  });

  it("treats emphasis markers inside a code span as literal", () => {
    expect(parseInline("`a * b`")).toEqual([{ kind: "code", text: "a * b" }]);
  });
});

// Team contact kinds. The tab offers every kind the Hub accepts, and marks the
// ones it will not deliver to rather than hiding them -- mirroring
// ContactKind.Deliverable, where PAGERDUTY is creatable but undeliverable.
describe("Team contact kinds", () => {
  it("offers exactly the Hub's contact kinds", () => {
    expect([...TEAM_CONTACT_KINDS]).toEqual(["EMAIL", "SLACK", "PAGERDUTY"]);
  });

  it("marks PAGERDUTY undeliverable while EMAIL and SLACK deliver", () => {
    expect(contactDeliverable("EMAIL")).toBe(true);
    expect(contactDeliverable("SLACK")).toBe(true);
    expect(contactDeliverable("PAGERDUTY")).toBe(false);
    // An unknown kind is not something the Hub delivers to either.
    expect(contactDeliverable("CARRIER_PIGEON")).toBe(false);
  });
});

// The cross-Release vulnerability inventory (Risk Management). It surfaces the
// Hub's own active findings, so the tests pin the ordering, bucketing and
// filtering rather than any re-derivation of what "active" means.
describe("Vulnerability inventory", () => {
  const row = (product: string, version: string, finding: VulnerabilityFinding): ActiveFindingRow => ({
    product,
    version,
    finding,
  });

  it("ranks the Hub's tiers off the score, including a severity-only finding", () => {
    // A CRITICAL finding with no CVSS scores 9.0 at the Hub; it must rank above a
    // 0.5-CVSS finding, not below it -- the case that inverts if you order on
    // CVSS alone.
    expect(severityTier({ id: "a", severity: "CRITICAL" })).toBe("CRITICAL");
    expect(severityTier({ id: "b", cvss: 0.5 })).toBe("LOW");
    expect(severityTier({ id: "c", cvss: 7.5 })).toBe("HIGH");
    // Unparsed severity is HIGH at the Hub, not harmless.
    expect(severityTier({ id: "d", severity: "???" })).toBe("HIGH");
    expect(severityTier({ id: "e", severity: "NONE" })).toBe("NONE");
  });

  it("orders rows worst-first with a stable Product/version/CVE tie-break", () => {
    const rows = [
      row("app.z", "1.0.0", { id: "CVE-3", cvss: 5 }),
      row("app.a", "1.0.0", { id: "CVE-2", cvss: 9.8 }),
      row("app.a", "1.0.0", { id: "CVE-1", cvss: 9.8 }),
    ];
    expect(orderInventoryRows(rows).map((r) => r.finding.id)).toEqual(["CVE-1", "CVE-2", "CVE-3"]);
  });

  it("offers only the tiers present, worst-first, and filters to one", () => {
    const rows = [
      row("app.a", "1.0.0", { id: "CVE-1", cvss: 9.8 }),
      row("app.b", "2.0.0", { id: "CVE-2", cvss: 5 }),
      row("app.b", "2.0.0", { id: "CVE-3", cvss: 5 }),
    ];
    expect(inventoryTiers(rows)).toEqual(["CRITICAL", "MEDIUM"]);
    expect(filterInventoryByTier(rows, "MEDIUM").map((r) => r.finding.id)).toEqual(["CVE-2", "CVE-3"]);
    // No tier selected returns everything.
    expect(filterInventoryByTier(rows, undefined)).toHaveLength(3);
  });
});

describe("installation source", () => {
  const row = (overrides: Record<string, unknown> = {}) =>
    ({
      environment: "prod-use1",
      installationId: "payments",
      product: "app.example:payments",
      namespace: "payments",
      health: "UNKNOWN",
      rolloutStatus: "OUT_OF_DATE",
      configStatus: "INDETERMINATE",
      lifecycle: "UNKNOWN",
      stale: true,
      ...overrides,
    }) as Parameters<typeof installationSource>[0];

  // The wire shape is not hypothetical: marshalling a desired-only row through
  // BuildFleet produces exactly this, Go's zero time included, because
  // encoding/json's omitempty does not omit a struct.
  it("reads Go's zero observedAt as never reported, not as a report in year 1", () => {
    expect(
      installationSource(row({ desiredVersion: "2.4.1", observedAt: "0001-01-01T00:00:00Z" })),
    ).toBe("DECLARED_ONLY");
    expect(hasInstant("0001-01-01T00:00:00Z")).toBe(false);
    expect(hasInstant(undefined)).toBe(false);
    expect(hasInstant("")).toBe(false);
    expect(hasInstant("2026-08-07T12:00:00Z")).toBe(true);
  });

  it("separates the two sides of the fleet merge", () => {
    expect(
      installationSource(
        row({ desiredVersion: "2.4.1", observedVersion: "2.4.1", observedAt: "2026-08-07T12:00:00Z" }),
      ),
    ).toBe("DECLARED_AND_REPORTED");
    expect(installationSource(row({ desiredVersion: "2.4.1" }))).toBe("DECLARED_ONLY");
    // Something is running that nothing declared.
    expect(
      installationSource(row({ observedVersion: "2.4.1", observedAt: "2026-08-07T12:00:00Z" })),
    ).toBe("REPORTED_ONLY");
  });

  it("calls an undeclared installation unmanaged, and says so in the title", () => {
    expect(installationSourceLabel("REPORTED_ONLY").label).toBe("unmanaged");
    expect(installationSourceLabel("REPORTED_ONLY").intent).toBe("warning");
    // The placeholder warning is the whole point of the declared-only tag.
    expect(installationSourceLabel("DECLARED_ONLY").title).toContain("placeholders");
  });
});

describe("installation RID", () => {
  it("splits a RID the Hub's search cannot match into filters it can", () => {
    expect(parseInstallationRID("installation/prod-use1/payments")).toEqual({
      environment: "prod-use1",
      installationId: "payments",
    });
    expect(parseInstallationRID("  installation/prod-use1/payments  ")).toEqual({
      environment: "prod-use1",
      installationId: "payments",
    });
  });

  it("leaves anything that is not an installation RID alone", () => {
    // A plain search term, another RID kind, and a malformed one. Silently
    // truncating any of these would search for something nobody typed.
    expect(parseInstallationRID("payments")).toBeUndefined();
    expect(parseInstallationRID("environment/prod-use1")).toBeUndefined();
    expect(parseInstallationRID("installation/prod-use1")).toBeUndefined();
    expect(parseInstallationRID("installation/prod-use1/payments/extra")).toBeUndefined();
    expect(parseInstallationRID("installation//payments")).toBeUndefined();
    expect(parseInstallationRID("installation/prod-use1/")).toBeUndefined();
  });
});

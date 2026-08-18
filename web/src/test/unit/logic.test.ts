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
import { latestForChannel, orderChannels } from "../../components/ReleaseChannelGraph.js";
import { statusIntent } from "../../api/imports.js";
import { findingScore, orderFindingsByHubScore } from "../../api/catalog.js";
import type { VulnerabilityFinding } from "../../api/catalog.js";
import { pipelineStatus, pipelineStatusIntent } from "../../api/exports.js";
import { NOTIFICATION_CHANNELS, NOTIFICATION_EVENT_TYPES, inboxPage } from "../../api/notifications.js";
import {
  deliversSomething,
  readChannel,
  toggleType,
  unknownTypes,
} from "../../features/notifications/NotificationsPage.js";
import { HEALTH_INTENT, ROLLOUT_INTENT, CONFIG_INTENT, LIFECYCLE_INTENT } from "../../components/statusTags.js";
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

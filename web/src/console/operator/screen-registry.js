/*
 * One classification per workspace screen. The engine builds its renderer
 * tables through classifyRenderers, so a screen cannot be painted without an
 * entry here, and the header badge reads the same entry.
 *
 *   live    — every row on the view is read from the Hub.
 *   example — the view is a local prototype; nothing on it is a Hub record.
 *   mixed   — Hub records and example content share the view.
 *
 * selfLabelled: the view labels each section Hub or Example itself, so the
 *   header badge is hidden rather than making one claim for the whole view.
 * liveWhen: an example view that becomes live once that evidence exists.
 * indexes: the bounded Hub indexes the view lists, for the truncation notice.
 */
const live = (...indexes) => Object.freeze({ data: "live", indexes });
const example = Object.freeze({ data: "example", indexes: [] });
const sectioned = Object.freeze({
  data: "mixed",
  selfLabelled: true,
  indexes: [],
});

export const SCREEN_REGISTRY = Object.freeze({
  "s01-intake": live(),
  "s02-workspaces": live(),
  "s03-saved-views": example,
  "s04-scope-plan": example,
  "s05-backlog": example,
  "s06-active-work": example,
  "s07-pipeline": live(),
  "s08-staged-run": live(),
  "s09-source": live(),
  "s10-diff": live("changes"),
  "s11-preview": live(),
  "s12-lifecycle": live(),
  "s13-fleet-overview": example,
  "s14-env-detail": live("environments", "installations", "plans"),
  "s15-plan-detail": live("plans"),
  "s16-approvals-board": live("plans"),
  "s17-spokes-audit": live("environments", "installations"),
  "s18-components": example,
  "s19-ontology": live(),
  "rm01-env-page": example,
  "rm02-env-activity": example,
  "rm03-plan-detail": live("plans"),
  "rm04-constraints": live("changes"),
  "rm05-release-rollout": live("installations"),
  "rm06-promotion-pipe": live("promotions"),
  "rm07-change-requests": live("changes"),
  "rm08-bundle-preview": live(),
  "rm09-recalls": live("plans"),
  "rm09-fleet-scheduler": live("environments", "installations", "plans"),
  "rm10-ramped-rollout": example,
  "rm11-debug-terminal": example,
  "rm-channels": live(),
  "rm-waves": live("promotions"),
  "rel-products": live("products", "installations"),
  "rel-dependencies": live("products"),
  "flt-environments": live("environments", "installations"),
  "cc-overview": live("environments", "installations", "changes", "plans"),
  "pkg-runs": live(),
  "au01-audit": live(),
  "adm-platform-status": live(),
  "adm01-user-prefs": example,
  "adm02-org-control-panel": sectioned,
  "adm03-auth-signin": example,
  "adm04-data-sources": example,
  "ai01-logic": live(),
  "ai02-evals": live(),
  "ai03-events": live(),
  "ai04-courses": live(),
  "ntf-inbox": live(),
  "pt01-proposal-trace": Object.freeze({
    data: "example",
    liveWhen: "packagingRuns",
    indexes: [],
  }),
  "pt02-proposals-evals": sectioned,
  "og01-ontology-graph": Object.freeze({
    data: "example",
    liveWhen: "ontologyGraph",
    indexes: [],
  }),
});

const DATA_MODES = new Set(["live", "example", "mixed"]);

export function screenEntry(screen) {
  return Object.hasOwn(SCREEN_REGISTRY, screen)
    ? SCREEN_REGISTRY[screen]
    : null;
}

/** Renderer keys with no valid classification. Empty when the table is sound. */
export function unclassifiedScreens(keys) {
  return [...keys].filter((key) => !DATA_MODES.has(screenEntry(key)?.data));
}

/**
 * The only way the engine obtains a renderer table. An unclassified key is a
 * programming error and stops the render, where the contained failure state
 * and the unit test both name it.
 */
export function classifyRenderers(table) {
  const missing = unclassifiedScreens(Object.keys(table));
  if (missing.length)
    throw new Error(
      `Screen has a renderer but no data classification: ${missing.join(", ")}`,
    );
  return table;
}

const LIVE_EVIDENCE = {
  packagingRuns: (context) =>
    (context.hub?.packaging || []).some((run) => run?.runId),
  ontologyGraph: (context) => context.graphLive === true,
};

/**
 * What the header badge says for a screen.
 * context: { hub, graphLive, unavailable, failed }
 *   unavailable — example content exists but example mode is off, or the
 *                 screen has no renderer.
 *   failed      — the renderer threw; nothing on view is a Hub read.
 */
export function screenBadge(screen, context = {}) {
  if (context.failed)
    return {
      mode: "unavailable",
      text: "UNAVAILABLE",
      title: "This view failed to render",
      hidden: false,
    };
  const entry = screenEntry(screen);
  if (entry?.selfLabelled)
    return { mode: "sections", text: "", title: "", hidden: true };
  const evidence = entry?.liveWhen ? LIVE_EVIDENCE[entry.liveWhen] : null;
  const data = evidence?.(context) ? "live" : entry?.data || "example";
  if (data === "live")
    return {
      mode: "live",
      text: "LIVE",
      title: "This view is read from the Hub",
      hidden: false,
    };
  if (context.unavailable)
    return {
      mode: "unavailable",
      text: "UNAVAILABLE",
      title:
        "No native operational data is available for this view; example mode is off",
      hidden: false,
    };
  if (data === "mixed")
    return {
      mode: "mixed",
      text: "MIXED DATA",
      title: "This view combines Hub records with example data",
      hidden: false,
    };
  return {
    mode: "example",
    text: "EXAMPLE DATA",
    title: "This view shows example data, not this Hub's records",
    hidden: false,
  };
}

const INDEX_LABELS = Object.freeze({
  environments: "environments",
  installations: "installations",
  products: "products",
  changes: "change requests",
  promotions: "promotions",
  plans: "plans",
});

/**
 * One sentence per index that stopped at the read bound. The total is the
 * Hub's own count when it returned one, and is never estimated otherwise.
 */
export function truncationNotices(hub, screen) {
  const marks = hub?.truncated || {};
  return (screenEntry(screen)?.indexes || [])
    .filter((index) => marks[index])
    .map((index) => {
      const { shown, total } = marks[index];
      const label = INDEX_LABELS[index] || index;
      return Number.isSafeInteger(total) && total > shown
        ? `Showing the first ${shown} of ${total} ${label}. The rest are not loaded in this view.`
        : `Showing the first ${shown} ${label}; the Hub has more that are not loaded in this view.`;
    });
}

export function truncationMarkup(hub, screen, esc) {
  const notices = truncationNotices(hub, screen);
  return notices.length
    ? `<div role="status" data-index-truncation class="so-warn-card p-2 text-[12px]">${notices.map((notice) => `<p>${esc(notice)}</p>`).join("")}</div>`
    : "";
}

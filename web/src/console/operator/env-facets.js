// Environment facets: counts per facet value with a proportional bar, and the
// environments that match every selected value. Built from /v1/environments,
// which live-hub.js already loads; nothing here writes.

import { emptyState } from "./empty-states.js";
import { freshnessPill, reportTime } from "./freshness.js";
const defaultEsc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );

const HEALTH_ORDER = [
  "UNHEALTHY",
  "DEGRADED",
  "PROGRESSING",
  "UNKNOWN",
  "HEALTHY",
];

/** Worst health across an environment's reported installations. */
function worstHealth(row) {
  const entities = row?.reported?.entities;
  if (!Array.isArray(entities) || !entities.length) return "No report";
  let worst = "HEALTHY";
  for (const entity of entities) {
    const value = String(entity?.health || "UNKNOWN").toUpperCase();
    const rank = HEALTH_ORDER.indexOf(value);
    if (rank !== -1 && rank < HEALTH_ORDER.indexOf(worst)) worst = value;
  }
  return worst;
}

function rollout(row) {
  const entities = row?.reported?.entities;
  if (!Array.isArray(entities) || !entities.length) return "No report";
  return entities.every((entity) => entity?.rolloutStatus === "UP_TO_DATE")
    ? "Up to date"
    : "Behind";
}

function maintenance(row) {
  const spec = row?.environment?.maintenance || {};
  return Object.keys(spec).length ? "Window set" : "No window";
}

export const FACETS = Object.freeze([
  {
    key: "spoke",
    label: "Spoke",
    value: (row) => row?.spoke?.state || "unknown",
  },
  { key: "health", label: "Health", value: worstHealth },
  { key: "rollout", label: "Rollout", value: rollout },
  { key: "maintenance", label: "Maintenance", value: maintenance },
]);

/** Counts per value of one facet, largest first, ties by name. */
export function countBy(rows, valueOf) {
  const counts = new Map();
  for (const row of Array.isArray(rows) ? rows : []) {
    const key = String(valueOf(row) ?? "Unspecified");
    counts.set(key, (counts.get(key) ?? 0) + 1);
  }
  return [...counts.entries()]
    .map(([group, count]) => ({ group, count }))
    .sort((a, b) => b.count - a.count || a.group.localeCompare(b.group));
}

/** Rows matching every selected facet value. */
export function applyFacets(rows, selected = {}) {
  const active = FACETS.filter((facet) => selected[facet.key]);
  return (Array.isArray(rows) ? rows : []).filter((row) =>
    active.every((facet) => String(facet.value(row)) === selected[facet.key]),
  );
}

export function facetsMarkup(
  rows,
  selected = {},
  { esc = defaultEsc, note = "", now = Date.now() } = {},
) {
  const list = Array.isArray(rows) ? rows : [];
  if (!list.length) {
    return emptyState(
      "environments",
      note || "The Hub returned no environments.",
      { esc },
    );
  }
  const groups = FACETS.map((facet) => {
    const counts = countBy(list, facet.value);
    const items = counts
      .map(({ group, count }) => {
        const active = selected[facet.key] === group;
        const pct = Math.round((count / list.length) * 100);
        return `<button type="button" class="kit-facet-row${active ? " is-active" : ""}" data-facet="${esc(facet.key)}" data-facet-value="${esc(group)}" aria-pressed="${active}">
          <span class="kit-facet-name">${esc(group)}</span><span class="kit-facet-count">${count}</span>
          <span class="kit-facet-track"><span class="kit-facet-fill" style="width:${pct}%"></span></span>
        </button>`;
      })
      .join("");
    return `<div class="kit-facet-group"><div class="kit-facet-label">${esc(facet.label)}</div>${items}</div>`;
  }).join("");
  const matched = applyFacets(list, selected);
  const anySelected = FACETS.some((facet) => selected[facet.key]);
  const rowsHtml = matched.length
    ? matched
        .map(
          (row) => `<tr>
        <td><button type="button" class="kit-link code-id" data-env-open="${esc(row.environment?.id)}">${esc(row.environment?.id)}</button></td>
        ${FACETS.map((facet) => `<td>${esc(facet.value(row))}</td>`).join("")}
        <td class="code-id">${esc(row.spoke?.agentId || "")}</td>
        <td>${freshnessPill(reportTime(row.reported?.observedAt, row.spoke?.observedAt), { esc, now })}</td>
      </tr>`,
        )
        .join("")
    : `<tr><td colspan="${FACETS.length + 3}" class="kit-muted">No environment matches the selected facets.</td></tr>`;
  return `<div class="kit-facet-layout">
    <aside class="so-panel kit-facet-side" aria-label="Environment facets">
      <div class="kit-view-head"><h2>Facets</h2>${anySelected ? `<button type="button" class="so-btn so-btn-sm" data-facet-clear>Clear</button>` : ""}</div>
      ${groups}
    </aside>
    <div class="so-panel kit-view-panel">
      <div class="kit-view-head"><h2>Environments</h2><span class="kit-muted">${matched.length} of ${list.length}</span></div>
      <div class="overflow-x-auto"><table class="so-table">
        <thead><tr><th>Environment</th>${FACETS.map((facet) => `<th>${esc(facet.label)}</th>`).join("")}<th>Agent</th><th>Last report</th></tr></thead>
        <tbody>${rowsHtml}</tbody>
      </table></div>
    </div>
  </div>`;
}

export const DETAIL_TABS = Object.freeze([
  ["state", "State & drift"],
  ["workloads", "Reported workloads"],
  ["constraints", "Maintenance"],
]);

/** The Hub writes Go's zero time for "never"; treat it as absent. */
export function when(value) {
  const text = String(value || "");
  return text && !text.startsWith("0001-01-01") ? text : "";
}

/** Desired against observed for one installation, from its own fields. */
export function installationDrift(row) {
  const observed =
    when(row?.observedAt) ||
    row?.observedVersion ||
    row?.observedReleaseDigest ||
    row?.observedConfigDigest;
  if (!observed) return "No report";
  if (row?.configStatus === "DRIFTED") return "Drifted";
  if (
    row?.desiredReleaseDigest &&
    row?.observedReleaseDigest &&
    row.desiredReleaseDigest !== row.observedReleaseDigest
  )
    return "Drifted";
  if (
    row?.desiredVersion &&
    row?.observedVersion &&
    row.desiredVersion !== row.observedVersion
  )
    return "Drifted";
  return "In sync";
}

/**
 * One environment, reached from the list: desired against reported state,
 * the workloads its agent reported, and its maintenance spec. Everything is
 * read from /v1/environments and /v1/installations; nothing here writes.
 */
export function envDetailMarkup(
  row,
  installations,
  { tab = "state", esc = defaultEsc, now = Date.now() } = {},
) {
  const id = row?.environment?.id || "";
  const back = `<button type="button" class="kit-link" data-env-back>&larr; All environments</button>`;
  if (!row) {
    return `<div class="so-panel kit-view-panel">${back}<p class="kit-muted">That environment is no longer in the Hub's list.</p></div>`;
  }
  const mine = (Array.isArray(installations) ? installations : []).filter(
    (item) => item?.environment === id,
  );
  const entities = Array.isArray(row?.reported?.entities)
    ? row.reported.entities
    : [];
  const drifted = mine.filter((item) => installationDrift(item) === "Drifted");
  const current = DETAIL_TABS.some(([key]) => key === tab) ? tab : "state";
  const tabs = DETAIL_TABS.map(
    ([key, label]) =>
      `<button type="button" class="kit-tab${key === current ? " is-active" : ""}" data-env-tab="${key}" aria-pressed="${key === current}">${esc(label)}</button>`,
  ).join("");
  let body = "";
  if (current === "state") {
    body = mine.length
      ? `<table class="so-table"><thead><tr><th>Product</th><th>Desired</th><th>Observed</th><th>State</th><th>Observed at</th></tr></thead><tbody>${mine
          .map(
            (item) => `<tr>
          <td>${esc(item.product)}</td>
          <td class="code-id">${esc(item.desiredVersion || "")}</td>
          <td class="code-id">${esc(item.observedVersion || "")}</td>
          <td>${esc(installationDrift(item))}</td>
          <td>${freshnessPill(item.observedAt, { esc, now, stale: item.stale })}</td>
        </tr>`,
          )
          .join("")}</tbody></table>`
      : `<p class="kit-muted">No installation targets this environment.</p>`;
  } else if (current === "workloads") {
    body = entities.length
      ? `<table class="so-table"><thead><tr><th>Product</th><th>Version</th><th>Namespace</th><th>Health</th><th>Rollout</th><th>Config</th></tr></thead><tbody>${entities
          .map(
            (entity) => `<tr>
          <td>${esc(entity.product || entity.installationId || "")}</td>
          <td class="code-id">${esc(entity.version || "")}</td>
          <td class="code-id">${esc(entity.namespace || "")}</td>
          <td>${esc(entity.health || "UNKNOWN")}</td>
          <td>${esc(entity.rolloutStatus || "")}</td>
          <td>${esc(entity.configStatus || "")}</td>
        </tr>`,
          )
          .join("")}</tbody></table>`
      : `<p class="kit-muted">This environment's agent has not reported any workloads.</p>`;
  } else {
    const spec = row?.environment?.maintenance || {};
    const keys = Object.keys(spec);
    body = keys.length
      ? `<dl class="kit-kv">${keys
          .map(
            (key) =>
              `<dt>${esc(key)}</dt><dd class="code-id">${esc(typeof spec[key] === "string" ? spec[key] : JSON.stringify(spec[key]))}</dd>`,
          )
          .join("")}</dl>`
      : `<p class="kit-muted">No maintenance window is set, so changes are not held to one.</p>`;
  }
  return `<div class="space-y-3">
    <div>${back}</div>
    <div class="so-panel kit-view-panel">
      <div class="kit-view-head"><h2 class="code-id">${esc(id)}</h2>
        <span class="kit-muted">Spoke ${esc(row?.spoke?.state || "unknown")} &middot; ${esc(worstHealth(row))} &middot; ${drifted.length} of ${mine.length} drifted &middot; last report</span> ${freshnessPill(reportTime(row?.reported?.observedAt, row?.spoke?.observedAt), { esc, now })}
      </div>
      <div class="kit-subnav" role="group" aria-label="Environment views">${tabs}</div>
      <div class="overflow-x-auto">${body}</div>
    </div>
  </div>`;
}

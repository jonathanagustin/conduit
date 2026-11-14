// Environment facets: counts per facet value with a proportional bar, and the
// environments that match every selected value. Built from /v1/environments,
// which live-hub.js already loads; nothing here writes.

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
  { esc = defaultEsc, note = "" } = {},
) {
  const list = Array.isArray(rows) ? rows : [];
  if (!list.length) {
    return `<div class="so-panel kit-view-panel"><p class="kit-muted">${esc(note || "The Hub returned no environments.")}</p></div>`;
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
        <td class="code-id">${esc(row.environment?.id)}</td>
        ${FACETS.map((facet) => `<td>${esc(facet.value(row))}</td>`).join("")}
        <td class="code-id">${esc(row.spoke?.agentId || "")}</td>
      </tr>`,
        )
        .join("")
    : `<tr><td colspan="${FACETS.length + 2}" class="kit-muted">No environment matches the selected facets.</td></tr>`;
  return `<div class="kit-facet-layout">
    <aside class="so-panel kit-facet-side" aria-label="Environment facets">
      <div class="kit-view-head"><h2>Facets</h2>${anySelected ? `<button type="button" class="so-btn so-btn-sm" data-facet-clear>Clear</button>` : ""}</div>
      ${groups}
    </aside>
    <div class="so-panel kit-view-panel">
      <div class="kit-view-head"><h2>Environments</h2><span class="kit-muted">${matched.length} of ${list.length}</span></div>
      <div class="overflow-x-auto"><table class="so-table">
        <thead><tr><th>Environment</th>${FACETS.map((facet) => `<th>${esc(facet.label)}</th>`).join("")}<th>Agent</th></tr></thead>
        <tbody>${rowsHtml}</tbody>
      </table></div>
    </div>
  </div>`;
}

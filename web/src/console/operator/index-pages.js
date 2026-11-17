import { screenEntry } from "./screen-registry.js";

export const INDEX_LIMIT = 20;
export const INDEX_DEFINITIONS = Object.freeze({
  environments: {
    path: "/v1/environments",
    key: "environments",
    label: "environments",
    view: "summary",
    filters: ["search"],
  },
  products: {
    path: "/v1/products",
    key: "products",
    label: "products",
    view: "summary",
    filters: ["search"],
  },
  plans: {
    path: "/v1/plans",
    key: "plans",
    label: "plans",
    view: "summary",
    filters: ["search", "environment", "status", "installationId"],
  },
  installations: {
    path: "/v1/fleet/installations",
    key: "installations",
    label: "installations",
    filters: [
      "search",
      "environment",
      "product",
      "health",
      "rolloutStatus",
      "configStatus",
      "lifecycle",
    ],
  },
  changes: {
    path: "/v1/change-requests",
    key: "changeRequests",
    label: "change requests",
    filters: ["search", "environment", "status"],
  },
  promotions: {
    path: "/v1/promotions",
    key: "promotions",
    label: "promotions",
    filters: ["search", "product", "status"],
  },
  packaging: {
    key: "items",
    label: "packaging runs",
    view: "summary",
    filters: ["search", "phase", "outcome", "planId"],
  },
});

const EXTRA_READS = Object.freeze({
  "cc-overview": ["workspaces", "packaging", "audit"],
  "s14-env-detail": ["plans", "products", "timeline", "blackout"],
  "s15-plan-detail": ["changes"],
  "rm03-plan-detail": ["changes"],
  "s16-approvals-board": ["changes"],
  "s17-spokes-audit": ["upgrades"],
  "rm09-fleet-scheduler": ["blackout", "timeline"],
  "rm05-release-rollout": ["rollouts"],
  "rm-waves": ["rollouts"],
  "rm10-ramped-rollout": ["rollouts"],
  "rm06-promotion-pipe": ["products", "environments"],
  "rm-channels": ["channels", "products", "environments"],
  "s02-workspaces": ["workspaces"],
  "s09-source": ["workspaces"],
  "s11-preview": ["workspaces"],
  "s12-lifecycle": ["workspaces"],
  "pkg-runs": ["packaging"],
  "s07-pipeline": ["packaging"],
  "s08-staged-run": ["packaging"],
  "pt01-proposal-trace": ["packaging"],
  "pt02-proposals-evals": ["packaging"],
  "rm08-bundle-preview": ["bundles", "environments"],
  "au01-audit": ["audit"],
  "ai01-logic": ["installations", "changes"],
  "ai04-courses": ["installations", "changes"],
});

/** Data dependencies describe the visible view, never every index on the Hub. */
export function screenReads(screen) {
  return new Set([
    ...(screenEntry(screen)?.indexes || []),
    ...(EXTRA_READS[screen] || []),
  ]);
}

export function routeQuery(hash = "") {
  const value = String(hash);
  const cut = value.indexOf("?");
  return new URLSearchParams(cut < 0 ? "" : value.slice(cut + 1));
}

export function indexFilters(index, query) {
  const definition = INDEX_DEFINITIONS[index];
  const params =
    query instanceof URLSearchParams ? query : new URLSearchParams(query || "");
  return Object.fromEntries(
    ["cursor", ...definition.filters].map((key) => [
      key,
      params.get(`${index}.${key}`) || "",
    ]),
  );
}

/** A filter changes the result set, so neither its cursor nor selection survives. */
export function changeIndexQuery(query, index, changes) {
  const next = new URLSearchParams(query || "");
  const definition = INDEX_DEFINITIONS[index];
  if (!definition) return next;
  for (const key of definition.filters) {
    if (!Object.hasOwn(changes, key)) continue;
    const value = String(changes[key] || "");
    if (value) next.set(`${index}.${key}`, value);
    else next.delete(`${index}.${key}`);
  }
  if (definition.filters.some((key) => Object.hasOwn(changes, key))) {
    next.delete(`${index}.cursor`);
    const selected = {
      plans: "plan",
      environments: "environment",
      changes: "change",
      packaging: "run",
    }[index];
    if (selected) next.delete(selected);
  } else if (Object.hasOwn(changes, "cursor")) {
    if (changes.cursor) next.set(`${index}.cursor`, String(changes.cursor));
    else next.delete(`${index}.cursor`);
    const selected = {
      plans: "plan",
      environments: "environment",
      changes: "change",
      packaging: "run",
    }[index];
    if (selected) next.delete(selected);
  }
  return next;
}

export function indexRequest(index, query, extra = {}) {
  const definition = INDEX_DEFINITIONS[index];
  const filters = indexFilters(index, query);
  const params = new URLSearchParams();
  if (definition.view) params.set("view", definition.view);
  params.set("limit", String(INDEX_LIMIT));
  for (const [key, value] of Object.entries({ ...filters, ...extra })) {
    if (value !== "" && value !== undefined && value !== null)
      params.set(key, String(value));
  }
  return `${definition.path}?${params}`;
}

export function indexPage(body, key, url) {
  if (
    !body ||
    !Array.isArray(body[key]) ||
    body[key].length > INDEX_LIMIT ||
    body[key].some((row) => !row || typeof row !== "object")
  )
    throw new Error("Invalid Hub index");
  const cursor = (key) => {
    if (body[key] === undefined || body[key] === "") return "";
    if (
      typeof body[key] !== "string" ||
      new TextEncoder().encode(body[key]).length > 2048
    )
      throw new Error("Invalid Hub cursor");
    return body[key];
  };
  // FleetProjection keeps its authorized, filtered count in FleetSummary.
  const reportedTotal =
    key === "installations" ? body.summary?.total : body.total;
  const total =
    typeof reportedTotal === "bigint" && reportedTotal >= 0n
      ? reportedTotal.toString()
      : Number.isSafeInteger(reportedTotal) && reportedTotal >= 0
        ? reportedTotal
        : null;
  return {
    rows: body[key],
    shown: body[key].length,
    total,
    nextCursor: cursor("nextCursor"),
    previousCursor: cursor("previousCursor"),
    hasPrevious: body.hasPrevious === true,
    url,
    error: "",
    filtered: [...new URL(url, "http://hub.local").searchParams.keys()].some(
      (key) => !["view", "limit", "cursor"].includes(key),
    ),
  };
}

/** Rich views expand one visible page, with at most four reads in flight. */
export async function expandIndexRows(index, rows, read) {
  const result = Array.from({ length: rows.length });
  let position = 0;
  async function worker() {
    while (position < rows.length) {
      const at = position++;
      const row = rows[at];
      if (index === "plans" && !row.plan) {
        const full = await read(`/v1/plans/${encodeURIComponent(row.id)}`);
        if (full?.id !== row.id || !full.plan)
          throw new Error("Plan details unavailable");
        result[at] = full;
      } else if (
        index === "environments" &&
        Number.isSafeInteger(row.reported?.entityCount) &&
        !Array.isArray(row.reported.entities)
      ) {
        const full = await read(
          `/v1/environments/${encodeURIComponent(row.environment?.id || "")}`,
        );
        if (full?.environment?.id !== row.environment?.id)
          throw new Error("Environment details unavailable");
        result[at] = full;
      } else if (index === "products" && !Array.isArray(row.releases)) {
        const full = await read(
          `/v1/products/${encodeURIComponent(row.product)}/releases`,
        );
        if (!Array.isArray(full?.releases))
          throw new Error("Product releases unavailable");
        result[at] = { ...row, releases: full.releases };
      } else result[at] = row;
    }
  }
  await Promise.all(
    Array.from({ length: Math.min(4, rows.length) }, () => worker()),
  );
  return result;
}

export async function readIndexPage(index, query, read, extra = {}) {
  const definition = INDEX_DEFINITIONS[index];
  const url = indexRequest(index, query, extra);
  const page = indexPage(await read(url), definition.key, url);
  page.rows = await expandIndexRows(index, page.rows, read);
  return page;
}

const FILTER_LABELS = {
  search: "Search",
  environment: "Environment",
  status: "Status",
  product: "Product",
  installationId: "Installation",
  health: "Health",
  rolloutStatus: "Rollout",
  configStatus: "Configuration",
  lifecycle: "Lifecycle",
  phase: "Phase",
  outcome: "Outcome",
  planId: "Plan",
};

export function indexControlsMarkup(hub, screen, query, esc) {
  const reads = screenReads(screen);
  return [...reads]
    .filter((index) => INDEX_DEFINITIONS[index])
    .map((index) => {
      const definition = INDEX_DEFINITIONS[index];
      const page = hub?.indexPages?.[index];
      const filters = indexFilters(index, query);
      const count = page
        ? `Showing ${page.shown}${page.total === null ? "" : ` of ${page.total}`} ${definition.label} in this page.`
        : `Loading ${definition.label}.`;
      return `<section class="so-panel kit-index-controls" data-index-controls="${index}" aria-label="${esc(definition.label)} pages">
      <form data-index-filter="${index}" class="kit-index-filters">${definition.filters.map((key) => `<label>${esc(FILTER_LABELS[key])}<input class="kit-input" name="${key}" value="${esc(filters[key])}" autocomplete="off" /></label>`).join("")}<button type="submit" class="so-btn so-btn-sm">Apply ${esc(definition.label)} filters</button></form>
      <p role="status" data-index-count>${esc(page?.error || count)}</p>
      ${page?.selectedOutside ? '<p class="kit-muted">The linked selection is outside this page.</p>' : ""}
      ${page?.selectionError ? `<p role="status">${esc(page.selectionError)}</p>` : ""}
      <div class="kit-index-actions"><button type="button" class="so-btn so-btn-sm" data-index-page="${index}" data-index-cursor="${esc(page?.previousCursor || "")}"${page?.hasPrevious && !page?.error ? "" : " disabled"}>Previous ${esc(definition.label)}</button><button type="button" class="so-btn so-btn-sm" data-index-page="${index}" data-index-cursor="${esc(page?.nextCursor || "")}"${page?.nextCursor && !page?.error ? "" : " disabled"}>Next ${esc(definition.label)}</button>${page?.error || page?.selectionError ? `<button type="button" class="so-btn so-btn-sm" data-index-retry="${index}">Retry ${esc(definition.label)}</button>` : ""}</div>
    </section>`;
    })
    .join("");
}

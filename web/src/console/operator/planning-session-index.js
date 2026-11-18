import {
  planningHash,
  planningSessionFromHash,
} from "./planning-session-link.js";

const escape = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
const ownerKey = (store) =>
  JSON.stringify([
    store.reviewOperatorActor || "",
    store.reviewOperatorIdentities || [],
  ]);

export async function fetchSessionPage(fetchImpl = fetch, cursor = "") {
  const query = new URLSearchParams({ limit: "100" });
  if (cursor) query.set("cursor", cursor);
  const response = await fetchImpl(`/v1/planning-sessions?${query}`);
  const body = await response.json();
  if (!response.ok)
    throw new Error(
      body?.error || `Planning inventory unavailable (${response.status}).`,
    );
  if (
    body?.valueTier !== "CONDUIT_NATIVE" ||
    !Array.isArray(body?.items) ||
    !Number.isInteger(body.total) ||
    body.total < body.items.length ||
    body.total > 1000 ||
    body.limit !== 100 ||
    body.pageSize !== body.items.length ||
    body.items.length > 100 ||
    typeof body.hasPrevious !== "boolean"
  )
    throw new Error("The Hub returned an invalid planning inventory page.");
  const ids = new Set();
  for (const row of body.items) {
    if (
      !row ||
      typeof row.id !== "string" ||
      !row.id ||
      typeof row.status !== "string" ||
      !row.status ||
      typeof row.profile?.name !== "string" ||
      ids.has(row.id)
    )
      throw new Error("The Hub returned an invalid planning session summary.");
    ids.add(row.id);
  }
  for (const key of ["nextCursor", "previousCursor"])
    if (
      body[key] !== undefined &&
      (typeof body[key] !== "string" ||
        body[key].length > 2048 ||
        (body[key] && !/^[A-Za-z0-9_-]+$/.test(body[key])))
    )
      throw new Error("The Hub returned an invalid planning continuation.");
  return body;
}

export function sessionIndexMarkup(planning) {
  const view = planning.sessions;
  let body = '<p class="kit-muted">Reading your planning sessions…</p>';
  if (view?.status === "failed")
    body = `<p role="status" data-plan-sessions-error>The Hub did not return planning sessions: ${escape(view.reason)}</p>`;
  if (view?.status === "ready") {
    const rows = view.items
      .map((row) => {
        const hash = planningHash(row.id);
        const label = `${escape(row.appName || row.repoUrl || row.id)} · ${escape(row.status)}`;
        const link =
          planningSessionFromHash(hash) === row.id
            ? `<a href="${escape(hash)}">${label}</a>`
            : `<span>${label}</span>`;
        return `<li class="plan-line" data-plan-session="${escape(row.id)}">${link}<div class="kit-muted">${escape(row.profile.name)} · <span class="code-id">${escape(row.id)}</span></div></li>`;
      })
      .join("");
    body = rows
      ? `<p class="kit-muted">${view.pageSize} shown · ${view.total} available</p><ul class="kit-stack">${rows}</ul>`
      : '<p class="kit-muted" data-plan-sessions-empty>No planning sessions are available to you.</p>';
  }
  const loading = !view || view.status === "loading";
  return `<section class="kit-panel kit-pad plan-card" data-plan-sessions data-value-tier="CONDUIT_NATIVE" aria-label="Your planning sessions"><div class="kit-section">Your planning sessions</div>${body}<div class="plan-actions"><button type="button" class="kit-btn kit-btn-sm" data-plan-sessions-refresh${loading ? " disabled" : ""}>Refresh</button><button type="button" class="kit-btn kit-btn-sm" data-plan-sessions-previous${view?.status === "ready" && view.hasPrevious ? "" : " disabled"}>Previous</button><button type="button" class="kit-btn kit-btn-sm" data-plan-sessions-next${view?.status === "ready" && view.nextCursor ? "" : " disabled"}>Next</button></div><p class="kit-muted" data-plan-sessions-note>Recent changes may take time to appear, and history retention follows Hub configuration. Opening a session reads its current state. This list does not cancel or approve a run.</p></section>`;
}

export function loadSessionIndex(
  store,
  fetchImpl = fetch,
  win = globalThis,
  cursor = "",
) {
  const owner = ownerKey(store);
  const loading = { status: "loading", items: [], owner };
  store.planning = {
    ...store.planning,
    sessionIndexOwner: owner,
    sessions: loading,
  };
  const repaint = () => {
    const node = win.document?.querySelector?.("[data-plan-sessions]");
    if (node) node.outerHTML = sessionIndexMarkup(store.planning);
  };
  repaint();
  return fetchSessionPage(fetchImpl, cursor).then(
    (page) => {
      if (store.planning.sessions !== loading || ownerKey(store) !== owner)
        return;
      store.planning = {
        ...store.planning,
        sessions: { ...page, status: "ready" },
      };
      repaint();
    },
    (error) => {
      if (store.planning.sessions !== loading || ownerKey(store) !== owner)
        return;
      store.planning = {
        ...store.planning,
        sessions: { status: "failed", reason: error.message, items: [] },
      };
      repaint();
    },
  );
}

export function syncSessionIndex(store, fetchImpl, win) {
  const owner = ownerKey(store);
  if (
    store.planning.sessionIndexOwner !== owner ||
    store.planning.sessions === undefined
  )
    void loadSessionIndex(store, fetchImpl, win);
}

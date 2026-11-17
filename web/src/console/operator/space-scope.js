/*
 * The Space selector in the top bar, and what selecting a Space does.
 *
 * The Hub scopes exactly one index by Space: GET /v1/environments accepts
 * `spaceId` (internal/controlplane/http.go, selectedSpace). No other index
 * route reads it. So a selection is sent to the Hub for environments, and the
 * rows that name an environment — installations, plans, change requests —
 * are narrowed to the environments the Hub returned for that Space. Indexes
 * whose rows name no environment are left whole and are listed as unscoped
 * rather than being made to look scoped.
 */
export const SPACE_PREF = "conduit.console.spaceId";

/** The Hub's native Space identity; anything else is refused with a 400. */
const SPACE_ID = /^space_[a-f0-9]{32}$/;

export const SPACE_SCOPED = Object.freeze([
  "environments (by the Hub)",
  "installations",
  "plans",
  "change requests",
]);
export const SPACE_UNSCOPED = Object.freeze([
  "products",
  "promotions",
  "channels",
  "packaging",
  "bundles",
  "audit",
]);

function esc(value) {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

/**
 * The Space a refresh reads under: the selection when the Hub lists it,
 * otherwise none. A remembered id the Hub no longer lists scopes nothing.
 */
export function activeSpaceId(selected, spaces) {
  const id = typeof selected === "string" ? selected : "";
  if (!SPACE_ID.test(id)) return "";
  return (Array.isArray(spaces) ? spaces : []).some((row) => row?.id === id)
    ? id
    : "";
}

/** Adds `spaceId` to the one index route that reads it. */
export function spaceScopedPath(path, spaceId) {
  if (!spaceId || !/^\/v1\/environments(\?|$)/.test(path)) return path;
  return `${path}${path.includes("?") ? "&" : "?"}spaceId=${encodeURIComponent(spaceId)}`;
}

/**
 * Narrow the loaded rows that name an environment to the Space's
 * environments. Mutates and returns `next`, recording what was narrowed.
 */
export function applySpaceScope(next, spaceId) {
  if (!spaceId) return next;
  const inSpace = new Set(
    (Array.isArray(next.environments) ? next.environments : [])
      .map((row) => row?.environment?.id)
      .filter(Boolean),
  );
  const keep = (rows, environmentOf) =>
    Array.isArray(rows)
      ? rows.filter((row) => inSpace.has(environmentOf(row)))
      : rows;
  next.installations = keep(next.installations, (row) => row?.environment);
  next.plans = keep(next.plans, (row) => row?.plan?.environment);
  next.changes = keep(next.changes, (row) => row?.environment);
  next.spaceScope = { spaceId, environments: inSpace.size };
  return next;
}

export function spaceSelectorMarkup(hubState, selectedId) {
  const spaces = Array.isArray(hubState?.spaces) ? hubState.spaces : [];
  const active = activeSpaceId(selectedId, spaces);
  const paged = hubState?.pagedIndexes === true;
  const scoped = paged ? ["environments (by the Hub)"] : SPACE_SCOPED;
  const unscoped = paged
    ? ["installations", "plans", "change requests", ...SPACE_UNSCOPED]
    : SPACE_UNSCOPED;
  const title = hubState?.spaceNote
    ? hubState.spaceNote
    : active
      ? `Scoped to this Space: ${scoped.join(", ")}. Not scoped by the Hub API: ${unscoped.join(", ")}.`
      : paged
        ? "All Spaces. Selecting one filters the environment index; other pages use their displayed server-side filters."
        : "All Spaces. Selecting one scopes environments at the Hub, and the installations, plans and change requests that name them.";
  const rows = spaces
    .map(
      (space) =>
        `<option value="${esc(space.id)}"${space.id === active ? " selected" : ""}>${esc(space.name || space.id)}</option>`,
    )
    .join("");
  return `<label class="hidden md:inline-flex items-center gap-1 text-[10.5px] text-[#8b96a5]" title="${esc(title)}">
    <span>Space</span>
    <select id="space-selector" class="so-input !h-[26px] text-[11px] max-w-[160px]" aria-label="Space"${spaces.length ? "" : " disabled"}>
      <option value=""${active ? "" : " selected"}>${hubState?.spaceNote ? "Unavailable" : "All Spaces"}</option>${rows}
    </select>
    ${active ? `<span class="code-id text-[9.5px] text-[#eab854]" data-space-scope>PARTLY SCOPED</span>` : ""}
  </label>`;
}

export function readSpaceSelection(storage = globalThis.localStorage) {
  try {
    const value = storage?.getItem(SPACE_PREF) || "";
    return SPACE_ID.test(value) ? value : "";
  } catch {
    return "";
  }
}

function writeSpaceSelection(value, storage = globalThis.localStorage) {
  try {
    if (value) storage?.setItem(SPACE_PREF, value);
    else storage?.removeItem(SPACE_PREF);
  } catch {
    /* A browser that refuses storage still scopes this page's reads. */
  }
}

/** A Space change starts a new Environment window and drops its old selection. */
export function changeSpaceSelection(state, value) {
  const spaceId = value || "";
  const query = new URLSearchParams(state.indexQuery || "");
  if ((query.get("space") || "") !== spaceId) {
    const environment =
      state.releaseEnvironmentId ||
      state.envDetailId ||
      query.get("environment");
    query.delete("environments.cursor");
    query.delete("environment");
    state.releaseEnvironmentId = "";
    state.envDetailId = "";
    // Drop filters derived from that selection; preserve independent filters.
    for (const index of ["plans", "installations", "changes"]) {
      if (!environment || query.get(`${index}.environment`) !== environment)
        continue;
      query.delete(`${index}.environment`);
      query.delete(`${index}.cursor`);
      if (index === "plans") {
        state.releasePlanId = "";
        query.delete("plan");
      }
      if (index === "changes") {
        if (state.crReview) {
          state.crReview.selectedId = "";
          state.crReview.draft = { decision: "", rationale: "" };
        }
        query.delete("change");
      }
    }
  }
  if (spaceId) query.set("space", spaceId);
  else query.delete("space");
  state.spaceId = spaceId;
  state.indexQuery = query.toString();
}

/**
 * Paint the selector into the top bar, left of the data badge. The host is
 * created here so the top-bar template itself is untouched.
 */
export function mountSpaceSelector(doc, state, refresh) {
  const anchor = doc?.getElementById?.("example-data-badge");
  if (!anchor?.parentElement) return;
  let host = doc.getElementById("space-selector-host");
  if (!host) {
    host = doc.createElement("div");
    host.id = "space-selector-host";
    anchor.parentElement.insertBefore(host, anchor);
    host.addEventListener("change", (event) => {
      const target = event.target;
      if (!target || target.id !== "space-selector") return;
      changeSpaceSelection(state, target.value);
      writeSpaceSelection(state.spaceId);
      void refresh();
    });
  }
  // A focused select is left alone: repainting would close its open list.
  if (doc.activeElement?.id === "space-selector") return;
  const markup = spaceSelectorMarkup(state.hub, state.spaceId);
  if (host.dataset.painted !== markup) {
    host.innerHTML = markup;
    host.dataset.painted = markup;
  }
}

/* Space selection narrows server indexes using persisted native anchors.
 * It never grants resource permission or infers membership from a loaded page. */
export const SPACE_PREF = "conduit.console.spaceId";

export const SPACE_SCOPED = Object.freeze([
  "environments",
  "products",
  "promotions",
  "bundles",
  "installations",
  "plans",
  "change requests",
]);
export const SPACE_UNSCOPED = Object.freeze([
  "channels",
  "packaging",
  "workspaces",
  "rollouts",
  "upgrades",
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

/** Keep missing and malformed remembered choices; only explicit clearing widens a read. */
export function activeSpaceId(selected) {
  return typeof selected === "string" ? selected : "";
}

/** Only routes implementing persisted Space selection receive the constraint. */
export function spaceScopedPath(path, spaceId) {
  if (!spaceId) return path;
  const url = new URL(path, "http://hub.local");
  if (
    !/^\/v1\/(?:environments(?:\/[^/]+(?:\/entities)?)?|products(?:\/[^/]+\/releases(?:\/[^/]+)?)?|plans(?:\/[^/]+)?|fleet\/installations|change-requests(?:\/[^/]+)?|promotions(?:\/[^/]+)?|bundles(?:\/[^/]+)?)$/.test(
      url.pathname,
    )
  )
    return path;
  url.searchParams.set("spaceId", spaceId);
  return url.pathname + url.search;
}

export function spaceSelectorMarkup(hubState, selectedId) {
  const spaces = Array.isArray(hubState?.spaces) ? hubState.spaces : [];
  const active = activeSpaceId(selectedId);
  const unavailable = active && !spaces.some((space) => space.id === active);
  const title = hubState?.spaceNote
    ? hubState.spaceNote
    : unavailable
      ? "The selected Space is unavailable. Reads retain that selection; choose All Spaces explicitly to clear it."
      : active
        ? `Scoped by the Hub: ${SPACE_SCOPED.join(", ")}. Not Space-scoped: ${SPACE_UNSCOPED.join(", ")}.`
        : "All Spaces. Selecting one narrows Environment, Product and Bundle resource views at the Hub.";
  const rows = spaces
    .map(
      (space) =>
        `<option value="${esc(space.id)}"${space.id === active ? " selected" : ""}>${esc(space.name || space.id)}</option>`,
    )
    .join("");
  return `<label class="hidden md:inline-flex items-center gap-1 text-[10.5px] text-[#8b96a5]" title="${esc(title)}">
    <span>Space</span>
    <select id="space-selector" class="so-input !h-[26px] text-[11px] max-w-[160px]" aria-label="Space"${spaces.length || active ? "" : " disabled"}>
      <option value=""${active ? "" : " selected"}>${active || !hubState?.spaceNote ? "All Spaces" : "Unavailable"}</option>${unavailable ? `<option value="${esc(active)}" selected>Unavailable Space</option>` : ""}${rows}
    </select>
    ${active ? `<span class="code-id text-[9.5px] text-[#eab854]" data-space-scope>${unavailable ? "SPACE UNAVAILABLE" : "PARTLY SCOPED"}</span>` : ""}
  </label>`;
}

export function readSpaceSelection(storage = globalThis.localStorage) {
  try {
    const value = storage?.getItem(SPACE_PREF) || "";
    return typeof value === "string" ? value : "";
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

/** A Space change starts new scoped index windows and drops old selections. */
export function changeSpaceSelection(state, value) {
  const spaceId = value || "";
  const query = new URLSearchParams(state.indexQuery || "");
  if ((query.get("space") || "") !== spaceId) {
    const environment =
      state.releaseEnvironmentId ||
      state.envDetailId ||
      query.get("environment");
    for (const index of [
      "environments",
      "products",
      "plans",
      "installations",
      "changes",
      "promotions",
    ])
      query.delete(`${index}.cursor`);
    query.delete("plan");
    query.delete("change");
    state.releasePlanId = "";
    if (state.crReview) {
      state.crReview.selectedId = "";
      state.crReview.draft = { decision: "", rationale: "" };
    }
    query.delete("environment");
    state.releaseEnvironmentId = "";
    state.envDetailId = "";
    // Drop filters derived from that selection; preserve independent filters.
    for (const index of ["plans", "installations", "changes"]) {
      if (!environment || query.get(`${index}.environment`) !== environment)
        continue;
      query.delete(`${index}.environment`);
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

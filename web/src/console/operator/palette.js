// Command palette: views, Hub objects, and console actions in one list with
// category filters and match highlighting. Objects come from the records
// live-hub.js already loaded; picking one only navigates. Pure builders; the
// caller owns state and wiring.

const defaultEsc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );

/** HTML-escapes text for markup and attribute values. */
export const escapeHtml = defaultEsc;

export const CATEGORIES = Object.freeze(["All", "Views", "Objects", "Actions"]);

const ACTIONS = Object.freeze([
  {
    title: "Check the Hub again",
    subtitle: "Re-read every view's records from the Hub",
    action: "refresh",
  },
  { title: "Switch theme", subtitle: "Light or dark", action: "theme" },
  {
    title: "Open settings",
    subtitle: "Console preferences",
    action: "settings",
  },
]);

const list = (value) => (Array.isArray(value) ? value : []);

/** Every palette entry: views first, then Hub objects, then actions. */
export function paletteItems(screens, hub) {
  const items = list(screens).map((s) => ({
    category: "Views",
    title: s.short,
    subtitle: s.subtitle || "",
    tagKey: "view",
    tagValue: s.parentLabel || "Conduit",
    target: { screen: s.id },
  }));
  for (const row of list(hub?.environments)) {
    const id = row?.environment?.id;
    if (!id) continue;
    items.push({
      category: "Objects",
      title: id,
      subtitle: row.environment.displayName || row.environment.name || "",
      tagKey: "environment",
      tagValue: row.environment.tier || row.environment.kind || "env",
      target: { screen: "flt-environments", env: id },
    });
  }
  for (const entry of list(hub?.products)) {
    if (!entry?.product) continue;
    const releases = list(entry.releases).length;
    items.push({
      category: "Objects",
      title: entry.product,
      subtitle: `${releases} release${releases === 1 ? "" : "s"}`,
      tagKey: "product",
      tagValue: "catalog",
      target: { screen: "rel-products" },
    });
  }
  for (const run of list(hub?.packaging)) {
    if (!run?.runId) continue;
    items.push({
      category: "Objects",
      title: run.runId,
      subtitle: [run.phase, run.outcome].filter(Boolean).join(" · "),
      tagKey: "packaging run",
      tagValue: run.phase || "run",
      target: { screen: "pkg-runs" },
    });
  }
  for (const request of list(hub?.changes)) {
    if (!request?.id) continue;
    items.push({
      category: "Objects",
      title: request.title || request.id,
      subtitle: request.id,
      tagKey: "change request",
      tagValue: request.status || "",
      target: { screen: "rm07-change-requests", change: request.id },
    });
  }
  for (const action of ACTIONS) {
    items.push({
      category: "Actions",
      title: action.title,
      subtitle: action.subtitle,
      tagKey: "action",
      tagValue: "console",
      target: { action: action.action },
    });
  }
  return items;
}

/** Entries in the category whose text contains the query, case-insensitive. */
export function filterPalette(items, query, category = "All") {
  const q = String(query || "")
    .trim()
    .toLowerCase();
  return list(items).filter((item) => {
    if (category !== "All" && item.category !== category) return false;
    if (!q) return true;
    return `${item.title} ${item.subtitle} ${item.tagKey} ${item.tagValue}`
      .toLowerCase()
      .includes(q);
  });
}

/** Escaped text with the first case-insensitive match wrapped in <mark>. */
export function highlight(text, query, esc = defaultEsc) {
  const value = String(text ?? "");
  const q = String(query || "").trim();
  const at = q ? value.toLowerCase().indexOf(q.toLowerCase()) : -1;
  if (at < 0) return esc(value);
  return `${esc(value.slice(0, at))}<mark>${esc(value.slice(at, at + q.length))}</mark>${esc(value.slice(at + q.length))}`;
}

/** Category buttons and result rows; the input is rendered by the caller. */
export function paletteResultsMarkup(
  hits,
  { query = "", category = "All", selected = 0, esc = defaultEsc } = {},
) {
  const chips = CATEGORIES.map(
    (c) =>
      `<button type="button" class="kit-tab${c === category ? " is-active" : ""}" aria-pressed="${c === category}" data-onclick="paletteCategory('${esc(c)}')">${esc(c)}</button>`,
  ).join("");
  const rows = hits.length
    ? hits
        .map(
          (h, idx) =>
            `<button type="button" role="option" aria-selected="${idx === selected}" class="so-nav-row kit-palette-row${idx === selected ? " is-active" : ""}" data-onclick="pickPaletteItem(${idx})"><span class="kit-palette-text"><span class="kit-palette-title">${highlight(h.title, query, esc)}</span>${h.subtitle ? `<span class="kit-palette-sub">${highlight(h.subtitle, query, esc)}</span>` : ""}</span><span class="kit-ctag"><span class="kit-ctag-k">${esc(h.tagKey)}</span><span class="kit-ctag-v">${esc(h.tagValue)}</span></span></button>`,
        )
        .join("")
    : `<p class="kit-palette-none" role="status">Nothing matches${query ? ` "${esc(query)}"` : ""}.</p>`;
  return `<div class="kit-tabs kit-palette-cats" role="group" aria-label="Filter results">${chips}</div><div class="kit-palette-list" role="listbox" aria-label="Results">${rows}</div>`;
}

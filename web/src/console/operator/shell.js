// Pure helpers for the console shell: header KPIs, site summary, dock sizing,
// and guarded preference storage.

export const DOCK_DEFAULT = 368;
export const DOCK_MIN = 280;
export const DOCK_MAX = 640;

const DASH = "—";
const TERMINAL_PHASES = new Set([
  "succeeded",
  "failed",
  "cancelled",
  "blocked",
]);

const list = (value) => (Array.isArray(value) ? value : null);
const lower = (value) => String(value || "").toLowerCase();

function kpi(id, label, items, compute) {
  if (!items) {
    return { id, value: DASH, label, sub: "", tone: "info", live: false };
  }
  const { value, sub, tone } = compute(items);
  return { id, value: String(value), label, sub, tone, live: true };
}

function isDrifted(row) {
  const desired = row?.desiredVersion || "";
  const reported = row?.observedVersion || "";
  return desired !== reported;
}

export function kpisFromHub(hub) {
  const h = hub || {};
  const installations = list(h.installations);
  const environments = list(h.environments);
  const packaging = list(h.packaging);
  const plans = list(h.plans);
  const changes = list(h.changes);

  return [
    kpi("convergence", "Convergence", installations, (rows) => {
      const converged = rows.filter((r) => !isDrifted(r)).length;
      const pct = rows.length ? Math.round((converged / rows.length) * 100) : 0;
      const tone = !rows.length
        ? "info"
        : pct >= 90
          ? "ok"
          : pct >= 60
            ? "warn"
            : "bad";
      return {
        value: `${pct}%`,
        sub: `${converged} of ${rows.length} converged`,
        tone,
      };
    }),
    kpi("environments", "Environments", environments, (rows) => {
      const reporting = rows.filter((r) => r?.reported).length;
      return {
        value: rows.length,
        sub: `${reporting} reporting`,
        tone: rows.length && reporting < rows.length ? "warn" : "ok",
      };
    }),
    kpi("packaging", "Packaging runs", packaging, (rows) => {
      const active = rows.filter((r) => !TERMINAL_PHASES.has(lower(r?.phase)));
      const blocked = rows.filter((r) => lower(r?.phase) === "blocked").length;
      return {
        value: active.length,
        sub: `${blocked} blocked`,
        tone: blocked ? "bad" : active.length ? "info" : "ok",
      };
    }),
    kpi("plans", "Plans", plans, (rows) => {
      const waiting = rows.filter((r) =>
        /pending|awaiting|review|proposed/.test(lower(r?.status)),
      ).length;
      return {
        value: rows.length,
        sub: `${waiting} awaiting approval`,
        tone: waiting ? "warn" : "ok",
      };
    }),
    kpi("changes", "Change requests", changes, (rows) => {
      const pending = rows.filter((r) =>
        /pending|open|awaiting|review|proposed/.test(lower(r?.status)),
      ).length;
      // Drift needs a report to differ from; never-reported is not drift.
      const drift = (installations || []).filter(
        (row) => row?.observedVersion && isDrifted(row),
      ).length;
      return {
        value: pending,
        sub: `${drift} drift`,
        tone: drift ? "warn" : pending ? "info" : "ok",
      };
    }),
  ];
}

export function allLive(kpis) {
  return (
    Array.isArray(kpis) &&
    kpis.length > 0 &&
    kpis.every((k) => k && k.live === true)
  );
}

export function siteSummary(hub) {
  const rows = list(hub?.environments);
  if (!rows || rows.length === 0) return null;
  const spokes = new Set();
  for (const row of rows) {
    const id = row?.reported?.agentId;
    if (id) spokes.add(id);
  }
  return { sites: rows.length, spokes: spokes.size };
}

export function clampDockWidth(px) {
  const n = Number(px);
  if (!Number.isFinite(n)) return DOCK_DEFAULT;
  return Math.min(DOCK_MAX, Math.max(DOCK_MIN, n));
}

export const DOCK_SURFACES = [
  { id: "inspector", label: "Inspector", glyph: "⊙" },
  { id: "assistant", label: "Assistant", glyph: "✦" },
  { id: "outline", label: "Outline", glyph: "☰" },
  { id: "windows", label: "Windows", glyph: "⛉" },
  { id: "live", label: "Live", glyph: "⚑" },
];

const defaultEsc = (v) =>
  String(v ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );

// HTML for the four non-assistant dock surfaces, keyed by surface id.
// helpers = { areaObject, objectHeader, esc } come from the caller.
export function dockSurfacesMarkup(state, helpers) {
  const esc = helpers?.esc || defaultEsc;
  const empty = (text) => `<p class="kit-muted text-[12px]">${esc(text)}</p>`;
  const hub = state?.hub || {};

  let inspector = empty("Select an object to inspect it.");
  const object = helpers?.areaObject
    ? helpers.areaObject(state?.hub, state?.screen)
    : null;
  if (object && helpers?.objectHeader) inspector = helpers.objectHeader(object);

  const outline = empty(
    "No turns yet. The outline lists the assistant thread once it has turns.",
  );

  const windowRows = (Array.isArray(hub.environments) ? hub.environments : [])
    .filter((row) => {
      const m = row?.environment?.maintenance;
      return m && typeof m === "object" && Object.keys(m).length > 0;
    })
    .map(
      (row) =>
        `<li class="text-[12px]"><span class="code-id text-white">${esc(row.environment.id)}</span> <span class="kit-muted">${esc(JSON.stringify(row.environment.maintenance))}</span></li>`,
    );
  const windows = windowRows.length
    ? `<ul class="space-y-1">${windowRows.join("")}</ul>`
    : empty("No maintenance window or suppression is recorded.");

  const entries = Array.isArray(hub.audit) ? hub.audit.slice(-8).reverse() : [];
  const who = (a) =>
    a && typeof a === "object" ? a.username || a.id || JSON.stringify(a) : a;
  const live = entries.length
    ? `<ul class="space-y-1">${entries
        .map(
          (e) =>
            `<li class="text-[12px]"><span class="code-id text-white">${esc(e?.sequence)}</span> <span class="kit-muted">${esc(who(e?.actor))} · ${esc(e?.action)}</span></li>`,
        )
        .join("")}</ul>`
    : empty("No audit entry is recorded.");

  return { inspector, outline, windows, live };
}

export function readPref(key, fallback) {
  try {
    if (typeof localStorage === "undefined" || !localStorage) return fallback;
    const value = localStorage.getItem(key);
    return value === null || value === undefined ? fallback : value;
  } catch {
    return fallback;
  }
}

export function writePref(key, value) {
  try {
    if (typeof localStorage === "undefined" || !localStorage) return false;
    localStorage.setItem(key, String(value));
    return true;
  } catch {
    return false;
  }
}

import { convergenceState } from "./release-convergence.js";
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
  "complete",
]);
const ACTIVE_PHASES = new Set([
  "assess",
  "propose",
  "validate",
  "review",
  "handoff",
  "running",
  "pending",
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

// Unknown, stale or never-reported installations cannot establish convergence.
export function kpisFromHub(hub) {
  const h = hub || {};
  const environments = h.loading || h.note ? null : list(h.environments);
  const installations =
    h.loading || h.installationNote ? null : list(h.installations);
  const packaging = h.loading || h.packagingNote ? null : list(h.packaging);
  const changes = h.loading || h.changeNote ? null : list(h.changes);
  const complete = environments && installations;
  const hasSpokeStates =
    environments &&
    environments.every(
      (row) =>
        ["live", "stale", "unreachable"].includes(row?.spoke?.state) &&
        (row.spoke.state !== "live" || Boolean(row.spoke.agentId)),
    );
  return [
    kpi(
      "convergence",
      "Convergence",
      complete ? environments : null,
      (rows) => {
        const converged = rows.filter((environment) => {
          const id = environment?.environment?.id;
          const members = installations.filter(
            (row) => id && row.environment === id,
          );
          return (
            members.length > 0 &&
            members.every((row) => convergenceState(row) === "converged")
          );
        }).length;
        return {
          value: converged,
          sub: `${converged} of ${rows.length} environments in sync`,
          tone: rows.length && converged === rows.length ? "ok" : "info",
        };
      },
    ),
    kpi("environments", "Environments", environments, (rows) => ({
      value: rows.length,
      sub: "authorized rows",
      tone: "info",
    })),
    kpi(
      "packaging",
      "Packaging runs",
      packaging?.every(
        (row) =>
          TERMINAL_PHASES.has(lower(row?.phase)) ||
          ACTIVE_PHASES.has(lower(row?.phase)),
      )
        ? packaging
        : null,
      (rows) => {
        const active = rows.filter(
          (r) => !TERMINAL_PHASES.has(lower(r?.phase)),
        );
        const blocked = rows.filter(
          (r) => lower(r?.phase) === "blocked",
        ).length;
        return {
          value: active.length,
          sub: `${blocked} blocked`,
          tone: blocked ? "bad" : active.length ? "info" : "ok",
        };
      },
    ),
    kpi(
      "spokes",
      "Spokes online",
      hasSpokeStates ? environments : null,
      (rows) => ({
        value: new Set(
          rows
            .filter((row) => row.spoke.state === "live")
            .map((row) => row.spoke.agentId),
        ).size,
        sub: "Hub report-freshness snapshot",
        tone: "info",
      }),
    ),
    kpi("changes", "Pending approvals", changes, (rows) => {
      const pending = rows.filter((r) =>
        /pending|open|awaiting|review|proposed/.test(lower(r?.status)),
      ).length;
      return {
        value: pending,
        sub: "change requests",
        tone: pending ? "info" : "ok",
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
  {
    id: "inspector",
    label: "Inspector",
    glyph: "⊙",
    icon: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6"><circle cx="8" cy="8" r="6"/><circle cx="8" cy="8" r="2.2"/></svg>',
  },
  {
    id: "assistant",
    label: "Assistant",
    glyph: "✦",
    icon: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M8 1.8l1.6 3.9L13.5 7.3l-3.9 1.6L8 12.8 6.4 8.9 2.5 7.3l3.9-1.6L8 1.8z"/></svg>',
  },
  {
    id: "outline",
    label: "Outline",
    glyph: "☰",
    icon: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M2.5 4h11M2.5 8h11M2.5 12h11"/></svg>',
  },
  {
    id: "windows",
    label: "Windows",
    glyph: "⛉",
    icon: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6"><circle cx="8" cy="8" r="6"/><path d="M8 4.5V8l2.5 2.5"/></svg>',
  },
  {
    id: "live",
    label: "Live",
    glyph: "⚑",
    icon: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M3.5 2v12M3.5 3h8.5l-2 4 2 4H3.5"/></svg>',
  },
];

export function dockSurfaceCounts(hub) {
  const envs = Array.isArray(hub?.environments) ? hub.environments : [];
  const windowCount = envs.filter((row) => {
    const m = row?.environment?.maintenance;
    return m && typeof m === "object" && Object.keys(m).length > 0;
  }).length;
  const auditEntries = Array.isArray(hub?.audit) ? hub.audit : [];
  return {
    inspector: 0,
    assistant: 0,
    outline: 0,
    windows: windowCount,
    live: auditEntries.length,
  };
}

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
    ? helpers.areaObject(state?.hub, state?.screen, {
        packagingRunId: state?.packagingRunId,
        releaseEnvironmentId: state?.releaseEnvironmentId,
        changeRequestId: state?.crReview?.selectedId,
        workspaceView:
          state?.screen === "area-packaging" &&
          String(state?.areaTab || "").startsWith("workspace"),
        workspace: state?.studioWorkspace,
      })
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

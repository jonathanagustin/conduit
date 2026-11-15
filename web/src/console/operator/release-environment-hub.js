import { releaseCalendarMarkup } from "./release-calendar.js";
import { convergenceState, convergenceMarkup } from "./release-convergence.js";
import { timelineMarkup } from "./release-plan-inspector.js";
const esc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
const mounts = new WeakMap();
export function selectedEnvironment(rows, id = "") {
  const list = Array.isArray(rows) ? rows : [];
  return id
    ? list.find((row) => row.environment?.id === id) || null
    : list[0] || null;
}
export function maintenanceMarkup(spec = {}) {
  const rules = ["downtime", "noDowntime"].flatMap((kind) => {
    const window = spec[kind];
    return window
      ? [
          `<dt>${esc(kind)}</dt><dd>${esc(window.timeZone || "time zone unavailable")} · ${Array.isArray(window.intervals) ? window.intervals.map(esc).join("; ") : "intervals unavailable"}</dd>`,
        ]
      : [];
  });
  const overrides = Array.isArray(spec.overrides) ? spec.overrides : [];
  return `<section class="so-panel p-3 space-y-2"><h3>Maintenance rules</h3>
    ${rules.length ? `<dl>${rules.join("")}</dl>` : "<p>No recurring environment maintenance rule is recorded. Product and installation constraints can still apply.</p>"}
    ${overrides.length ? `<h4>Recorded overrides</h4><ul>${overrides.map((row) => `<li>${esc(row.type)} · ${esc(row.startsAt)} → ${esc(row.endsAt)} · ${esc(row.reason)} · ${esc(row.approvedBy)}</li>`).join("")}</ul>` : "<p>No environment override is recorded.</p>"}
    <p class="kit-muted">Rules and recorded intervals are read-only. Windows can propose a separate reviewed Entity override; a proposal does not schedule or execute a workload.</p></section>`;
}
/**
 * @param {any} hub
 * @param {string} id
 * @param {{timeline?: Array<Record<string, unknown>>, blackout?: {environment: string, blocked: boolean, reason?: string}|null, note?: string}} options
 */
export function environmentHubMarkup(
  hub,
  id = "",
  { timeline = [], blackout = null, note = "" } = {},
) {
  const rows = Array.isArray(hub?.environments) ? hub.environments : [];
  const selected = selectedEnvironment(rows, id);
  const picker = `<label>Environment <select data-release-environment>${rows.map((row) => `<option value="${esc(row.environment?.id)}"${row === selected ? " selected" : ""}>${esc(row.environment?.id)}</option>`).join("")}</select></label>`;
  if (!selected)
    return `<section class="so-panel p-3">${picker}<p>No selected environment is in the visible Hub index.</p></section>`;
  const environment = selected.environment.id;
  const installations = (
    Array.isArray(hub.installations) ? hub.installations : []
  ).filter((row) => row.environment === environment);
  return `<div data-release-environment-hub class="space-y-3"><section class="so-panel p-3 space-y-2"><h2>${esc(environment)}</h2>${picker}<p>Spoke: ${esc(selected.spoke?.state || "unknown")} · Agent: ${esc(selected.spoke?.agentId || "unavailable")}</p>
    <p>Blackout: ${blackout && blackout.environment === environment && typeof blackout.blocked === "boolean" ? (blackout.blocked ? `blocked · ${esc(blackout.reason || "reason unavailable")}` : "not blocked at the Hub evaluation time") : "unavailable"}</p>${note ? `<p role="status">${esc(note)}</p>` : ""}</section>
    ${convergenceMarkup(installations, hub.installationNote)}
    ${installations
      .filter((row) => convergenceState(row) === "diverged")
      .map(
        (row) =>
          `<button type="button" class="so-btn so-btn-sm" data-release-propose-fix="${esc(row.installationId)}">Propose fix for ${esc(row.installationId)}</button>`,
      )
      .join(
        "",
      )}${maintenanceMarkup(selected.environment.maintenance)}${timelineMarkup(timeline)}${releaseCalendarMarkup(selected.environment.maintenance?.overrides)}</div>`;
}
export function mountEnvironmentHub(
  root,
  hub,
  { selectedId = "", onSelect = () => {}, onProposeFix = null } = {},
) {
  const token = {};
  mounts.set(root, token);
  let id = selectedId;
  let controller;
  const current = () => mounts.get(root) === token && root.isConnected;
  async function load() {
    controller?.abort();
    controller = new AbortController();
    const request = controller;
    const row = selectedEnvironment(hub?.environments, id);
    root.innerHTML = environmentHubMarkup(hub, id, {
      note: row ? "Loading selected environment evidence…" : "",
    });
    if (!row?.environment?.id) return;
    const env = row.environment.id;
    const read = async (path) => {
      const response = await fetch(path, {
        headers: { accept: "application/json" },
        signal: AbortSignal.any([request.signal, AbortSignal.timeout(15000)]),
      });
      if (!response.ok) throw new Error("unavailable");
      return response.json();
    };
    const results = await Promise.allSettled([
      read(`/v1/environments/${encodeURIComponent(env)}/timeline`),
      read(`/v1/blackouts?environment=${encodeURIComponent(env)}`),
    ]);
    if (!current() || controller !== request) return;
    const timeline =
      results[0].status === "fulfilled" &&
      Array.isArray(results[0].value?.timeline) &&
      results[0].value.timeline.every(
        (entry) => entry && typeof entry === "object",
      )
        ? results[0].value.timeline
        : null;
    const blackout =
      results[1].status === "fulfilled" &&
      results[1].value?.environment === env &&
      typeof results[1].value.blocked === "boolean"
        ? results[1].value
        : null;
    root.innerHTML = environmentHubMarkup(hub, id, {
      timeline: timeline || [],
      blackout,
      note:
        !timeline || !blackout
          ? "Some selected environment evidence is unavailable."
          : "",
    });
  }
  root.onchange = (event) => {
    const target = event.target;
    if (
      !(target instanceof HTMLSelectElement) ||
      !target.matches("[data-release-environment]")
    )
      return;
    id = target.value;
    onSelect(id);
    if (current()) void load();
  };
  root.onclick = async (event) => {
    const target =
      event.target instanceof Element
        ? event.target.closest("[data-release-propose-fix]")
        : null;
    if (!(target instanceof HTMLButtonElement) || !onProposeFix) return;
    const environment = selectedEnvironment(hub?.environments, id)?.environment
      ?.id;
    const installation = (hub?.installations || []).find(
      (row) =>
        row.environment === environment &&
        row.installationId ===
          target.getAttribute("data-release-propose-fix") &&
        convergenceState(row) === "diverged",
    );
    if (!installation) return;
    target.disabled = true;
    try {
      const result = await onProposeFix(
        environment,
        installation.installationId,
      );
      target.textContent = result.changeRequestId || "Proposed";
    } catch {
      target.textContent = "Refused";
    }
  };
  void load();
}

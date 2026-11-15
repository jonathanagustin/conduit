import { timestampDate } from "@bufbuild/protobuf/wkt";
import { fetchOverview, overviewTransport } from "../../api/overview.js";
const esc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
const PANELS = [
  ["awaitingApprovals", "Awaiting review"],
  ["failedPlans", "Failed Plans"],
  ["runningPlans", "Running Plans"],
  ["unhealthyEntities", "Unhealthy installations"],
  ["staleReports", "Stale reports"],
  ["blockedPromotions", "Blocked promotions"],
];
export function overviewPanelValue(panel) {
  return panel?.status === "CURRENT" &&
    typeof panel.count === "bigint" &&
    panel.count >= 0n
    ? panel.count.toString()
    : "Unavailable";
}
export function overviewSummaryMarkup(view) {
  if (!view?.generatedAt)
    throw new Error("Overview snapshot timestamp is unavailable.");
  const date = timestampDate(view.generatedAt);
  if (!Number.isFinite(date.getTime()))
    throw new Error("Overview snapshot timestamp is invalid.");
  return `<section class="so-panel p-3" data-overview-summary data-value-tier="CONDUIT_NATIVE"><h2>Authorized overview</h2><p class="kit-muted">Hub snapshot ${esc(date.toISOString())}. Counts cover authorized rows, not only the lists loaded in this browser. Unavailable panels do not establish a zero count.</p><dl class="kit-cc-grid">${PANELS.map(([key, label]) => `<div data-overview-panel="${key}"><dt>${label}</dt><dd>${esc(overviewPanelValue(view[key]))}</dd></div>`).join("")}</dl><button type="button" class="so-btn so-btn-sm" data-overview-refresh>Refresh overview</button></section>`;
}
export function createOverviewSummary({
  getIdentity,
  getReady,
  read = fetchOverview,
}) {
  let generation = 0,
    identity = "",
    cache = null,
    cachedAt = 0,
    flight = null,
    controller = null,
    transport = null;
  return async function mount(root, { force = false } = {}) {
    const token = ++generation,
      actor = JSON.stringify(getIdentity());
    const current = () =>
      token === generation &&
      root.isConnected &&
      actor === JSON.stringify(getIdentity());
    if (actor !== identity || !getReady()) {
      controller?.abort();
      flight = null;
      cache = null;
      cachedAt = 0;
      identity = actor;
    }
    if (!getReady()) {
      root.innerHTML = '<p role="status">Reading native identity and Hub…</p>';
      return;
    }
    const paint = (view) => {
      if (!current()) return;
      root.innerHTML = overviewSummaryMarkup(view);
      root.querySelector("[data-overview-refresh]").onclick = () =>
        void mount(root, { force: true });
    };
    try {
      if (!force && cache && Date.now() - cachedAt < 15000) {
        paint(cache);
        return;
      }
      if (force) {
        controller?.abort();
        flight = null;
        cache = null;
      }
      root.innerHTML =
        '<p role="status" data-overview-loading>Reading authorized overview…</p>';
      if (!flight) {
        controller = new AbortController();
        transport ??= overviewTransport(window.location.origin);
        const pending = read(transport, controller.signal);
        flight = pending;
        pending
          .finally(() => {
            if (flight === pending) flight = null;
          })
          .catch(() => {});
      }
      const view = await flight;
      if (!current()) return;
      paint(view);
      cache = view;
      cachedAt = Date.now();
    } catch (error) {
      if (!current()) return;
      root.innerHTML = `<section class="so-panel p-3" data-overview-summary><h2>Authorized overview unavailable</h2><p role="status">${esc(error.message || "Overview unavailable")}</p><button type="button" class="so-btn so-btn-sm" data-overview-refresh>Retry overview</button></section>`;
      root.querySelector("[data-overview-refresh]").onclick = () =>
        void mount(root, { force: true });
    }
  };
}

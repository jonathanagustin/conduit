/*
 * Session capabilities. GET /v1/session is the console's authority for which
 * applications an operator has (docs/ui/contracts/session-capabilities.md).
 * Navigation is derived from that answer through one table, and write controls
 * ask canPerform. Hiding an entry is presentation, not authorization: the Hub
 * routes still decide.
 */
import { HUB_APPLICATIONS } from "./hub-vocabulary.js";
import { screenEntry } from "./screen-registry.js";

/**
 * Navigation entry -> the application ids that enable it; any one suffices.
 * A key is an area id, "area/tab", or a screen id in the rail. An entry with no
 * key here stays visible (the Hub publishes no application for it). An entry
 * with a key is shown only when the session returned one of its ids enabled.
 */
export const NAV_APPLICATIONS = Object.freeze({
  "area-changes": ["change-requests"],
  "area-fleet": ["fleet"],
  "area-command/tower": ["plans"],
  "area-releases/plans": ["plans"],
  "area-releases/planning": ["plans"],
  "area-releases/promotion": ["promotions"],
  "area-releases/waves": ["promotions"],
  "area-packaging/bundle": ["export", "import"],
  "adm02-org-control-panel": ["teams"],
});

export const PLATFORM_ROOT = "platform/root";
const READ_RESOURCE = "resource:read";
const MAXIMUM_RESOURCES = 32;
const RESOURCE_RID =
  /^[A-Za-z0-9][A-Za-z0-9._:-]*(\/[A-Za-z0-9][A-Za-z0-9._:-]*){0,3}$/;

const blank = (status, reason) =>
  Object.freeze({
    status,
    reason,
    applications: [],
    enabled: [],
    globalOperations: [],
    resources: {},
  });

export const CAPABILITIES_LOADING = blank("loading", "");

export function capabilitiesUnavailable(reason) {
  return blank(
    "unavailable",
    reason || "The Hub did not answer the session read.",
  );
}

const strings = (value) =>
  Array.isArray(value) ? value.filter((item) => typeof item === "string") : [];

/** The capability state for one /v1/session answer. Never a default set. */
export function capabilitiesFromSession(session) {
  if (!session || typeof session !== "object") return capabilitiesUnavailable();
  if (!Array.isArray(session.applications))
    return capabilitiesUnavailable(
      "The Hub's session answer carried no application list.",
    );
  const applications = session.applications
    .filter((item) => item && typeof item.id === "string" && item.id !== "")
    .map((item) => ({ id: item.id, enabled: item.enabled === true }));
  const resources = {};
  for (const item of Array.isArray(session.resources) ? session.resources : [])
    if (item && typeof item.resourceRid === "string")
      resources[item.resourceRid] = strings(item.operations);
  return Object.freeze({
    status: "ready",
    reason: "",
    applications,
    enabled: applications.filter((app) => app.enabled).map((app) => app.id),
    globalOperations: strings(session.globalOperations),
    resources,
  });
}

/** Whether one navigation entry is shown, and why not when it is not. */
export function navDecision(capabilities, navId) {
  const requires = NAV_APPLICATIONS[navId];
  if (!requires) return { visible: true, state: "unmapped", requires: [] };
  const status = capabilities?.status || "loading";
  if (status !== "ready") return { visible: false, state: status, requires };
  const enabled = requires.some((id) => capabilities.enabled.includes(id));
  return {
    visible: enabled,
    state: enabled ? "enabled" : "disabled",
    requires,
  };
}

/**
 * The areas and tabs this session may navigate to. `hidden` names every entry
 * that was withheld, so the rail can say what is missing instead of silently
 * showing less.
 * @param {Record<string, {label: string, tabs: {id: string, label: string}[]}>} areas
 * @returns {{areas: Record<string, {label: string, tabs: {id: string, label: string}[]}>, hidden: {id: string, label: string, visible: boolean, state: string, requires: string[]}[]}}
 */
export function deriveNavigation(areas, capabilities) {
  const visible = {};
  const hidden = [];
  for (const [id, area] of Object.entries(areas)) {
    const decision = navDecision(capabilities, id);
    if (!decision.visible) {
      hidden.push({ id, label: area.label, ...decision });
      continue;
    }
    const tabs = area.tabs.filter((tab) => {
      const tabDecision = navDecision(capabilities, `${id}/${tab.id}`);
      if (!tabDecision.visible)
        hidden.push({
          id: `${id}/${tab.id}`,
          label: `${area.label} / ${tab.label}`,
          ...tabDecision,
        });
      return tabDecision.visible;
    });
    if (tabs.length) visible[id] = { ...area, tabs };
    else
      hidden.push({
        id,
        label: area.label,
        visible: false,
        state: "disabled",
        requires: [],
      });
  }
  return { areas: visible, hidden };
}

/**
 * Rail items for areas and screens, minus the ones this session lacks.
 * @template {{id: string}} T
 * @param {T[]} items
 * @returns {T[]}
 */
export function filterNavItems(items, areas, capabilities) {
  const navigation = deriveNavigation(areas, capabilities);
  return items.filter((item) =>
    Object.hasOwn(areas, item.id)
      ? Object.hasOwn(navigation.areas, item.id)
      : navDecision(capabilities, item.id).visible,
  );
}

/**
 * Whether the session may perform `operation` on `resourceRid`.
 * allowed is true or false only when the session answer says so; null means
 * the session did not report that resource and the Hub decides at the write.
 * A control is disabled on `allowed === false` and shows `reason`.
 */
export function decideOperation(
  capabilities,
  operation,
  resourceRid = PLATFORM_ROOT,
) {
  if (capabilities?.status !== "ready")
    return {
      allowed: null,
      reason:
        "Session capabilities are unavailable; the Hub decides when you act.",
    };
  const operations =
    resourceRid === PLATFORM_ROOT
      ? capabilities.globalOperations
      : capabilities.resources[resourceRid];
  if (!operations)
    return {
      allowed: null,
      reason: `The session read did not report ${resourceRid}; the Hub decides when you act.`,
    };
  return operations.includes(operation)
    ? { allowed: true, reason: "" }
    : {
        allowed: false,
        reason: `Your session does not hold ${operation} on ${resourceRid}.`,
      };
}

/** The read path, with the bounded, canonical coordinates the Hub accepts. */
export function sessionPath(resourceRids = []) {
  const rids = [
    ...new Set(
      resourceRids.filter(
        (rid) =>
          typeof rid === "string" &&
          rid.length <= 512 &&
          rid !== PLATFORM_ROOT &&
          RESOURCE_RID.test(rid),
      ),
    ),
  ]
    .slice(-MAXIMUM_RESOURCES)
    .sort((left, right) => (left < right ? -1 : left > right ? 1 : 0));
  if (!rids.length) return "/v1/session";
  return `/v1/session?${rids.map((rid) => `resourceRid=${encodeURIComponent(rid)}`).join("&")}`;
}

/** The refresh note that marks a failed read of each index, not an empty one. */
const INDEX_NOTES = Object.freeze({
  environments: "note",
  installations: "installationNote",
  plans: "planNote",
  changes: "changeNote",
  promotions: "promotionNote",
  products: "productsNote",
});

/**
 * Why a list that came back empty may not be an empty Hub. The Hub filters
 * every index by grant before it counts, so for a session without
 * resource:read at platform/root an empty index is "nothing you are granted",
 * which a session holding that read can tell apart from "nothing recorded".
 * Empty string when that is not knowable or not the case.
 */
export function grantNotice(capabilities, hub, indexes = []) {
  if (capabilities?.status !== "ready" || !hub || hub.loading !== false)
    return "";
  if (capabilities.globalOperations.includes(READ_RESOURCE)) return "";
  const filtered = indexes.filter(
    (index) =>
      Array.isArray(hub[index]) &&
      hub[index].length === 0 &&
      !hub[INDEX_NOTES[index] || ""],
  );
  if (!filtered.length) return "";
  return `You have no grants here. This session does not hold ${READ_RESOURCE} at ${PLATFORM_ROOT}, and the Hub returned no ${filtered.join(", ")} it is entitled to read. The Hub filters these lists by grant, so this is not evidence that none are recorded.`;
}

export function grantNoticeMarkup(capabilities, hub, indexes, esc) {
  const notice = grantNotice(capabilities, hub, indexes);
  return notice
    ? `<div role="status" data-no-grants class="so-warn-card p-2 text-[12px]"><p>${esc(notice)}</p></div>`
    : "";
}

/** The rail's statement of what navigation is missing, and why. */
export function navNoticeMarkup(capabilities, hidden, esc) {
  const status = capabilities?.status || "loading";
  if (status === "loading")
    return `<div role="status" data-capabilities="loading" class="px-2.5 text-[11px] text-[#8b96a5]">Reading session capabilities…</div>`;
  if (status === "unavailable")
    return `<div role="alert" data-capabilities="unavailable" class="so-warn-card p-2 text-[11px] space-y-1">
      <div class="font-semibold">Capabilities unavailable</div>
      <p>${esc(capabilities.reason)} Application navigation is incomplete: ${hidden.length} ${hidden.length === 1 ? "entry is" : "entries are"} not shown until the Hub answers.</p>
      <button type="button" class="so-btn so-btn-sm" data-onclick="retryCapabilities()">Retry</button>
    </div>`;
  const off = hidden.filter((item) => item.state === "disabled");
  if (!off.length) return "";
  return `<div role="status" data-capabilities="restricted" class="px-2.5 text-[11px] text-[#8b96a5]" title="${esc(off.map((item) => item.label).join(", "))}">${off.length} ${off.length === 1 ? "entry is" : "entries are"} not enabled for this session.</div>`;
}

/** What replaces an area or tab the session may not open. */
export function blockedMarkup(capabilities, label, decision, esc) {
  const requires = decision.requires.join(" or ");
  if (decision.state === "loading")
    return `<section class="so-panel p-3" role="status" data-capability-blocked="loading">Reading session capabilities for ${esc(label)}…</section>`;
  if (decision.state === "unavailable")
    return `<section class="so-panel p-3 space-y-2" role="alert" data-capability-blocked="unavailable">
      <h2 class="text-[13px] font-semibold">Capabilities unavailable</h2>
      <p class="text-[12px]">${esc(capabilities.reason)} ${esc(label)} is shown only when the session enables ${esc(requires)}, and the console does not assume it does.</p>
      <button type="button" class="so-btn so-btn-sm" data-onclick="retryCapabilities()">Retry</button>
    </section>`;
  return `<section class="so-panel p-3 space-y-2" role="status" data-capability-blocked="disabled">
    <h2 class="text-[13px] font-semibold">${esc(label)} is not enabled for this session</h2>
    <p class="text-[12px]">The Hub returned ${requires ? `the ${esc(requires)} application as off` : "none of this area's applications as enabled"} for your session. This is a missing grant, not missing data. A platform administrator can grant access.</p>
  </section>`;
}

/** The Settings table: each application, its state, and what it gates. */
export function capabilityTableMarkup(capabilities, esc) {
  const status = capabilities?.status || "loading";
  if (status !== "ready")
    return `<div class="so-warn-card p-2 text-[12px] mt-3" role="${status === "loading" ? "status" : "alert"}" data-session-applications="${esc(status)}">${
      status === "loading"
        ? "Reading session capabilities…"
        : `Capabilities unavailable. ${esc(capabilities.reason)} Application navigation is incomplete until the Hub answers; the console does not substitute a default set.`
    }</div>`;
  const gates = (id) =>
    Object.entries(NAV_APPLICATIONS)
      .filter(([, requires]) => requires.includes(id))
      .map(([nav]) => nav)
      .join(", ");
  const rows = capabilities.applications
    .map(
      (app) =>
        `<tr><td class="code-id">${esc(app.id)}</td><td><span class="kit-badge kit-badge-${app.enabled ? "ok" : "info"}">${app.enabled ? "Enabled" : "Off"}</span></td><td class="code-id text-[11px]">${esc(gates(app.id) || (HUB_APPLICATIONS.includes(app.id) ? "no console view" : "unknown to this console"))}</td></tr>`,
    )
    .join("");
  return `<table class="so-table mt-3" data-session-applications="ready">
    <thead><tr><th>Application</th><th>State for this session</th><th>Navigation it enables</th></tr></thead>
    <tbody>${rows || `<tr><td colspan="3" class="text-[#8b96a5]">The Hub returned no applications for this session.</td></tr>`}</tbody>
  </table>
  <div class="text-[11px] text-[#8b96a5] mt-2">Operations at ${esc(PLATFORM_ROOT)}: <span class="code-id">${esc(capabilities.globalOperations.join(", ") || "none")}</span></div>`;
}

async function readSession(path) {
  const response = await fetch(path, {
    headers: { accept: "application/json" },
    credentials: "same-origin",
    signal: AbortSignal.timeout(15000),
  });
  if (!response.ok)
    throw new Error(`The Hub answered ${response.status} to the session read.`);
  return response.json();
}

/**
 * The engine's binding. `sync` re-reads the session whenever the Hub read
 * model was replaced, which is the console's existing refresh cycle.
 */
export function createCapabilities({ state, render, read = readSession }) {
  let seenHub;
  let generation = 0;
  let watched = [];
  state.capabilities = CAPABILITIES_LOADING;

  async function load() {
    const mine = ++generation;
    let next;
    try {
      next = capabilitiesFromSession(await read(sessionPath(watched)));
    } catch (error) {
      next = capabilitiesUnavailable(
        error instanceof Error && /^The Hub answered \d+/.test(error.message)
          ? error.message
          : "The Hub did not answer the session read.",
      );
    }
    if (mine !== generation) return;
    const changed = JSON.stringify(next) !== JSON.stringify(state.capabilities);
    state.capabilities = next;
    if (changed) render();
  }

  function watch(resourceRids) {
    const fresh = resourceRids.filter(
      (rid) =>
        typeof rid === "string" &&
        rid !== PLATFORM_ROOT &&
        RESOURCE_RID.test(rid) &&
        !watched.includes(rid),
    );
    if (!fresh.length) return;
    watched = [...watched, ...fresh].slice(-MAXIMUM_RESOURCES);
    void load();
  }

  return {
    sync() {
      if (state.hub === seenHub) return;
      seenHub = state.hub;
      void load();
    },
    retry: () => load(),
    watch,
    /**
     * canPerform(operation, resourceRid) -> { allowed: true|false|null, reason }.
     * Asking about a resource the session has not reported adds it to the
     * next session read and re-renders when the answer arrives.
     */
    canPerform(operation, resourceRid = PLATFORM_ROOT) {
      const decision = decideOperation(
        state.capabilities,
        operation,
        resourceRid,
      );
      if (decision.allowed === null && state.capabilities.status === "ready")
        watch([resourceRid]);
      return decision;
    },
    navItems: (items, areas) =>
      filterNavItems(items, areas, state.capabilities),
    /** The tabs to paint for an area, or the markup that replaces it. */
    gateArea(areaId, area, tabId, esc) {
      const areaDecision = navDecision(state.capabilities, areaId);
      if (!areaDecision.visible)
        return {
          tabs: [],
          blocked: blockedMarkup(
            state.capabilities,
            area.label,
            areaDecision,
            esc,
          ),
        };
      const requested = area.tabs.find((tab) => tab.id === tabId);
      const tabDecision = requested
        ? navDecision(state.capabilities, `${areaId}/${requested.id}`)
        : { visible: true };
      if (!tabDecision.visible)
        return {
          tabs: [],
          blocked: blockedMarkup(
            state.capabilities,
            `${area.label} / ${requested.label}`,
            tabDecision,
            esc,
          ),
        };
      const tabs = area.tabs.filter(
        (tab) => navDecision(state.capabilities, `${areaId}/${tab.id}`).visible,
      );
      return { tabs, blocked: "" };
    },
    grantMarkup: (screen, esc) =>
      grantNoticeMarkup(
        state.capabilities,
        state.hub,
        screenEntry(screen)?.indexes || [],
        esc,
      ),
    /** The rail's statement of what is missing, kept above the sections. */
    paintNotice(areas, collapsed, esc) {
      const sections = document.getElementById("left-nav-sections");
      if (!sections?.parentElement) return;
      let notice = document.getElementById("capability-notice");
      if (!notice) {
        notice = document.createElement("div");
        notice.id = "capability-notice";
        // The rail spreads its children apart, so the notice shares one
        // wrapper with the sections to stay directly above them.
        const wrapper = document.createElement("div");
        wrapper.style.cssText =
          "display:flex;flex-direction:column;min-height:0;overflow-y:auto";
        sections.parentElement.insertBefore(wrapper, sections);
        wrapper.append(notice, sections);
      }
      const { hidden } = deriveNavigation(areas, state.capabilities);
      const html = navNoticeMarkup(state.capabilities, hidden, esc);
      const status = state.capabilities.status;
      const title =
        status === "unavailable"
          ? `Capabilities unavailable. ${state.capabilities.reason}`
          : status === "loading"
            ? "Reading session capabilities"
            : `${hidden.length} navigation entries are not enabled for this session.`;
      const next =
        collapsed && html
          ? `<div role="status" data-capabilities="${esc(status)}" class="text-center text-[#eab854] text-[12px]" title="${esc(title)}">!</div>`
          : html;
      // The rail repaints often; an unchanged notice keeps its Retry button.
      if (notice.dataset.painted === next) return;
      notice.dataset.painted = next;
      notice.className = html ? (collapsed ? "px-1 pt-2" : "px-2.5 pt-3") : "";
      notice.innerHTML = next;
    },
    bind(target) {
      target.retryCapabilities = () => load();
    },
  };
}

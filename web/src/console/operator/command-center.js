import { createOverviewSummary } from "./overview-summary.js";
import { newestAuditEntries } from "./audit-order.js";
// Command Center overview: what needs attention, from the Hub's own records.
// Decisions waiting, packaging runs that stopped, each environment's last
// report, and the newest audit entries. Pure markup; nothing here writes.

import { geographyMarkup, recordedSites } from "./command-center-map.js";
import { hubSetupMarkup } from "./empty-states.js";
import { freshnessPill, reportTime } from "./freshness.js";
import { gateResults, gateStepper, runState } from "./packaging-runs.js";
import { entityTitleMarkup } from "./kit.js";

const defaultEsc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );

const list = (value) => (Array.isArray(value) ? value : []);
const lower = (value) => String(value || "").toLowerCase();

const PENDING = /pending|awaiting|review|proposed/;
const ATTENTION = new Set(["blocked", "review", "failed"]);

function panel(title, count, screen, body, esc) {
  return `<section class="so-panel kit-view-panel kit-cc-panel">
    <div class="kit-view-head"><h2>${esc(title)}</h2><span class="kit-count" title="${esc(count)} ${esc(title.toLowerCase())}">${esc(count)}</span>
      <button type="button" class="so-btn so-btn-sm kit-cc-open" data-onclick="switchScreen('${esc(screen)}')">Open</button></div>
    ${body}
  </section>`;
}

const none = (text, esc) => `<p class="kit-muted">${esc(text)}</p>`;

/** Pending change requests and plans awaiting approval. */
export function decisions(hub) {
  const changes = list(hub?.changes).filter((r) =>
    PENDING.test(lower(r?.status)),
  );
  const plans = list(hub?.plans).filter((r) => PENDING.test(lower(r?.status)));
  return { changes, plans };
}

/** Packaging runs that are blocked, failed, or waiting on review. */
export function runsNeedingAttention(hub) {
  return list(hub?.packaging).filter((run) => ATTENTION.has(runState(run)));
}

export function activeWorkspaceProfiles(hub) {
  if (!Array.isArray(hub?.devWorkspaces) || hub.workspaceNote)
    return { available: false, groups: [], unknown: 0 };
  const profiles = new Map();
  let unknown = 0;
  for (const item of hub.devWorkspaces) {
    if (!["starting", "running", "stopped", "failed"].includes(item?.status)) {
      unknown++;
      continue;
    }
    if (!["starting", "running"].includes(item.status)) continue;
    if (typeof item.profileId !== "string" || !item.profileId) {
      unknown++;
      continue;
    }
    profiles.set(item.profileId, (profiles.get(item.profileId) || 0) + 1);
  }
  return {
    available: true,
    groups: [...profiles].sort(([a], [b]) => a.localeCompare(b)),
    unknown,
  };
}

export function governanceOpportunities(hub) {
  const changes = hub?.changeNote
    ? []
    : decisions(hub)
        .changes.map((change) => ({
          id: change.id,
          objectType: "ChangeRequest",
          label: change.title || change.id,
          detail: change.status,
          environment: change.environment || "",
        }))
        .filter((item) => typeof item.id === "string" && item.id);
  const drift = new Map();
  if (!hub?.installationNote)
    for (const item of list(hub?.installations)) {
      if (
        typeof item.environment !== "string" ||
        !item.environment ||
        typeof item.desiredVersion !== "string" ||
        !item.desiredVersion ||
        typeof item.observedVersion !== "string" ||
        !item.observedVersion ||
        item.desiredVersion === item.observedVersion
      )
        continue;
      drift.set(item.environment, (drift.get(item.environment) || 0) + 1);
    }
  return [
    ...changes,
    ...[...drift].map(([id, count]) => ({
      id,
      objectType: "Environment",
      label: `${id}: observed version drift`,
      detail: `${count} reported installation${count === 1 ? "" : "s"}`,
      environment: id,
    })),
  ];
}

const initialPresentation = () => ({
  selected: "",
  rotation: null,
  layers: { borders: true, links: true, labels: true, globe: true },
  message: "",
});

export function overviewMarkup(
  hub,
  { esc = defaultEsc, now = Date.now(), ui = initialPresentation() } = {},
) {
  if (!hub || hub.loading) {
    return `<div class="so-panel kit-view-panel"><p class="kit-muted">Reading the Hub…</p></div>`;
  }
  const { changes, plans } = decisions(hub);
  const decisionRows = [
    ...changes.map(
      (r) =>
        `<li><span class="kit-badge kit-badge-warn">change</span> <strong>${esc(r.title || r.id)}</strong> <code class="code-id">${esc(r.id)}</code></li>`,
    ),
    ...plans.map(
      (p) =>
        `<li><span class="kit-badge kit-badge-info">plan</span> <code class="code-id">${esc(p.id)}</code> ${esc(p.plan?.environment || "")} <span class="kit-muted">${esc(p.status)}</span></li>`,
    ),
  ];
  const decisionBody = hub.changeNote
    ? none(hub.changeNote, esc)
    : decisionRows.length
      ? `<ul class="kit-cc-list">${decisionRows.slice(0, 6).join("")}</ul>`
      : none("Nothing is waiting on a decision.", esc);

  const runs = runsNeedingAttention(hub);
  const runBody = hub.packagingNote
    ? none(hub.packagingNote, esc)
    : runs.length
      ? `<ul class="kit-cc-list">${runs
          .slice(0, 5)
          .map(
            (run) =>
              `<li><code class="code-id">${esc(run.runId)}</code> <span class="kit-badge kit-badge-${runState(run) === "review" ? "info" : runState(run) === "failed" ? "err" : "warn"}">${esc(runState(run))}</span>${gateStepper(gateResults(hub.packagingEventsByRun?.[run.runId]), esc)}</li>`,
          )
          .join("")}</ul>`
      : none(
          list(hub.packaging).length
            ? "No packaging run is blocked, failed, or waiting on review."
            : "No packaging run is recorded.",
          esc,
        );

  const installs = list(hub.installations);
  const envs = list(hub.environments);
  const envBody = hub.note
    ? none(hub.note, esc)
    : envs.length
      ? `<table class="so-table"><thead><tr><th>Environment</th><th>Spoke</th><th>Drifted</th><th>Last report</th></tr></thead><tbody>${envs
          .slice(0, 8)
          .map((row) => {
            const id = row?.environment?.id || "";
            const mine = installs.filter((i) => i?.environment === id);
            const drifted = mine.filter(
              (i) =>
                i?.observedVersion &&
                i?.desiredVersion &&
                i.observedVersion !== i.desiredVersion,
            ).length;
            return `<tr><td class="code-id">${esc(id)}</td><td>${esc(row?.spoke?.state || "unknown")}</td><td>${drifted} of ${mine.length}</td><td>${freshnessPill(reportTime(row?.reported?.observedAt, row?.spoke?.observedAt), { esc, now })}</td></tr>`;
          })
          .join("")}</tbody></table>`
      : none("No environment is enrolled.", esc);

  const audit = list(hub.audit);
  const auditBody = hub.auditNote
    ? none(hub.auditNote, esc)
    : audit.length
      ? `<ul class="kit-cc-list">${newestAuditEntries(audit, hub.auditOrder, 6)
          .map(
            (e) =>
              `<li><span class="code-id kit-muted">${esc(e.sequence)}</span> ${esc(e.actor || "")} · ${esc(e.action || "")}</li>`,
          )
          .join("")}</ul>`
      : none("No audit entry is recorded.", esc);

  const profiles = activeWorkspaceProfiles(hub);
  const profileBody = !profiles.available
    ? none("Workspace profile status is unavailable.", esc)
    : profiles.groups.length
      ? `<ul class="kit-cc-list">${profiles.groups.map(([id, count]) => `<li><code>${esc(id)}</code> <span>${count} recorded active workspace${count === 1 ? "" : "s"}</span></li>`).join("")}</ul>`
      : none("No known workspace is recorded as starting or running.", esc);
  const shieldSvg =
    '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M8 1.8l5 2v4.3c0 3.3-2.2 5.3-5 6.4-2.8-1.1-5-3.1-5-6.4V3.8l5-2z"/><path d="M5.5 8l1.8 1.8 3.2-3.3"/></svg>';
  const pulseSvg =
    '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6"><circle cx="8" cy="8" r="6.2"/><path d="M2.5 8h3l1.5-3 2 6 1.5-3h3"/></svg>';

  const opportunities = governanceOpportunities(hub);
  const governanceBody =
    opportunities
      .map((item) => {
        const isCr = item.objectType === "ChangeRequest";
        const iconSvg = isCr ? shieldSvg : pulseSvg;
        const tagTone = isCr ? "warn" : "info";
        const tagsHtml = `<span class="kit-badge kit-badge-${tagTone}">${esc(item.objectType)}</span>`;
        const titleMarkup = entityTitleMarkup({
          headingTag: "h3",
          title: esc(item.label),
          iconSvg,
          tagsHtml,
        });
        return `<article class="kit-cc-opportunity" data-opportunity-kind="${esc(item.objectType)}">${titleMarkup}<p class="kit-muted">${esc(item.detail)}</p><div class="kit-cc-actions"><button type="button" class="so-btn so-btn-sm" data-cc-opportunity="${esc(item.objectType)}:${esc(item.id)}" data-cc-purpose="ask" aria-label="Ask about ${esc(item.label)}">Ask</button><button type="button" class="so-btn so-btn-sm" data-cc-opportunity="${esc(item.objectType)}:${esc(item.id)}" data-cc-purpose="review" aria-label="Review ${esc(item.objectType)} ${esc(item.id)}">Review</button></div></article>`;
      })
      .join("") || none("No known review opportunity is recorded.", esc);

  return `<div class="kit-cc">
    ${hubSetupMarkup(hub, { esc })}
    <div data-overview-rpc></div>
    ${ui.message ? `<p class="kit-muted" role="status">${esc(ui.message)}</p>` : ""}
    ${geographyMarkup(hub, ui, esc)}
    ${panel("Governance opportunities", `${opportunities.length}${hub.changeNote || hub.installationNote ? " known" : ""}`, "rm07-change-requests", governanceBody + (hub.changeNote ? none(hub.changeNote, esc) : "") + (hub.installationNote ? none(hub.installationNote, esc) : ""), esc)}
    <div class="kit-cc-grid">
      ${panel("Active workspace profiles", profiles.available ? (profiles.unknown ? "Partial data" : `${profiles.groups.length} profiles`) : "Unavailable", "s02-workspaces", profileBody + (profiles.unknown ? none(`${profiles.unknown} workspace record(s) have unknown status or profile metadata.`, esc) : "") + none("Recorded workspace states; not a worker heartbeat.", esc), esc)}
      <section class="so-panel kit-view-panel"><div class="kit-view-head"><h2>Release control</h2></div><p class="kit-muted">Use the existing release views. The control-tower simulation is labeled example data.</p><div class="kit-cc-actions"><button type="button" class="so-btn so-btn-sm" data-cc-navigate="s15-plan-detail">Control tower · Example</button><button type="button" class="so-btn so-btn-sm" data-cc-navigate="rm06-promotion-pipe">Promotion pipeline</button></div></section>
    </div>
    <div class="kit-cc-grid">
      ${panel("Waiting on a decision", `${decisionRows.length}`, "rm07-change-requests", decisionBody, esc)}
      ${panel("Packaging runs needing attention", hub.packagingNote ? "Unavailable" : `${runs.length}`, "pkg-runs", runBody, esc)}
      ${panel("Environments", hub.note ? "Unavailable" : `${envs.length}`, "flt-environments", envBody, esc)}
      ${panel("Recorded operations", hub.auditNote ? "Unavailable" : `${audit.length} loaded`, "au01-audit", none("Recorded audit entries; live streaming is not available.", esc) + auditBody, esc)}
    </div>
  </div>`;
}

/** Mount behavior on the existing overview. These actions only navigate or draft. */
export function createCommandCenter({
  getHub,
  navigate,
  addContext,
  getIdentity = () => null,
}) {
  const mountOverview = createOverviewSummary({
    getIdentity,
    getReady: () => getHub()?.loading === false,
  });
  const ui = initialPresentation();
  let currentNode = null;
  function render(node) {
    currentNode = node;
    node.innerHTML = overviewMarkup(getHub(), { ui });
    // The loading markup has no overview slot. Mounting into the missing
    // node rejected on every first paint, unhandled.
    const overview = node.querySelector("[data-overview-rpc]");
    if (overview) void mountOverview(overview);
    const select = (id) => {
      const site = recordedSites(getHub()?.environments).find(
        (item) => item.id === id,
      );
      if (!site) return;
      ui.selected = site.id;
      if (site.location)
        ui.rotation = [-site.location[0], -site.location[1], 0];
      render(node);
      node.querySelector('[role="dialog"] button')?.focus();
    };
    const close = () => {
      const selected = ui.selected;
      ui.selected = "";
      render(node);
      [...node.querySelectorAll("[data-cc-site]")]
        .find((element) => element.getAttribute("data-cc-site") === selected)
        ?.focus();
    };
    const draft = (item, purpose) => {
      ui.message = addContext(item, purpose)
        ? ""
        : "Context could not be added. Review the existing assistant draft first.";
      render(node);
    };
    node.onclick = (event) => {
      const target = event.target instanceof Element ? event.target : null;
      if (!target) return;
      const layer = target.closest("[data-cc-layer]");
      if (layer && Object.hasOwn(ui.layers, layer.dataset.ccLayer)) {
        const name = layer.dataset.ccLayer;
        ui.layers[name] = !ui.layers[name];
        render(node);
        [...node.querySelectorAll("[data-cc-layer]")]
          .find((element) => element.getAttribute("data-cc-layer") === name)
          ?.focus();
        return;
      }
      const site = target.closest("[data-cc-site]");
      if (site) {
        select(site.dataset.ccSite);
        return;
      }
      if (target.closest("[data-cc-close]")) {
        close();
        return;
      }
      const nav = target.closest("[data-cc-navigate]");
      if (
        nav &&
        ["s15-plan-detail", "rm06-promotion-pipe"].includes(
          nav.dataset.ccNavigate,
        )
      ) {
        navigate(nav.dataset.ccNavigate);
        return;
      }
      const inspect = target.closest("[data-cc-inspect]");
      const environment = target.closest("[data-cc-environment]");
      if (inspect || environment) {
        const id =
          inspect?.dataset.ccInspect || environment?.dataset.ccEnvironment;
        if (
          !recordedSites(getHub()?.environments).some((item) => item.id === id)
        )
          return;
        const chip = {
          objectType: "Environment",
          id,
          label: id,
          environment: id,
        };
        if (inspect) navigate("flt-environments", chip);
        else if (
          ["ask", "coa", "share"].includes(environment.dataset.ccContext)
        )
          draft(chip, environment.dataset.ccContext);
        return;
      }
      const opportunity = target.closest("[data-cc-opportunity]");
      if (opportunity) {
        const item = governanceOpportunities(getHub()).find(
          (candidate) =>
            `${candidate.objectType}:${candidate.id}` ===
            opportunity.dataset.ccOpportunity,
        );
        if (!item || !["ask", "review"].includes(opportunity.dataset.ccPurpose))
          return;
        if (opportunity.dataset.ccPurpose === "review")
          navigate(
            item.objectType === "ChangeRequest"
              ? "rm07-change-requests"
              : "flt-environments",
            item,
          );
        else draft(item, "ask");
      }
    };
    node.onkeydown = (event) => {
      const target = event.target instanceof Element ? event.target : null;
      const marker = target?.closest('[role="button"][data-cc-site]');
      if (marker && ["Enter", " "].includes(event.key)) {
        event.preventDefault();
        select(marker.dataset.ccSite);
      }
      if (event.key === "Escape" && ui.selected) {
        event.preventDefault();
        close();
      }
    };
  }
  return {
    render,
    refresh: () => {
      if (currentNode?.isConnected) render(currentNode);
    },
  };
}

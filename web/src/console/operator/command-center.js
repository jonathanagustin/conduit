// Command Center overview: what needs attention, from the Hub's own records.
// Decisions waiting, packaging runs that stopped, each environment's last
// report, and the newest audit entries. Pure markup; nothing here writes.

import { hubSetupMarkup } from "./empty-states.js";
import { freshnessPill, reportTime } from "./freshness.js";
import { gateResults, gateStepper, runState } from "./packaging-runs.js";

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
    <div class="kit-view-head"><h2>${esc(title)}</h2><span class="kit-muted">${esc(count)}</span>
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

export function overviewMarkup(
  hub,
  { esc = defaultEsc, now = Date.now() } = {},
) {
  if (!hub) {
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
                i?.observedVersion && i.observedVersion !== i.desiredVersion,
            ).length;
            return `<tr><td class="code-id">${esc(id)}</td><td>${esc(row?.spoke?.state || "unknown")}</td><td>${drifted} of ${mine.length}</td><td>${freshnessPill(reportTime(row?.reported?.observedAt, row?.spoke?.observedAt), { esc, now })}</td></tr>`;
          })
          .join("")}</tbody></table>`
      : none("No environment is enrolled.", esc);

  const audit = list(hub.audit);
  const auditBody = hub.auditNote
    ? none(hub.auditNote, esc)
    : audit.length
      ? `<ul class="kit-cc-list">${audit
          .slice(-6)
          .reverse()
          .map(
            (e) =>
              `<li><span class="code-id kit-muted">${esc(e.sequence)}</span> ${esc(e.actor || "")} · ${esc(e.action || "")}</li>`,
          )
          .join("")}</ul>`
      : none("No audit entry is recorded.", esc);

  return `<div class="kit-cc">
    ${hubSetupMarkup(hub, { esc })}
    <div class="kit-cc-grid">
      ${panel("Waiting on a decision", `${decisionRows.length}`, "rm07-change-requests", decisionBody, esc)}
      ${panel("Packaging runs needing attention", `${runs.length}`, "pkg-runs", runBody, esc)}
      ${panel("Environments", `${envs.length}`, "flt-environments", envBody, esc)}
      ${panel("Recent audit", `${audit.length} recorded`, "au01-audit", auditBody, esc)}
    </div>
  </div>`;
}

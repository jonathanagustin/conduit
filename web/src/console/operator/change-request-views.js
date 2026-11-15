import { reviewState } from "./change-review.js";
const esc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
const columns = [
  ["Needs review", ["AWAITING_APPROVAL"]],
  ["Approved", ["APPROVED"]],
  ["Active", ["LEASED", "RUNNING"]],
  ["Succeeded", ["SUCCEEDED"]],
  ["Stopped", ["FAILED", "CANCELLED"]],
  ["Recorded only", ["RECORDED"]],
];
export function approvalColumns(plans) {
  const rows = Array.isArray(plans) ? plans : [];
  const known = new Set(columns.flatMap(([, statuses]) => statuses));
  return [
    ...columns.map(([label, statuses]) => ({
      label,
      rows: rows.filter((row) => statuses.includes(row.status)),
    })),
    {
      label: "Other / unavailable",
      rows: rows.filter((row) => !known.has(row.status)),
    },
  ];
}
export function approvalsMarkup(plans, note = "") {
  return `<section data-approvals-board class="space-y-3"><h2>Plan approvals pipeline</h2><p class="kit-muted">Visible Hub plan statuses. Counts are recorded approval rows, not an independent quorum, signature or execution decision. Recorded-only plans are not executable.</p>${note ? `<p>${esc(note)}</p>` : ""}<div class="grid md:grid-cols-3 gap-3">${approvalColumns(
    plans,
  )
    .map(
      (column) =>
        `<section class="so-panel p-3 space-y-2"><h3>${esc(column.label)} · ${column.rows.length}</h3>${column.rows.length ? column.rows.map((row) => `<button type="button" class="so-card p-2 text-left w-full" data-review-plan="${esc(row.id)}"><strong>${esc(row.id)}</strong><p>${esc(row.plan?.environment || "environment unavailable")} · ${esc(row.status || "status unavailable")}</p><p>${Array.isArray(row.approvals) ? row.approvals.length : "unavailable"} recorded approvals · required ${Number.isInteger(row.requiredApprovals) ? esc(row.requiredApprovals) : "unavailable"}</p>${row.changeRequestId ? `<p>Request ${esc(row.changeRequestId)}</p>` : ""}${row.failure ? `<p>${esc(row.failure)}</p>` : ""}</button>`).join("") : "<p>No visible plan.</p>"}</section>`,
    )
    .join("")}</div></section>`;
}
export function constraintGateMarkup(
  request,
  operatorName,
  operatorIdentities,
) {
  if (!request)
    return '<section class="so-panel p-3"><h2>Action constraint gate</h2><p>No selected request is in the visible index.</p></section>';
  const state = reviewState(request, operatorName, operatorIdentities);
  const requirements = Array.isArray(request.requirements)
    ? request.requirements
    : [];
  const evaluations = Array.isArray(request.evaluations)
    ? request.evaluations
    : [];
  return `<section data-cr-gate class="so-panel p-3 space-y-2"><h2>Action constraint gate</h2><h3>${esc(request.id)} · ${esc(request.status)}</h3><p>Two-person rule: the author cannot approve their own request.</p><p>Author: ${esc(request.author?.username || "unavailable")} · Current operator: ${esc(operatorName || "unavailable")}</p><p>${state.canReview ? "Review may be requested through the existing Change Requests flow; the Hub remains authoritative." : esc(state.reason)}</p><h4>Recorded policy evaluations</h4>${evaluations.length ? `<ul>${evaluations.map((row) => `<li>${esc(row.policyId)} · ${esc(row.outcome)} · ${esc(row.reason)}</li>`).join("")}</ul>` : "<p>No policy evaluation is recorded.</p>"}<h4>Required approvers</h4>${requirements.length ? `<ul>${requirements.map((row) => `<li>${esc(row.kind)} · ${esc(row.team || row.role || row.operation || "scope unavailable")} · ${esc(row.reason)} · satisfied by ${Array.isArray(row.satisfiedBy) && row.satisfiedBy.length ? row.satisfiedBy.map(esc).join(", ") : "none recorded"}</li>`).join("")}</ul>` : "<p>No reviewer requirement is recorded; this view does not infer permission to execute.</p>"}<p>Requirements satisfied: ${request.requirementsSatisfied === true ? "reported yes" : request.requirementsSatisfied === false ? "reported no" : "unavailable"}. No signature, maintenance or deployment grant is inferred here.</p><button type="button" class="so-btn" data-open-cr-review="${esc(request.id)}">Open existing request review</button></section>`;
}

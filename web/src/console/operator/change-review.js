import { changeDiffMarkup, diffValue } from "./change-diff.js";
// Change-request review: list, detail, and a two-person review form.
// Pure markup builders; the caller owns fetching and event wiring.

export const STATUS_FILTERS = [
  ["all", "All"],
  ["pending", "Pending"],
  ["approved", "Approved"],
  ["rejected", "Rejected"],
];

const FILTER_STATUS = {
  pending: "PENDING_APPROVALS",
  approved: "APPROVED",
  rejected: "REJECTED",
};

const BADGE = {
  PENDING_APPROVALS: "kit-badge-warn",
  APPROVED: "kit-badge-ok",
  REJECTED: "kit-badge-err",
};

function localEsc(value) {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

export function filterRequests(rows, filter) {
  const list = Array.isArray(rows) ? rows : [];
  const status = FILTER_STATUS[filter];
  if (!status) return list.slice();
  return list.filter((r) => r && r.status === status);
}

export function reviewState(request, operatorName) {
  const author = request?.author?.username ?? "";
  if (!author || !operatorName)
    return {
      isAuthor: false,
      canReview: false,
      reason:
        "Author or signed-in operator identity is unavailable; review is disabled.",
    };
  const isAuthor = Boolean(author) && author === operatorName;
  if (isAuthor) {
    return {
      isAuthor,
      canReview: false,
      reason:
        "You wrote this request. Another person must approve or reject it.",
    };
  }
  if (request?.status !== "PENDING_APPROVALS") {
    return {
      isAuthor,
      canReview: false,
      reason: `This request is ${request?.status ?? "unknown"}; it no longer takes a review.`,
    };
  }
  return { isAuthor, canReview: true, reason: "" };
}

function listColumn(all, shown, selectedId, filter, esc) {
  const chips = STATUS_FILTERS.map(
    ([key, label]) =>
      `<button type="button" class="kit-tab${key === filter ? " is-active" : ""}" data-cr-filter="${esc(key)}" aria-pressed="${key === filter}">${esc(label)}</button>`,
  ).join("");
  let body;
  if (all.length === 0) {
    body = `<p class="kit-empty">No change request is recorded.</p>`;
  } else if (shown.length === 0) {
    body = `<p class="kit-empty">No request matches.</p>`;
  } else {
    body = shown
      .map((r) => {
        const badge = BADGE[r.status] || "kit-badge-info";
        const selected = r.id === selectedId;
        const requirements = Array.isArray(r.requirements)
          ? r.requirements
          : [];
        const approvers =
          requirements
            .map(
              (row) => row.team || row.role || row.kind || "scope unavailable",
            )
            .join(", ") || "not recorded";
        return `<button type="button" class="so-list-item${selected ? " is-selected" : ""}" data-cr-select="${esc(r.id)}" aria-current="${selected}"><strong>${esc(r.title)}</strong> <code class="code-id">${esc(r.id)}</code> <span class="kit-badge ${badge}">${esc(r.status)}</span> <span class="so-muted">${esc(r.author?.username)}</span> <span class="kit-badge kit-badge-muted">Risk: not recorded</span><span>Required approvers: ${esc(approvers)}</span></button>`;
      })
      .join("");
  }
  return `<div class="so-col so-list"><div class="kit-tabs">${chips}</div>${body}</div>`;
}

function detailColumn(request, opts, esc) {
  const { operatorName, draft, pending, error } = opts;
  const state = reviewState(request, operatorName);
  const evaluations = Array.isArray(request.evaluations)
    ? request.evaluations
    : [];
  const changes = Array.isArray(request.changes) ? request.changes : [];
  const reviews = Array.isArray(request.reviews) ? request.reviews : [];
  const rationale = String(draft.rationale ?? "");
  const blocked = !state.canReview || pending || rationale.trim() === "";

  const why = evaluations.length
    ? `<ul>${evaluations
        .map(
          (e) =>
            `<li><code class="code-id">${esc(e.policyId)}</code> ${esc(e.outcome)}: ${esc(e.reason)}</li>`,
        )
        .join("")}</ul>`
    : `<p class="so-muted">No policy evaluation is recorded.</p>`;

  const changeList = changes.length
    ? `<ul>${changes
        .map(
          (c) =>
            `<li><code class="code-id">${esc(c.id)}</code> ${esc(c.type)} on <code class="code-id">${esc(c.installationId)}</code><dl>${[
              "entity",
              "startTime",
              "endTime",
              "type",
              "version",
              "targetVersion",
              "channel",
            ]
              .filter((key) => Object.hasOwn(c.request || {}, key))
              .map(
                (key) =>
                  `<dt>${esc(key)}</dt><dd><code>${esc(diffValue(c.request[key], key))}</code></dd>`,
              )
              .join("")}</dl></li>`,
        )
        .join("")}</ul>`
    : `<p class="so-muted">No change is attached.</p>`;

  const reviewList = reviews.length
    ? `<ul>${reviews
        .map(
          (r) =>
            `<li>${esc(r.decision)} by ${esc(r.subject)}: ${esc(r.rationale)}</li>`,
        )
        .join("")}</ul>`
    : `<p class="so-muted">No review is recorded.</p>`;

  // Approve and Reject only exist while this operator can still review: a
  // decided request, or one the operator wrote, shows why instead of buttons.
  const dis = `${blocked ? " disabled" : ""} data-can-review="1"`;
  const form = state.canReview
    ? `<form class="so-form" data-cr-form>
<label>Reason (required)<textarea data-cr-rationale required>${esc(rationale)}</textarea></label>
<div class="so-actions">
<button type="button" class="so-btn so-btn-primary" data-cr-decide="approve"${dis}>Approve</button>
<button type="button" class="so-btn so-btn-danger" data-cr-decide="reject"${dis}>Reject</button>
</div>
</form>`
    : `<p class="so-muted" data-cr-reason>${esc(state.reason)}</p>`;

  const confirm =
    state.canReview && draft.decision && !pending
      ? `<div class="so-confirm"><span>Confirm ${esc(draft.decision)} of ${esc(request.id)}?</span> <button type="button" class="so-btn so-btn-primary" data-cr-confirm>Confirm</button> <button type="button" class="so-btn" data-cr-cancel>Cancel</button></div>`
      : "";

  const err = error
    ? `<div class="kit-card kit-bar-err">${esc(error)}</div>`
    : "";

  return `<div class="so-col so-detail">
<h3>${esc(request.title)}</h3>
<p>${esc(request.description)}</p>
<p class="so-muted">${esc(request.environment)} · ${esc(request.targetKind)} <code class="code-id">${esc(request.targetId)}</code></p>
<div class="kit-card kit-bar-info">Two-person rule: the author cannot approve their own request. The Hub refuses a self-approval and records it.</div>
<h4>Why it needs review</h4>
${why}
<h4>Changes</h4>
${changeList}
${changeDiffMarkup(request)}
<h4>Required approvers</h4>
${(Array.isArray(request.requirements) ? request.requirements : []).map((row) => `<p>${esc(row.kind)} · ${esc(row.team || row.role || row.operation || "scope unavailable")} · ${esc(row.reason)}</p>`).join("") || '<p class="so-muted">No reviewer requirement is recorded.</p>'}
<h4>Reviews</h4>
${reviewList}
${form}
${confirm}
${err}
</div>`;
}

/**
 * @param {any[]} rows
 * @param {{ filter?: string, selectedId?: string, operatorName?: string, draft?: { decision: string, rationale: string }, pending?: boolean, error?: string, esc?: (value: any) => string }} [options]
 */
export function reviewMarkup(
  rows,
  {
    filter = "all",
    selectedId,
    operatorName,
    draft = { decision: "", rationale: "" },
    pending = false,
    error = "",
    esc = localEsc,
  } = {},
) {
  const all = Array.isArray(rows) ? rows : [];
  const shown = filterRequests(all, filter);
  const selected = selectedId
    ? shown.find((r) => r.id === selectedId)
    : shown[0];
  const left = listColumn(all, shown, selected?.id, filter, esc);
  const right = selected
    ? detailColumn(
        selected,
        {
          operatorName,
          draft: draft || { decision: "", rationale: "" },
          pending,
          error,
        },
        esc,
      )
    : `<div class="so-col so-detail">${selectedId ? "<p>The selected request is no longer in the visible filtered index.</p>" : ""}${error ? `<div class="kit-card kit-bar-err">${esc(error)}</div>` : ""}</div>`;
  return `<div class="so-split">${left}${right}</div>`;
}

/** Keep one review draft bound to a visible request and its current revision. */
export function bindReviewContext(rows, review, operatorName = "") {
  const shown = filterRequests(rows, review.filter || "all");
  if (!review.selectedId) review.selectedId = shown[0]?.id || "";
  const selected = shown.find((row) => row.id === review.selectedId) || null;
  const contextKey = selected
    ? JSON.stringify([
        operatorName,
        selected.id,
        selected.revision,
        selected.status,
        selected.author,
        selected.description,
        selected.changes,
        selected.evaluations,
        selected.requirements,
        selected.labels,
      ])
    : `missing:${review.selectedId}`;
  if (review.contextKey !== contextKey) {
    const hadDraft = Boolean(review.draft?.decision || review.draft?.rationale);
    review.draft = { decision: "", rationale: "" };
    if (hadDraft)
      review.error =
        "Request context changed; review the current record again.";
    review.contextKey = contextKey;
  }
  return selected;
}

/*
 * The delivery journey's panels: propose a Plan, approve or reject one, and
 * evaluate a promotion. Request shapes and state live in delivery-actions.js;
 * this file paints them and wires the buttons.
 *
 * Every write goes: form -> an explicit confirmation showing the exact
 * request -> in-flight with the controls disabled -> the Hub's answer. A
 * success is painted only from what the Hub returned, and is followed by a
 * refresh of the Hub indexes. A refusal is painted with the Hub's own reason.
 */
import {
  actionTransition,
  buildPlanDecision,
  buildPlanProposal,
  buildPromotionEvaluation,
  catalogReleases,
  defaultProposalBase,
  capabilityBlocks,
  deliveryCapabilityHook,
  idleAction,
  planReviewState,
  promotionEvidence,
  promotionOutcome,
  promotionStages,
  proposalBases,
  proposalOutcome,
  readValidationDetail,
  sendAction,
} from "./delivery-actions.js";

export function esc(value) {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

/** The stated reason a session capability withholds a write, inline. */
function capabilityNote(gate, escape) {
  return capabilityBlocks(gate)
    ? `<p role="status" data-dj-capability>${escape(gate.reason)}</p>`
    : "";
}

/** The same reason as the disabled control's tooltip. */
function capabilityTitle(gate, escape) {
  return capabilityBlocks(gate)
    ? ` data-dj-capability-blocked title="${escape(gate.reason)}"`
    : "";
}

const capabilityAttrs = (gate, escape) =>
  capabilityBlocks(gate) ? ` disabled${capabilityTitle(gate, escape)}` : "";

const TONE = {
  ok: "kit-bar-ok",
  err: "kit-bar-err",
  warn: "kit-bar-warn",
  info: "kit-bar-info",
};

/** The exact request, as text an operator can read before confirming. */
export function requestText(request) {
  const headers = Object.entries(request.headers || {})
    .map(([name, value]) => `${name}: ${value}`)
    .join("\n");
  return `${request.method} ${request.path}\n${headers ? `${headers}\n` : ""}\n${JSON.stringify(request.body, null, 2)}`;
}

/** Confirmation, in-flight and refusal chrome shared by every write. */
export function actionChromeMarkup(action, { noun = "request" } = {}) {
  const current = action || idleAction();
  if (current.phase === "confirm") {
    const gate = deliveryCapabilityHook(
      current.request.capability?.operation,
      current.request.capability?.resourceRid,
    );
    const rows = (current.request.summary || [])
      .map(([label, value]) => `<dt>${esc(label)}</dt><dd>${esc(value)}</dd>`)
      .join("");
    return `<div class="kit-card kit-bar-info space-y-2" data-dj-confirm-step>
      <strong>Confirm this ${esc(noun)}</strong>
      <p class="kit-muted">Nothing has been sent. Confirm sends exactly this to the Hub.</p>
      <dl class="kit-dl">${rows}</dl>
      <details><summary>Exact request</summary><pre class="code-id whitespace-pre-wrap break-all" data-dj-request>${esc(requestText(current.request))}</pre></details>
      ${capabilityNote(gate, esc)}
      <div class="so-actions"><button type="button" class="so-btn so-btn-primary" data-dj-confirm${capabilityAttrs(gate, esc)}>Confirm and send</button> <button type="button" class="so-btn" data-dj-cancel>Cancel</button></div>
    </div>`;
  }
  if (current.phase === "inflight")
    return `<div class="kit-card kit-bar-info" role="status" data-dj-inflight>Sent. Waiting for the Hub to answer; nothing is recorded as done yet.</div>`;
  if (current.phase === "failure")
    return `<div class="kit-card kit-bar-err" role="alert" data-dj-error>The Hub did not accept this ${esc(noun)}: ${esc(current.error)}</div>`;
  if (current.error)
    return `<div class="kit-card kit-bar-warn" role="alert" data-dj-invalid>${esc(current.error)}</div>`;
  return "";
}

const locked = (action) =>
  action?.phase === "confirm" || action?.phase === "inflight";
const dis = (flag) => (flag ? " disabled" : "");

function slot(state, key) {
  const store = (state.delivery ||= {});
  return (store[key] ||= { action: idleAction(), draft: {}, paint: null });
}

/** Two stacked hosts under one renderer root: the read view, then actions. */
export function deliverySlots(root) {
  root.innerHTML =
    '<div class="space-y-3"><div data-delivery-view></div><div data-delivery-actions></div></div>';
  return [
    root.querySelector("[data-delivery-view]"),
    root.querySelector("[data-delivery-actions]"),
  ];
}

/**
 * Wire one panel. `markup` paints from the slot; `build` turns the draft
 * into a request; `after` reads whatever the outcome needs once the Hub has
 * accepted the write.
 */
function mountPanel(
  node,
  state,
  key,
  { markup, build, after, explain, fetchImpl },
) {
  if (!node) return;
  const panel = slot(state, key);
  const paint = () => {
    if (node.isConnected) node.innerHTML = markup(panel);
  };
  panel.paint = paint;
  const repaint = () => panel.paint?.();
  // A background refresh repaints the whole view. The draft survives in the
  // slot; this puts the caret back in the field that was being typed in.
  node.onfocusin = (event) => {
    const target = event.target instanceof Element ? event.target : null;
    panel.focus = target?.getAttribute("data-dj-field") || "";
  };
  node.onfocusout = () => {
    if (node.isConnected) panel.focus = "";
  };
  const refocus = () => {
    if (!panel.focus) return;
    const field = [...node.querySelectorAll("[data-dj-field]")].find(
      (item) => item.getAttribute("data-dj-field") === panel.focus,
    );
    if (!field || field.disabled) return;
    field.focus();
    if (typeof field.value === "string" && field.setSelectionRange)
      field.setSelectionRange(field.value.length, field.value.length);
  };
  node.oninput = (event) => {
    const target = event.target instanceof Element ? event.target : null;
    const field = target?.getAttribute("data-dj-field");
    if (!field || target.tagName === "SELECT") return;
    panel.draft = { ...panel.draft, [field]: target.value };
    if (panel.action.error && panel.action.phase !== "inflight") {
      panel.action = actionTransition(panel.action, { type: "reset" });
    }
    node.querySelectorAll("[data-dj-needs]").forEach((button) => {
      const needed = (button.getAttribute("data-dj-needs") || "").split(",");
      button.disabled =
        button.hasAttribute("data-dj-capability-blocked") ||
        needed.some((name) => !String(panel.draft[name] || "").trim());
    });
  };
  node.onchange = (event) => {
    const target = event.target instanceof Element ? event.target : null;
    const field = target?.getAttribute("data-dj-field");
    if (!field || target.tagName !== "SELECT") return;
    panel.draft = { ...panel.draft, [field]: target.value };
    panel.action = actionTransition(panel.action, { type: "reset" });
    paint();
  };
  node.onclick = async (event) => {
    const hit = (selector) =>
      event.target instanceof Element ? event.target.closest(selector) : null;
    const review = hit("[data-dj-review]");
    if (review?.disabled) return;
    if (review) {
      panel.action = actionTransition(panel.action, {
        type: "review",
        request: build(panel, review.getAttribute("data-dj-review") || ""),
      });
      paint();
      return;
    }
    if (hit("[data-dj-cancel]")) {
      panel.action = actionTransition(panel.action, { type: "cancel" });
      paint();
      return;
    }
    if (hit("[data-dj-again]")) {
      panel.action = actionTransition(panel.action, { type: "reset" });
      panel.draft = {};
      paint();
      return;
    }
    const confirm = hit("[data-dj-confirm]");
    if (!confirm || confirm.disabled || panel.action.phase !== "confirm")
      return;
    const request = panel.action.request;
    panel.action = actionTransition(panel.action, { type: "confirm" });
    paint();
    let accepted = false;
    try {
      const answer = await sendAction(request, fetchImpl);
      const result = after ? await after(answer, request) : answer.body;
      panel.action = actionTransition(panel.action, {
        type: "resolved",
        result,
      });
      accepted = true;
    } catch (error) {
      // The Hub's reason first, verbatim; then any detail the Hub can add.
      const detail = explain ? await explain(error, request) : "";
      panel.action = actionTransition(panel.action, {
        type: "rejected",
        error: detail ? `${error?.message} (${detail})` : error?.message,
      });
    }
    repaint();
    // The affected indexes are read again only after the Hub answered.
    if (accepted) await state.liveHub?.refresh?.();
  };
  paint();
  refocus();
}

function options(rows, selected, label) {
  return rows
    .map(
      ([value, textLabel]) =>
        `<option value="${esc(value)}"${value === selected ? " selected" : ""}>${esc(textLabel ?? label?.(value) ?? value)}</option>`,
    )
    .join("");
}

function outcomeMarkup(tone, lines, extra = "") {
  return `<div class="kit-card ${TONE[tone] || TONE.info} space-y-1" data-dj-outcome="${esc(tone)}">${lines
    .map((line) => `<p>${esc(line)}</p>`)
    .join(
      "",
    )}${extra}<div class="so-actions"><button type="button" class="so-btn" data-dj-again>Done</button></div></div>`;
}

// ---------------------------------------------------------------------------
// Propose a Plan
// ---------------------------------------------------------------------------

export function proposalDraft(hub, environmentId, draft = {}) {
  const bases = proposalBases(hub?.plans, environmentId);
  const base =
    bases.find((plan) => plan.id === draft.basePlanId) ||
    defaultProposalBase(hub?.plans, environmentId);
  const entities = Array.isArray(base?.desiredState?.entities)
    ? base.desiredState.entities
    : [];
  const entity =
    entities.find((row) => row.name === draft.entityName) || entities[0];
  const releases = entity ? catalogReleases(hub?.products, entity.product) : [];
  const version = releases.some((row) => row.version === draft.version)
    ? draft.version
    : releases.find((row) => row.version !== entity?.version)?.version ||
      releases[0]?.version ||
      "";
  return { bases, base, entities, entity, releases, version };
}

export function proposalMarkup(hub, environmentId, panel) {
  const action = panel?.action || idleAction();
  const draft = panel?.draft || {};
  const head = `<h3>Propose a plan</h3>`;
  if (!environmentId)
    return `<section class="so-panel p-3 space-y-2" data-dj-panel="propose">${head}<p class="kit-muted">No environment is selected.</p></section>`;
  if (action.phase === "success") {
    const outcome = proposalOutcome(
      action.result?.plan,
      action.result?.changeRequest,
    );
    return `<section class="so-panel p-3 space-y-2" data-dj-panel="propose">${head}${outcomeMarkup(outcome.tone, outcome.lines)}</section>`;
  }
  const view = proposalDraft(hub, environmentId, draft);
  if (!view.base)
    return `<section class="so-panel p-3 space-y-2" data-dj-panel="propose">${head}<p class="kit-muted" data-dj-unavailable>No plan with a desired state is loaded for ${esc(environmentId)}. The console edits a desired state the Hub already accepted; the first one is proposed with <code class="code-id">conduit plan propose</code>.</p></section>`;
  const off = locked(action);
  const gate = deliveryCapabilityHook(
    "change:propose",
    `environment/${environmentId}`,
  );
  const ready =
    String(draft.generation || "").trim() &&
    String(draft.rationale || "").trim();
  return `<section class="so-panel p-3 space-y-2" data-dj-panel="propose">${head}
    <p class="kit-muted">Moves one installation of ${esc(environmentId)} to a release the Hub's catalog lists, by proposing the next desired state. Proposing does not approve or deploy anything: policy decides the review, and a second person approves.</p>
    <div class="so-form grid gap-2 md:grid-cols-2">
      <label>Base desired state <select data-dj-field="basePlanId"${dis(off)}>${options(
        view.bases.map((plan) => [
          plan.id,
          `${plan.id} · ${plan.status} · generation ${plan.desiredState.environment?.generation || "none"}`,
        ]),
        view.base.id,
      )}</select></label>
      <label>Installation <select data-dj-field="entityName"${dis(off)}>${options(
        view.entities.map((row) => [
          row.name,
          `${row.name} · ${row.product} · ${row.version}`,
        ]),
        view.entity?.name,
      )}</select></label>
      <label>Target release <select data-dj-field="version"${dis(off || !view.releases.length)}>${options(
        view.releases.map((row) => [
          row.version,
          row.version === view.entity?.version
            ? `${row.version} (current)`
            : row.version,
        ]),
        view.version,
      )}</select></label>
      <label>Generation (commit or immutable revision) <input type="text" data-dj-field="generation" value="${esc(draft.generation || "")}" autocomplete="off"${dis(off)} /></label>
      <label class="md:col-span-2">Reason (required) <textarea data-dj-field="rationale" rows="2"${dis(off)}>${esc(draft.rationale || "")}</textarea></label>
    </div>
    ${view.releases.length ? "" : `<p role="status" data-dj-no-release>The Hub's catalog lists no release for ${esc(view.entity?.product || "this product")}.</p>`}
    ${capabilityNote(gate, esc)}
    ${off ? "" : `<div class="so-actions"><button type="button" class="so-btn so-btn-primary" data-dj-review="propose" data-dj-needs="generation,rationale"${dis(!ready || !view.releases.length || capabilityBlocks(gate))}${capabilityTitle(gate, esc)}>Review proposal</button></div>`}
    ${actionChromeMarkup(action, { noun: "plan proposal" })}
  </section>`;
}

/** After a 201: read the governing change request, where a refusal lives. */
export async function readProposalOutcome(answer, fetchImpl = fetch) {
  const plan = answer.body;
  const id =
    answer.headers?.get?.("X-Change-Request-ID") || plan?.changeRequestId || "";
  if (!id) return { plan, changeRequest: null };
  try {
    const response = await fetchImpl(
      `/v1/change-requests/${encodeURIComponent(id)}`,
      { headers: { accept: "application/json" }, credentials: "same-origin" },
    );
    if (!response.ok) return { plan, changeRequest: null };
    return { plan, changeRequest: await response.json() };
  } catch {
    return { plan, changeRequest: null };
  }
}

export function mountPlanProposal(
  node,
  state,
  { environmentId = "", fetchImpl = fetch } = {},
) {
  const panel = slot(state, "propose");
  // The same rule the environment view uses: the selection when it names a
  // loaded environment, otherwise the first loaded one.
  const loaded = (
    Array.isArray(state.hub?.environments) ? state.hub.environments : []
  )
    .map((row) => row?.environment?.id)
    .filter(Boolean);
  environmentId = loaded.includes(environmentId)
    ? environmentId
    : loaded[0] || "";
  if (panel.environmentId !== environmentId && !locked(panel.action)) {
    panel.environmentId = environmentId;
    panel.draft = {};
    panel.action = idleAction();
  }
  mountPanel(node, state, "propose", {
    fetchImpl,
    markup: (current) =>
      proposalMarkup(state.hub, current.environmentId, current),
    build: (current) => {
      const view = proposalDraft(
        state.hub,
        current.environmentId,
        current.draft,
      );
      return buildPlanProposal({
        plans: state.hub?.plans,
        products: state.hub?.products,
        environmentId: current.environmentId,
        basePlanId: view.base?.id,
        entityName: view.entity?.name,
        version: view.version,
        generation: current.draft.generation,
        rationale: current.draft.rationale,
      });
    },
    after: (answer) => readProposalOutcome(answer, fetchImpl),
    // A refused document: ask the Hub's validator which rule it broke.
    explain: (error, request) =>
      error?.status === 409 || error?.status === 422
        ? readValidationDetail(request.body, fetchImpl)
        : "",
  });
}

// ---------------------------------------------------------------------------
// Approve or reject a Plan
// ---------------------------------------------------------------------------

export function decisionMarkup(plan, review, panel, { picker = "" } = {}) {
  const action = panel?.action || idleAction();
  const draft = panel?.draft || {};
  const head = `<h3>Approve or reject this plan</h3>${picker}`;
  const rule = `<div class="kit-card kit-bar-info">Two-person rule: the author of a change cannot approve it. The Hub refuses a self-approval and records it.</div>`;
  if (action.phase === "success") {
    const body = action.result || {};
    return `<section class="so-panel p-3 space-y-2" data-dj-panel="decide">${head}${outcomeMarkup(
      "ok",
      [
        `The Hub recorded the ${action.request?.summary?.find(([label]) => label === "Decision")?.[1] || "decision"}.`,
        `${body.id || "The record"} is now ${body.status || "status unavailable"}.`,
      ],
    )}</section>`;
  }
  if (!plan)
    return `<section class="so-panel p-3 space-y-2" data-dj-panel="decide">${head}<p class="kit-muted" data-dj-reason>No plan awaiting approval is selected.</p></section>`;
  const off = locked(action);
  const gate = deliveryCapabilityHook(
    "change:review",
    `environment/${plan.plan?.environment || ""}`,
  );
  const ready = String(draft.rationale || "").trim() !== "";
  const blocked = !ready || capabilityBlocks(gate);
  const form = review.canReview
    ? `<div class="so-form"><label>Reason (required)<textarea data-dj-field="rationale" rows="2"${dis(off)}>${esc(draft.rationale || "")}</textarea></label></div>
      ${review.note ? `<p class="kit-muted">${esc(review.note)}</p>` : ""}
      ${capabilityNote(gate, esc)}
      ${off ? "" : `<div class="so-actions"><button type="button" class="so-btn so-btn-primary" data-dj-review="approve" data-dj-needs="rationale"${dis(blocked)}${capabilityTitle(gate, esc)}>Approve</button> <button type="button" class="so-btn so-btn-danger" data-dj-review="reject" data-dj-needs="rationale"${dis(blocked)}${capabilityTitle(gate, esc)}>Reject</button></div>`}`
    : `<p class="so-muted" data-dj-reason>${esc(review.reason)}</p><div class="so-actions"><button type="button" class="so-btn so-btn-primary" disabled title="${esc(review.reason)}">Approve</button> <button type="button" class="so-btn so-btn-danger" disabled title="${esc(review.reason)}">Reject</button></div>`;
  return `<section class="so-panel p-3 space-y-2" data-dj-panel="decide">${head}
    <p><code class="code-id">${esc(plan.id)}</code> · ${esc(plan.status)} · ${esc(plan.plan?.environment || "environment unavailable")}${plan.changeRequestId ? ` · change request <code class="code-id">${esc(plan.changeRequestId)}</code>` : ""}</p>
    ${rule}${form}
    ${actionChromeMarkup(action, { noun: "plan decision" })}
  </section>`;
}

export function mountPlanDecision(
  node,
  state,
  { planId = "", board = false, fetchImpl = fetch } = {},
) {
  const key = board ? "decideBoard" : "decide";
  const panel = slot(state, key);
  const plans = () => (Array.isArray(state.hub?.plans) ? state.hub.plans : []);
  const awaiting = () =>
    plans().filter((plan) => plan.status === "AWAITING_APPROVAL");
  const selected = () => {
    if (!board)
      return planId
        ? plans().find((plan) => plan.id === planId) || null
        : plans()[0] || null;
    const rows = awaiting();
    return rows.find((plan) => plan.id === panel.draft.planId) || rows[0];
  };
  const targetId = selected()?.id || "";
  if (panel.planId !== targetId && !locked(panel.action)) {
    const kept = board ? { planId: targetId } : {};
    // A recorded decision stays on screen until dismissed, even though the
    // plan it answered has since left the awaiting list.
    if (panel.action.phase !== "success") {
      panel.planId = targetId;
      panel.draft = kept;
      panel.action = idleAction();
    }
  }
  mountPanel(node, state, key, {
    fetchImpl,
    markup: (current) => {
      const plan = selected();
      if (
        current.planId !== (plan?.id || "") &&
        !locked(current.action) &&
        current.action.phase !== "success"
      ) {
        current.planId = plan?.id || "";
        current.draft = board ? { planId: current.planId } : {};
        current.action = idleAction();
      }
      const picker = board
        ? `<label>Plan awaiting approval <select data-dj-field="planId"${dis(locked(current.action))}>${options(
            awaiting().map((row) => [
              row.id,
              `${row.id} · ${row.plan?.environment || "environment unavailable"}`,
            ]),
            plan?.id,
          )}</select></label>`
        : "";
      return decisionMarkup(
        plan,
        planReviewState(
          plan,
          state.hub?.changes,
          state.reviewOperatorName || "",
          state.reviewOperatorIdentities || [],
        ),
        current,
        { picker },
      );
    },
    build: (current, decision) => {
      const plan = selected();
      const review = planReviewState(
        plan,
        state.hub?.changes,
        state.reviewOperatorName || "",
        state.reviewOperatorIdentities || [],
      );
      if (!review.canReview) return { error: review.reason };
      return buildPlanDecision(plan, decision, current.draft.rationale);
    },
  });
}

// ---------------------------------------------------------------------------
// Promote a Release
// ---------------------------------------------------------------------------

export function promotionMarkup(hub, panel) {
  const action = panel?.action || idleAction();
  const draft = panel?.draft || {};
  const head = `<h3>Promote a release</h3>`;
  if (action.phase === "success") {
    const outcome = promotionOutcome(action.result);
    const reasons = outcome.reasons.length
      ? `<ul data-dj-blockers>${outcome.reasons.map((row) => `<li>${row.kind ? `<code class="code-id">${esc(row.kind)}</code> · ` : ""}${esc(row.message)}</li>`).join("")}</ul>`
      : "";
    return `<section class="so-panel p-3 space-y-2" data-dj-panel="promote">${head}${outcomeMarkup(
      outcome.tone,
      [outcome.headline, outcome.detail].filter(Boolean),
      reasons,
    )}</section>`;
  }
  const stages = promotionStages(hub?.plans, hub?.promotions);
  if (!stages.length)
    return `<section class="so-panel p-3 space-y-2" data-dj-panel="promote">${head}<p class="kit-muted" data-dj-unavailable>No promotion stage with a policy is loaded. The console evaluates a stage the Hub already holds a policy for; declare a pipeline in desired state with <code class="code-id">conduit product promotion set</code>.</p></section>`;
  const stage = stages.find((row) => row.key === draft.stage) || stages[0];
  const releases = catalogReleases(hub?.products, stage.product);
  const version = releases.some((row) => row.version === draft.version)
    ? draft.version
    : releases[0]?.version || "";
  const release = releases.find((row) => row.version === version);
  const evidence = release
    ? promotionEvidence(hub?.installations, release)
    : [];
  const off = locked(action);
  const gate = deliveryCapabilityHook("resource:operate", "platform/root");
  return `<section class="so-panel p-3 space-y-2" data-dj-panel="promote">${head}
    <p class="kit-muted">Asks the Hub to evaluate one release across one channel boundary. The Hub decides: PASSED promotes, anything else is recorded with its reasons and promotes nothing.</p>
    <div class="so-form grid gap-2 md:grid-cols-2">
      <label>Stage <select data-dj-field="stage"${dis(off)}>${options(
        stages.map((row) => [
          row.key,
          `${row.product} · ${row.fromChannel} -> ${row.toChannel} · ${row.source}`,
        ]),
        stage.key,
      )}</select></label>
      <label>Release <select data-dj-field="version"${dis(off || !releases.length)}>${options(
        releases.map((row) => [row.version, row.version]),
        version,
      )}</select></label>
    </div>
    <p class="kit-muted" data-dj-evidence>${evidence.length ? `${evidence.length} reported installation(s) at this version will be submitted as health evidence.` : "No loaded installation reports this version, so no health evidence will be submitted."} Healthy-since times are not in the fleet index and are not sent.</p>
    ${releases.length ? "" : `<p role="status" data-dj-no-release>The Hub's catalog lists no release for ${esc(stage.product)}.</p>`}
    ${capabilityNote(gate, esc)}
    ${off ? "" : `<div class="so-actions"><button type="button" class="so-btn so-btn-primary" data-dj-review="promote"${dis(!releases.length || capabilityBlocks(gate))}${capabilityTitle(gate, esc)}>Review evaluation</button></div>`}
    ${actionChromeMarkup(action, { noun: "promotion evaluation" })}
  </section>`;
}

export function mountPromotion(node, state, { fetchImpl = fetch } = {}) {
  mountPanel(node, state, "promote", {
    fetchImpl,
    markup: (current) => promotionMarkup(state.hub, current),
    build: (current) => {
      const stages = promotionStages(state.hub?.plans, state.hub?.promotions);
      const stage =
        stages.find((row) => row.key === current.draft.stage) || stages[0];
      const releases = catalogReleases(state.hub?.products, stage?.product);
      const version = releases.some(
        (row) => row.version === current.draft.version,
      )
        ? current.draft.version
        : releases[0]?.version || "";
      return buildPromotionEvaluation({
        stage,
        products: state.hub?.products,
        version,
        installations: state.hub?.installations,
      });
    },
  });
}

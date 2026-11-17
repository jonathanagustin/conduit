// Context chips and tool cards for the workbench assistant rail.
// Tool results come from the Hub. This file does not invent them.
import { postTool as postAssistantTool } from "./assistant-api.js";
import { attachControl, attachmentList } from "./attachments.js";
import { resolvePackagingActionContext } from "./packaging-action-context.js";

const MODES = [
  { id: "explore", label: "Explore" },
  { id: "release_plan", label: "Release & Plan" },
  { id: "config_diff", label: "Config & Diff" },
  { id: "ontology", label: "Ontology" },
];

const QUERY_TYPES = new Set([
  "Environment",
  "ChangeRequest",
  "Plan",
  "Artifact",
  "Spoke",
  "WorkProfile",
  "ReleaseChannel",
]);

function esc(value) {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function keyOf(chip) {
  return `${chip.objectType}:${chip.id}`;
}

export function contextChips(state) {
  const hub = state.hub || {};
  const found = [];
  const push = (chip) => {
    if (!chip?.id) return;
    if (found.some((item) => keyOf(item) === keyOf(chip))) return;
    found.push(chip);
  };
  const screen = state.detailScreen || state.screen;
  if (
    ["s08-staged-run", "pt02-proposals-evals", "s07-pipeline"].includes(screen)
  ) {
    for (const run of hub.packaging || []) {
      push({ id: run.runId, objectType: "PackagingRun", label: run.runId });
    }
  }
  if (screen === "rm08-bundle-preview") {
    const preview = hub.preview || {};
    push({
      id: preview.bundleId,
      objectType: "Artifact",
      label: preview.bundleId || "bundle",
    });
  }
  if (["rm07-change-requests", "s10-diff"].includes(screen)) {
    for (const change of hub.changes || []) {
      push({
        id: change.id,
        objectType: "ChangeRequest",
        label: change.title || change.id,
        environment: change.environment,
      });
    }
  }
  if (
    ["s14-env-detail", "rm01-env-page", "rm02-env-activity"].includes(screen)
  ) {
    for (const row of hub.environments || []) {
      const id = row.environment?.id || row.id;
      push({ id, objectType: "Environment", label: id });
    }
  }
  if (screen === "rm06-promotion-pipe") {
    for (const row of hub.promotions || []) {
      if (row.id)
        push({ id: row.id, objectType: "Plan", label: row.product || row.id });
    }
    for (const plan of hub.plans || []) {
      push({ id: plan.id, objectType: "Plan", label: plan.id });
    }
  }
  if (screen === "rm09-recalls") {
    for (const plan of hub.plans || []) {
      for (const recall of plan?.desiredState?.recalls || []) {
        push({
          id: recall.id,
          objectType: "Recall",
          label: recall.id || recall.product || "recall",
        });
      }
    }
  }
  const removed = new Set(state.assistantRemoved || []);
  const extras = state.assistantAdded || [];
  return [...found.filter((chip) => !removed.has(keyOf(chip))), ...extras];
}

function modeAllows(mode, tool) {
  const table = {
    explore: ["query_ontology_objects"],
    release_plan: ["query_ontology_objects", "propose_governed_action"],
    config_diff: ["query_ontology_objects", "review_change_diff"],
    ontology: [
      "query_ontology_objects",
      "propose_governed_action",
      "review_change_diff",
    ],
  };
  return (table[mode] || []).includes(tool);
}

// Guidance describes existing mode controls, not a permission or model-readiness claim.
function workspaceGuidance(mode) {
  const tools = [
    [
      "query_ontology_objects",
      "Inspect ontology objects",
      "Read recorded objects visible to the caller.",
    ],
    [
      "review_change_diff",
      "Review change diff",
      "Inspect the recorded before/after fields of a Change Request.",
    ],
    [
      "propose_governed_action",
      "Propose governed action",
      "Prepare a proposal; submission still follows Confirm or Change Request review.",
    ],
  ].filter(([tool]) => modeAllows(mode, tool));
  const toolRows = tools
    .map(
      ([, label, description]) =>
        `<li><span class="font-medium text-[#dce4ee]">${esc(label)}</span><p>${esc(description)}</p></li>`,
    )
    .join("");
  return `<section data-engineer-guidance aria-label="Workspace guidance" class="shrink-0 max-h-[180px] overflow-y-auto text-[10.5px] text-[#8b96a5]">
    <details class="kit-card"><summary class="cursor-pointer py-1 text-[#dce4ee]">Capabilities</summary><p>Tools shown in this mode. Each request uses your Hub permissions.</p>${toolRows ? `<ul class="space-y-2 mt-2">${toolRows}</ul>` : "<p>No tools are listed for this mode.</p>"}</details>
    <details class="kit-card"><summary class="cursor-pointer py-1 text-[#dce4ee]">Skills</summary><p>No skills inventory is available for this workspace.</p></details>
    <details class="kit-card"><summary class="cursor-pointer py-1 text-[#dce4ee]">Docs</summary><ul class="space-y-1"><li><a class="underline" href="https://github.com/jonathanagustin/conduit/blob/main/docs/architecture/engineer-conversation-focus.md" target="_blank" rel="noopener noreferrer">Engineer conversation guide</a></li><li><a class="underline" href="https://github.com/jonathanagustin/conduit/blob/main/docs/architecture/workspace-studio-scope.md" target="_blank" rel="noopener noreferrer">Workspace Studio scope</a></li><li><a class="underline" href="https://github.com/jonathanagustin/conduit/blob/main/docs/architecture/identity-and-governance.md" target="_blank" rel="noopener noreferrer">Identity and governed actions</a></li></ul></details>
  </section>`;
}

export function assistantChrome(state) {
  const mode = state.assistantMode || "explore";
  const chips = contextChips(state);
  const chipHtml = chips.length
    ? chips
        .map(
          (chip) =>
            `<button type="button" data-remove-chip="${esc(keyOf(chip))}" class="kit-chip" title="Remove ${esc(chip.objectType)} ${esc(chip.id)}">${esc(chip.objectType)} · ${esc(chip.label || chip.id)} ×</button>`,
        )
        .join("")
    : `<span class="text-[10.5px] text-[#6d7885]">No object on this screen.</span>`;
  const modes = MODES.map((item) => {
    const on = item.id === mode;
    return `<button type="button" data-set-mode="${item.id}" class="h-[23px] px-2 rounded text-[10.5px] border ${on ? "bg-[#162536] border-[#355982] text-[#dce8f6]" : "bg-[#12181e] border-[#222c36] text-[#8b96a5]"}">${esc(item.label)}</button>`;
  }).join("");
  const actions = chips
    .flatMap((chip) => actionsFor(state, chip))
    .map(
      (action) =>
        `<button type="button" data-assistant-action="${esc(action.kind)}" data-chip-key="${esc(action.key)}" class="h-[23px] px-2 rounded bg-[#12181e] border border-[#222c36] text-[10.5px] text-[#dce4ee] hover:text-white">${esc(action.label)}</button>`,
    )
    .join("");
  return `
    <header class="h-[38px] -mx-2.5 -mt-2.5 px-3 border-b border-[#1c242c] bg-[#0d1317] flex items-center justify-between gap-2 shrink-0">
      <div class="flex items-center gap-2 min-w-0">
        <span class="w-2 h-2 rounded-full bg-[#34d399]"></span>
        <span class="text-[12.5px] font-semibold text-white truncate">${state.engineerFocus ? "Engineer workspace" : "Assistant"}</span>
        <span class="kit-badge kit-badge-muted">${mode === "explore" ? "Read only" : "Governed"}</span>
      </div>
      <div class="flex items-center gap-1">
        <button type="button" data-engineer-focus-toggle data-onclick="toggleEngineerWorkspace()" class="kit-btn kit-btn-sm" aria-pressed="${Boolean(state.engineerFocus)}" aria-label="${state.engineerFocus ? "Return to workbench" : "Open engineer workspace"}">${state.engineerFocus ? "Return" : "Focus"}</button>
        <button type="button" data-onclick="toggleChatRail()" class="h-[23px] px-1.5 rounded bg-[#12181e] border border-[#222c36] text-[#8b96a5]" aria-label="Close assistant panel">×</button>
      </div>
    </header>
    ${state.engineerFocus ? '<nav class="flex flex-wrap gap-1" aria-label="Workspace studio"><button type="button" class="kit-btn" data-onclick="switchScreen(\'s09-source\')">Source</button><button type="button" class="kit-btn" data-onclick="switchScreen(\'s10-diff\')">Diff</button><button type="button" class="kit-btn" data-onclick="switchScreen(\'s11-preview\')">Preview</button></nav>' : ""}
    <div class="shrink-0 space-y-2">
      <div class="text-[10px] font-semibold uppercase tracking-[0.08em] text-[#7c8794]">Mode · ${esc(state.screen || "")}</div>
      <div class="flex flex-wrap gap-1" data-assistant-modes>${modes}</div>
      <div class="text-[10px] font-semibold uppercase tracking-[0.08em] text-[#7c8794]">Context</div>
      <div class="flex flex-wrap gap-1" data-assistant-chips>${chipHtml}</div>
      <div class="flex flex-wrap gap-1" data-assistant-actions>${actions}</div>
      <div class="text-[10px] font-semibold uppercase tracking-[0.08em] text-[#7c8794]">Attachments (${(state.attachments?.items || []).length}) · read only</div>
      <div class="max-h-[120px] overflow-y-auto">${attachmentList(state.attachments?.items)}</div>
      ${attachControl(state.attachments?.reason, "assistant")}
    </div>
    ${state.engineerFocus ? workspaceGuidance(mode) : ""}
    <div data-assistant-results class="shrink-0 max-h-[42%] overflow-y-auto space-y-2"></div>
  `;
}

function actionsFor(state, chip) {
  const mode = state.assistantMode || "explore";
  const actions = [];
  if (
    modeAllows(mode, "query_ontology_objects") &&
    QUERY_TYPES.has(chip.objectType)
  ) {
    actions.push({
      kind: "query",
      key: keyOf(chip),
      label: `Query ${chip.objectType}`,
    });
  }
  if (
    modeAllows(mode, "propose_governed_action") &&
    chip.objectType === "PackagingRun"
  ) {
    actions.push({
      kind: "propose-rerun",
      key: keyOf(chip),
      label: "Propose rerun",
    });
    actions.push({
      kind: "propose-cancel",
      key: keyOf(chip),
      label: "Propose cancel",
    });
    actions.push({
      kind: "write-case",
      key: keyOf(chip),
      label: "Add eval case",
    });
  }
  if (
    modeAllows(mode, "review_change_diff") &&
    chip.objectType === "ChangeRequest"
  ) {
    actions.push({ kind: "diff", key: keyOf(chip), label: "Review diff" });
  }
  return actions;
}

export function resultCard(result) {
  const label = result.label || "No model call";
  const tone = result.allowed
    ? "text-[#34d399]"
    : result.ok === false || result.allowed === false
      ? "text-[#f5a3a6]"
      : "text-[#dce4ee]";
  const entities = (result.entities || [])
    .map(
      (entity) =>
        `<button type="button" data-open-screen="${esc(routeFor(entity.objectType))}" class="px-1.5 py-0.5 rounded bg-[#14202e] border border-[#28405c] text-[10.5px] text-[#7cb4ff]">${esc(entity.label || entity.id)} · ${esc(entity.status || entity.objectType)}</button>`,
    )
    .join("");
  const constraints = (result.constraints || [])
    .map(
      (row) =>
        `<div class="${row.status === "ALLOW" ? "text-[#34d399]" : "text-[#f5a3a6]"}">${esc(row.status)} ${esc(row.id)} — ${esc(row.reason)}</div>`,
    )
    .join("");
  const hunks = (result.hunks || [])
    .map(
      (hunk) =>
        `<div class="font-mono text-[10.5px]"><div class="text-[#f5a3a6]">- ${esc(hunk.path)}: ${esc(hunk.before)}</div><div class="text-[#34d399]">+ ${esc(hunk.path)}: ${esc(hunk.after)}</div></div>`,
    )
    .join("");
  const ops = `R:${esc(result.readOps ?? 0)} W:${esc(result.writeOps ?? 0)}`;
  const opsTone =
    Number(result.writeOps) > 0
      ? "bg-[#2a2012] border-[#4f391b] text-[#eab854]"
      : "bg-[#10241d] border-[#1f4737] text-[#34d399]";
  const bar = result.applied
    ? "kit-bar-ok"
    : result.needsConfirm
      ? "kit-bar-ok"
      : result.changeRequestId
        ? "kit-bar-warn"
        : result.allowed === false
          ? "kit-bar-err"
          : "kit-bar-info";
  return `<article data-result-index="${esc(result.index ?? "")}" class="kit-card ${bar} kit-tool">
    <div class="flex items-center justify-between gap-2">
      <span class="font-medium text-[#dce4ee]">${esc(result.tool || "tool")}</span>
      <span class="text-[10px] px-1.5 py-0.5 rounded border tabular-nums ${opsTone}">${ops}</span>
    </div>
    <div class="text-[10px] text-[#8b96a5]">${esc(label)}</div>
    ${result.error ? `<div class="text-[#f5a3a6]">${esc(result.error)}</div>` : ""}
    ${result.reason ? `<div class="${tone}">${esc(result.reason)}</div>` : ""}
    ${result.execution ? `<div data-assistant-execution class="text-[#8b96a5]">${esc(executionText(result.execution))}</div>` : ""}
    ${constraints}
    ${result.matchedCount != null && result.tool === "query_ontology_objects" ? `<div class="text-[#8b96a5]">${esc(result.matchedCount)} matched · ${esc(result.durationMs || 0)} ms</div>` : ""}
    ${entities ? `<div class="flex flex-wrap gap-1">${entities}</div>` : ""}
    ${result.changeRequestId ? `<div class="text-[#8b96a5]">Change request <span class="code-id text-white">${esc(result.changeRequestId)}</span> ${esc(result.changeRequestStatus || "")}</div>` : ""}
    ${result.decisionAuditEventId ? `<div class="text-[#8b96a5]">Rule audit <span class="code-id text-white">${esc(result.decisionAuditEventId)}</span></div>` : ""}
    ${result.auditEventId ? `<div class="text-[#8b96a5]">Tool audit <span class="code-id text-white">${esc(result.auditEventId)}</span></div>` : ""}
    ${hunks || (result.tool === "review_change_diff" ? `<div class="text-[#8b96a5]">${result.hunks?.length ? "" : "This change request has no stored field diff."}</div>` : "")}
    ${result.title ? `<div class="text-[#dce4ee]">${esc(result.title)}</div>` : ""}
    ${result.objectId ? `<div class="text-[#8b96a5]">Object <a class="code-id text-white underline" href="${result.changeRequestId ? "#area-changes" : "#area-intelligence"}">${esc(result.objectId)}</a> · ${esc(result.state || "")}</div>` : ""}
    ${formFor(result)}
  </article>`;
}

function formFor(result) {
  const fields = result.fields || [];
  if (!fields.length && !result.needsConfirm) return "";
  const inputs = fields
    .map((field) => {
      if (field.options && field.options.length) {
        return `<label class="kit-muted">${esc(field.label)}<select name="${esc(field.name)}" class="kit-select">${field.options
          .map(
            (option) =>
              `<option value="${esc(option)}">${esc(option)}</option>`,
          )
          .join("")}</select></label>`;
      }
      return `<label class="kit-muted">${esc(field.label)}<input name="${esc(field.name)}" value="${esc(field.value || "")}" class="kit-input" /></label>`;
    })
    .join("");
  return `<form data-elicit class="kit-card ${result.needsConfirm ? "kit-bar-ok" : "kit-bar-warn"} kit-tool">${result.needsConfirm ? "" : `<div class="kit-section">Required inputs</div>`}${inputs}<button type="button" data-resume-write="1" data-confirm="${result.needsConfirm ? "1" : "0"}" class="kit-btn kit-btn-sm kit-btn-ok">${result.needsConfirm ? "Confirm" : "Continue"}</button></form>`;
}

function routeFor(objectType) {
  switch (objectType) {
    case "Environment":
      return "s14-env-detail";
    case "ChangeRequest":
      return "rm07-change-requests";
    case "Plan":
      return "s15-plan-detail";
    case "Artifact":
      return "rm08-bundle-preview";
    case "ReleaseChannel":
      return "rm06-promotion-pipe";
    default:
      return "s19-ontology";
  }
}

export function publishAssistantContext(state) {
  const chips = contextChips(state).map((chip) => ({
    id: chip.id,
    objectType: chip.objectType,
    label: chip.label || chip.id,
  }));
  window.__conduitAssistantContext = {
    mode: state.assistantMode || "explore",
    chips,
  };
}

export async function resumeWrite(state, args) {
  return postTool(state, {
    mode: state.assistantMode || "ontology",
    tool: "governed_write",
    arguments: args,
  });
}

export async function runAssistantAction(state, kind, chipKey) {
  const chip = contextChips(state).find((item) => keyOf(item) === chipKey);
  if (!chip)
    return {
      label: "No model call",
      ok: false,
      error: "That chip is no longer on this screen.",
      tool: kind,
    };
  const mode = state.assistantMode || "explore";
  if (kind === "query") {
    return postTool(state, {
      mode,
      tool: "query_ontology_objects",
      arguments: {
        objectType: chip.objectType,
        filter: { field: "id", value: chip.id },
      },
    });
  }
  if (kind === "diff") {
    return postTool(state, {
      mode,
      tool: "review_change_diff",
      arguments: { changeRequestId: chip.id },
    });
  }
  // There is no "propose memory" action here: the Hub has no action rule for
  // propose_memory and refuses it, so the rail does not offer it.

  if (kind === "write-case") {
    return postTool(state, {
      mode: "ontology",
      tool: "governed_write",
      arguments: { action: "add_eval_case", targetId: chip.id },
    });
  }
  if (!["propose-cancel", "propose-rerun"].includes(kind)) {
    return {
      label: "No model call",
      tool: "governed_write",
      ok: false,
      allowed: false,
      reason: "That action has no assistant executor.",
      readOps: 0,
      writeOps: 0,
    };
  }
  const actionId =
    kind === "propose-cancel" ? "cancel_packaging_run" : "rerun_packaging_run";
  let fields;
  try {
    fields = await resolvePackagingActionContext(state, chip.id);
    // The resolver's return is an async boundary too. A removal, new
    // selection or newer action can happen before this continuation runs.
    if (
      !fields.isCurrent() ||
      !contextChips(state).some((candidate) => keyOf(candidate) === chipKey)
    )
      throw new Error(
        "The selected packaging context changed. Refresh the run before proposing an action.",
      );
  } catch (error) {
    return {
      label: "No model call",
      tool: "propose_governed_action",
      ok: false,
      allowed: false,
      reason:
        error instanceof Error
          ? error.message
          : "The selected packaging context is unavailable.",
      readOps: 0,
      writeOps: 0,
    };
  }
  return postTool(state, {
    mode,
    tool: "propose_governed_action",
    arguments: {
      actionId,
      targetEntityId: chip.id,
      targetObjectType: chip.objectType,
      environment: fields.environment,
      installationId: fields.installation,
      productId: fields.product,
    },
  });
}

// The rail's tool calls leave through the assistant's one Hub client.
function postTool(_state, body) {
  return postAssistantTool(body);
}

export function rememberResult(state, result) {
  state.assistantResults = [result, ...(state.assistantResults || [])].slice(
    0,
    6,
  );
}

export function resultsMarkup(state) {
  return (state.assistantResults || [])
    .map((result, index) => resultCard({ ...result, index }))
    .join("");
}

function executionText(receipt) {
  if (receipt.error) return "Execution failed: " + receipt.error;
  switch (receipt.outcome) {
    case "CANCEL_REQUEST_ACCEPTED":
      return "Cancellation requested; termination has not been observed.";
    case "CANCELLATION_OBSERVED":
      return "Run cancellation observed.";
    case "RUN_STARTED":
      return "New packaging run requested: " + (receipt.runId || "");
    case "RUN_OBSERVED":
      return "Existing successor run observed: " + (receipt.runId || "");
    default:
      return (
        "Execution " +
        (receipt.state || "unavailable") +
        "; resume the recorded approval to recover a missing acknowledgement."
      );
  }
}

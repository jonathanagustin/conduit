// Packaging progress is a recorded worker history, never Plan/approval state.
const esc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/gu,
    (char) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        char
      ],
  );
export function stepTone(outcome) {
  const value = String(outcome || "").toUpperCase();
  if (value === "PASS") return "ok";
  if (/FAIL|REJECT|BLOCK|TIMED_OUT|CANCEL/u.test(value)) return "err";
  if (["WAITING", "RUNNING", "STARTED", "PENDING"].includes(value))
    return "info";
  return "muted";
}
export function selectedPackagingRun(runs, id) {
  const list = Array.isArray(runs) ? runs : [];
  return id ? list.find((run) => run.runId === id) || null : list[0] || null;
}
// Preserve contiguous visits, including a step revisited after a later step.
// Do not synthesize future steps, reorder by wall clock, or mutate records.
export function stageChunks(events) {
  const chunks = [];
  let previous = null;
  let firstSequence = null;
  let ordered = true;
  for (const event of Array.isArray(events) ? events : []) {
    if (!event || typeof event.step !== "string" || !event.step) {
      ordered = false;
      continue;
    }
    if (
      !Number.isSafeInteger(event.sequence) ||
      event.sequence < 1 ||
      (previous !== null && event.sequence !== previous + 1)
    )
      ordered = false;
    if (firstSequence === null) firstSequence = event.sequence;
    previous = event.sequence;
    let chunk = chunks.at(-1);
    if (!chunk || chunk.step !== event.step) {
      chunk = { id: String(chunks.length), step: event.step, events: [] };
      chunks.push(chunk);
    }
    chunk.events.push(event);
  }
  return { chunks, ordered, firstSequence: firstSequence ?? 0 };
}
export async function readPackagingHistory(runId, read) {
  const base = `/v1/packaging-runs/${encodeURIComponent(runId)}/events`;
  const items = [];
  const seen = new Set();
  let cursor = "";
  let total = 0;
  const incomplete = (note) => ({ items, total, complete: false, note });
  for (let page = 0; page < 16; page++) {
    let body;
    try {
      body = await read(
        base + (cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""),
      );
    } catch {
      return incomplete(
        "Recorded history could not be read completely. Refresh to retry.",
      );
    }
    if (!Array.isArray(body?.items) || items.length + body.items.length > 256)
      return incomplete("Recorded history exceeded the bounded view.");
    items.push(...body.items);
    total =
      Number.isSafeInteger(body.total) && body.total >= 0
        ? body.total
        : items.length;
    if (!body.nextCursor)
      return {
        items,
        total,
        complete: items.length === total,
        note:
          items.length !== total
            ? "Only a partial or changed history was returned."
            : Number.isSafeInteger(items[0]?.sequence) && items[0].sequence > 1
              ? `Only retained history is available, starting at #${items[0].sequence}.`
              : "",
      };
    if (
      typeof body.nextCursor !== "string" ||
      body.nextCursor.length > 2048 ||
      seen.has(body.nextCursor)
    )
      return incomplete("History continuation was refused. Refresh to resync.");
    cursor = body.nextCursor;
    seen.add(cursor);
  }
  return incomplete(
    "Recorded history reached the page limit. Refresh to resync.",
  );
}
const badge = (outcome) =>
  `<span class="kit-badge kit-badge-${stepTone(outcome)}">${esc(outcome || "Not recorded")}</span>`;
const rows = (values) =>
  `<dl class="kit-stack">${values.map(([key, value]) => `<div><dt class="kit-muted">${esc(key)}</dt><dd class="code-id">${esc(value || "Not recorded")}</dd></div>`).join("")}</dl>`;
function selector(runs, run) {
  return `<label class="plan-label">Packaging run <select class="kit-select" data-packaging-run><option value="">Choose a run</option>${runs.map((item) => `<option value="${esc(item.runId)}"${item.runId === run?.runId ? " selected" : ""}>${esc(item.runId)}</option>`).join("")}</select></label>`;
}
function eventOutput(events) {
  return `<pre class="code-id" style="white-space:pre-wrap;overflow-wrap:anywhere">${events.map((event) => esc(`#${event.sequence ?? "?"} ${event.at || "Time not recorded"} ${event.outcome || "Outcome not recorded"}\n${event.reason || "No step output recorded."}`)).join("\n\n")}</pre>`;
}
function notice(history, ordered, firstSequence) {
  const retained =
    !history?.note && Number.isSafeInteger(firstSequence) && firstSequence > 1
      ? `<p role="status" class="kit-muted">Only retained history is available, starting at #${firstSequence}.</p>`
      : "";
  return `${retained}${history?.note ? `<p role="status" class="kit-muted">${esc(history.note)}</p>` : ""}${ordered ? "" : '<p role="status" class="kit-muted">History is incomplete or not in strict sequence. Only recorded entries are shown.</p>'}`;
}
function graph(chunks, selected) {
  return chunks.length
    ? `<ol class="kit-header-title" aria-label="Recorded stage graph">${chunks.map((chunk, index) => `<li><button type="button" class="kit-btn${chunk.id === selected?.id ? " so-btn so-btn-blue" : ""}" aria-pressed="${chunk.id === selected?.id}" data-packaging-stage="${chunk.id}"><span>${esc(chunk.step)}</span> ${badge(chunk.events.at(-1).outcome)}</button>${index < chunks.length - 1 ? '<span aria-hidden="true"> → </span>' : ""}</li>`).join("")}</ol>`
    : '<p class="kit-muted">No packaging step has been recorded.</p>';
}
function header(runs, run, title) {
  return `<header class="kit-card kit-pad"><h2>${title}</h2>${selector(runs, run)}${run ? `<p>${badge(run.outcome)} <span class="kit-muted">Phase: ${esc(run.phase || "Not recorded")}</span></p><p class="kit-muted">Run progress is separate from Plan and approval status.</p>${run.configured === false ? '<p role="status">The packaging worker is not configured.</p>' : ""}` : '<p class="kit-muted">No selected packaging run is available.</p>'}</header>`;
}
/**
 * @param {any[]} runs
 * @param {any[]} events
 * @param {{runId?: string, stageId?: string, history?: {note?: string, total?: number, complete?: boolean}}} options
 */
export function pipelineBuilderMarkup(
  runs,
  events,
  { runId = "", stageId = "", history } = {},
) {
  const run = selectedPackagingRun(runs, runId);
  const head = header(runs, run, "Packaging pipeline");
  if (!run) return head;
  const { chunks, ordered, firstSequence } = stageChunks(events);
  const selected =
    chunks.find((chunk) => chunk.id === stageId) || chunks.at(-1);
  const latest = selected?.events.at(-1);
  return `<div class="kit-stack" data-packaging-pipeline>${head}${notice(history, ordered, firstSequence)}<div class="kit-grid">
    <section class="kit-card kit-pad" data-stage-graph><h3>Recorded stage graph</h3><p class="kit-muted">Read-only recorded order. Pipeline edits require a reviewed change request.</p>${graph(chunks, selected)}</section>
    <section class="kit-card kit-pad" data-stage-inspector><h3>Selected stage</h3>${
      latest
        ? rows([
            ["Step", selected.step],
            ["Outcome", latest.outcome],
            ["First event", selected.events[0].sequence],
            ["Last event", latest.sequence],
            ["Recorded at", latest.at],
            ["Reason", latest.reason],
          ])
        : '<p class="kit-muted">Select a recorded stage.</p>'
    }</section>
    <section class="kit-card kit-pad" data-stage-preview><h3>Recorded output preview</h3>${selected ? eventOutput(selected.events) : '<p class="kit-muted">No output recorded.</p>'}</section>
    <section class="kit-card kit-pad" data-stage-references><h3>Recorded references</h3>${rows(
      [
        ["Run", run.runId],
        ["Source digest", latest?.sourceDigest || run.sourceDigest],
        ["Stage artifact digest", latest?.artifactDigest],
        ["Run artifact digest", run.artifactDigest],
        ["Proposal revision", latest?.proposalRevision || run.proposalRevision],
        ["Stage Plan reference", latest?.planId],
        ["Run Plan reference", run.planId || run.targetPlanRef],
        ["Approval reference", latest?.approvalId],
      ],
    )}<p class="kit-muted">References do not establish approval or signature verification.</p><button type="button" class="kit-btn" data-packaging-navigate="s08-staged-run">Open staged run</button></section>
  </div></div>`;
}
/**
 * @param {any[]} runs
 * @param {any[]} events
 * @param {{runId?: string, stageId?: string, section?: string, history?: {note?: string, total?: number, complete?: boolean}}} options
 */
export function stagedRunMarkup(
  runs,
  events,
  { runId = "", section = "stages", stageId = "", history } = {},
) {
  const run = selectedPackagingRun(runs, runId);
  const head = header(runs, run, "Staged packaging run");
  if (!run) return head;
  const { chunks, ordered, firstSequence } = stageChunks(events);
  const selected =
    chunks.find((chunk) => chunk.id === stageId) || chunks.at(-1);
  const sections = [
    ["stages", "Stages"],
    ["inputs", "Inputs & config"],
    ["artifacts", "Artifacts & evidence"],
    ["output", "Recorded output"],
  ];
  const active = sections.some(([id]) => id === section) ? section : "stages";
  let content;
  if (active === "stages")
    content = `${graph(chunks, selected)}<div class="kit-stack">${chunks.map((chunk) => `<details class="kit-card kit-pad" data-stage-chunk="${chunk.id}"${chunk.id === selected?.id ? " open" : ""}><summary>${esc(chunk.step)} · #${esc(chunk.events[0].sequence)}–#${esc(chunk.events.at(-1).sequence)} ${badge(chunk.events.at(-1).outcome)}</summary>${eventOutput(chunk.events)}</details>`).join("") || '<p class="kit-muted">No stage events recorded.</p>'}</div>`;
  else if (active === "inputs")
    content = `${rows([
      ["Source digest", run.sourceDigest],
      ["Template", run.templateVersion],
      ["Tool versions", run.toolVersions],
      ["Target Plan reference", run.targetPlanRef],
    ])}<p class="kit-muted">No source file bodies or rendered configuration are included in this run record.</p>`;
  else if (active === "artifacts")
    content = `${rows([
      ["Artifact digest", run.artifactDigest],
      ["Proposal revision", run.proposalRevision],
      ["Plan reference", run.planId],
    ])}<p class="kit-muted">No downloadable artifact reference, SBOM, or signature verification receipt is supplied by this run record.</p>`;
  else
    content = `<p class="kit-muted">Worker-recorded step output; raw subprocess logs are not included.</p>${eventOutput(events)}`;
  return `<div class="kit-stack" data-staged-run>${head}${notice(history, ordered, firstSequence)}<div class="kit-header-title" role="group" aria-label="Run detail sections">${sections.map(([id, label]) => `<button type="button" class="kit-btn" aria-pressed="${active === id}" data-packaging-section="${id}">${label}</button>`).join("")}<section class="kit-card kit-pad">${content}</section><button type="button" class="kit-btn" data-packaging-navigate="s07-pipeline">Open pipeline graph</button></div>`;
}

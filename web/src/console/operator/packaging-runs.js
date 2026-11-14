// Packaging runs list: KPI tiles by state and one row per recorded run with
// its gate results. Built from the runs and step events live-hub.js loads.

const defaultEsc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );

/** Groups a run into succeeded, running, blocked, or failed. */
export function runState(run) {
  const outcome = String(run?.outcome || "").toUpperCase();
  const phase = String(run?.phase || "").toLowerCase();
  if (outcome === "PASS" || outcome === "SUCCEEDED" || phase === "succeeded") {
    return "succeeded";
  }
  if (outcome.startsWith("FAIL") || outcome === "TIMED_OUT" || phase === "failed") {
    return phase === "blocked" ? "blocked" : "failed";
  }
  if (phase === "blocked" || outcome === "WAITING") return "blocked";
  return "running";
}

const TONE = { succeeded: "ok", running: "info", blocked: "warn", failed: "err" };

/** Latest outcome per step for one run, in first-seen step order. */
export function gateResults(events) {
  const order = [];
  const latest = new Map();
  for (const event of Array.isArray(events) ? events : []) {
    if (!event?.step) continue;
    if (!latest.has(event.step)) order.push(event.step);
    latest.set(event.step, event.outcome || "");
  }
  return order.map((step) => ({ step, outcome: latest.get(step) }));
}

export function runCounts(runs) {
  const counts = { succeeded: 0, running: 0, blocked: 0, failed: 0 };
  for (const run of Array.isArray(runs) ? runs : []) counts[runState(run)] += 1;
  return counts;
}

function gateTone(outcome) {
  const value = String(outcome || "").toUpperCase();
  if (value === "PASS") return "ok";
  if (value === "WAITING" || value === "") return "info";
  return "err";
}

const short = (digest) => String(digest || "").replace(/^sha256:/, "").slice(0, 12);

export function runsMarkup(runs, eventsByRun, { esc = defaultEsc, note = "" } = {}) {
  const list = Array.isArray(runs) ? runs : [];
  if (!list.length) {
    return `<div class="so-panel kit-view-panel"><p class="kit-muted">${esc(note || "No packaging run is recorded.")}</p></div>`;
  }
  const counts = runCounts(list);
  const tiles = [
    ["succeeded", "Succeeded"],
    ["running", "In progress"],
    ["blocked", "Blocked"],
    ["failed", "Failed"],
  ]
    .map(
      ([key, label]) => `<div class="kit-card kit-run-tile kit-bar-${TONE[key]}">
        <div class="kit-run-tile-value">${counts[key]}</div>
        <div class="kit-muted">${label}</div>
      </div>`,
    )
    .join("");
  const rows = list
    .map((run) => {
      const stateName = runState(run);
      const gates = gateResults(eventsByRun?.[run.runId])
        .map(
          (gate) =>
            `<span class="kit-badge kit-badge-${gateTone(gate.outcome)}" title="${esc(gate.outcome || "no result")}">${esc(gate.step)}</span>`,
        )
        .join(" ");
      return `<tr>
        <td><button type="button" class="kit-link code-id" data-open-run="${esc(run.runId)}">${esc(run.runId)}</button></td>
        <td><span class="kit-badge kit-badge-${TONE[stateName]}">${esc(stateName)}</span></td>
        <td>${esc(run.outcome || "")}<div class="kit-muted">${esc(run.reason || "")}</div></td>
        <td class="kit-run-gates">${gates || `<span class="kit-muted">no step recorded</span>`}</td>
        <td class="code-id" title="${esc(run.sourceDigest || "")}">${esc(short(run.sourceDigest))}</td>
        <td class="code-id">${esc(run.targetPlanRef || "")}</td>
        <td class="code-id">${esc(run.templateVersion || "")}</td>
      </tr>`;
    })
    .join("");
  return `<div class="so-panel kit-view-panel">
    <div class="kit-view-head"><h2>Packaging runs</h2><span class="kit-muted">${list.length} recorded</span></div>
    <div class="kit-run-tiles">${tiles}</div>
    <div class="overflow-x-auto"><table class="so-table">
      <thead><tr><th>Run</th><th>State</th><th>Outcome</th><th>Gates</th><th>Source</th><th>Target plan</th><th>Template</th></tr></thead>
      <tbody>${rows}</tbody>
    </table></div>
  </div>`;
}

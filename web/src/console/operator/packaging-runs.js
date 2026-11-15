// Packaging runs list: KPI tiles by state and one row per recorded run with
// its gate results. Built from the runs and step events live-hub.js loads.

import { emptyState } from "./empty-states.js";
const defaultEsc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );

/** Groups a run into succeeded, review, running, blocked, or failed. */
export function runState(run) {
  const outcome = String(run?.outcome || "").toUpperCase();
  const phase = String(run?.phase || "").toLowerCase();
  if (run?.configured === false || phase === "blocked") return "blocked";
  if (phase === "review" && !/FAIL|TIMED_OUT|REJECT|CANCEL/u.test(outcome))
    return "review";
  if (phase === "failed") return "failed";
  if (outcome === "PASS" || outcome === "SUCCEEDED" || phase === "succeeded") {
    return "succeeded";
  }
  if (
    outcome.startsWith("FAIL") ||
    outcome === "TIMED_OUT" ||
    phase === "failed"
  ) {
    return phase === "blocked" ? "blocked" : "failed";
  }
  // A run in the review phase is waiting on a person, not still running.
  if (phase === "review") return "review";
  if (phase === "blocked" || outcome === "WAITING") return "blocked";
  return "running";
}

const TONE = {
  succeeded: "ok",
  review: "info",
  running: "info",
  blocked: "warn",
  failed: "err",
};

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
  const counts = { succeeded: 0, review: 0, running: 0, blocked: 0, failed: 0 };
  for (const run of Array.isArray(runs) ? runs : []) counts[runState(run)] += 1;
  return counts;
}

function gateTone(outcome) {
  const value = String(outcome || "").toUpperCase();
  if (value === "PASS") return "ok";
  if (value === "WAITING" || value === "") return "info";
  return "err";
}

const GATE_NODE = {
  ok: { cls: "is-done", mark: "&#10003;", word: "passed" },
  info: { cls: "is-active", mark: "", word: "waiting" },
  err: { cls: "is-blocked", mark: "&#10005;", word: "did not pass" },
};

/**
 * Connected gate stepper (kit v7.1): one node per recorded step, in order,
 * with its name beside it. Done, waiting and blocked come from the Hub's own
 * step outcomes; the full outcome is in each node's title.
 */
export function gateStepper(gates, esc = defaultEsc) {
  const list = Array.isArray(gates) ? gates : [];
  if (!list.length) return "";
  const items = list
    .map((gate, idx) => {
      const node = GATE_NODE[gateTone(gate.outcome)];
      const outcome = gate.outcome || "no result";
      const prevDone =
        idx > 0 && gateTone(list[idx - 1].outcome) === "ok"
          ? " is-after-done"
          : "";
      return `<li class="kit-gate ${node.cls}${prevDone}" title="${esc(gate.step)}: ${esc(outcome)}"><span class="kit-gate-node" aria-hidden="true">${node.mark}</span><span class="kit-gate-label">${esc(gate.step)}</span><span class="kit-sr">: ${node.word} (${esc(outcome)})</span></li>`;
    })
    .join("");
  return `<ol class="kit-gates" aria-label="Gate results">${items}</ol>`;
}

const short = (digest) =>
  String(digest || "")
    .replace(/^sha256:/, "")
    .slice(0, 12);

export function runsMarkup(
  runs,
  eventsByRun,
  { esc = defaultEsc, note = "" } = {},
) {
  const list = Array.isArray(runs) ? runs : [];
  if (!list.length) {
    return emptyState("packaging", note || "No packaging run is recorded.", {
      esc,
    });
  }
  const counts = runCounts(list);
  const tiles = [
    ["succeeded", "Succeeded"],
    ["review", "In review"],
    ["running", "In progress"],
    ["blocked", "Blocked"],
    ["failed", "Failed"],
  ]
    .map(
      ([
        key,
        label,
      ]) => `<div class="kit-card kit-run-tile kit-bar-${TONE[key]}">
        <div class="kit-run-tile-value">${counts[key]}</div>
        <div class="kit-muted">${label}</div>
      </div>`,
    )
    .join("");
  const rows = list
    .map((run) => {
      const stateName = runState(run);
      const gates = gateStepper(gateResults(eventsByRun?.[run.runId]), esc);
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
    <div class="kit-view-head"><h2>Packaging runs</h2><span class="kit-count" title="${list.length} recorded">${list.length}</span></div>
    <div class="kit-run-tiles">${tiles}</div>
    <div class="overflow-x-auto"><table class="so-table">
      <thead><tr><th>Run</th><th>State</th><th>Outcome</th><th>Gates</th><th>Source</th><th>Target plan</th><th>Template</th></tr></thead>
      <tbody>${rows}</tbody>
    </table></div>
  </div>`;
}

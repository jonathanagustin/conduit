import { releaseCalendarMarkup } from "./release-calendar.js";
const esc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
const mounts = new WeakMap();

export function selectedPlan(plans, id = "") {
  const rows = Array.isArray(plans) ? plans : [];
  return id ? rows.find((row) => row.id === id) || null : rows[0] || null;
}

export function timelineMarkup(rows, note = "") {
  const entries = Array.isArray(rows) ? rows : [];
  return `<section class="so-panel p-3 space-y-2"><h3>Environment timeline</h3>
    <p class="kit-muted">Environment-wide entries, including other plans. Chain verification is the Hub's reported result; plan rows are not chain-verified. No subprocess logs or step durations are inferred.</p>
    ${note ? `<p role="status">${esc(note)}</p>` : ""}
    ${
      entries.length
        ? `<ol>${entries
            .slice(0, 512)
            .map((entry) => {
              const verified =
                entry.kind === "audit" &&
                entry.verified === true &&
                typeof entry.digest === "string" &&
                entry.digest.length > 0 &&
                Number.isSafeInteger(entry.sequence) &&
                entry.sequence > 0;
              return `<li class="border-b p-2"><time>${esc(entry.at)}</time> · ${esc(entry.kind)} · ${esc(entry.summary)}<p>${verified ? `Hub-verified chain link ${esc(entry.sequence)} · ${esc(entry.digest)}` : "Not chain-verified"}</p></li>`;
            })
            .join(
              "",
            )}</ol>${entries.length > 512 ? "<p>Only the first 512 timeline entries are displayed.</p>" : ""}`
        : "<p>No timeline entries are available.</p>"
    }
  </section>`;
}

export function planInspectorMarkup(plans, id = "", timeline = [], note = "") {
  const rows = Array.isArray(plans) ? plans : [];
  const plan = selectedPlan(rows, id);
  const picker = `<label>Plan <select data-release-plan>${rows.map((row) => `<option value="${esc(row.id)}"${row.id === plan?.id ? " selected" : ""}>${esc(row.id)} · ${esc(row.status)}</option>`).join("")}</select></label>`;
  if (!plan)
    return `<section class="so-panel p-3">${picker}<p>No selected plan is in the visible Hub index.</p></section>`;
  const steps = Array.isArray(plan.plan?.steps) ? plan.plan.steps : [];
  return `<div data-release-plan-inspector class="space-y-3"><section class="so-panel p-3 space-y-2">
    <h2>Plan execution inspector</h2>${picker}
    <p>${esc(plan.id)} · ${esc(plan.status)} · Environment: ${esc(plan.plan?.environment || "unavailable")}</p>
    <p>Created: ${esc(plan.createdAt || "unavailable")} · Updated: ${esc(plan.updatedAt || "unavailable")}</p>
    ${plan.failure ? `<p role="status">Recorded failure: ${esc(plan.failure)}</p>` : ""}
    ${plan.failedInstallationId ? `<p>Failed installation: ${esc(plan.failedInstallationId)}</p>` : ""}
    <h3>Planned steps</h3><p class="kit-muted">Instructions, not per-step execution receipts.</p>
    ${steps.length ? `<ol>${steps.map((step) => `<li>${esc(step.position)}. ${esc(step.action)} · ${esc(step.installationId)} · ${esc(step.product)} ${esc(step.version)}</li>`).join("")}</ol>` : "<p>No step is recorded.</p>"}
    </section>${timelineMarkup(timeline, note)}${releaseCalendarMarkup(steps.flatMap((step) => (Array.isArray(step.maintenance?.overrides) ? step.maintenance.overrides : [])))}</div>`;
}

export function mountPlanInspector(
  root,
  plans,
  { selectedId = "", onSelect = () => {} } = {},
) {
  const token = {};
  mounts.set(root, token);
  let controller;
  let id = selectedId;
  const current = () => mounts.get(root) === token && root.isConnected;
  async function load() {
    controller?.abort();
    controller = new AbortController();
    const request = controller;
    const plan = selectedPlan(plans, id);
    root.innerHTML = planInspectorMarkup(
      plans,
      id,
      [],
      plan ? "Loading recorded timeline…" : "",
    );
    if (!plan?.plan?.environment) return;
    try {
      const response = await fetch(
        `/v1/environments/${encodeURIComponent(plan.plan.environment)}/timeline`,
        { headers: { accept: "application/json" }, signal: request.signal },
      );
      if (!response.ok) throw new Error("timeline unavailable");
      const body = await response.json();
      if (
        !Array.isArray(body.timeline) ||
        body.timeline.some((row) => !row || typeof row !== "object")
      )
        throw new Error("invalid timeline");
      if (current() && controller === request)
        root.innerHTML = planInspectorMarkup(plans, id, body.timeline);
    } catch {
      if (current() && controller === request && !request.signal.aborted)
        root.innerHTML = planInspectorMarkup(
          plans,
          id,
          [],
          "Hub did not return this environment's timeline.",
        );
    }
  }
  root.onchange = (event) => {
    const target = event.target;
    if (
      !(target instanceof HTMLSelectElement) ||
      !target.matches("[data-release-plan]")
    )
      return;
    id = target.value;
    onSelect(id);
    void load();
  };
  void load();
}

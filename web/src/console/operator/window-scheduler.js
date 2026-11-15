import {
  mountEnvironmentHub,
  selectedEnvironment,
} from "./release-environment-hub.js";
const esc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
const mounts = new WeakMap();
export function scheduleTargets(plans, environment) {
  const seen = new Set();
  const rows = [];
  for (const plan of Array.isArray(plans) ? plans : []) {
    if (plan?.plan?.environment !== environment) continue;
    for (const step of Array.isArray(plan.plan.steps) ? plan.plan.steps : []) {
      if (
        typeof step.entity !== "string" ||
        !step.entity ||
        seen.has(step.entity)
      )
        continue;
      seen.add(step.entity);
      rows.push({
        entity: step.entity,
        installationId: step.installationId || "",
      });
    }
  }
  return rows;
}
export function scheduleBody(environment, targets, values, now = Date.now()) {
  if (!environment || !targets.some((row) => row.entity === values.entity))
    throw new Error("Select a visible Entity in this environment.");
  const start = Date.parse(`${values.start}:00Z`),
    minutes = Number(values.minutes),
    reason = String(values.reason || "").trim();
  if (
    !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(values.start) ||
    !Number.isFinite(start) ||
    new Date(start).toISOString().slice(0, 16) !== values.start ||
    start < now
  )
    throw new Error("Choose a future UTC start.");
  if (
    !Number.isInteger(minutes) ||
    minutes < 1 ||
    minutes > 216000 ||
    start + minutes * 60000 - now > 216000 * 60000
  )
    throw new Error("The complete proposed window must end within 3600 hours.");
  if (
    !reason ||
    reason.length > 4096 ||
    String(values.ticket || "").length > 512
  )
    throw new Error(
      "Provide a reason within 4096 characters and a ticket within 512 characters.",
    );
  return {
    entities: [{ environment, entity: values.entity }],
    startsAt: new Date(start).toISOString(),
    duration: `${minutes}m`,
    downtime: values.downtime === true,
    reason,
    complianceTicket: String(values.ticket || ""),
  };
}
export function adjustSchedule(values, deltaMinutes, resize = false) {
  const result = { ...values };
  if (resize)
    result.minutes = Math.max(
      1,
      Math.min(216000, Number(values.minutes) + deltaMinutes),
    );
  else {
    const start = Date.parse(`${values.start}:00Z`);
    if (Number.isFinite(start))
      result.start = new Date(start + deltaMinutes * 60000)
        .toISOString()
        .slice(0, 16);
  }
  return result;
}
export function verifyScheduleReceipt(body, result, dryRun, expectedId = "") {
  const row = result?.requests?.[0];
  if (
    result?.valueTier !== "CONDUIT_NATIVE" ||
    result.dryRun !== dryRun ||
    result.failure ||
    result.requests?.length !== 1 ||
    !row ||
    row.environment !== body.entities[0].environment ||
    row.entity !== body.entities[0].entity ||
    typeof row.changeRequestId !== "string" ||
    !row.changeRequestId ||
    typeof row.status !== "string" ||
    !row.status ||
    (expectedId && row.changeRequestId !== expectedId) ||
    Date.parse(row.startsAt) !== Date.parse(body.startsAt) ||
    Date.parse(row.endsAt) !==
      Date.parse(body.startsAt) + Number(body.duration.slice(0, -1)) * 60000 ||
    row.type !== (body.downtime ? "DOWNTIME" : "NO_DOWNTIME")
  )
    throw new Error(
      "Hub response did not match the reviewed window. Inspect change requests before retrying.",
    );
  return row;
}
export function mountWindowScheduler(root, hub, options = {}) {
  const token = {};
  mounts.set(root, token);
  const current = () => mounts.get(root) === token && root.isConnected;
  const selected = selectedEnvironment(hub?.environments, options.selectedId);
  const environment = selected?.environment?.id;
  const targets = scheduleTargets(hub?.plans, environment);
  root.innerHTML =
    '<div data-window-evidence></div><section class="so-panel p-3 space-y-3" data-window-scheduler></section>';
  mountEnvironmentHub(
    root.querySelector("[data-window-evidence]"),
    hub,
    options,
  );
  const form = root.querySelector("[data-window-scheduler]");
  if (!environment || !targets.length) {
    form.innerHTML =
      "<h2>Propose a maintenance window</h2><p>No visible planned Entity is available for the selected environment.</p>";
    return;
  }
  let values = {
    entity: targets[0].entity,
    start: new Date(Math.ceil((Date.now() + 3600000) / 60000) * 60000)
      .toISOString()
      .slice(0, 16),
    minutes: 120,
    reason: "",
    ticket: "",
    downtime: false,
  };
  let preview = null,
    busy = false,
    confirmed = false,
    message = "";
  const attempted = new Set();
  let drag = null;
  const fields = () => {
    for (const name of ["entity", "start", "minutes", "reason", "ticket"]) {
      const input = form.querySelector(`[name="${name}"]`);
      if (input) input.value = String(values[name]);
    }
    const bar = form.querySelector("[data-draft-bar]");
    if (bar) {
      bar.style.width = `${Math.min(100, (Number(values.minutes) / Math.max(1440, Number(values.minutes) * 2)) * 100)}%`;
      bar.querySelector("[data-draft-label]").textContent =
        `${values.start} UTC · ${values.minutes}m`;
      bar.setAttribute(
        "aria-valuetext",
        `${values.start} UTC, ${values.minutes} minutes`,
      );
      bar.setAttribute("aria-valuenow", String(values.minutes));
    }
  };
  const read = () => ({
    entity: form.querySelector('[name="entity"]').value,
    start: form.querySelector('[name="start"]').value,
    minutes: Number(form.querySelector('[name="minutes"]').value),
    reason: form.querySelector('[name="reason"]').value,
    ticket: form.querySelector('[name="ticket"]').value,
    downtime: form.querySelector('[name="downtime"]').checked,
  });
  function paint() {
    if (!current()) return;
    form.innerHTML = `<h2>Propose a maintenance window</h2><p>Draft only. Drag the bar to shift the start, or its end handle to resize. Arrow keys shift by 15 minutes; Shift + arrow resizes. No recorded window is edited.</p>
   <fieldset ${busy ? "disabled" : ""}><label>Entity <select name="entity">${targets.map((row) => `<option value="${esc(row.entity)}">${esc(row.entity)}</option>`).join("")}</select></label>
   <label>Start (UTC) <input name="start" type="datetime-local" required /></label>
   <label>Duration (minutes) <input name="minutes" type="number" min="1" max="216000" required /></label>
   <label>Reason <input name="reason" maxlength="4096" required /></label><label>Compliance ticket <input name="ticket" maxlength="512" /></label>
   <label><input name="downtime" type="checkbox" ${values.downtime ? "checked" : ""} />Permit downtime in this proposal</label>
   <div data-draft-track style="height:40px;background:var(--so-card)"><div data-draft-bar role="slider" tabindex="0" aria-label="Draft maintenance interval" aria-valuemin="1" aria-valuemax="216000" aria-valuenow="${esc(values.minutes)}" aria-valuetext="${esc(values.start)} UTC, ${esc(values.minutes)} minutes" style="height:40px;background:var(--bar-info);touch-action:none;position:relative"><span data-draft-label></span><span data-draft-resize role="button" tabindex="0" aria-label="Resize draft duration" style="position:absolute;right:0;top:0;height:40px;cursor:ew-resize">↔</span></div></div>
   <button type="button" data-preview-window ${busy ? "disabled" : ""}>Preview change request</button>
   <label><input type="checkbox" data-confirm-window ${confirmed ? "checked" : ""} ${!preview || busy ? "disabled" : ""}/>Confirm this reviewed maintenance proposal</label>
   <button type="button" data-save-window ${!preview || !confirmed || busy ? "disabled" : ""}>Create change request</button>
   </fieldset><p role="status" data-window-message>${esc(message)}</p><p>Normal Hub policy decides approval. Creating a request does not execute a Plan or publish a release. Existing windows, blackouts and dependency timing still require their native evidence.</p>`;
    fields();
  }
  function invalidate() {
    preview = null;
    confirmed = false;
    message = "Draft changed; preview and confirm again.";
    const save = form.querySelector("[data-save-window]");
    if (save) save.disabled = true;
    const confirm = form.querySelector("[data-confirm-window]");
    if (confirm) {
      confirm.checked = false;
      confirm.disabled = true;
    }
    form.querySelector("[data-window-message]").textContent = message;
  }
  form.oninput = (event) => {
    if (busy) return;
    if (event.target.matches("[data-confirm-window]")) {
      confirmed = event.target.checked;
      form.querySelector("[data-save-window]").disabled =
        !preview || !confirmed;
      return;
    }
    values = read();
    invalidate();
    fields();
  };
  form.onkeydown = (event) => {
    if (
      busy ||
      !event.target.closest("[data-draft-bar]") ||
      !["ArrowLeft", "ArrowRight"].includes(event.key)
    )
      return;
    event.preventDefault();
    values = adjustSchedule(
      read(),
      event.key === "ArrowRight" ? 15 : -15,
      event.shiftKey || !!event.target.closest("[data-draft-resize]"),
    );
    invalidate();
    fields();
  };
  form.onpointerdown = (event) => {
    if (busy || !event.target.closest("[data-draft-bar]")) return;
    const track = form.querySelector("[data-draft-track]");
    drag = {
      x: event.clientX,
      values: read(),
      resize: !!event.target.closest("[data-draft-resize]"),
      width: track.getBoundingClientRect().width,
    };
    track.setPointerCapture(event.pointerId);
    event.preventDefault();
  };
  form.onpointermove = (event) => {
    if (!drag || busy) return;
    const range = Math.max(1440, Number(drag.values.minutes) * 2);
    const delta =
      Math.round(
        (((event.clientX - drag.x) / Math.max(1, drag.width)) * range) / 15,
      ) * 15;
    values = adjustSchedule(drag.values, delta, drag.resize);
    invalidate();
    fields();
  };
  form.onpointerup = () => {
    drag = null;
  };
  form.onpointercancel = () => {
    drag = null;
  };
  form.onclick = async (event) => {
    const button = event.target.closest(
      "[data-preview-window],[data-save-window]",
    );
    if (!button || busy) return;
    const saving = button.hasAttribute("data-save-window");
    let body, fingerprint;
    try {
      values = read();
      body = scheduleBody(environment, targets, values);
      fingerprint = JSON.stringify(body);
      if (
        saving &&
        (!confirmed || !preview || preview.fingerprint !== fingerprint)
      )
        throw new Error("Preview and confirm this exact draft first.");
      if (saving && attempted.has(fingerprint))
        throw new Error(
          "Submission already attempted; inspect change requests before retrying.",
        );
    } catch (error) {
      message = error.message;
      paint();
      return;
    }
    const expectedId = preview?.receipt?.changeRequestId;
    if (saving) attempted.add(fingerprint);
    busy = true;
    paint();
    try {
      const response = await fetch("/v1/temporary-maintenance-overrides", {
        method: "POST",
        headers: {
          "content-type": "application/json",
          accept: "application/json",
        },
        body: JSON.stringify({
          ...body,
          dryRun: !saving,
          ...(saving ? { expectedChangeRequestId: expectedId } : {}),
        }),
        signal: AbortSignal.timeout(15000),
      });
      if (!response.ok)
        throw new Error(
          saving
            ? "Submission was not confirmed. Inspect change requests before retrying."
            : "Hub refused the preview.",
        );
      const receipt = verifyScheduleReceipt(
        body,
        await response.json(),
        !saving,
        saving ? expectedId : "",
      );
      if (!current()) return;
      if (saving) {
        message = `Change request ${receipt.changeRequestId} · ${receipt.status}. Review it through Change Requests; no execution was requested.`;
        preview = null;
        confirmed = false;
      } else {
        preview = { fingerprint, receipt };
        confirmed = false;
        message = `Preview ${receipt.changeRequestId} · ${receipt.startsAt} → ${receipt.endsAt} · ${receipt.status}. Confirm before creating.`;
      }
    } catch (error) {
      if (current()) {
        preview = null;
        confirmed = false;
        message = error.message;
      }
    } finally {
      busy = false;
      paint();
    }
  };
  paint();
}

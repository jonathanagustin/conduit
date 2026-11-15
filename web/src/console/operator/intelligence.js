// Native memory-gate evidence and mandatory-human-review proposals.
const esc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
const mounts = new WeakMap();
async function readJSON(path, options = {}) {
  const response = await fetch(path, {
    ...options,
    headers: { accept: "application/json", ...options.headers },
    signal: AbortSignal.timeout(45000),
  });
  const body = await response.json();
  if (!response.ok)
    throw new Error(body.error || `Request failed (${response.status})`);
  return body;
}
const post = (path, body) =>
  readJSON(path, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
  });
export function intelligenceTargets(installations) {
  const seen = new Set();
  return (Array.isArray(installations) ? installations : [])
    .filter((row) => {
      if (
        !row ||
        !row.environment ||
        !row.installationId ||
        !row.product ||
        !row.desiredVersion
      )
        return false;
      const key = JSON.stringify([
        row.environment,
        row.installationId,
        row.product,
      ]);
      if (seen.has(key)) return false;
      seen.add(key);
      return true;
    })
    .map((row) => ({
      id: JSON.stringify([row.environment, row.installationId, row.product]),
      environment: row.environment,
      installationId: row.installationId,
      product: row.product,
    }));
}
export function canStageCourse(option) {
  return (
    option?.blocked === false &&
    option.outcome === "PASS" &&
    typeof option.value === "string" &&
    option.value !== ""
  );
}
export function expectationSummary(payload) {
  const totals = new Map();
  for (const row of Array.isArray(payload?.results) ? payload.results : []) {
    for (const column of Array.isArray(row.columns) ? row.columns : []) {
      if (typeof column.passed !== "boolean") continue;
      const name = String(column.adapter || "adapter unavailable");
      const count = totals.get(name) || { adapter: name, matched: 0, total: 0 };
      count.total += 1;
      if (column.passed) count.matched += 1;
      totals.set(name, count);
    }
  }
  return [...totals.values()];
}
export function verifyCourseReceipt(body, result) {
  if (
    result?.valueTier !== "CONDUIT_NATIVE" ||
    result.environment !== body.Environment ||
    result.installationId !== body.InstallationID ||
    result.productId !== body.ProductID ||
    result.value !== body.Value ||
    typeof result.changeRequestId !== "string" ||
    !result.changeRequestId ||
    typeof result.status !== "string"
  )
    throw new Error(
      "Response did not match the staged scope. Inspect change requests before retrying.",
    );
  return result;
}
export function evalMarkup(payload) {
  const source = Array.isArray(payload?.results)
    ? payload.results
    : Array.isArray(payload?.cases)
      ? payload.cases
      : [];
  const summary = expectationSummary(payload);
  return `<section data-intel-evals class="space-y-3"><h2>Evaluation suites</h2><p>Memory-gate cases. Matching an expected failure is a successful test expectation, not a deployment gate pass. No environment deployment or live-provider validation is inferred.</p><p>${esc(payload.label || "No observed result supplied")}</p>
 <div class="grid md:grid-cols-3 gap-2">${summary.length ? summary.map((row) => `<article class="so-card p-2">${esc(row.adapter)}: ${row.matched} / ${row.total} expectations matched</article>`).join("") : "<p>No observed adapter matches are supplied. Cases below are not run results.</p>"}</div>
 <div class="overflow-x-auto"><table class="so-table"><thead><tr><th>Case / source</th><th>Scenario / object</th><th>Input</th><th>Expected</th><th>Observed adapters</th></tr></thead><tbody>${source
   .slice(0, 256)
   .map((row) => {
     const item = row.case || row;
     const columns = Array.isArray(row.columns) ? row.columns : [];
     return `<tr><td>${esc(item.id)} · ${esc(item.source)}</td><td>${esc(item.scenario)} · ${esc(item.boundObject)}</td><td>${esc(item.value)}</td><td>${esc(item.expectedOutcome)}</td><td>${columns.length ? columns.map((column) => `<p>${esc(column.adapter)} · ${esc(column.outcome)} · ${typeof column.passed === "boolean" ? (column.passed ? "expectation matched" : "expectation differed") : "match unavailable"} · ${esc(column.reason)} · ${esc(column.label)}</p>`).join("") : "not run"}</td></tr>`;
   })
   .join(
     "",
   )}</tbody></table></div>${source.length > 256 ? "<p>Only the first 256 cases are displayed.</p>" : ""}</section>`;
}
export function createIntelligence({
  getHub = () => ({}),
  getOperator = () => "",
  onReviewRequest = () => {},
} = {}) {
  const attempted = new Set();
  const begin = (root) => {
    const token = {};
    mounts.set(root, token);
    return () => mounts.get(root) === token && root.isConnected;
  };
  const openButtons = (root) =>
    root
      .querySelectorAll("[data-intel-open-request]")
      .forEach(
        (button) =>
          (button.onclick = () =>
            onReviewRequest(button.getAttribute("data-intel-open-request"))),
      );
  async function renderStudio(root, logic) {
    const current = begin(root);
    root.innerHTML = "<p>Reading configured proposal evidence.</p>";
    try {
      const [config, scored] = await Promise.all([
        readJSON("/v1/intelligence/logic"),
        post("/v1/intelligence/courses", {}),
      ]);
      if (!current()) return;
      const options = Array.isArray(scored.options) ? scored.options : [];
      const targets = intelligenceTargets(getHub()?.installations);
      const caller = getOperator();
      const pending = (
        Array.isArray(getHub()?.changes) ? getHub().changes : []
      ).filter(
        (row) =>
          row.targetKind === "CONDUIT_NATIVE_INTELLIGENCE_COURSE" &&
          row.status === "PENDING_APPROVALS",
      );
      const cards = options
        .map(
          (option) =>
            `<article class="so-panel p-3"><h3>${esc(option.title)}</h3><p>${esc(option.value)} · ${esc(option.outcome)} · ${option.blocked === true ? "blocked" : option.blocked === false ? "not blocked by this gate" : "gate state unavailable"}</p><p>${esc(option.reason)} · ${esc(option.tradeoff)}</p><p>${esc(option.label)}</p></article>`,
        )
        .join("");
      root.innerHTML = `<section data-intel-studio class="space-y-3"><h2>${logic ? "Logic workbench / staged writes" : "Courses of action"}</h2><p>Configured adapter: ${esc(config.adapter)} · ${esc(config.timeout)} · configured mode ${esc(config.label)}. Configuration is not evidence that a model ran.</p>
   <label>Configured gate (read-only)<textarea readonly>${esc(config.gateRule)}</textarea></label><p>Rule persistence, version comparison, automatic recommendation ranking and simulation statistics are not supplied by this contract. Only recorded gate outcomes and trade-offs are shown.</p>
   ${logic ? '<ol class="so-panel p-3"><li>Choose visible desired target</li><li>Compare supplied memory-gate values</li><li>Review a scoped draft</li><li>Confirm creation of a mandatory-human-review request</li><li>Inspect the request through the existing review flow</li></ol><p>This is configured workflow, not a model reasoning trace. Model-driven deployment proposals are unavailable in this studio. The scoped draft below makes no model call.</p>' : ""}
   <div class="grid md:grid-cols-3 gap-3">${cards || "<p>No scored option is supplied.</p>"}</div>
   <form data-intel-stage class="so-panel p-3 space-y-2"><h3>Stage a memory proposal</h3>
    <label>Target <select name="target">${targets.map((row) => `<option value="${esc(row.id)}">${esc(row.environment)} / ${esc(row.installationId)} · ${esc(row.product)}</option>`).join("")}</select></label>
    <label>Proposed value <select name="value">${options.map((row) => `<option value="${esc(row.id)}"${canStageCourse(row) ? "" : " disabled"}>${esc(row.value)} · ${esc(row.outcome)}</option>`).join("")}</select></label>
    <p>No change is applied. The Hub rechecks current target scope and proposal permission; passing the memory gate is not approval.</p>
    <label><input type="checkbox" data-intel-confirm/>Confirm this target and value for human review</label>
    <button type="submit" data-intel-create disabled>Create change request</button><p role="status" data-intel-note>${caller ? "" : "Signed-in identity is unavailable; proposal creation is disabled."}${targets.length ? "" : " No visible desired target is supplied."}</p>
   </form><section><h3>Recorded pending course proposals</h3>${pending.length ? pending.map((row) => `<p>${esc(row.title)} · ${esc(row.id)} <button type="button" data-intel-open-request="${esc(row.id)}">Review existing request</button></p>`).join("") : "<p>No pending course proposal is supplied by the visible index. The list is not ranked.</p>"}</section></section>`;
      openButtons(root);
      const form = root.querySelector("[data-intel-stage]");
      const confirm = form.querySelector("[data-intel-confirm]");
      const button = form.querySelector("[data-intel-create]");
      const note = form.querySelector("[data-intel-note]");
      let busy = false;
      const selected = () => ({
        target: targets.find(
          (row) => row.id === form.elements.namedItem("target").value,
        ),
        option: options.find(
          (row) => row.id === form.elements.namedItem("value").value,
        ),
      });
      form.oninput = (event) => {
        if (busy) return;
        if (event.target !== confirm) confirm.checked = false;
        const choice = selected();
        button.disabled =
          !confirm.checked ||
          !caller ||
          getOperator() !== caller ||
          !choice.target ||
          !canStageCourse(choice.option);
      };
      form.onsubmit = async (event) => {
        event.preventDefault();
        if (busy || !confirm.checked || !caller || getOperator() !== caller)
          return;
        const choice = selected();
        if (!choice.target || !canStageCourse(choice.option)) return;
        const fresh = intelligenceTargets(getHub()?.installations).find(
          (row) => row.id === choice.target.id,
        );
        if (!fresh) {
          note.textContent = "The target is no longer visible. Review again.";
          return;
        }
        const body = {
          Value: choice.option.value,
          Environment: fresh.environment,
          InstallationID: fresh.installationId,
          ProductID: fresh.product,
        };
        const fingerprint = JSON.stringify([caller, body]);
        if (attempted.has(fingerprint)) {
          note.textContent =
            "Submission already attempted; inspect change requests before retrying.";
          return;
        }
        attempted.add(fingerprint);
        busy = true;
        form
          .querySelectorAll("input,select,button")
          .forEach((element) => (element.disabled = true));
        try {
          const result = verifyCourseReceipt(
            body,
            await post("/v1/intelligence/courses/select", body),
          );
          if (!current()) return;
          note.textContent = `Request ${result.changeRequestId} · ${result.status}. This selection applied no workload change.`;
          const open = document.createElement("button");
          open.type = "button";
          open.textContent = "Open associated change request";
          open.onclick = () => onReviewRequest(result.changeRequestId);
          note.append(open);
        } catch (error) {
          if (current())
            note.textContent = `Creation was not confirmed: ${error.message}. Inspect change requests before retrying.`;
        } finally {
          busy = false;
          if (current()) {
            form
              .querySelectorAll("input,select")
              .forEach((element) => (element.disabled = false));
            confirm.checked = false;
            button.disabled = true;
          }
        }
      };
    } catch (error) {
      if (current())
        root.innerHTML = `<p role="status">${esc(error.message)}</p>`;
    }
  }
  return {
    renderLogic: (root) => renderStudio(root, true),
    renderCourses: (root) => renderStudio(root, false),
    async renderEvals(root) {
      const current = begin(root);
      root.innerHTML = "<p>Reading evaluation cases.</p>";
      function paint(payload) {
        if (!current()) return;
        root.innerHTML =
          evalMarkup(payload) +
          `<button type="button" class="so-btn" data-intel-run-evals>Run supplied cases</button><form data-intel-add-case class="so-panel p-3 space-y-2"><h3>Capture feedback case</h3><label>Scenario <input name="scenario" required maxlength="4096"/></label><label>Memory value <input name="value" required/></label><label>Object label <input name="boundObject" required/></label><label>Expected <select name="expectedOutcome"><option>PASS</option><option>FAIL_REQUIRED</option></select></label><label><input type="checkbox" data-case-confirm required/>Confirm adding this case; it is not durable across Hub restart</label><button type="submit">Add feedback case</button></form><p role="status" data-eval-note></p>`;
        const note = root.querySelector("[data-eval-note]");
        root.querySelector("[data-intel-run-evals]").onclick = async (
          event,
        ) => {
          event.target.disabled = true;
          try {
            const result = await post("/v1/intelligence/evals/run", {});
            paint(result);
          } catch (error) {
            if (current()) note.textContent = error.message;
          } finally {
            if (current() && event.target.isConnected)
              event.target.disabled = false;
          }
        };
        root.querySelector("[data-intel-add-case]").onsubmit = async (
          event,
        ) => {
          event.preventDefault();
          const form = event.target;
          if (!form.querySelector("[data-case-confirm]").checked) return;
          const data = new FormData(form);
          const button = form.querySelector("button");
          button.disabled = true;
          try {
            await post("/v1/intelligence/evals/cases", {
              Scenario: String(data.get("scenario") || ""),
              Value: String(data.get("value") || ""),
              BoundObject: String(data.get("boundObject") || ""),
              ExpectedOutcome: String(data.get("expectedOutcome") || ""),
            });
            paint(await readJSON("/v1/intelligence/evals"));
          } catch (error) {
            if (current()) note.textContent = error.message;
          } finally {
            if (current() && button.isConnected) button.disabled = false;
          }
        };
      }
      try {
        paint(await readJSON("/v1/intelligence/evals"));
      } catch (error) {
        if (current())
          root.innerHTML = `<p role="status">${esc(error.message)}</p>`;
      }
    },
    async renderEvents(root) {
      const current = begin(root);
      root.innerHTML = "<p>Reading supplied automation evidence.</p>";
      try {
        const body = await readJSON("/v1/intelligence/events");
        if (!current()) return;
        const observed = Array.isArray(body.observed) ? body.observed : [];
        const rules = Array.isArray(body.rules) ? body.rules : [];
        root.innerHTML = `<section data-intel-events class="space-y-3"><h2>Event automations</h2><p>${esc(body.label)} · ${esc(body.note)}</p><h3>Observed packaging runs</h3>${
          observed.length
            ? `<table class="so-table"><thead><tr><th>Run</th><th>Phase</th><th>Outcome</th></tr></thead><tbody>${observed
                .slice(0, 256)
                .map(
                  (row) =>
                    `<tr><td>${esc(row.runId)}</td><td>${esc(row.phase)}</td><td>${esc(row.outcome)}</td></tr>`,
                )
                .join("")}</tbody></table>`
            : "<p>No observed run is supplied; this does not establish that no run exists.</p>"
        }${observed.length > 256 ? "<p>Only the first 256 visible observed runs are displayed.</p>" : ""}<h3>Supplied rules</h3>${rules.map((row) => `<article class="so-panel p-2"><h4>${esc(row.name)}</h4><p>${esc(row.label)} · execution ${row.executes === true ? "reported enabled" : row.executes === false ? "reported disabled" : "unavailable"}</p><button type="button" disabled>Pause unavailable</button></article>`).join("") || "<p>No rule is supplied.</p>"}<p>No live pause or rule-persistence endpoint is supplied. This view does not execute these examples.</p></section>`;
      } catch (error) {
        if (current())
          root.innerHTML = `<p role="status">${esc(error.message)}</p>`;
      }
    },
  };
}

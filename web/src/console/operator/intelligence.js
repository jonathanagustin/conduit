// Proposal logic, evals, courses, and example automations.
// Scores come from the Hub. This file does not invent gate results.

async function readJSON(path, options) {
  const response = await fetch(path, options);
  const body = await response.json();
  if (!response.ok) throw new Error(body.error || "request failed");
  return body;
}

function esc(value) {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

export function createIntelligence() {
  return {
    async renderLogic(root) {
      root.innerHTML = `<p class="text-[12px] text-[#8b96a5]">Reading the proposal pipeline.</p>`;
      try {
        const config = await readJSON("/v1/intelligence/logic");
        const runs = await readJSON("/v1/packaging-runs");
        const items = Array.isArray(runs.items) ? runs.items : [];
        const options = items
          .map(
            (run) =>
              `<option value="${esc(run.runId)}">${esc(run.runId)} · ${esc(run.phase || "")}</option>`,
          )
          .join("");
        const blocks = [
          [
            "Bound context",
            "Packaging run source",
            "chart, memory, image, gate rule",
          ],
          ["Model call", config.adapter, config.label],
          ["Schema", "one JSON object", "field, value, rationale"],
          ["Gate", config.gateRule, "deterministic"],
          ["Change request", "human approval", "not applied"],
          ["Audit", "authorization chain", config.timeout],
        ];
        root.innerHTML = `<div class="so-view-enter space-y-3">
          <div class="so-card p-3">
            <div class="text-[14px] font-semibold text-white">Logic workbench</div>
            <p class="text-[12px] text-[#8b96a5] mt-1">Adapter <span class="text-white">${esc(config.adapter)}</span> · timeout ${esc(config.timeout)} · ${esc(config.label)}</p>
          </div>
          <div class="grid gap-2 md:grid-cols-2">${blocks
            .map(
              (
                block,
                index,
              ) => `<article class="rounded-[6px] border border-[#222c36] bg-[#151b1f] p-2.5">
                <div class="text-[12px] font-semibold text-white">${String.fromCharCode(65 + index)}. ${esc(block[0])}</div>
                <div class="text-[11.5px] text-[#9ec5ff]">${esc(block[1])}</div>
                <div class="text-[11px] text-[#8b96a5]">${esc(block[2])}</div>
              </article>`,
            )
            .join("")}</div>
          <form data-logic-run class="flex flex-wrap items-center gap-2">
            <select name="runId" class="so-input text-[12px]">${options || `<option value="">No packaging run</option>`}</select>
            <button class="so-btn so-btn-sm" type="submit">Run on this packaging run</button>
            <span data-logic-note class="text-[12px] text-[#8b96a5]"></span>
          </form>
        </div>`;
        root
          .querySelector("form[data-logic-run]")
          ?.addEventListener("submit", async (event) => {
            event.preventDefault();
            const runID = new FormData(event.target).get("runId");
            const note = root.querySelector("[data-logic-note]");
            if (!runID) return;
            try {
              const body = await readJSON(
                `/v1/packaging-runs/${encodeURIComponent(runID)}/model-proposal`,
                {
                  method: "POST",
                  headers: {
                    "content-type": "application/json",
                    accept: "application/json",
                  },
                  body: "{}",
                },
              );
              if (note)
                note.textContent =
                  `${body.label || body.outcome || "recorded"} ${body.reason || body.error || ""}`.trim();
            } catch (error) {
              if (note) note.textContent = error.message;
            }
          });
      } catch (error) {
        root.innerHTML = `<p class="text-[12px] text-[#f5a3a6]">${esc(error.message)}</p>`;
      }
    },
    async renderEvals(root) {
      root.innerHTML = `<p class="text-[12px] text-[#8b96a5]">Reading eval cases.</p>`;
      const paint = (payload) => {
        const rows = (payload.results || payload.cases || [])
          .map((row) => {
            const item = row.case || row;
            const columns = row.columns || [];
            const cells = columns
              .map(
                (column) =>
                  `<div class="text-[11px]"><span class="text-[#9ec5ff]">${esc(column.adapter)}</span> ${esc(column.outcome)} · ${esc(column.label)} · ${column.passed ? "matches" : "differs"}</div>`,
              )
              .join("");
            return `<tr>
              <td class="text-white">${esc(item.id)}</td>
              <td>${esc(item.scenario)}</td>
              <td>${esc(item.boundObject)}</td>
              <td>${esc(item.expectedOutcome)}</td>
              <td>${esc(item.source)}</td>
              <td>${cells || `<span class="text-[#8b96a5]">not run</span>`}</td>
            </tr>`;
          })
          .join("");
        root.innerHTML = `<div class="so-view-enter space-y-3">
          <div class="flex items-center justify-between gap-2">
            <div>
              <div class="text-[14px] font-semibold text-white">Eval cases</div>
              <p class="text-[12px] text-[#8b96a5]">Scored by the packaging gate. ${esc(payload.label || "No model call")}</p>
            </div>
            <button type="button" data-run-evals class="so-btn so-btn-sm">Run cases</button>
          </div>
          <table class="so-table"><thead><tr><th>Case</th><th>Scenario</th><th>Object</th><th>Expected</th><th>Source</th><th>Adapters</th></tr></thead><tbody>${rows}</tbody></table>
          <form data-add-case class="so-card p-3 grid gap-2 md:grid-cols-2">
            <input name="scenario" class="so-input text-[12px]" placeholder="Scenario" />
            <input name="value" class="so-input text-[12px]" placeholder="Memory value" />
            <input name="boundObject" class="so-input text-[12px]" placeholder="Bound object" />
            <input name="expectedOutcome" class="so-input text-[12px]" placeholder="Expected outcome" />
            <button class="so-btn so-btn-sm" type="submit">Capture feedback as a case</button>
          </form>
        </div>`;
        root
          .querySelector("[data-run-evals]")
          ?.addEventListener("click", async () => {
            paint(
              await readJSON("/v1/intelligence/evals/run", {
                method: "POST",
                headers: { accept: "application/json" },
              }),
            );
          });
        root
          .querySelector("form[data-add-case]")
          ?.addEventListener("submit", async (event) => {
            event.preventDefault();
            const data = new FormData(event.target);
            await readJSON("/v1/intelligence/evals/cases", {
              method: "POST",
              headers: {
                "content-type": "application/json",
                accept: "application/json",
              },
              body: JSON.stringify({
                Scenario: String(data.get("scenario") || ""),
                Value: String(data.get("value") || ""),
                BoundObject: String(data.get("boundObject") || ""),
                ExpectedOutcome: String(data.get("expectedOutcome") || ""),
              }),
            });
            paint(await readJSON("/v1/intelligence/evals"));
          });
      };
      try {
        paint(await readJSON("/v1/intelligence/evals"));
      } catch (error) {
        root.innerHTML = `<p class="text-[12px] text-[#f5a3a6]">${esc(error.message)}</p>`;
      }
    },
    async renderCourses(root) {
      root.innerHTML = `<p class="text-[12px] text-[#8b96a5]">Scoring recorded options.</p>`;
      try {
        const body = await readJSON("/v1/intelligence/courses", {
          method: "POST",
          headers: { accept: "application/json" },
        });
        const cards = (body.options || [])
          .map(
            (
              option,
            ) => `<article class="rounded-[6px] border border-[#202830] bg-[#151b1f] p-3 space-y-2">
              <div class="flex items-center justify-between"><span class="font-semibold text-white">${esc(option.title)}</span><span class="text-[10px] text-[#8b96a5]">${esc(option.label)}</span></div>
              <div class="text-[12px] ${option.blocked ? "text-[#f5a3a6]" : "text-[#34d399]"}">${esc(option.outcome)} · ${esc(option.reason)}</div>
              <div class="text-[11.5px] text-[#9aa5b4]">${esc(option.tradeoff)}</div>
              <button type="button" data-select-value="${esc(option.value)}" class="so-btn so-btn-sm">Open change request</button>
            </article>`,
          )
          .join("");
        root.innerHTML = `<div class="so-view-enter space-y-3">
          <div class="text-[14px] font-semibold text-white">Courses of action</div>
          <p class="text-[12px] text-[#8b96a5]">${esc(body.label)}. Selecting an option opens a change request. It does not apply the change.</p>
          <div class="grid gap-2 md:grid-cols-3">${cards}</div>
          <p data-course-note class="text-[12px] text-[#dce4ee]"></p>
        </div>`;
        root.querySelectorAll("[data-select-value]").forEach((button) => {
          button.addEventListener("click", async () => {
            const note = root.querySelector("[data-course-note]");
            try {
              const selected = await readJSON(
                "/v1/intelligence/courses/select",
                {
                  method: "POST",
                  headers: {
                    "content-type": "application/json",
                    accept: "application/json",
                  },
                  body: JSON.stringify({
                    Value: button.getAttribute("data-select-value"),
                    Environment: "dev",
                    InstallationID: "sample-app",
                    ProductID: "app.example:sample-app",
                  }),
                },
              );
              if (note)
                note.textContent = `${selected.label} ${selected.changeRequestId} ${selected.status}`;
            } catch (error) {
              if (note) note.textContent = error.message;
            }
          });
        });
      } catch (error) {
        root.innerHTML = `<p class="text-[12px] text-[#f5a3a6]">${esc(error.message)}</p>`;
      }
    },
    async renderEvents(root) {
      root.innerHTML = `<p class="text-[12px] text-[#8b96a5]">Reading observed runs.</p>`;
      try {
        const body = await readJSON("/v1/intelligence/events");
        const observed = (body.observed || [])
          .map(
            (run) =>
              `<tr><td class="text-white">${esc(run.runId)}</td><td>${esc(run.phase)}</td><td>${esc(run.outcome)}</td></tr>`,
          )
          .join("");
        const rules = (body.rules || [])
          .map(
            (rule) =>
              `<li class="text-[12px] text-[#dce4ee]">${esc(rule.name)} · ${esc(rule.label)} · executes ${rule.executes ? "yes" : "no"}</li>`,
          )
          .join("");
        root.innerHTML = `<div class="so-view-enter space-y-3">
          <div class="so-warn-card p-2 text-[12px] text-[#eab854]">${esc(body.label)}</div>
          <p class="text-[12px] text-[#8b96a5]">${esc(body.note)}</p>
          <div class="so-card p-3">
            <div class="text-[13px] font-semibold text-white mb-2">Observed packaging runs</div>
            <table class="so-table"><thead><tr><th>Run</th><th>Phase</th><th>Outcome</th></tr></thead><tbody>${observed || ""}</tbody></table>
          </div>
          <div class="so-card p-3">
            <div class="text-[13px] font-semibold text-white mb-2">Rules</div>
            <ul class="space-y-1">${rules}</ul>
          </div>
        </div>`;
      } catch (error) {
        root.innerHTML = `<p class="text-[12px] text-[#f5a3a6]">${esc(error.message)}</p>`;
      }
    },
  };
}

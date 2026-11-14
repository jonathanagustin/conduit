/* Hub-backed workbench rows. Empty when the Hub does not answer. */
import { channelTable } from "./channel-board.js";

function esc(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

function initials(name) {
  const parts = String(name || "")
    .split(/[^A-Za-z0-9]+/)
    .filter(Boolean);
  if (!parts.length) return "OP";
  return parts
    .slice(0, 2)
    .map((part) => part[0].toUpperCase())
    .join("");
}

async function readJSON(path) {
  const response = await fetch(path, {
    headers: { accept: "application/json" },
  });
  if (!response.ok) {
    throw new Error(`${path} returned ${response.status}`);
  }
  return response.json();
}

async function postJSON(path, body) {
  const response = await fetch(path, {
    method: "POST",
    headers: { "content-type": "application/json", accept: "application/json" },
    body: JSON.stringify(body),
  });
  if (!response.ok) throw new Error(`${path} returned ${response.status}`);
  return response.json();
}

async function readPages(path, key) {
  const rows = [];
  let cursor = "";
  for (let page = 0; page < 5; page += 1) {
    const url = cursor
      ? `${path}${path.includes("?") ? "&" : "?"}cursor=${encodeURIComponent(cursor)}`
      : path;
    const body = await readJSON(url);
    const items = Array.isArray(body[key]) ? body[key] : [];
    rows.push(...items);
    cursor = body.nextCursor || "";
    if (!cursor) break;
  }
  return rows;
}

function panel(title, body) {
  return `<section class="so-panel p-3 space-y-2">
    <h2 class="text-[13px] font-semibold text-white">${esc(title)}</h2>
    ${body}
  </section>`;
}

function missing(message) {
  return `<section class="kit-card kit-bar-warn kit-state" role="status" data-state="empty"><div class="kit-state-title">EMPTY</div><p>${esc(message)}</p></section>`;
}

function hub(state) {
  return state.hub || {};
}

function latestPlan(plans, environment) {
  const rows = (plans || []).filter(
    (plan) => !environment || plan?.plan?.environment === environment,
  );
  return rows[rows.length - 1] || null;
}

function recallsFrom(plans) {
  const rows = [];
  for (const plan of plans || []) {
    const recalls = plan?.desiredState?.recalls || [];
    for (const recall of recalls) {
      rows.push({ recall, plan });
    }
  }
  return rows;
}

// reportedDrift reads only fields a spoke report can supply. A desired
// installation with no observation is unknown, including when the fleet marks
// that gap stale or out of date. Drift requires an observed digest or version
// that differs, or a reported config status of DRIFTED.
export function reportedDrift(row) {
  const observedAt = row?.observedAt || "";
  const observedVersion = row?.observedVersion || "";
  const observedDigest = row?.observedReleaseDigest || "";
  const observedConfig = row?.observedConfigDigest || "";
  if (!observedAt && !observedVersion && !observedDigest && !observedConfig)
    return "unknown";
  if (row?.configStatus === "DRIFTED") return "drifted";
  const desiredDigest = row?.desiredReleaseDigest || "";
  if (desiredDigest && observedDigest && desiredDigest !== observedDigest)
    return "drifted";
  const desiredVersion = row?.desiredVersion || "";
  if (desiredVersion && observedVersion && desiredVersion !== observedVersion)
    return "drifted";
  if (row?.stale) return "stale";
  if (
    row?.configStatus === "APPLIED" ||
    row?.configStatus === "INDETERMINATE"
  ) {
    return row.configStatus === "APPLIED" ? "in sync" : "unknown";
  }
  return "unknown";
}

function digestRows(plan, environments) {
  const steps = plan?.plan?.steps || [];
  const rows = [];
  for (const environment of environments || []) {
    const reported = environment?.reported?.entities || [];
    for (const entity of reported) {
      const step =
        steps.find((item) => item.installationId === entity.installationId) ||
        steps.find((item) => item.product === entity.product);
      const desired = step?.releaseDigest || "";
      const observed = entity.releaseDigest || "";
      let status = "spoke has not reported a digest";
      if (desired && observed && desired === observed) status = "converged";
      else if (desired && observed) status = "spoke digest differs";
      rows.push({
        environment:
          environment?.environment?.id ||
          environment?.reported?.environment ||
          "",
        installation: entity.installationId || "",
        desired,
        observed,
        status,
        spoke: environment?.spoke?.state || "",
      });
    }
  }
  return rows;
}

function promotionEdges(state) {
  const recorded = hub(state).promotions || [];
  const devStaging = recorded.filter(
    (row) => row.fromChannel === "DEV" && row.toChannel === "STAGING",
  );
  if (devStaging.length) return devStaging;
  const stages = [];
  for (const plan of hub(state).plans || []) {
    for (const setting of plan?.desiredState?.productSettings || []) {
      for (const stage of setting?.promotionPipeline?.stages || []) {
        if (stage.fromChannel === "DEV" && stage.toChannel === "STAGING") {
          stages.push({
            id: plan.id,
            product: setting.product,
            fromChannel: stage.fromChannel,
            toChannel: stage.toChannel,
            status: "DECLARED",
            version: "",
          });
        }
      }
    }
  }
  return stages.length ? stages : recorded;
}

export function recordedProposal(state) {
  const runs = hub(state).packaging || [];
  const run = runs.find((item) => item && item.runId) || null;
  if (!run) return null;
  const events = hub(state).packagingEvents || [];
  const failed = events.some((event) =>
    /FAIL|REJECT|BLOCK|TIMED_OUT|CANCEL/i.test(event.outcome || ""),
  );
  const passed = events.some((event) => event.outcome === "PASS");
  return { run, events, failed, passed };
}

function outcomeDot(outcome) {
  if (outcome === "PASS") return "bg-[#34d399]";
  if (/FAIL|REJECT|BLOCK|TIMED_OUT|CANCEL/.test(outcome || ""))
    return "bg-[#e76a6e]";
  return "bg-[#4c90f0]";
}

function stepTimeline(events) {
  if (!events.length) {
    return `<p class="text-[12px] text-[#8b96a5]">No step events recorded.</p>`;
  }
  return `<ol class="space-y-2">${events
    .map(
      (event) => `<li class="flex gap-2">
          <span class="mt-1.5 w-2 h-2 rounded-full shrink-0 ${outcomeDot(event.outcome)}"></span>
          <div class="min-w-0">
            <div class="text-[12px] text-white">${esc(event.step || "")} · ${esc(event.outcome || "")}</div>
            <div class="text-[11.5px] text-[#8b96a5]">${esc(event.reason || "No reason recorded.")}</div>
          </div>
        </li>`,
    )
    .join("")}</ol>`;
}

function constraintRows(events) {
  if (!events.length) {
    return `<p class="text-[12px] text-[#8b96a5]">No gate events recorded.</p>`;
  }
  return `<div class="space-y-2">${events
    .map((event) => {
      const blocked = /FAIL|REJECT|BLOCK|TIMED_OUT|CANCEL/.test(
        event.outcome || "",
      );
      const tone =
        event.outcome === "PASS"
          ? "border-[#1f4737] bg-[#10241d]"
          : blocked
            ? "border-[#52252c] bg-[#241417]"
            : "border-[#1f2a35] bg-[#10161c]";
      const ink =
        event.outcome === "PASS"
          ? "text-[#34d399]"
          : blocked
            ? "text-[#f5a3a6]"
            : "text-[#dce4ee]";
      return `<div class="rounded-[6px] border ${tone} px-2.5 py-2">
          <div class="flex items-center justify-between gap-2 text-[12px]">
            <span class="text-white">${esc(event.step || "")}</span>
            <span class="${ink}">${esc(event.outcome || "")}</span>
          </div>
          <div class="text-[11.5px] text-[#b6c2cf] mt-1">${esc(event.reason || "No reason recorded.")}</div>
        </div>`;
    })
    .join("")}</div>`;
}

export function createLiveHub({ state, render }) {
  state.operatorName = state.operatorName || "Operator";
  state.hub = {
    packaging: [],
    packagingEvents: [],
    changes: [],
    promotions: [],
    plans: [],
    environments: [],
    preview: null,
    note: "Reading the Hub.",
  };
  let started = false;

  function eventsFor(runId) {
    const byRun = hub(state).packagingEventsByRun || {};
    if (byRun[runId]) return byRun[runId];
    return hub(state).packagingEvents || [];
  }

  function auditTable(entries) {
    return `<table class="so-table"><thead><tr><th>Entry</th><th>Actor</th><th>Action</th><th>Previous hash</th><th>Hash</th></tr></thead><tbody>${entries
      .map(
        (entry) => `<tr>
          <td class="code-id">${esc(entry.sequence)}. ${esc(entry.id)}</td>
          <td>${esc(entry.actor || "")}</td>
          <td>${esc(entry.action || "")}</td>
          <td class="code-id break-all">${esc(entry.previousDigest || "")}</td>
          <td class="code-id break-all">${esc(entry.digest || "")}</td>
        </tr>`,
      )
      .join("")}</tbody></table>`;
  }

  function auditPanel(limit) {
    const entries = hub(state).audit || [];
    if (!entries.length) {
      return panel(
        "Audit chain",
        `<p class="text-[12px] text-[#8b96a5]">${esc(hub(state).auditNote || "No audit entries are recorded.")}</p>`,
      );
    }
    const shown = limit ? entries.slice(0, limit) : entries;
    const items = shown
      .map(
        (entry) => `<li class="space-y-1 border-b border-[#1c242c] pb-2">
          <div class="text-[12px] text-white">${esc(entry.sequence)}. <span class="code-id">${esc(entry.id)}</span></div>
          <div class="text-[12px] text-[#dce4ee]">${esc(entry.actor || "")} · ${esc(entry.action || "")}</div>
          <div class="text-[11px] text-[#8b96a5]">Previous <span class="code-id break-all text-[#dce4ee]">${esc(entry.previousDigest || "")}</span></div>
          <div class="text-[11px] text-[#8b96a5]">Hash <span class="code-id break-all text-[#dce4ee]">${esc(entry.digest || "")}</span></div>
        </li>`,
      )
      .join("");
    return panel(
      "Audit chain",
      `<p class="text-[12px] text-[#8b96a5]">${shown.length} of ${entries.length} recorded entries. Each hash includes the previous hash.</p><ol class="space-y-2 mt-2">${items}</ol>`,
    );
  }

  function split(main) {
    return `<div class="so-view-enter so-split"><div class="space-y-3 min-w-0">${main}</div>${auditPanel(6)}</div>`;
  }

  function paintOperator() {
    const name = state.operatorName || "Operator";
    const chip = document.getElementById("operator-chip");
    const label = document.getElementById("operator-name");
    const mark = document.getElementById("operator-initials");
    if (label) label.textContent = name;
    if (mark) mark.textContent = initials(name);
    if (chip) chip.title = `Operator: ${name}`;
  }

  async function refresh() {
    const next = {
      packaging: [],
      packagingEvents: [],
      changes: [],
      promotions: [],
      plans: [],
      environments: [],
      installations: [],
      channels: null,
      preview: null,
      note: "",
    };
    try {
      const session = await readJSON("/v1/session");
      const name =
        session?.effective?.username || session?.actor?.username || "";
      state.operatorName = name || "Operator";
    } catch {
      state.operatorName = "Operator";
    }
    try {
      next.environments = await readPages("/v1/environments", "environments");
    } catch {
      next.note = "Hub did not return environments.";
    }
    try {
      next.installations = await readPages(
        "/v1/fleet/installations",
        "installations",
      );
    } catch {
      next.installationNote = "Hub did not return reported installations.";
    }
    try {
      next.channels = await readJSON("/v1/channel-subscriptions");
    } catch {
      next.channelNote = "Hub did not return channel subscriptions.";
    }
    try {
      const checkin = await postJSON("/v1/sites/check-in", { site: "north", environment: "north", reportOld: false });
      next.connectivity = `${checkin.state || ""} ${checkin.lastCheckIn || ""}`.trim();
    } catch {
      next.connectivity = "";
    }
    try {
      const blackout = await readJSON("/v1/blackouts?environment=dev");
      next.blackoutReason = blackout.blocked ? blackout.reason : "";
    } catch {
      next.blackoutReason = "";
    }
    try {
      const rollouts = await readJSON("/v1/rollouts");
      next.rollouts = Array.isArray(rollouts.rollouts) ? rollouts.rollouts : [];
    } catch {
      next.rolloutNote = "Hub did not return staged rollouts.";
    }
    try {
      const body = await readJSON("/v1/packaging-runs");
      next.packaging = Array.isArray(body.items) ? body.items : [];
      next.packagingEventsByRun = {};
      next.feedbackByRun = {};
      for (const run of next.packaging) {
        if (!run.runId) continue;
        const events = await readJSON(
          `/v1/packaging-runs/${encodeURIComponent(run.runId)}/events`,
        );
        next.packagingEventsByRun[run.runId] = Array.isArray(events.items)
          ? events.items
          : [];
        try {
          const notes = await readJSON(
            `/v1/packaging-runs/${encodeURIComponent(run.runId)}/feedback`,
          );
          next.feedbackByRun[run.runId] = Array.isArray(notes.items)
            ? notes.items
            : [];
        } catch {
          next.feedbackByRun[run.runId] = [];
        }
      }
      const first = next.packaging.find((item) => item.runId);
      next.packagingEvents = first
        ? next.packagingEventsByRun[first.runId] || []
        : [];
    } catch {
      next.packagingNote = "Hub did not return packaging runs.";
    }
    try {
      const chain = await readJSON("/v1/authorization-audit?view=chain");
      next.audit = Array.isArray(chain.entries) ? chain.entries : [];
    } catch {
      next.auditNote = "Hub did not return the audit chain.";
    }
    try {
      next.changes = await readPages("/v1/change-requests", "changeRequests");
    } catch {
      next.changeNote = "Hub did not return change requests.";
    }
    try {
      next.promotions = await readPages("/v1/promotions", "promotions");
    } catch {
      next.promotionNote = "Hub did not return promotions.";
    }
    try {
      next.plans = await readPages("/v1/plans", "plans");
    } catch {
      next.planNote = "Hub did not return plans.";
    }
    try {
      const listed = await readJSON("/v1/bundles");
      const bundles = Array.isArray(listed.bundles) ? listed.bundles : [];
      const newest = bundles[bundles.length - 1];
      if (newest?.id) {
        const envelope = await readJSON(
          `/v1/bundles/${encodeURIComponent(newest.id)}`,
        );
        try {
          next.preview = await postJSON("/v1/bundles/preview", envelope);
        } catch {
          next.preview = await postJSON("/v1/bundles/verification", envelope);
        }
      }
    } catch {
      next.bundleNote = "Hub did not return a signed bundle.";
    }
    state.hub = next;
    paintOperator();
    render();
  }

  function reviewNotes(runID) {
    const notes = (hub(state).feedbackByRun || {})[runID] || [];
    const rows = notes
      .map(
        (note) => `<li class="text-[12px] text-[#dce4ee]">
          <span class="text-[#8b96a5]">${esc(note.target)} ${esc(note.selector)}</span>
          — ${esc(note.note)}
          <span class="text-[#6d7885]">(${esc(note.author || "operator")}, not an approval)</span>
        </li>`,
      )
      .join("");
    return panel(
      "Review notes",
      `<p class="text-[12px] text-[#8b96a5]">A note on a file, manifest field, or step is review input. It does not approve the run.</p>
       <ul class="space-y-1 mb-2">${rows || `<li class="text-[12px] text-[#8b96a5]">No review note is recorded.</li>`}</ul>
       <form data-run="${esc(runID)}" class="grid gap-2">
         <label class="text-[12px] text-[#8b96a5]">Element
           <select name="target" class="mt-1 w-full bg-[#0d1317] border border-[#1e252b] text-white text-[12px] p-1">
             <option value="step">step</option>
             <option value="file">file</option>
             <option value="manifest">manifest</option>
           </select>
         </label>
         <label class="text-[12px] text-[#8b96a5]">Selector
           <input name="selector" class="mt-1 w-full bg-[#0d1317] border border-[#1e252b] text-white text-[12px] p-1" placeholder="gate or field" />
         </label>
         <label class="text-[12px] text-[#8b96a5]">Note
           <textarea name="note" class="mt-1 w-full bg-[#0d1317] border border-[#1e252b] text-white text-[12px] p-1" rows="2"></textarea>
         </label>
         <button type="submit" class="so-btn so-btn-sm justify-self-start">Record review note</button>
         <p data-error class="text-[12px] text-[#dce4ee]"></p>
       </form>`,
    );
  }

  function bindReviewNotes(root) {
    root.querySelectorAll("form[data-run]").forEach((form) => {
      form.addEventListener("submit", async (event) => {
        event.preventDefault();
        const data = new FormData(form);
        const runID = form.getAttribute("data-run") || "";
        const error = form.querySelector("[data-error]");
        try {
          await postJSON(
            `/v1/packaging-runs/${encodeURIComponent(runID)}/feedback`,
            {
              target: String(data.get("target") || ""),
              selector: String(data.get("selector") || ""),
              note: String(data.get("note") || ""),
            },
          );
          await refresh();
        } catch {
          if (error) error.textContent = "The note was not recorded.";
        }
      });
    });
  }

  return {
    ensure() {
      paintOperator();
      if (started) return;
      started = true;
      refresh();
    },
    renderPackaging(root) {
      const runs = hub(state).packaging || [];
      if (!runs.length) {
        root.innerHTML = missing(
          hub(state).packagingNote || "No packaging run is recorded.",
        );
        return;
      }
      const cards = runs
        .map((run) => {
          return `${panel(
            run.runId || "Packaging run",
            `<dl class="grid grid-cols-1 sm:grid-cols-2 gap-2 text-[12px]">
              <div><dt class="text-[#6d7885]">Phase</dt><dd class="text-white">${esc(run.phase || "")}</dd></div>
              <div><dt class="text-[#6d7885]">Outcome</dt><dd class="text-white">${esc(run.outcome || "")}</dd></div>
              <div><dt class="text-[#6d7885]">Plan</dt><dd class="code-id text-white">${esc(run.planId || run.targetPlanRef || "")}</dd></div>
              <div><dt class="text-[#6d7885]">Template</dt><dd class="text-white">${esc(run.templateVersion || "")}</dd></div>
              <div class="sm:col-span-2"><dt class="text-[#6d7885]">Source digest</dt><dd class="code-id text-white break-all">${esc(run.sourceDigest || "")}</dd></div>
            </dl>`,
          )}
          ${panel("Plan timeline", stepTimeline(eventsFor(run.runId)))}
          ${reviewNotes(run.runId)}`;
        })
        .join("");
      root.innerHTML = split(cards);
      bindReviewNotes(root);
    },
    renderBundle(root) {
      const preview = hub(state).preview;
      if (!preview) {
        root.innerHTML = missing(
          hub(state).bundleNote || "No signed bundle is recorded.",
        );
        return;
      }
      const components = (preview.sbom || [])
        .map(
          (item) => `<tr>
            <td>${esc(item.kind)}</td>
            <td class="text-white">${esc(item.name)}</td>
            <td>${esc(item.version || "")}</td>
            <td class="code-id break-all">${esc(item.digest)}</td>
          </tr>`,
        )
        .join("");
      const signature = preview.signature || {};
      const status =
        signature.status || (signature.verified ? "verified" : "unknown");
      const scans = (preview.assessments || [])
        .map(
          (item) => `<tr>
            <td>${esc(item.release || "")}</td>
            <td class="code-id break-all">${esc(item.artifactDigest || "")}</td>
            <td>${esc(item.vulnerabilityStatus || "")}</td>
            <td>${esc(item.virusStatus || "")}</td>
          </tr>`,
        )
        .join("");
      const contents =
        status === "verified"
          ? panel(
              "SBOM",
              components
                ? `<table class="so-table"><thead><tr><th>Kind</th><th>Name</th><th>Version</th><th>Digest</th></tr></thead><tbody>${components}</tbody></table>`
                : `<p class="text-[12px] text-[#8b96a5]">No chart or image components in this bundle.</p>`,
            )
          : panel(
              "SBOM",
              `<p class="text-[12px] text-[#8b96a5]">Contents stay hidden until signature verification succeeds.</p>`,
            );
      root.innerHTML = split(`${panel(
        "Bundle preview",
        `<dl class="space-y-2 text-[12px]">
            <div><dt class="text-[#6d7885]">Bundle</dt><dd class="code-id text-white">${esc(preview.bundleId)}</dd></div>
            <div><dt class="text-[#6d7885]">Manifest digest</dt><dd class="code-id text-white break-all">${esc(preview.manifestDigest)}</dd></div>
            <div><dt class="text-[#6d7885]">Signature key</dt><dd class="code-id text-white break-all">${esc(signature.keyId || "")}</dd></div>
            <div><dt class="text-[#6d7885]">Signature status</dt><dd class="text-white">${esc(status)}</dd></div>
            <div><dt class="text-[#6d7885]">Scan outcome</dt><dd class="text-white">${esc(preview.securityOutcome || "not recorded")}</dd></div>
          </dl>`,
      )}
        ${contents}
        ${panel(
          "Scan results",
          scans
            ? `<table class="so-table"><thead><tr><th>Release</th><th>Digest</th><th>Vulnerabilities</th><th>Malware</th></tr></thead><tbody>${scans}</tbody></table>`
            : `<p class="text-[12px] text-[#8b96a5]">No scan result is recorded on this bundle.</p>`,
        )}`);
    },
    renderChanges(root) {
      const rows = hub(state).changes || [];
      if (!rows.length) {
        root.innerHTML = missing(
          hub(state).changeNote || "No change request is recorded.",
        );
        return;
      }
      const cards = rows
        .map((request) => {
          const reviews = (request.reviews || [])
            .map(
              (review) =>
                `<li class="text-[12px] text-[#dce4ee]">${esc(review.decision)} by ${esc(review.subject)} — ${esc(review.rationale || "")}</li>`,
            )
            .join("");
          return panel(
            request.title || request.id,
            `<p class="text-[12.5px] text-[#dce4ee]">${esc(request.description || "")}</p>
            <dl class="grid grid-cols-1 sm:grid-cols-2 gap-2 text-[12px] mt-2">
              <div><dt class="text-[#6d7885]">Change request</dt><dd class="code-id text-white">${esc(request.id)}</dd></div>
              <div><dt class="text-[#6d7885]">Status</dt><dd class="text-white">${esc(request.status)}</dd></div>
              <div><dt class="text-[#6d7885]">Author</dt><dd class="text-white">${esc(request.author?.username || "")}</dd></div>
              <div><dt class="text-[#6d7885]">Environment</dt><dd class="text-white">${esc(request.environment || "")}</dd></div>
            </dl>
            <ul class="mt-2 space-y-1">${reviews || `<li class="text-[12px] text-[#8b96a5]">No approval recorded.</li>`}</ul>`,
          );
        })
        .join("");
      root.innerHTML = split(cards);
    },
    renderPromotion(root) {
      const edges = promotionEdges(state);
      if (!edges.length) {
        root.innerHTML = missing(
          hub(state).promotionNote ||
            "No promotion from dev to staging is recorded.",
        );
        return;
      }
      const pipe = edges
        .map(
          (edge) => `<div class="flex flex-wrap items-center gap-2">
            <div class="rounded-[6px] border border-[#253b54] bg-[#13202e] px-3 py-2 min-w-[88px]">
              <div class="text-[10px] uppercase tracking-[0.07em] text-[#6d7885]">From</div>
              <div class="text-[13px] text-white">${esc(edge.fromChannel || "")}</div>
            </div>
            <span class="text-[#7cb4ff]">→</span>
            <div class="rounded-[6px] border border-[#1f4737] bg-[#10241d] px-3 py-2 min-w-[88px]">
              <div class="text-[10px] uppercase tracking-[0.07em] text-[#6d7885]">To</div>
              <div class="text-[13px] text-white">${esc(edge.toChannel || "")}</div>
            </div>
            <div class="text-[12px] text-[#8b96a5]">${esc(edge.product || "")} ${esc(edge.version || "")} · ${esc(edge.status || "")}</div>
          </div>`,
        )
        .join("");
      root.innerHTML = split(
        panel(
          "Promotion pipeline",
          `<p class="text-[12px] text-[#8b96a5] mb-2">Recorded promotion edges.</p><div class="space-y-2">${pipe}</div>`,
        ),
      );
    },
    renderWaves(root) {
      const rollouts = hub(state).rollouts || [];
      const note = hub(state).rolloutNote || "No staged rollout is recorded.";
      const body = rollouts.length
        ? `<ul class="space-y-2">${rollouts
            .map(
              (rollout) =>
                `<li class="text-[12px] text-white">${esc(rollout.id)} · ${esc(rollout.product)} ${esc(rollout.version)} · ${esc(rollout.status)}</li>`,
            )
            .join("")}</ul>`
        : `<p class="text-[12px] text-[#8b96a5]">${esc(note)}</p>`;
      root.innerHTML = `<div class="so-view-enter">${panel(
        "Staged waves",
        `<p class="text-[12px] text-[#8b96a5]">Each wave waits on health, an approval, or a time window. Pause confirms. Start, resume, and roll back open a change request.</p>
         <p class="text-[12px] text-[#8b96a5]">Blackout: ${esc(hub(state).blackoutReason || "none")}</p>${body}`,
      )}</div>`;
    },
    renderChannels(root) {
      const view = hub(state).channels;
      const table = channelTable(view);
      if (!table) {
        root.innerHTML = missing(
          hub(state).channelNote || "No channel subscription is recorded.",
        );
        return;
      }
      root.innerHTML = `<div class="so-view-enter">${panel(
        "Channel subscriptions",
        `<p class="text-[12px] text-[#8b96a5]">The version on dev, candidate, and stable, and the channel this environment subscribes to. Running is the version the environment last reported.</p>${table}`,
      )}</div>`;
    },
    renderEnvironments(root) {
      const rows = hub(state).environments || [];
      const installations = hub(state).installations || [];
      if (!rows.length && !installations.length) {
        root.innerHTML = missing(
          hub(state).note || "No environment is recorded.",
        );
        return;
      }
      const body = rows
        .map((row) => {
          const spoke = row.spoke || {};
          const reportedAt = row.reported?.observedAt || spoke.observedAt || "";
          return `<tr>
            <td class="text-white">${esc(row.environment?.id || "")}</td>
            <td class="code-id">${esc(spoke.agentId || row.reported?.agentId || "")}</td>
            <td>${esc(spoke.state || "")}</td>
            <td class="text-[#8b96a5]">${esc(reportedAt || "no report")}</td>
          </tr>`;
        })
        .join("");
      const installs = installations
        .map(
          (row) => `<tr>
            <td class="text-white">${esc(row.product || "")}</td>
            <td>${esc(row.environment || "")}</td>
            <td>${esc(row.desiredVersion || "")}</td>
            <td>${esc(row.observedVersion || "")}</td>
            <td>${esc(reportedDrift(row))}</td>
            <td class="text-[#8b96a5]">${esc(row.observedAt || "no report")}</td>
            <td>${reportedDrift(row) === "drifted" ? `<button type="button" class="so-btn so-btn-sm" data-propose-fix="${esc(row.environment)}" data-installation="${esc(row.installationId)}">Propose fix</button>` : ""}</td>
          </tr>`,
        )
        .join("");
      const headers = rows
        .map((row) => {
          const spoke = row.spoke || {};
          const id = row.environment?.id || "";
          const reportedAt = row.reported?.observedAt || spoke.observedAt || "";
          return `<div class="rounded-[6px] border border-[#1f2a35] bg-[#10161c] p-3">
            <div class="flex items-center justify-between gap-2">
              <div class="text-[16px] font-semibold text-white">${esc(id)}</div>
              <span class="px-1.5 py-0.5 rounded border border-[#253b54] text-[10px] text-[#7cb4ff]">${esc(spoke.state || "unknown")}</span>
            </div>
            <div class="mt-2 text-[12px] text-[#8b96a5]">Spoke <span class="text-white">${esc(spoke.agentId || row.reported?.agentId || "not recorded")}</span></div>
            <div class="text-[12px] text-[#8b96a5]">Latest report <span class="text-white">${esc(reportedAt || "no report")}</span></div>
            <div class="text-[12px] text-[#8b96a5]">Connectivity <span class="text-white">${esc(hub(state).connectivity || "no check-in")}</span></div>
            <div class="text-[12px] text-[#8b96a5]">Offline bundle <span class="text-white">${esc(hub(state).offlineBundle || "none handed off")}</span></div>
          </div>`;
        })
        .join("");
      const ledger = (hub(state).audit || [])
        .slice(0, 8)
        .map(
          (entry) => `<tr>
            <td class="code-id">${esc(entry.sequence)}</td>
            <td>${esc(entry.actor || "")}</td>
            <td>${esc(entry.action || "")}</td>
          </tr>`,
        )
        .join("");
      const spokeTable = rows.length
        ? `${panel("Environments", `<div class="grid gap-2 md:grid-cols-2">${headers}</div>`)}
           ${panel(
             "Activity",
             ledger
               ? `<p class="text-[12px] text-[#8b96a5] mb-2">Recorded authorization decisions.</p><table class="so-table"><thead><tr><th>#</th><th>Actor</th><th>Action</th></tr></thead><tbody>${ledger}</tbody></table>`
               : `<p class="text-[12px] text-[#8b96a5]">No activity is recorded.</p>`,
           )}
           ${panel(
             "Spokes",
             `<table class="so-table"><thead><tr><th>Environment</th><th>Spoke</th><th>Liveness</th><th>Latest report</th></tr></thead><tbody>${body}</tbody></table>`,
           )}`
        : missing(hub(state).note || "No environment is recorded.");
      const installTable = installations.length
        ? panel(
            "Reported installations",
            `<p class="text-[12px] text-[#8b96a5]">Desired and observed versions. Drift is shown only when a spoke reported a differing digest, version, or config status. A missing report stays unknown.</p>
             <table class="so-table"><thead><tr><th>Product</th><th>Environment</th><th>Desired</th><th>Observed</th><th>Drift</th><th>Report</th><th></th></tr></thead><tbody>${installs}</tbody></table>`,
          )
        : panel(
            "Reported installations",
            `<p class="text-[12px] text-[#8b96a5]">${esc(hub(state).installationNote || "No installation report is recorded.")}</p>`,
          );
      root.innerHTML = split(`${spokeTable}${installTable}`);
      root.querySelectorAll("[data-propose-fix]").forEach((button) => {
        button.addEventListener("click", async () => {
          button.disabled = true;
          try {
            const opened = await postJSON("/v1/fleet/propose-fix", {
              environment: button.getAttribute("data-propose-fix"),
              installationId: button.getAttribute("data-installation"),
            });
            button.textContent = opened.changeRequestId || "Proposed";
          } catch {
            button.textContent = "Refused";
          }
        });
      });
    },
    renderRecall(root) {
      const pairs = recallsFrom(hub(state).plans);
      if (!pairs.length) {
        root.innerHTML = missing(
          hub(state).planNote || "No recall is recorded.",
        );
        return;
      }
      const blocks = pairs
        .map(({ recall, plan }) => {
          const digests = digestRows(plan, hub(state).environments);
          const table = digests.length
            ? `<table class="so-table"><thead><tr><th>Environment</th><th>Installation</th><th>Desired digest</th><th>Spoke digest</th><th>Spoke</th></tr></thead>
              <tbody>${digests
                .map(
                  (row) => `<tr>
                    <td>${esc(row.environment)}</td>
                    <td>${esc(row.installation)}</td>
                    <td class="code-id break-all">${esc(row.desired)}</td>
                    <td class="code-id break-all">${esc(row.observed)}</td>
                    <td>${esc(row.status)} (${esc(row.spoke)})</td>
                  </tr>`,
                )
                .join("")}</tbody></table>`
            : `<p class="text-[12px] text-[#8b96a5]">No spoke report for this recall.</p>`;
          return panel(
            recall.id || "Recall",
            `<p class="text-[12px] text-white">Terminal reason: ${esc(recall.reason || "")}</p>
             <p class="text-[12px] text-[#8b96a5]">${esc(recall.product || "")} ${esc(recall.startingVersion || "")}${recall.endingVersionExclusive ? ` until ${esc(recall.endingVersionExclusive)}` : ""}</p>
             ${table}`,
          );
        })
        .join("");
      root.innerHTML = split(blocks);
    },
    renderAudit(root) {
      const entries = hub(state).audit || [];
      if (!entries.length) {
        root.innerHTML = missing(
          hub(state).auditNote || "No audit entries are recorded.",
        );
        return;
      }
      root.innerHTML = `<div class="so-view-enter">${panel(
        "Authorization audit",
        `<p class="text-[12px] text-[#8b96a5]">Hash chain of the recorded decisions. The previous hash of each entry is the hash of the entry before it.</p>${auditTable(entries)}`,
      )}</div>`;
    },
  };
}

export function recordedGateRows(state) {
  const runs = hub(state).packaging || [];
  const byRun = hub(state).packagingEventsByRun || {};
  const rows = [];
  for (const run of runs) {
    for (const event of byRun[run.runId] || []) {
      if (!event.outcome || event.outcome === "WAITING") continue;
      rows.push({
        run: run.runId,
        gate: event.step,
        outcome: event.outcome,
        reason: event.reason || "",
      });
    }
  }
  return rows;
}

export function renderLiveProposal(root, state) {
  const runs = hub(state).packaging || [];
  const byRun = hub(state).packagingEventsByRun || {};
  const proposals = runs.filter((run) => run && run.runId);
  if (!proposals.length) return false;
  const first = proposals[0];
  const live = hub(state).modelProposal;
  const liveCard = live
    ? panel(
        "Live model call",
        live.error
          ? `<p class="text-[12.5px] text-white">${esc(live.error)}</p>`
          : `<p class="text-[12.5px] text-white">${esc(live.outcome || "")} ${esc(live.reason || "")}</p>
             <p class="text-[12px] text-[#8b96a5] mt-1">Live model call</p>
             <p class="text-[12px] text-white mt-1">${esc(live.proposal?.value || "")} — ${esc(live.proposal?.rationale || "")}</p>
             <p class="text-[12px] text-[#8b96a5]">${esc(live.changeRequestId || "No change request.")}</p>`,
      )
    : "";
  const ask = `<form data-model-run="${esc(first.runId)}" class="mb-3">
      <button type="submit" class="so-btn so-btn-sm">Propose with model</button>
      <p data-model-error class="text-[12px] text-[#dce4ee] mt-1"></p>
    </form>`;
  const cards = proposals
    .map((run) => {
      const events = byRun[run.runId] || [];
      const failed = events.some((event) =>
        /FAIL|REJECT|BLOCK|TIMED_OUT|CANCEL/.test(event.outcome || ""),
      );
      const passed =
        events.some((event) => event.outcome === "PASS") && !failed;
      const verdict = failed
        ? "Deterministic checks blocked this proposal."
        : passed
          ? "Deterministic checks passed."
          : "The proposal is recorded. Checks have not finished.";
      return `${panel(
        `Recorded proposal · ${run.runId}`,
        `<p class="text-[12.5px] text-white">${esc(verdict)}</p>
         <p class="text-[12px] text-[#8b96a5] mt-1">Recorded by the packaging assessment. No model call.</p>
         <dl class="grid grid-cols-1 sm:grid-cols-2 gap-2 text-[12px] mt-2">
           <div><dt class="text-[#6d7885]">Phase</dt><dd class="text-white">${esc(run.phase || "")}</dd></div>
           <div><dt class="text-[#6d7885]">Outcome</dt><dd class="text-white">${esc(run.outcome || "")}</dd></div>
           <div class="sm:col-span-2"><dt class="text-[#6d7885]">Source digest</dt><dd class="code-id text-white break-all">${esc(run.sourceDigest || "")}</dd></div>
         </dl>`,
      )}
      ${panel("Constraints", constraintRows(events))}`;
    })
    .join("");
  root.innerHTML = `<div class="so-view-enter space-y-3">${ask}${liveCard}${cards}</div>`;
  root.querySelectorAll("form[data-model-run]").forEach((form) => {
    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      const runID = form.getAttribute("data-model-run") || "";
      const error = form.querySelector("[data-model-error]");
      try {
        const response = await fetch(
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
        const body = await response.json();
        if (!response.ok) {
          state.hub = state.hub || {};
          state.hub.modelProposal = {
            error:
              body.error || "The local model CLI did not return a proposal.",
          };
        } else {
          state.hub.modelProposal = body;
        }
        renderLiveProposal(root, state);
      } catch {
        if (error)
          error.textContent = "The local model CLI did not return a proposal.";
      }
    });
  });
  return true;
}

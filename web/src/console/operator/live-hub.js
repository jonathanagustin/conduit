/* Hub-backed workbench rows. Empty when the Hub does not answer. */

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
  const response = await fetch(path, { headers: { accept: "application/json" } });
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
    const url = cursor ? `${path}${path.includes("?") ? "&" : "?"}cursor=${encodeURIComponent(cursor)}` : path;
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
  return `<div class="so-view-enter"><div class="so-panel p-4 text-[12.5px] text-[#8b96a5]">${esc(message)}</div></div>`;
}

function hub(state) {
  return state.hub || {};
}

function latestPlan(plans, environment) {
  const rows = (plans || []).filter((plan) => !environment || plan?.plan?.environment === environment);
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

function digestRows(plan, environments) {
  const steps = plan?.plan?.steps || [];
  const rows = [];
  for (const environment of environments || []) {
    const reported = environment?.reported?.entities || [];
    for (const entity of reported) {
      const step = steps.find((item) => item.installationId === entity.installationId) ||
        steps.find((item) => item.product === entity.product);
      const desired = step?.releaseDigest || "";
      const observed = entity.releaseDigest || "";
      let status = "spoke has not reported a digest";
      if (desired && observed && desired === observed) status = "converged";
      else if (desired && observed) status = "spoke digest differs";
      rows.push({
        environment: environment?.environment?.id || environment?.reported?.environment || "",
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
  const failed = events.some((event) => /FAIL|REJECT|BLOCK|TIMED_OUT|CANCEL/i.test(event.outcome || ""));
  const passed = events.some((event) => event.outcome === "PASS");
  return { run, events, failed, passed };
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
      preview: null,
      note: "",
    };
    try {
      const session = await readJSON("/v1/session");
      const name = session?.effective?.username || session?.actor?.username || "";
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
      const body = await readJSON("/v1/packaging-runs");
      next.packaging = Array.isArray(body.items) ? body.items : [];
      const run = next.packaging.find((item) => item.runId);
      if (run) {
        const events = await readJSON(`/v1/packaging-runs/${encodeURIComponent(run.runId)}/events`);
        next.packagingEvents = Array.isArray(events.items) ? events.items : [];
      }
    } catch {
      next.packagingNote = "Hub did not return packaging runs.";
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
        const envelope = await readJSON(`/v1/bundles/${encodeURIComponent(newest.id)}`);
        next.preview = await postJSON("/v1/bundles/preview", envelope);
      }
    } catch {
      next.bundleNote = "Hub did not return a signed bundle.";
    }
    state.hub = next;
    paintOperator();
    render();
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
        root.innerHTML = missing(hub(state).packagingNote || "No packaging run is recorded.");
        return;
      }
      const run = runs[0];
      const events = (hub(state).packagingEvents || [])
        .map(
          (event) => `<tr>
            <td>${esc(event.step)}</td>
            <td>${esc(event.outcome || "")}</td>
            <td class="text-[#8b96a5]">${esc(event.reason || "")}</td>
          </tr>`,
        )
        .join("");
      root.innerHTML = `<div class="so-view-enter space-y-3">
        ${panel(
          "Packaging run",
          `<dl class="grid grid-cols-1 sm:grid-cols-2 gap-2 text-[12px]">
            <div><dt class="text-[#6d7885]">Run</dt><dd class="code-id text-white">${esc(run.runId)}</dd></div>
            <div><dt class="text-[#6d7885]">Phase</dt><dd class="text-white">${esc(run.phase || "")}</dd></div>
            <div><dt class="text-[#6d7885]">Outcome</dt><dd class="text-white">${esc(run.outcome || "")}</dd></div>
            <div><dt class="text-[#6d7885]">Plan</dt><dd class="code-id text-white">${esc(run.planId || run.targetPlanRef || "")}</dd></div>
            <div><dt class="text-[#6d7885]">Template</dt><dd class="text-white">${esc(run.templateVersion || "")}</dd></div>
            <div><dt class="text-[#6d7885]">Tools</dt><dd class="text-white">${esc(run.toolVersions || "")}</dd></div>
            <div class="sm:col-span-2"><dt class="text-[#6d7885]">Source digest</dt><dd class="code-id text-white break-all">${esc(run.sourceDigest || "")}</dd></div>
          </dl>`,
        )}
        ${panel(
          "Steps",
          events
            ? `<table class="so-table"><thead><tr><th>Step</th><th>Outcome</th><th>Reason</th></tr></thead><tbody>${events}</tbody></table>`
            : `<p class="text-[12px] text-[#8b96a5]">No step events recorded.</p>`,
        )}
      </div>`;
    },
    renderBundle(root) {
      const preview = hub(state).preview;
      if (!preview) {
        root.innerHTML = missing(hub(state).bundleNote || "No signed bundle is recorded.");
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
      root.innerHTML = `<div class="so-view-enter space-y-3">
        ${panel(
          "Bundle preview",
          `<dl class="space-y-2 text-[12px]">
            <div><dt class="text-[#6d7885]">Bundle</dt><dd class="code-id text-white">${esc(preview.bundleId)}</dd></div>
            <div><dt class="text-[#6d7885]">Manifest digest</dt><dd class="code-id text-white break-all">${esc(preview.manifestDigest)}</dd></div>
            <div><dt class="text-[#6d7885]">Signature key</dt><dd class="code-id text-white break-all">${esc(signature.keyId || "")}</dd></div>
            <div><dt class="text-[#6d7885]">Signature verified</dt><dd class="text-white">${signature.verified ? "yes" : "no"}</dd></div>
          </dl>`,
        )}
        ${panel(
          "SBOM",
          components
            ? `<table class="so-table"><thead><tr><th>Kind</th><th>Name</th><th>Version</th><th>Digest</th></tr></thead><tbody>${components}</tbody></table>`
            : `<p class="text-[12px] text-[#8b96a5]">No chart or image components in this bundle.</p>`,
        )}
      </div>`;
    },
    renderChanges(root) {
      const rows = hub(state).changes || [];
      if (!rows.length) {
        root.innerHTML = missing(hub(state).changeNote || "No change request is recorded.");
        return;
      }
      const cards = rows
        .map((request) => {
          const reviews = (request.reviews || [])
            .map(
              (review) => `<li class="text-[12px] text-[#dce4ee]">${esc(review.decision)} by ${esc(review.subject)} — ${esc(review.rationale || "")}</li>`,
            )
            .join("");
          return panel(
            request.title || request.id,
            `<dl class="grid grid-cols-1 sm:grid-cols-2 gap-2 text-[12px]">
              <div><dt class="text-[#6d7885]">Change request</dt><dd class="code-id text-white">${esc(request.id)}</dd></div>
              <div><dt class="text-[#6d7885]">Status</dt><dd class="text-white">${esc(request.status)}</dd></div>
              <div><dt class="text-[#6d7885]">Author</dt><dd class="text-white">${esc(request.author?.username || "")}</dd></div>
              <div><dt class="text-[#6d7885]">Environment</dt><dd class="text-white">${esc(request.environment || "")}</dd></div>
            </dl>
            <ul class="mt-2 space-y-1">${reviews || `<li class="text-[12px] text-[#8b96a5]">No approval recorded.</li>`}</ul>`,
          );
        })
        .join("");
      root.innerHTML = `<div class="so-view-enter space-y-3">${cards}</div>`;
    },
    renderPromotion(root) {
      const edges = promotionEdges(state);
      if (!edges.length) {
        root.innerHTML = missing(hub(state).promotionNote || "No promotion from dev to staging is recorded.");
        return;
      }
      const rows = edges
        .map(
          (edge) => `<tr>
            <td class="text-white">${esc(edge.product || "")}</td>
            <td>${esc(edge.version || "")}</td>
            <td class="code-id">${esc(edge.fromChannel)} → ${esc(edge.toChannel)}</td>
            <td>${esc(edge.status || "")}</td>
          </tr>`,
        )
        .join("");
      root.innerHTML = `<div class="so-view-enter">${panel(
        "Promotion",
        `<p class="text-[12px] text-[#8b96a5]">DEV to STAGING.</p>
         <table class="so-table"><thead><tr><th>Product</th><th>Version</th><th>Edge</th><th>Status</th></tr></thead><tbody>${rows}</tbody></table>`,
      )}</div>`;
    },
    renderEnvironments(root) {
      const rows = hub(state).environments || [];
      if (!rows.length) {
        root.innerHTML = missing(hub(state).note || "No environment is recorded.");
        return;
      }
      const body = rows
        .map((row) => {
          const spoke = row.spoke || {};
          return `<tr>
            <td class="text-white">${esc(row.environment?.id || "")}</td>
            <td class="code-id">${esc(spoke.agentId || "")}</td>
            <td>${esc(spoke.state || "")}</td>
            <td class="text-[#8b96a5]">${esc(spoke.observedAt || "")}</td>
          </tr>`;
        })
        .join("");
      root.innerHTML = `<div class="so-view-enter">${panel(
        "Environments",
        `<table class="so-table"><thead><tr><th>Environment</th><th>Spoke</th><th>Liveness</th><th>Observed</th></tr></thead><tbody>${body}</tbody></table>`,
      )}</div>`;
    },
    renderRecall(root) {
      const pairs = recallsFrom(hub(state).plans);
      if (!pairs.length) {
        root.innerHTML = missing(hub(state).planNote || "No recall is recorded.");
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
      root.innerHTML = `<div class="so-view-enter space-y-3">${blocks}</div>`;
    },
  };
}

export function renderLiveProposal(root, state) {
  const proposal = recordedProposal(state);
  if (!proposal) return false;
  const verdict = proposal.failed
    ? "Deterministic checks blocked this proposal."
    : proposal.passed
      ? "Deterministic checks passed."
      : "The proposal is recorded. Checks have not finished.";
  const events = proposal.events
    .map(
      (event) => `<tr><td>${esc(event.step)}</td><td>${esc(event.outcome || "")}</td><td class="text-[#8b96a5]">${esc(event.reason || "")}</td></tr>`,
    )
    .join("");
  root.innerHTML = `<div class="so-view-enter space-y-3">
    ${panel(
      "Recorded proposal",
      `<p class="text-[12.5px] text-white">${esc(verdict)}</p>
       <dl class="grid grid-cols-1 sm:grid-cols-2 gap-2 text-[12px] mt-2">
         <div><dt class="text-[#6d7885]">Run</dt><dd class="code-id text-white">${esc(proposal.run.runId)}</dd></div>
         <div><dt class="text-[#6d7885]">Phase</dt><dd class="text-white">${esc(proposal.run.phase || "")}</dd></div>
         <div class="sm:col-span-2"><dt class="text-[#6d7885]">Source digest</dt><dd class="code-id text-white break-all">${esc(proposal.run.sourceDigest || "")}</dd></div>
       </dl>`,
    )}
    ${panel(
      "Checks",
      events
        ? `<table class="so-table"><thead><tr><th>Step</th><th>Outcome</th><th>Reason</th></tr></thead><tbody>${events}</tbody></table>`
        : `<p class="text-[12px] text-[#8b96a5]">No check events recorded.</p>`,
    )}
  </div>`;
  return true;
}

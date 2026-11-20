import {
  fetchAdminAITranscriptExport,
  fetchAdminAITranscripts,
  fetchAdminAIUsage,
} from "../../api/ai-governance.js";

function esc(value) {
  return String(value ?? "").replace(
    /[&<>"']/g,
    (character) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        character
      ],
  );
}

function formatBudget(limit, unit) {
  return limit === null ? "Unlimited" : `${limit.toLocaleString()} ${unit}/day`;
}

function formatTokens(row) {
  if (row.promptTokens === null && row.completionTokens === null) {
    return "Not reported";
  }
  const prompt = row.promptTokens ?? 0;
  const completion = row.completionTokens ?? 0;
  return `${(prompt + completion).toLocaleString()} (${prompt.toLocaleString()} in / ${completion.toLocaleString()} out)`;
}

/**
 * @param {{
 *   days?: number;
 *   usage?: import("../../api/ai-governance.js").AdminAIUsageReport | null;
 *   transcripts?: import("../../api/ai-governance.js").AdminAITranscriptsList | null;
 *   selectedExport?: import("../../api/ai-governance.js").AdminAITranscriptExport | null;
 *   exportError?: string;
 *   loadingExportId?: string;
 *   error?: string;
 * }} [options]
 */
export function adminAIGovernanceHTML({
  days = 7,
  usage = null,
  transcripts = null,
  selectedExport = null,
  exportError = "",
  loadingExportId = "",
  error = "",
} = {}) {
  const rows = Array.isArray(usage?.usage) ? usage.usage : [];
  const runs = Array.isArray(transcripts?.runs) ? transcripts.runs : [];
  const budgetSummary = usage
    ? `Daily cap: ${formatBudget(usage.dailyCallBudget, "calls")} · ${formatBudget(usage.dailyTokenBudget, "tokens")}`
    : "Fleet model budget summary unavailable.";
  const transcriptSummary = transcripts
    ? `Transcripts: ${esc(transcripts.transcripts)} · retention ${esc(transcripts.retention)}`
    : "Transcript policy unavailable.";

  return `<section class="so-panel p-3 space-y-3" data-admin-ai-governance>
    <div class="flex flex-wrap items-center justify-between gap-2">
      <div>
        <h3>AI model governance &amp; audited transcripts</h3>
        <p>${esc(budgetSummary)} · ${transcriptSummary}</p>
      </div>
      <div class="flex items-center gap-2">
        <label class="text-sm">Window
          <select data-admin-ai-days aria-label="Fleet AI usage window">
            ${[1, 7, 30]
              .map(
                (option) =>
                  `<option value="${option}"${option === days ? " selected" : ""}>Last ${option}d</option>`,
              )
              .join("")}
          </select>
        </label>
        <button type="button" class="kit-btn" data-admin-ai-refresh>Refresh AI governance</button>
      </div>
    </div>
    ${error ? `<p role="alert">${esc(error)}</p>` : ""}
    <div>
      <h4>Fleet model usage (${rows.length} row${rows.length === 1 ? "" : "s"})</h4>
      ${
        rows.length
          ? `<table class="w-full text-sm"><thead><tr><th>Day</th><th>Owner</th><th>Capability</th><th>Model</th><th>Outcome</th><th>Calls (charged)</th><th>Tokens</th></tr></thead><tbody>${rows
              .slice(0, 100)
              .map(
                (row) =>
                  `<tr><td>${esc(row.day)}</td><td>${esc(row.owner)}</td><td>${esc(row.capability)}</td><td class="code-id">${esc(row.providerModel || row.configuredModel || "unreported")}</td><td>${esc(row.outcome)}</td><td>${esc(row.calls)} (${esc(row.budgetedCalls)})</td><td>${esc(formatTokens(row))}</td></tr>`,
              )
              .join("")}</tbody></table>`
          : "<p>No metered model calls recorded in this window.</p>"
      }
    </div>
    <div class="space-y-2">
      <h4>Recorded AI runs (${runs.length})</h4>
      <p class="text-sm">Inspecting a transcript exports the run from the Hub and writes an immutable audit event before returning bytes.</p>
      ${
        runs.length
          ? `<table class="w-full text-sm"><thead><tr><th>Run ID</th><th>Owner</th><th>Capability</th><th>Rounds</th><th>Bytes</th><th>Action</th></tr></thead><tbody>${runs
              .slice(0, 50)
              .map(
                (run) =>
                  `<tr><td class="code-id">${esc(run.runId)}</td><td>${esc(run.owner)}</td><td>${esc(run.capability)}</td><td>${esc(run.rounds)}</td><td>${esc(run.bytes)}</td><td><button type="button" class="kit-btn" data-admin-ai-export="${esc(run.runId)}"${loadingExportId === run.runId ? " disabled" : ""}>${loadingExportId === run.runId ? "Exporting…" : "Verify &amp; export"}</button></td></tr>`,
              )
              .join("")}</tbody></table>`
          : "<p>No unexpired AI run transcripts are stored.</p>"
      }
      ${exportError ? `<p role="alert">${esc(exportError)}</p>` : ""}
      ${
        selectedExport
          ? `<div class="so-panel p-3 space-y-2" data-admin-ai-export-detail>
              <p><strong>Run <span class="code-id">${esc(selectedExport.runId)}</span></strong> · Owner ${esc(selectedExport.owner || "unknown")} · Chain verification: <strong>${selectedExport.verified ? "VERIFIED" : "FAILED"}</strong></p>
              ${
                selectedExport.problems.length
                  ? `<ul role="alert">${selectedExport.problems.map((problem) => `<li>${esc(problem)}</li>`).join("")}</ul>`
                  : ""
              }
              <ul class="space-y-2">${selectedExport.exchanges
                .map(
                  (exchange) =>
                    `<li><p>Round ${esc(exchange.round)} · ${esc(exchange.capability)} · ${esc(exchange.outcome || "unknown")} · transcript ${esc(exchange.transcript)} · chain <span class="code-id">${esc(exchange.chainSha256.slice(0, 16) || "none")}</span></p>${
                      exchange.present
                        ? `<details><summary>Prompt (${esc(exchange.promptFidelity || "exact")}) &amp; response (${esc(exchange.responseFidelity || "exact")})</summary><pre class="whitespace-pre-wrap code-id">${esc(exchange.prompt)}</pre><pre class="whitespace-pre-wrap code-id">${esc(exchange.response)}</pre></details>`
                        : "<p>Transcript text unavailable or expired; audit digests preserved.</p>"
                    }</li>`,
                )
                .join("")}</ul>
            </div>`
          : ""
      }
    </div>
  </section>`;
}

export function createAdminAIGovernance({ fetchImpl = fetch } = {}) {
  let revision = 0;
  let days = 7;
  let usage = null;
  let transcripts = null;
  let selectedExport = null;
  let exportError = "";
  let loadingExportId = "";
  let error = "";

  async function load(root, signal) {
    const current = ++revision;
    error = "";
    try {
      const [usageData, transcriptsData] = await Promise.all([
        fetchAdminAIUsage(days, "", fetchImpl, signal),
        fetchAdminAITranscripts("", fetchImpl, signal),
      ]);
      if (current !== revision || root.isConnected === false) return;
      usage = usageData;
      transcripts = transcriptsData;
      paint(root);
    } catch (err) {
      if (current !== revision || signal?.aborted || root.isConnected === false)
        return;
      error =
        err?.status === 401 || err?.status === 403
          ? "Administrator access required for fleet AI governance."
          : "Fleet AI governance data is unavailable.";
      paint(root);
    }
  }

  function paint(root) {
    root.innerHTML = adminAIGovernanceHTML({
      days,
      usage,
      transcripts,
      selectedExport,
      exportError,
      loadingExportId,
      error,
    });
    const select = root.querySelector("[data-admin-ai-days]");
    if (select) {
      select.onchange = () => {
        days = Number(select.value) || 7;
        void load(root);
      };
    }
    root.onclick = (event) => {
      if (event.target?.closest?.("[data-admin-ai-refresh]")) {
        void load(root);
        return;
      }
      const exportBtn = event.target?.closest?.("[data-admin-ai-export]");
      if (exportBtn) {
        const runId = exportBtn.getAttribute("data-admin-ai-export") || "";
        if (!runId) return;
        const stamp = ++revision;
        loadingExportId = runId;
        exportError = "";
        paint(root);
        void fetchAdminAITranscriptExport(runId, fetchImpl)
          .then((result) => {
            if (stamp !== revision || root.isConnected === false) return;
            loadingExportId = "";
            selectedExport = result;
            paint(root);
          })
          .catch(() => {
            if (stamp !== revision || root.isConnected === false) return;
            loadingExportId = "";
            exportError = `Audited export failed for run ${runId}.`;
            paint(root);
          });
      }
    };
  }

  return {
    async mount(root, signal) {
      root.innerHTML =
        '<section class="so-panel p-3" data-admin-ai-governance role="status">Loading AI model governance…</section>';
      await load(root, signal);
    },
  };
}

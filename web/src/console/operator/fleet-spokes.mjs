import { convergenceMarkup } from "./release-convergence.js";
const esc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
const timestamp = (value) =>
  typeof value === "string" &&
  !value.startsWith("0001-") &&
  Number.isFinite(Date.parse(value))
    ? value
    : "unavailable";

// CONDUIT-INVENTED: reporting.SpokeWorkloadView
// Displaced-by: A public or authorized sanitized Apollo spoke/workload view and its backing report contract establishing inventory grain, freshness and missing-data semantics.
export function workloadMarkup(record) {
  const environment = record?.environment?.id;
  const report = record?.reported;
  const rows = Array.isArray(report?.entities)
    ? report.entities.filter((row) => row && typeof row === "object")
    : [];
  return `<section data-spoke-workloads class="so-panel p-3 space-y-2"><h3>Reported workloads · ${esc(environment || "unavailable")}</h3><p>Agent report: ${esc(report?.agentId || "unavailable")} · observed ${esc(timestamp(report?.observedAt))}. Historical report fields do not establish current cluster health. CPU, memory utilisation and cluster topology are unavailable.</p>${
    rows.length
      ? `<div class="overflow-x-auto"><table class="so-table"><thead><tr><th>Installation</th><th>Product</th><th>Namespace</th><th>Version</th><th>Release digest</th><th>Health</th><th>Lifecycle</th><th>Observed at</th></tr></thead><tbody>${rows
          .slice(0, 256)
          .map(
            (row) =>
              `<tr><td>${esc(row.installationId || "unavailable")}</td><td>${esc(row.product || "unavailable")}</td><td>${esc(row.namespace || "unavailable")}</td><td>${esc(row.version || "unavailable")}</td><td>${esc(row.releaseDigest || "unavailable")}</td><td>${esc(row.health || "UNKNOWN")}</td><td>${esc(row.lifecycle || "UNKNOWN")}</td><td>${esc(timestamp(row.observedAt))}</td></tr>`,
          )
          .join(
            "",
          )}</tbody></table></div>${rows.length > 256 ? "<p>Showing the first 256 reported workloads.</p>" : ""}`
      : "<p>No workload report is recorded for this environment.</p>"
  }</section>`;
}
export function spokesMarkup(hub = {}) {
  const records = Array.isArray(hub.environments)
    ? hub.environments.filter((row) => row && typeof row === "object")
    : [];
  return `<div data-spoke-operations class="p-3 space-y-3" data-value-tier="CONDUIT_NATIVE"><h2>Spoke operations</h2><p>Conduit native Hub observations. Liveness is the Hub's report-freshness classification, not a network probe. No site pending-sync status, transfer progress, cluster count or verified installation is inferred.</p>${hub.note ? `<p role="status">${esc(hub.note)}</p>` : ""}${
    records.length
      ? records
          .slice(0, 256)
          .map(
            (record) =>
              `<section class="so-panel p-3"><h3>${esc(record.environment?.id || "unavailable")}</h3><p>Spoke: ${esc(record.spoke?.state || "unknown")} · Agent: ${esc(record.spoke?.agentId || "unavailable")} · report observed ${esc(timestamp(record.spoke?.observedAt))}</p><button type="button" class="so-btn" data-spoke-environment="${esc(record.environment?.id || "")}">Open environment evidence</button>${workloadMarkup(record)}</section>`,
          )
          .join("")
      : "<p>No visible environment is recorded.</p>"
  }${records.length > 256 ? "<p>Showing the first 256 visible environments.</p>" : ""}${convergenceMarkup(hub.installations, hub.installationNote)}</div>`;
}

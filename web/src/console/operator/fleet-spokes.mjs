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

const booleanFact = (value) =>
  typeof value === "boolean" ? (value ? "yes" : "no") : "unavailable";
export function namespaceMarkup(environment = {}) {
  const rows = Array.isArray(environment.namespaces)
    ? environment.namespaces.filter((row) => row && typeof row === "object")
    : [];
  return `<section data-spoke-namespaces class="so-panel p-3"><h4>Declared delivery namespaces</h4><p>Connection setting: ${esc(environment.connectionSetting || "unavailable")} · state source: ${esc(environment.declaredStateSource || "unavailable")}. Operator configuration is not Kubernetes discovery or observed namespace health.</p>${
    rows.length
      ? `<table class="so-table"><thead><tr><th>Namespace</th><th>Identifier</th><th>Deletion protected</th><th>Trashed</th></tr></thead><tbody>${rows
          .slice(0, 256)
          .map(
            (row) =>
              `<tr><td>${esc(row.name || "unavailable")}</td><td>${esc(row.rid || "unavailable")}</td><td>${booleanFact(row.deletionProtected)}</td><td>${booleanFact(row.trashed)}</td></tr>`,
          )
          .join(
            "",
          )}</tbody></table>${rows.length > 256 ? "<p>Showing the first 256 declared namespaces.</p>" : ""}`
      : "<p>No namespace declaration is available.</p>"
  }</section>`;
}
export function instanceMarkup(entities) {
  const rows = [];
  let truncated = false;
  outer: for (const entity of Array.isArray(entities) ? entities : []) {
    for (const node of Array.isArray(entity?.healthDetails?.nodes)
      ? entity.healthDetails.nodes
      : []) {
      if (!node || typeof node !== "object") continue;
      if (rows.length === 256) {
        truncated = true;
        break outer;
      }
      rows.push({ entity, node });
    }
  }
  return `<section data-spoke-instances class="so-panel p-3"><h4>Reported workload instances</h4><p>Runtime-defined instance IDs and container checks from the Entity health report. The Kubernetes Agent uses Pod UID/name for these IDs; they are not physical cluster-node inventory. Values may be historical.</p>${
    rows.length
      ? `<table class="so-table"><thead><tr><th>Installation</th><th>Instance ID</th><th>Version</th><th>Result</th><th>Reported container checks</th></tr></thead><tbody>${rows
          .map(({ entity, node }) => {
            const containers = Array.isArray(node.containers)
              ? node.containers.filter((row) => row && typeof row === "object")
              : [];
            return `<tr><td>${esc(entity.installationId || "unavailable")}</td><td>${esc(node.nodeId || "unavailable")}</td><td>${esc(node.productVersion || "unavailable")}</td><td>${esc(node.result || "UNKNOWN")}</td><td>${
              containers.length
                ? containers
                    .slice(0, 32)
                    .map(
                      (row) =>
                        `<div>${esc(row.containerId || "unavailable")} · liveness ${booleanFact(row.liveness)} · readiness ${booleanFact(row.readiness)} · ${esc(row.result || "UNKNOWN")}</div>`,
                    )
                    .join("")
                : "No container checks reported."
            }${containers.length > 32 ? "<p>First 32 reported containers shown.</p>" : ""}</td></tr>`;
          })
          .join(
            "",
          )}</tbody></table>${truncated ? "<p>First 256 reported instances shown.</p>" : ""}`
      : "<p>No workload-instance health details are recorded.</p>"
  }</section>`;
}

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
  }${namespaceMarkup(record?.environment)}${instanceMarkup(rows)}</section>`;
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

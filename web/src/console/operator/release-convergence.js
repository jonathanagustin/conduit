// Classify only the desired and reported installation fields returned by Hub.
export function convergenceState(row) {
  if (!row.observedAt || row.observedAt.startsWith("0001-"))
    return "unreported";
  if (row.stale) return "stale";
  if (row.stale !== false || !Number.isFinite(Date.parse(row.observedAt)))
    return "unknown";
  if (
    !row.desiredVersion ||
    !row.desiredReleaseDigest ||
    !row.desiredConfigDigest
  )
    return "unknown";
  if (
    row.observedVersion !== row.desiredVersion ||
    row.observedReleaseDigest !== row.desiredReleaseDigest ||
    row.observedConfigDigest !== row.desiredConfigDigest ||
    row.configStatus === "DRIFTED"
  )
    return "diverged";
  if (
    row.lifecycle !== "INSTALLED" ||
    row.configStatus !== "APPLIED" ||
    row.rolloutStatus !== "UP_TO_DATE" ||
    row.health !== "HEALTHY"
  )
    return "pending";
  return "converged";
}

const esc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );

export function convergenceMarkup(installations, note = "") {
  const rows = Array.isArray(installations) ? installations : [];
  return `<section class="so-panel p-3 space-y-2" data-release-convergence>
    <h2>Fleet release convergence</h2>
    <p class="kit-muted">Reported installation state. Convergence requires fresh, healthy, installed reports matching the desired version and both digests.</p>
    ${note ? `<p role="status">${esc(note)}</p>` : ""}
    ${rows.length ? `<div class="overflow-x-auto"><table class="so-table"><thead><tr><th>Environment</th><th>Installation</th><th>Product</th><th>Desired</th><th>Reported</th><th>State</th><th>Observed at</th></tr></thead><tbody>${rows.map((row) => `<tr><td>${esc(row.environment)}</td><td>${esc(row.installationId)}</td><td>${esc(row.product)}</td><td>${esc(row.desiredVersion || "unavailable")}</td><td>${esc(row.observedVersion || "unavailable")}</td><td>${esc(convergenceState(row))}</td><td>${esc(row.observedAt || "unavailable")}</td></tr>`).join("")}</tbody></table></div>` : '<p class="kit-muted">No installation report is available. No convergence result can be inferred.</p>'}
  </section>`;
}

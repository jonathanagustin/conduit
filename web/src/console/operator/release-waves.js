const esc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );

/** Render recorded wave order, never infer dates or progress from array position. */
export function releaseWavesMarkup(rollouts, note = "") {
  const rows = Array.isArray(rollouts) ? rollouts : [];
  return `<section class="so-panel p-3 space-y-3" data-release-waves>
    <h2>Staged rollout waves</h2>
    <p class="kit-muted">Recorded wave order and gate decisions. These records do not contain calendar intervals, maintenance windows or dependency edges; scheduling is unavailable here.</p>
    ${note ? `<p role="status">${esc(note)}</p>` : ""}
    ${
      rows.length
        ? rows
            .map(
              (rollout) => `<article class="so-panel p-3 space-y-2">
      <h3>${esc(rollout.id)} · ${esc(rollout.product)} ${esc(rollout.version)}</h3>
      <p>Status: ${esc(rollout.status || "unknown")}${rollout.pausedBy ? ` · Paused by: ${esc(rollout.pausedBy)}` : ""}</p>
      ${Array.isArray(rollout.waves) && rollout.waves.length ? `<ol>${rollout.waves.map((wave) => `<li class="border-b p-2"><strong>${esc(wave.name)}</strong> · ${esc(wave.status || "unknown")} · Gate: ${esc(wave.gate || "unknown")}<p>Environments: ${Array.isArray(wave.environments) && wave.environments.length ? wave.environments.map(esc).join(", ") : "unavailable"}</p>${wave.reason ? `<p>Reason: ${esc(wave.reason)}</p>` : ""}</li>`).join("")}</ol>` : "<p>No wave detail was recorded.</p>"}
    </article>`,
            )
            .join("")
        : "<p>No staged rollout is recorded.</p>"
    }
  </section>`;
}

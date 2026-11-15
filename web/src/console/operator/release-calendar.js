// Actual maintenance overrides can be drawn; wave order is not a schedule.
const esc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
export function recordedIntervals(overrides) {
  const rows = [];
  const seen = new Set();
  for (const row of Array.isArray(overrides) ? overrides : []) {
    if (
      !row ||
      typeof row.startsAt !== "string" ||
      typeof row.endsAt !== "string" ||
      row.startsAt.startsWith("0001-")
    )
      continue;
    const start = Date.parse(row.startsAt),
      end = Date.parse(row.endsAt);
    if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start)
      continue;
    const id = JSON.stringify([
      row.type,
      row.installationId,
      row.startsAt,
      row.endsAt,
      row.reason,
      row.approvedBy,
    ]);
    if (seen.has(id)) continue;
    seen.add(id);
    rows.push({ ...row, start, end });
  }
  return rows.sort((a, b) => a.start - b.start || a.end - b.end).slice(0, 64);
}
export function releaseCalendarMarkup(overrides = []) {
  const rows = recordedIntervals(overrides);
  const start = rows.length ? Math.min(...rows.map((row) => row.start)) : 0;
  const end = rows.length ? Math.max(...rows.map((row) => row.end)) : 1;
  return `<section class="so-panel p-3 space-y-2" data-release-calendar>
    <h3>Rollout and maintenance calendar</h3>
    ${rows.length ? `<p>Recorded maintenance override intervals, ordered by start. These are not rollout execution appointments. At most 64 distinct valid intervals are shown.</p><ol>${rows.map((row) => `<li class="p-2 border-b"><p>${esc(row.type)} · ${esc(row.installationId || "environment")} · ${esc(row.startsAt)} → ${esc(row.endsAt)}</p><p>${esc(row.reason)} · ${esc(row.approvedBy)}</p><div role="img" aria-label="Recorded maintenance override interval" style="height:12px;background:var(--so-card)"><div style="height:12px;background:var(--bar-info);margin-left:${(100 * (row.start - start)) / (end - start)}%;width:${(100 * (row.end - row.start)) / (end - start)}%"></div></div></li>`).join("")}</ol>` : '<p role="status">Resolved scheduling data is unavailable.</p>'}
    <p class="kit-muted">Hub rollout waves do not provide start/end intervals or resolved calendar dependency edges. Weekly maintenance rules constrain execution but are not appointments. No Gantt bars for rollouts, blackout spans, drag, resize, snap or Save can be inferred from these records. A governed scheduling proposal and resolved schedule are required.</p>
  </section>`;
}

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
/** A legacy decision-only response cannot establish any interval coordinates. */
export function blackoutIntervals(snapshot) {
  if (
    !snapshot ||
    snapshot.valueTier !== "CONDUIT_NATIVE" ||
    snapshot.persistence !== "process-local" ||
    typeof snapshot.environment !== "string" ||
    !snapshot.environment ||
    typeof snapshot.truncated !== "boolean" ||
    !Number.isFinite(Date.parse(snapshot.evaluatedAt)) ||
    !Array.isArray(snapshot.intervals) ||
    snapshot.intervals.length > 64
  )
    return null;
  for (const row of snapshot.intervals) {
    if (
      !row ||
      row.environment !== snapshot.environment ||
      typeof row.reason !== "string" ||
      !row.reason ||
      typeof row.author !== "string" ||
      !row.author ||
      typeof row.startsAt !== "string" ||
      typeof row.endsAt !== "string" ||
      !Number.isFinite(Date.parse(row.startsAt)) ||
      !Number.isFinite(Date.parse(row.endsAt)) ||
      Date.parse(row.endsAt) <= Date.parse(row.startsAt) ||
      row.startsAt.startsWith("0001-")
    )
      return null;
  }
  return recordedIntervals(
    snapshot.intervals.map((row) => ({
      ...row,
      type: "BLACKOUT",
      installationId: row.environment,
      approvedBy: row.author,
    })),
  );
}
/** @param {Array<Record<string, any>>} overrides
 * @param {unknown} blackout Untrusted native snapshot, validated before display.
 */
export function releaseCalendarMarkup(overrides = [], blackout = null) {
  const blackouts = blackoutIntervals(blackout);
  const rows = [...recordedIntervals(overrides), ...(blackouts || [])].sort(
    (a, b) => a.start - b.start || a.end - b.end,
  );
  const start = rows.length ? Math.min(...rows.map((row) => row.start)) : 0;
  const end = rows.length ? Math.max(...rows.map((row) => row.end)) : 1;
  const evidence =
    blackouts === null
      ? "Blackout interval evidence is unavailable; an evaluated reason alone is not a calendar span."
      : `Process-local blackout snapshot at ${esc(blackout.evaluatedAt)}. ${blackout.truncated ? "Only the first 64 stored intervals are shown; omitted intervals can still block changes." : blackouts.length ? "Stored blackout intervals are shown." : "No stored blackout interval is recorded in this snapshot."}`;
  return `<section class="so-panel p-3 space-y-2" data-release-calendar>
    <h3>Rollout and maintenance calendar</h3>
    <p data-blackout-evidence>${evidence}</p>
    ${rows.length ? `<p>Recorded maintenance override and blackout intervals, ordered by start. These are not rollout execution appointments. At most 64 distinct valid intervals of each kind are shown.</p><ol>${rows.map((row) => `<li class="p-2 border-b"${row.type === "BLACKOUT" ? " data-blackout-interval" : ""}><p>${esc(row.type)} · ${esc(row.installationId || "environment")} · ${esc(row.startsAt)} → ${esc(row.endsAt)}</p><p>${esc(row.reason)} · ${esc(row.approvedBy)}</p><div role="img" aria-label="Recorded ${row.type === "BLACKOUT" ? "blackout" : "maintenance override"} interval" style="height:12px;background:var(--so-card)"><div style="height:12px;background:${row.type === "BLACKOUT" ? "repeating-linear-gradient(135deg,var(--text-2) 0 2px,transparent 2px 6px)" : "var(--bar-info)"};margin-left:${(100 * (row.start - start)) / (end - start)}%;width:${(100 * (row.end - row.start)) / (end - start)}%"></div></div></li>`).join("")}</ol>` : '<p role="status">Resolved scheduling data is unavailable.</p>'}
    <p class="kit-muted">Hub rollout waves do not provide start/end intervals or resolved calendar dependency edges. Weekly maintenance rules constrain execution but are not appointments. No Gantt bars for rollouts or resolved calendar dependency edges are inferred. Blackout spans require the native stored-interval snapshot and disappear on Hub restart. A governed scheduling proposal is distinct from recorded intervals. The Windows draft form can propose an Entity override after preview and confirmation; it cannot approve or execute a workload.</p>
  </section>`;
}

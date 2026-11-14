// Report freshness (kit v7.1 data-freshness pill): how long ago an agent last
// reported, from the timestamps the Hub returns. A report older than a day is
// stale. The Hub writes Go's zero time for "never", which counts as no report.

const defaultEsc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );

const DAY_MS = 24 * 60 * 60 * 1000;

/** The first timestamp that is set and is not the zero time. */
export function reportTime(...values) {
  for (const value of values) {
    const text = String(value || "");
    if (text && !text.startsWith("0001-01-01")) return text;
  }
  return "";
}

function ago(ms) {
  const minutes = Math.floor(ms / 60000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

/**
 * { state: none | fresh | recent | stale | unknown, label } for one time.
 * `hubStale` is the Hub's own verdict (an installation's `stale` field); when
 * it is set, a report counts as stale whatever its age.
 */
export function freshness(value, now = Date.now(), hubStale = false) {
  const text = reportTime(value);
  if (!text) return { state: "none", label: "No report" };
  const at = Date.parse(text);
  if (Number.isNaN(at)) return { state: "unknown", label: text };
  const age = Math.max(0, now - at);
  if (age >= DAY_MS || hubStale)
    return { state: "stale", label: `Stale · ${ago(age)}` };
  if (age < 15 * 60 * 1000) return { state: "fresh", label: ago(age) };
  return { state: "recent", label: ago(age) };
}

/** A small pill with the age; the exact timestamp is in its title. */
export function freshnessPill(
  value,
  { now = Date.now(), esc = defaultEsc } = {},
) {
  const f = freshness(value, now);
  const title = reportTime(value) || "This agent has not reported.";
  return `<span class="kit-fresh kit-fresh-${f.state}" title="${esc(title)}">${esc(f.label)}</span>`;
}

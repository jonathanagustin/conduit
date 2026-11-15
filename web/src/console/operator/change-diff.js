const esc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
const sensitive = /password|secret|credential|token|private.?key/i;
export function diffValue(value, path, recorded = true) {
  if (!recorded) return "[not recorded]";
  if (sensitive.test(path)) return "[redacted]";
  try {
    const text = JSON.stringify(
      value,
      (key, item) => (sensitive.test(key) ? "[redacted]" : item),
      2,
    );
    if (typeof text !== "string") return "[unavailable]";
    return text.length > 8192
      ? text.slice(0, 8192) + "\n[display truncated]"
      : text;
  } catch {
    return "[unavailable]";
  }
}
function lines(value) {
  return `<ol>${value
    .split("\n")
    .slice(0, 80)
    .map(
      (line, i) =>
        `<li><span class="kit-muted">${i + 1}</span> <code>${esc(line)}</code></li>`,
    )
    .join(
      "",
    )}</ol>${value.split("\n").length > 80 ? "<p>Display truncated after 80 lines.</p>" : ""}`;
}
export function changeDiffMarkup(request) {
  const entries = [];
  for (const change of Array.isArray(request?.changes) ? request.changes : []) {
    if (
      !change?.diff ||
      typeof change.diff !== "object" ||
      Array.isArray(change.diff)
    )
      continue;
    for (const [path, pair] of Object.entries(change.diff).sort(([a], [b]) =>
      a.localeCompare(b),
    )) {
      if (!pair || typeof pair !== "object") continue;
      entries.push({
        change: change.id,
        path,
        before: diffValue(pair.before, path, Object.hasOwn(pair, "before")),
        after: diffValue(pair.after, path, Object.hasOwn(pair, "after")),
      });
    }
  }
  return `<section data-cr-diff class="so-panel p-3 space-y-2"><h4>Recorded configuration diff</h4><p class="kit-muted">Before and after fields supplied by the Hub. Line numbers refer to this JSON display, not source-file lines. Known sensitive field names are masked.</p>${
    entries.length
      ? entries
          .slice(0, 128)
          .map(
            (row) =>
              `<article class="border-b p-2"><h5>${esc(row.change)} · ${esc(row.path)}</h5><div class="grid md:grid-cols-2 gap-3"><div><h6>Before</h6>${lines(row.before)}</div><div><h6>After</h6>${lines(row.after)}</div></div></article>`,
          )
          .join("")
      : "<p>No field-level diff is recorded for this request.</p>"
  }${entries.length > 128 ? "<p>Only the first 128 diff entries are displayed.</p>" : ""}</section>`;
}

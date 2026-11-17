function esc(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

// channelTable renders the subscription view. An empty view is an empty string
// so the caller can show its own empty state.
export function channelTable(view) {
  const channels =
    Array.isArray(view?.channels) && view.channels.length
      ? view.channels
      : ["dev", "candidate", "stable"];
  const rows = Array.isArray(view?.rows) ? view.rows : [];
  if (!rows.length) return "";
  const head = channels.map((channel) => `<th>${esc(channel)}</th>`).join("");
  const body = rows
    .map((row) => {
      const versions = row.versions || {};
      const cells = channels
        .map(
          (channel) =>
            `<td class="code-id">${esc(versions[channel] || "")}</td>`,
        )
        .join("");
      const subscribed = row.subscribed || "not subscribed";
      return `<tr><td class="text-[var(--text-1)]">${esc(row.environment)}</td><td>${esc(row.product)}</td><td>${esc(subscribed)}</td>${cells}<td class="code-id">${esc(row.running || "")}</td></tr>`;
    })
    .join("");
  return `<table class="so-table"><thead><tr><th>Environment</th><th>Product</th><th>Subscribed</th>${head}<th>Running</th></tr></thead><tbody>${body}</tbody></table>`;
}

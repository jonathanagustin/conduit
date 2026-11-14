// Products and versions: each catalog product with its newest release per
// channel and how many reported installations run the newest release.

const CHANNEL_ORDER = ["DEV", "CANDIDATE", "STABLE"];

const defaultEsc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );

/** Compares dotted numeric versions; non-numeric parts compare as text. */
export function compareVersions(left, right) {
  const a = String(left ?? "").split(/[.+-]/);
  const b = String(right ?? "").split(/[.+-]/);
  for (let i = 0; i < Math.max(a.length, b.length); i += 1) {
    const x = a[i] ?? "";
    const y = b[i] ?? "";
    const nx = Number(x);
    const ny = Number(y);
    const diff =
      x !== "" && y !== "" && Number.isFinite(nx) && Number.isFinite(ny)
        ? nx - ny
        : x.localeCompare(y);
    if (diff !== 0) return diff < 0 ? -1 : 1;
  }
  return 0;
}

/** One row per product, from `/v1/products` and `/v1/fleet/installations`. */
export function productRows(products, installations) {
  const installs = Array.isArray(installations) ? installations : [];
  return (Array.isArray(products) ? products : []).map((entry) => {
    const releases = Array.isArray(entry.releases) ? entry.releases : [];
    const latest = releases.reduce(
      (best, release) =>
        !best || compareVersions(release.version, best.version) > 0
          ? release
          : best,
      null,
    );
    const channels = {};
    for (const release of releases) {
      for (const channel of release.channels || []) {
        const current = channels[channel];
        if (!current || compareVersions(release.version, current) > 0) {
          channels[channel] = release.version;
        }
      }
    }
    const running = installs.filter((row) => row.product === entry.product);
    const onLatest = latest
      ? running.filter((row) => row.observedVersion === latest.version).length
      : 0;
    return {
      product: entry.product,
      releases: releases.length,
      latest: latest?.version || "",
      latestDigest: latest?.chart?.digest || "",
      channels,
      installations: running.length,
      onLatest,
    };
  });
}

export function productsMarkup(rows, { esc = defaultEsc, note = "" } = {}) {
  if (!rows.length) {
    return `<div class="so-panel kit-view-panel"><p class="kit-muted">${esc(note || "No product is in the catalog.")}</p></div>`;
  }
  const channelNames = [
    ...CHANNEL_ORDER,
    ...[...new Set(rows.flatMap((row) => Object.keys(row.channels)))]
      .filter((name) => !CHANNEL_ORDER.includes(name))
      .sort(),
  ];
  const head = channelNames.map((name) => `<th>${esc(name)}</th>`).join("");
  const body = rows
    .map((row) => {
      const cells = channelNames
        .map((name) =>
          row.channels[name]
            ? `<td class="code-id">${esc(row.channels[name])}</td>`
            : `<td class="kit-muted">—</td>`,
        )
        .join("");
      const tone =
        row.installations === 0
          ? "info"
          : row.onLatest === row.installations
            ? "ok"
            : "warn";
      return `<tr>
        <td class="code-id">${esc(row.product)}</td>
        <td class="code-id" title="${esc(row.latestDigest)}">${esc(row.latest)}</td>
        ${cells}
        <td>${esc(row.releases)}</td>
        <td><span class="kit-badge kit-badge-${tone}">${esc(row.onLatest)} of ${esc(row.installations)}</span></td>
      </tr>`;
    })
    .join("");
  return `<div class="so-panel kit-view-panel">
    <div class="kit-view-head"><h2>Products and versions</h2><span class="kit-muted">${rows.length} in the catalog</span></div>
    <p class="kit-muted">Newest release per channel. The last column counts reported installations that run the newest release.</p>
    <div class="overflow-x-auto"><table class="so-table">
      <thead><tr><th>Product</th><th>Newest</th>${head}<th>Releases</th><th>On newest</th></tr></thead>
      <tbody>${body}</tbody>
    </table></div>
  </div>`;
}

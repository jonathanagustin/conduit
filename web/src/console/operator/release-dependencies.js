import { spaceScopedPath } from "./space-scope.js";

const esc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
const mounts = new WeakMap();
const key = (row) => JSON.stringify([row.product, row.version]);
export function catalogCoordinates(products) {
  const seen = new Set();
  const rows = [];
  for (const product of Array.isArray(products) ? products : []) {
    if (typeof product?.product !== "string" || !product.product) continue;
    for (const release of Array.isArray(product.releases)
      ? product.releases
      : []) {
      if (typeof release?.version !== "string" || !release.version) continue;
      const row = { product: product.product, version: release.version };
      const id = key(row);
      if (!seen.has(id)) {
        seen.add(id);
        rows.push({ ...row, id });
      }
    }
  }
  return { rows: rows.slice(0, 32), truncated: rows.length > 32 };
}
/** Potential metadata reachability includes optional edges; it resolves no range. */
export function potentialDependents(records, product) {
  const impacted = new Set([product]);
  let changed = true;
  while (changed) {
    changed = false;
    for (const row of records) {
      if (!Array.isArray(row.dependencies)) continue;
      if (
        row.dependencies.some((dep) => impacted.has(dep.product)) &&
        !impacted.has(row.product)
      ) {
        impacted.add(row.product);
        changed = true;
      }
    }
  }
  return [...impacted].filter((name) => name !== product).sort();
}
export function dependencyMarkup(records, selectedId = "", note = "") {
  const row = selectedId
    ? records.find((item) => item.id === selectedId)
    : records[0];
  const picker = `<label>Release <select data-release-dependency>${records.map((item) => `<option value="${esc(item.id)}"${item === row ? " selected" : ""}>${esc(item.product)} ${esc(item.version)}</option>`).join("")}</select></label>`;
  if (!row)
    return `<section class="so-panel p-3"><h2>Release dependencies</h2>${picker}<p>No selected release is in the visible catalog subset.</p>${note ? `<p>${esc(note)}</p>` : ""}</section>`;
  const dependencies = Array.isArray(row.dependencies) ? row.dependencies : [];
  const impacted = potentialDependents(records, row.product);
  const height = Math.max(100, dependencies.length * 64);
  return `<section class="so-panel p-3 space-y-2" data-release-dependencies><h2>Release dependency constraints</h2>${picker}
    <p class="kit-muted">Hub-returned product/version constraints, not resolved installation edges. Potential blast radius is reachability within the successfully loaded visible catalog subset, includes optional dependencies and does not resolve version ranges or prove deployed impact.</p>
    ${note ? `<p role="status">${esc(note)}</p>` : ""}
    ${!Array.isArray(row.dependencies) ? "<p>Hub dependency metadata is unavailable for this release.</p>" : !dependencies.length ? "<p>Hub returned no dependency rows. This route does not distinguish an empty dependency set from absent captured metadata.</p>" : `<div class="overflow-x-auto"><svg role="img" aria-label="Recorded direct dependency constraints" viewBox="0 0 800 ${height}" style="min-width:600px"><defs><marker id="release-dependency-arrow" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto"><path d="M0,0 L8,4 L0,8" fill="currentColor"/></marker></defs><rect x="8" y="${height / 2 - 25}" width="220" height="50" fill="var(--bg-1)" stroke="currentColor"/><text x="16" y="${height / 2}" fill="currentColor">${esc(row.product)} ${esc(row.version)}</text>${dependencies.map((dep, i) => `<path d="M228,${height / 2} L470,${i * 64 + 28}" fill="none" stroke="currentColor" ${dep.optional ? 'stroke-dasharray="5 4"' : ""} marker-end="url(#release-dependency-arrow)"/><rect x="478" y="${i * 64 + 4}" width="310" height="50" fill="var(--bg-1)" stroke="currentColor"/><text x="486" y="${i * 64 + 24}" fill="currentColor">${esc(dep.product)} · ${dep.optional ? "optional" : "required"}</text><text x="486" y="${i * 64 + 43}" fill="currentColor">${esc(dep.minimumVersion || "no lower bound")} → ${esc(dep.maximumVersion || "no upper bound")}</text>`).join("")}</svg></div>`}
    <h3>Potential dependent products in loaded metadata</h3><p>${impacted.length ? impacted.map(esc).join(", ") : "No dependent product was found in the loaded subset; this does not prove no impact."}</p>
    </section>`;
}
export function upgradeLagMarkup(rows, available) {
  return `<section class="so-panel p-3 space-y-2" data-release-upgrade-lag><h2>Fleet upgrade lag</h2><p class="kit-muted">Hub-reported version differences and reasons. This does not establish digest convergence or infer a blackout/disconnection cause.</p>${
    !available
      ? "<p>Hub upgrade evidence is unavailable.</p>"
      : !rows.length
        ? "<p>Hub returned no version-lag rows.</p>"
        : `<div class="overflow-x-auto"><table class="so-table"><thead><tr><th>Environment</th><th>Installation</th><th>Product</th><th>Observed</th><th>Desired</th><th>Recorded reason</th></tr></thead><tbody>${rows
            .slice(0, 512)
            .map(
              (row) =>
                `<tr><td>${esc(row.environment)}</td><td>${esc(row.installationId)}</td><td>${esc(row.product)}</td><td>${esc(row.observedVersion || "unreported")}</td><td>${esc(row.desiredVersion)}</td><td>${esc(row.reason || "unavailable")}</td></tr>`,
            )
            .join(
              "",
            )}</tbody></table></div>${rows.length > 512 ? "<p>Only the first 512 lag rows are displayed.</p>" : ""}`
  }</section>`;
}
export function mountReleaseDependencies(
  root,
  products,
  { selectedId = "", spaceId = "", onSelect = () => {} } = {},
) {
  const token = {};
  mounts.set(root, token);
  const catalog = catalogCoordinates(products);
  const records = catalog.rows.map((row) => ({ ...row, dependencies: null }));
  let id = selectedId;
  let upgrades = [];
  let available = false;
  let loading = true;
  let failures = 0;
  const current = () => mounts.get(root) === token && root.isConnected;
  const paint = () => {
    if (current())
      root.innerHTML =
        dependencyMarkup(
          records,
          id,
          [
            loading ? "Loading bounded dependency metadata…" : "",
            catalog.truncated
              ? "Only the first 32 visible release coordinates were queried."
              : "",
            failures ? `${failures} dependency reads were unavailable.` : "",
          ]
            .filter(Boolean)
            .join(" "),
        ) + upgradeLagMarkup(upgrades, available);
  };
  root.onchange = (event) => {
    const target = event.target;
    if (
      !(target instanceof HTMLSelectElement) ||
      !target.matches("[data-release-dependency]")
    )
      return;
    id = target.value;
    onSelect(id);
    paint();
  };
  const read = async (path) => {
    const response = await fetch(path, {
      headers: { accept: "application/json" },
      signal: AbortSignal.timeout(15000),
    });
    if (!response.ok) throw new Error("unavailable");
    return response.json();
  };
  paint();
  void (async () => {
    let cursor = 0;
    const workers = Array.from(
      { length: Math.min(4, records.length) },
      async () => {
        while (cursor < records.length && current()) {
          const row = records[cursor++];
          try {
            const body = await read(
              `/v1/products/${encodeURIComponent(row.product)}/dependencies/${encodeURIComponent(row.version)}`,
            );
            if (
              !Array.isArray(body.dependencies) ||
              body.dependencies.length > 64 ||
              body.dependencies.some(
                (dep) =>
                  !dep ||
                  typeof dep.product !== "string" ||
                  !dep.product ||
                  typeof dep.minimumVersion !== "string" ||
                  typeof dep.maximumVersion !== "string" ||
                  (dep.optional !== undefined &&
                    typeof dep.optional !== "boolean"),
              )
            )
              throw new Error("invalid dependencies");
            row.dependencies = body.dependencies;
          } catch {
            failures += 1;
          }
        }
      },
    );
    workers.push(
      (async () => {
        try {
          const body = await read(
            spaceScopedPath("/v1/fleet/upgrades", spaceId),
          );
          if (
            !Array.isArray(body.upgrades) ||
            body.upgrades.some((row) => !row || typeof row !== "object")
          )
            throw new Error("invalid upgrades");
          upgrades = body.upgrades;
          available = true;
        } catch {
          available = false;
        }
      })(),
    );
    await Promise.all(workers);
    loading = false;
    paint();
  })();
}

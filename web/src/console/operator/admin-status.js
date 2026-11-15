const links = new Set([
  "area-packaging", "rm07-change-requests", "rm06-promotion-pipe", "area-fleet", "au01-audit",
]);
function esc(value) {
  return String(value ?? "").replace(/[&<>"']/g, (character) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[character]);
}
function bytes(value) {
  if (!Number.isFinite(value) || value < 0) return "Unavailable";
  const units = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];
  let size = value, unit = 0;
  while (size >= 1024 && unit < units.length - 1) { size /= 1024; unit++; }
  return `${size.toFixed(unit ? 1 : 0)} ${units[unit]}`;
}
export function adminStatusHTML(snapshot) {
  const components = Array.isArray(snapshot?.components) ? snapshot.components : [];
  const counters = Array.isArray(snapshot?.counters) ? snapshot.counters : [];
  const storage = Array.isArray(snapshot?.storage) ? snapshot.storage : [];
  const date = new Date(snapshot?.generatedAt);
  const captured = Number.isFinite(date.getTime()) ? date.toLocaleString() : "Unknown capture time";
  const audit = snapshot?.audit || {};
  return `<div class="space-y-3" data-admin-status>
    <section class="so-panel p-3 space-y-2"><h2>Platform status</h2><p>Read-only administrator measurements · ${esc(captured)}</p>
      <button type="button" class="kit-btn" data-admin-refresh>Refresh measurements</button></section>
    <section class="so-panel p-3 space-y-2"><h3>Services</h3><table class="w-full text-sm"><thead><tr><th>Component</th><th>Observation</th><th>Scope</th></tr></thead><tbody>${components.map((component) => `<tr><td>${esc(component.label)}</td><td>${esc(component.status)}</td><td>${esc(component.detail)}</td></tr>`).join("")}</tbody></table></section>
    <section class="grid grid-cols-1 md:grid-cols-3 gap-3" aria-label="Recorded activity">${counters.map((counter) => {
      const available = counter.status === "AVAILABLE" && Number.isFinite(counter.value) && counter.value >= 0;
      const link = links.has(counter.link) ? `<button type="button" class="kit-btn" data-admin-open="${esc(counter.link)}">Open records</button>` : "";
      return `<article class="so-panel p-3 space-y-2"><h3>${esc(counter.label)}</h3><strong>${available ? esc(counter.value) : "Unavailable"}</strong><p>${esc(counter.detail)}</p>${link}</article>`;
    }).join("")}</section>
    <section class="so-panel p-3 space-y-2"><h3>Storage filesystems</h3><table class="w-full text-sm"><thead><tr><th>Storage</th><th>Usage</th><th>State</th><th>Thresholds</th></tr></thead><tbody>${storage.map((item) => {
      const available = item.status !== "UNAVAILABLE" && Number.isFinite(item.usedPercent);
      return `<tr><td>${esc(item.label)}<p>${esc(item.detail)}</p></td><td>${available ? `${bytes(item.usedBytes)} / ${bytes(item.capacityBytes)} (${item.usedPercent.toFixed(1)}%)` : "Unavailable"}</td><td>${esc(item.status)}</td><td>Warning ${esc(item.warningPercent)}% · Critical ${esc(item.criticalPercent)}%</td></tr>`;
    }).join("")}</tbody></table></section>
    <section class="so-panel p-3 space-y-2"><h3>Stored audit chain</h3><p>${esc(audit.status)} · ${Number.isFinite(audit.chainLength) ? `${esc(audit.chainLength)} entries` : "Length unavailable"}</p><p>${audit.verifiedAt ? `Checked ${esc(new Date(audit.verifiedAt).toLocaleString())}` : "Verification time unavailable"}</p><p>${esc(audit.detail)}</p><button type="button" class="kit-btn" data-admin-open="au01-audit">Open audit records</button></section>
  </div>`;
}

/** @param {{fetchImpl?: typeof fetch, navigate?: (screen: string) => void}} options */
export function createAdminStatus({ fetchImpl = fetch, navigate = () => {} } = {}) {
  let revision = 0, controller = null;
  async function render(root) {
    const current = ++revision;
    controller?.abort(); controller = new AbortController();
    root.innerHTML = '<div class="so-panel p-3" role="status">Loading platform measurements…</div>';
    root.onclick = (event) => {
      if (event.target?.closest?.("[data-admin-refresh]")) { void render(root); return; }
      const button = event.target?.closest?.("[data-admin-open]");
      const screen = button?.getAttribute("data-admin-open");
      if (links.has(screen)) navigate(screen);
    };
    try {
      const response = await fetchImpl("/v1/admin/status", { method: "GET", credentials: "same-origin", headers: { accept: "application/json" }, signal: controller.signal });
      if (current !== revision || root.isConnected === false) return;
      if (response.status === 401 || response.status === 403) { root.innerHTML = '<div class="so-panel p-3" role="status">Administrator access required.</div>'; return; }
      if (!response.ok) throw new Error("status unavailable");
      const snapshot = await response.json();
      if (current === revision && root.isConnected !== false) root.innerHTML = adminStatusHTML(snapshot);
    } catch (error) {
      if (current !== revision || controller.signal.aborted || root.isConnected === false) return;
      root.innerHTML = '<div class="so-panel p-3" role="status">Platform status is unavailable.</div>';
    }
  }
  return { render };
}

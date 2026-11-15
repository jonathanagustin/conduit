const esc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
const mounts = new WeakMap();
async function json(path, body) {
  const response = await fetch(path, {
    method: body ? "POST" : "GET",
    headers: {
      accept: "application/json",
      ...(body ? { "content-type": "application/json" } : {}),
    },
    ...(body ? { body: JSON.stringify(body) } : {}),
    signal: AbortSignal.timeout(15000),
  });
  const result = await response.json();
  if (!response.ok)
    throw new Error(result.error || `Request failed (${response.status})`);
  return result;
}
export function verifiedSelection(item, envelope, preview) {
  return Boolean(
    item?.id &&
    item.manifestDigest &&
    envelope?.manifest?.id === item.id &&
    envelope.manifestDigest === item.manifestDigest &&
    preview?.bundleId === item.id &&
    preview.manifestDigest === item.manifestDigest &&
    preview.signature?.status === "verified",
  );
}
export function validHandoff(receipt, item, site, actor) {
  const handoff = receipt?.handoff;
  return Boolean(
    handoff?.id &&
    handoff.bundleId === item?.id &&
    handoff.digest === item?.manifestDigest &&
    handoff.site === site &&
    handoff.takenBy === actor &&
    handoff.status === "handed_off",
  );
}

// CONDUIT-INVENTED: delivery.OfflineBundleConsole
// Displaced-by: a public or authorized sanitized Apollo offline bundle console contract establishing selection, signature evidence, handoff and report-back semantics.
export function createOfflineBundleFlow({
  getHub,
  getIdentity,
  onPreview = () => {},
  onReview,
}) {
  const attempted = new Set();
  return async function mount(root, onPreviewForMount = onPreview) {
    const token = {};
    mounts.set(root, token);
    const current = () => mounts.get(root) === token && root.isConnected;
    const showPreview = (preview) => {
      if (current()) onPreviewForMount(preview);
    };
    const identity = JSON.stringify(getIdentity());
    const actor = getIdentity()?.actor;
    let selected = null,
      envelope = null,
      preview = null,
      handoff = null,
      busy = false;
    const sameIdentity = () =>
      Boolean(actor) && JSON.stringify(getIdentity()) === identity;
    const environments = () =>
      (getHub()?.environments || [])
        .map((row) => row?.environment?.id)
        .filter((id) => typeof id === "string" && id !== "");
    const knownSite = (site) => environments().includes(site);
    const show = (message) => {
      if (current())
        root.querySelector("[data-offline-note]").textContent = message;
    };
    root.innerHTML = `<section data-offline-flow data-value-tier="CONDUIT_NATIVE" class="so-panel p-3 space-y-3"><h2>Offline bundle handoff</h2><p>Conduit native operator records. Select an existing signed envelope; no new bundle is built or signed here. Download is not proof of transfer. Report-back is not Agent-attested installation, and records remain process-local.</p><ol><li>1. Select an existing bundle <select aria-label="Offline bundle" data-offline-select><option value="">Loading visible bundles…</option></select></li><li>2. Verify its existing signature <span data-offline-signature>Not selected</span></li><li>3. Copy out the signed envelope <button type="button" class="so-btn" data-offline-download disabled>Download signed envelope</button></li><li>4. Record handoff, then operator report-back</li></ol><form data-offline-handoff><label>Site / environment <select name="site" aria-label="Offline bundle site"><option value="">Choose a visible environment</option>${environments()
      .map((id) => `<option value="${esc(id)}">${esc(id)}</option>`)
      .join(
        "",
      )}</select></label><label><input type="checkbox" name="confirm"> I reviewed the digest and copied this signed envelope to the selected site.</label><button class="so-btn" type="submit" disabled>Record handoff</button></form><form data-offline-report hidden><label>Operator-reported outcome <select name="outcome"><option value="">Choose an outcome</option><option value="installed">Reported installed</option><option value="failed">Reported failed</option></select></label><label><input type="checkbox" name="confirm"> Record this operator report; it is not installation verification or approval.</label><button class="so-btn" type="submit" disabled>Record report-back</button></form><p role="status" data-offline-note>Loading bundle catalog…</p><button type="button" class="so-btn" data-offline-review>Open existing change requests</button></section>`;
    const picker = root.querySelector("[data-offline-select]");
    const download = root.querySelector("[data-offline-download]");
    const signature = root.querySelector("[data-offline-signature]");
    const handoffForm = root.querySelector("[data-offline-handoff]");
    const reportForm = root.querySelector("[data-offline-report]");
    const site = () => handoffForm.elements.namedItem("site").value;
    const confirm = (form) => form.elements.namedItem("confirm");
    const ready = () =>
      current() &&
      sameIdentity() &&
      selected &&
      verifiedSelection(selected, envelope, preview);
    const handoffMatches = () =>
      handoff &&
      handoff.bundleId === selected?.id &&
      handoff.digest === selected?.manifestDigest &&
      handoff.site === site() &&
      handoff.takenBy === actor;
    const fingerprint = (action, outcome = "") =>
      JSON.stringify([
        identity,
        selected?.id,
        selected?.manifestDigest,
        site(),
        action,
        outcome,
      ]);
    const sync = () => {
      handoffForm.querySelector("button").disabled =
        busy ||
        !ready() ||
        !knownSite(site()) ||
        !confirm(handoffForm).checked ||
        Boolean(handoff) ||
        attempted.has(fingerprint("handoff"));
      reportForm.querySelector("button").disabled =
        busy ||
        !ready() ||
        !handoffMatches() ||
        !knownSite(site()) ||
        !confirm(reportForm).checked ||
        !reportForm.elements.namedItem("outcome").value ||
        attempted.has(fingerprint("report"));
      picker.disabled =
        busy ||
        getHub()?.loading !== false ||
        !Array.isArray(getHub()?.environments);
      handoffForm.elements.namedItem("site").disabled =
        busy || Boolean(handoff);
      download.disabled = !ready() || busy;
    };
    root.querySelector("[data-offline-review]").onclick = () => onReview();
    let catalog;
    try {
      const body = await json("/v1/bundles");
      if (!Array.isArray(body.bundles))
        throw new Error("Bundle catalog is unavailable.");
      catalog = body.bundles.filter(
        (item) =>
          item &&
          typeof item.id === "string" &&
          typeof item.manifestDigest === "string",
      );
      if (!current()) return;
      picker.innerHTML = `<option value="">Choose a visible bundle</option>${catalog
        .slice(0, 256)
        .map(
          (item) => `<option value="${esc(item.id)}">${esc(item.id)}</option>`,
        )
        .join("")}`;
      show(
        catalog.length > 256
          ? "Showing the first 256 bundles."
          : "Select a bundle; no transfer or installation is inferred.",
      );
    } catch (error) {
      show(error.message);
      picker.disabled = true;
      return;
    }
    let selectionGeneration = 0;
    picker.onchange = async () => {
      if (!current() || busy) return;
      const generation = ++selectionGeneration;
      selected = catalog.find((item) => item.id === picker.value) || null;
      envelope = null;
      preview = null;
      handoff = null;
      confirm(handoffForm).checked = false;
      confirm(reportForm).checked = false;
      reportForm.hidden = true;
      signature.textContent = "Not verified";
      showPreview(null);
      sync();
      if (!selected) return;
      const item = selected;
      try {
        const loaded = await json(`/v1/bundles/${encodeURIComponent(item.id)}`);
        const checked = await json("/v1/bundles/verification", loaded);
        if (!current() || generation !== selectionGeneration || !sameIdentity())
          return;
        if (!verifiedSelection(item, loaded, checked))
          throw new Error(
            "Signature or bundle coordinates did not verify; handoff is disabled.",
          );
        envelope = loaded;
        preview = checked;
        signature.textContent = `Verified by Hub · ${checked.signature.keyId || "key identity unavailable"} · ${checked.manifestDigest}`;
        showPreview(checked);
        show(
          "Existing signature verified by Hub. Download and physically copy the envelope before confirming handoff.",
        );
      } catch (error) {
        if (current() && generation === selectionGeneration)
          show(error.message);
      }
      sync();
    };
    handoffForm.oninput = (event) => {
      if (event.target !== confirm(handoffForm))
        confirm(handoffForm).checked = false;
      sync();
    };
    reportForm.oninput = (event) => {
      if (event.target !== confirm(reportForm))
        confirm(reportForm).checked = false;
      sync();
    };
    download.onclick = () => {
      if (!ready()) return;
      const href = URL.createObjectURL(
        new Blob([JSON.stringify(envelope)], { type: "application/json" }),
      );
      const link = document.createElement("a");
      link.href = href;
      link.download = `${selected.id.replace(/[^a-zA-Z0-9._-]/g, "_")}.json`;
      link.click();
      URL.revokeObjectURL(href);
      show(
        "Signed envelope download requested. This does not prove it was copied or installed.",
      );
    };
    handoffForm.onsubmit = async (event) => {
      event.preventDefault();
      if (
        busy ||
        !ready() ||
        !knownSite(site()) ||
        !confirm(handoffForm).checked ||
        handoff
      )
        return;
      const key = fingerprint("handoff");
      if (attempted.has(key)) return;
      const targetSite = site(),
        item = selected;
      attempted.add(key);
      busy = true;
      sync();
      try {
        const result = await json("/v1/offline-bundles", {
          action: "handoff",
          bundleId: item.id,
          site: targetSite,
          confirm: true,
        });
        if (!current() || !sameIdentity()) return;
        if (!validHandoff(result, item, targetSite, actor))
          throw new Error("Handoff receipt did not match the confirmed scope.");
        handoff = result.handoff;
        reportForm.hidden = false;
        show(
          `Operator handoff recorded: ${handoff.id}. No transfer or installation verification is inferred.`,
        );
      } catch (error) {
        show(
          `${error.message} The write may have been recorded; inspect existing records before another attempt. Automatic retry is disabled.`,
        );
      } finally {
        busy = false;
        if (current()) sync();
      }
    };
    reportForm.onsubmit = async (event) => {
      event.preventDefault();
      const outcome = reportForm.elements.namedItem("outcome").value;
      if (
        busy ||
        !ready() ||
        !handoffMatches() ||
        !knownSite(site()) ||
        !confirm(reportForm).checked ||
        !["installed", "failed"].includes(outcome)
      )
        return;
      const key = fingerprint("report");
      if (attempted.has(key)) return;
      attempted.add(key);
      busy = true;
      sync();
      try {
        const result = await json("/v1/offline-bundles", {
          action: "report",
          id: handoff.id,
          outcome,
        });
        if (!current() || !sameIdentity()) return;
        if (
          result.handoff?.id !== handoff.id ||
          result.handoff?.digest !== selected.manifestDigest ||
          result.handoff?.site !== site() ||
          result.handoff?.bundleId !== selected.id
        )
          throw new Error("Report receipt did not match the handoff.");
        show(
          `Operator report-back: ${result.handoff.status || "status unavailable"} · ${result.reason || "reason unavailable"}. Review through existing change requests; no approval or execution was granted here.`,
        );
      } catch (error) {
        show(
          `${error.message} The write may have been recorded; automatic retry is disabled.`,
        );
      } finally {
        busy = false;
        if (current()) sync();
      }
    };
    sync();
  };
}

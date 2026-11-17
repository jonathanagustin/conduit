/* Native workspace snapshots and explicitly confirmed owner actions. */
const esc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
async function json(path, method = "GET", body) {
  const response = await fetch(path, {
    method,
    credentials: "same-origin",
    headers: {
      accept: "application/json",
      ...(body === undefined ? {} : { "content-type": "application/json" }),
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    signal: AbortSignal.timeout(15000),
  });
  const result = response.status === 204 ? null : await response.json();
  if (!response.ok)
    throw new Error(
      result?.error || `Workspace request failed (${response.status}).`,
    );
  return result;
}
export function workspaceScope(item, identity) {
  return JSON.stringify([
    identity,
    item?.workspaceId,
    item?.owner,
    item?.environmentId,
    item?.proposalRevision,
    item?.sourceDigest,
    item?.status,
    item?.artifactDigest,
  ]);
}
export function currentWorkspaceDiff(diff, item) {
  return Boolean(
    diff &&
    diff.current === true &&
    diff.workspaceId === item?.workspaceId &&
    diff.proposalRevision === item?.proposalRevision &&
    diff.sourceDigest === item?.sourceDigest,
  );
}
export function safeWorkspaceDownload(path, id) {
  try {
    const url = new URL(
      path,
      globalThis.location?.origin || "http://localhost",
    );
    const origin = globalThis.location?.origin || "http://localhost";
    return url.origin === origin &&
      [
        "/v1/dev-workspaces/" + encodeURIComponent(id) + "/file",
        "/v1/dev-workspaces/" + encodeURIComponent(id) + "/download",
      ].includes(url.pathname)
      ? url.pathname + url.search
      : "";
  } catch {
    return "";
  }
}
export const WORKSPACE_ISOLATION_NOTICE =
  "container isolation now, stronger isolation planned";
export const WORKSPACE_HOST_PREVIEW_NOTICE =
  "A local process preview runs on the host and is not that boundary.";
export function createDevWorkspaces({
  getIdentity,
  getReady = () => true,
  onSelection = () => {},
}) {
  const state = {
    view: "list",
    selected: "",
    items: [],
    tree: [],
    cursor: "",
    treeTruncated: false,
    prefix: "",
    file: null,
    diff: null,
    runtime: null,
    logs: null,
    error: "",
    message: "",
    draft: "",
    busy: false,
    open: {
      tree: false,
      source: false,
      diff: false,
      runtime: false,
      lifecycle: false,
    },
  };
  const attempted = new Set();
  const channels = new Map();
  let host = null,
    mount = 0,
    selection = 0,
    identity = "";
  const key = () => JSON.stringify(getIdentity());
  const selected = () =>
    state.items.find((row) => row.workspaceId === state.selected) || null;
  const clearReads = () => {
    state.tree = [];
    state.cursor = "";
    state.treeTruncated = false;
    state.prefix = "";
    state.file = null;
    state.diff = null;
    state.runtime = null;
    state.logs = null;
    state.draft = "";
  };
  const snapshot = () => ({
    host,
    mount,
    selection,
    id: state.selected,
    identity,
  });
  const readToken = (channel) => {
    const readVersion = (channels.get(channel) || 0) + 1;
    channels.set(channel, readVersion);
    return { ...snapshot(), channel, readVersion };
  };
  const current = (token) =>
    (!token.channel || channels.get(token.channel) === token.readVersion) &&
    token.host === host &&
    token.mount === mount &&
    token.selection === selection &&
    token.id === state.selected &&
    token.identity === identity &&
    identity === key() &&
    host?.isConnected;
  const owner = (item) =>
    Boolean(getIdentity()?.actor) && item?.owner === getIdentity().actor;
  function section(id, title, body) {
    return `<section class="so-panel ws-section"><button type="button" class="so-btn ws-section-toggle" data-ws-action="toggle" data-panel="${id}" aria-expanded="${Boolean(state.open[id])}"><span>${title}</span><span class="ws-section-icon" aria-hidden="true">${state.open[id] ? "−" : "+"}</span></button>${state.open[id] ? `<div class="ws-section-body">${body}</div>` : ""}</section>`;
  }
  function fileMarkup() {
    if (!state.file)
      return '<p class="ws-muted">No selected file response is available.</p>';
    const file = state.file;
    const href = safeWorkspaceDownload(file.downloadPath, state.selected);
    return `<p class="ws-meta code-id">${esc(file.path)} · ${esc(file.digest || "digest unavailable")}${file.redacted ? " · redacted by Hub" : ""}</p>${file.binary || file.tooLarge ? `<p class="ws-muted">Binary or oversized file; inline content is unavailable.</p>${href ? `<div class="ws-btn-row"><a class="so-btn" href="${esc(href)}">Download authorized file view</a></div>` : ""}` : `<pre class="whitespace-pre-wrap code-id ws-code-block">${esc(String(file.content || "").slice(0, 65536))}</pre>${String(file.content || "").length > 65536 ? '<p class="ws-muted">First 65,536 characters shown.</p>' : ""}`}`;
  }
  function diffMarkup(item) {
    if (!currentWorkspaceDiff(state.diff, item))
      return '<p class="ws-muted">A current, matching workspace diff is unavailable.</p>';
    const files = Array.isArray(state.diff.files) ? state.diff.files : [];
    return `<p class="ws-muted">Recorded diff for <span class="code-id">${esc(state.diff.proposalRevision)}</span>. Displaying it does not include it in a Plan.</p>${
      files.length
        ? `<div class="ws-diff-list">${files
            .slice(0, 128)
            .map(
              (file) =>
                `<article class="ws-diff-file"><h4 class="code-id">${esc(file.change)} ${esc(file.path)}</h4><pre class="whitespace-pre-wrap code-id ws-code-block">${esc(
                  (file.hunks || [])
                    .map((hunk) => (hunk.lines || []).join("\n"))
                    .join("\n")
                    .slice(0, 32768),
                )}</pre></article>`,
            )
            .join("")}</div>`
        : '<p class="ws-muted">No changed files were returned for this revision.</p>'
    }${files.length > 128 ? '<p class="ws-muted">First 128 changed files shown.</p>' : ""}`;
  }
  function draftMarkup(item) {
    if (state.busy)
      return '<p class="ws-status" role="status">Confirmed workspace action in progress. No automatic retries.</p>';
    const action = state.draft;
    if (!action) return "";
    const fields =
      action === "upgrade"
        ? '<label class="ws-field"><span>Reviewed source digest</span><input class="kit-input" name="sourceDigest" required></label><label class="ws-field"><span>Template version</span><input class="kit-input" name="templateVersion" required></label><label class="ws-field"><span>Tool versions</span><input class="kit-input" name="toolVersions" required></label>'
        : action === "fork" || action === "submit"
          ? `<label class="ws-field"><span>Title</span><input class="kit-input" name="title" required></label>${action === "submit" ? '<label class="ws-field"><span>Review description</span><textarea class="kit-input ws-textarea" name="description" required></textarea></label>' : ""}`
          : "";
    return `<form data-ws-confirm class="so-panel ws-confirm-form"><h3>Review ${esc(action)} for ${esc(item.workspaceId)}</h3><p class="ws-meta">Owner <span class="code-id">${esc(item.owner)}</span> · revision <span class="code-id">${esc(item.proposalRevision || "unavailable")}</span> · source <span class="code-id">${esc(item.sourceDigest || "unavailable")}</span></p>${action === "upgrade" ? '<p class="ws-muted">This endpoint updates recorded workspace metadata; it does not prove files were upgraded.</p>' : ""}<div class="ws-fields-grid">${fields}</div><label class="ws-checkbox"><input type="checkbox" name="confirm"> <span>Confirm this action for the displayed workspace and revision.</span></label><div class="ws-btn-row"><button type="submit" class="so-btn so-btn-blue" disabled>Confirm workspace action</button><button type="button" class="so-btn" data-ws-action="cancel">Cancel</button></div></form>`;
  }
  function paint() {
    if (!host?.isConnected || identity !== key()) return;
    const item = selected();
    onSelection(item);
    const canWrite = owner(item) && !state.busy;
    host.innerHTML = `<div data-workspace-studio data-value-tier="CONDUIT_NATIVE" class="ws-studio"><section class="so-panel ws-studio-card"><div class="ws-studio-head"><div><h2 class="ws-studio-title">Developer workspaces</h2><p class="ws-studio-desc">Hub records and authorized file views. ${esc(WORKSPACE_ISOLATION_NOTICE)}. Governed workspace commands run as a non-root container with default-deny egress and no cluster credentials. ${esc(WORKSPACE_HOST_PREVIEW_NOTICE)} Source is read only here; a branch-backed editor and coding-agent bridge are separate features. No commit history is inferred.</p></div><div class="ws-btn-row ws-studio-toolbar"><button type="button" class="so-btn" data-ws-action="refresh"${state.busy || !getReady() ? " disabled" : ""}>Refresh workspaces</button><button type="button" class="so-btn so-btn-blue" data-engineer-studio-entry data-onclick="toggleEngineerWorkspace('studio')">Open engineer workspace</button></div></div>${state.error ? `<p class="ws-alert" role="alert">${esc(state.error)}</p>` : ""}${state.message ? `<p class="ws-status" role="status">${esc(state.message)}</p>` : ""}${
      state.items.length
        ? `<ul class="ws-studio-list">${state.items
            .slice(0, 256)
            .map(
              (row) =>
                `<li><button type="button" class="so-btn ws-studio-item${row.workspaceId === state.selected ? " is-selected" : ""}" data-ws-action="open" data-id="${esc(row.workspaceId)}"${state.busy || !getReady() ? " disabled" : ""}>${esc(row.title || row.workspaceId)} · ${esc(row.status || "unknown")}</button></li>`,
            )
            .join(
              "",
            )}</ul>${state.items.length > 256 ? '<p class="ws-muted">First 256 visible workspaces shown.</p>' : ""}`
        : '<p class="ws-muted ws-empty">No visible workspace snapshot is available.</p>'
    }</section>${
      item
        ? `<section class="so-panel ws-studio-detail" data-workspace-detail><div class="ws-detail-head"><h3 class="ws-detail-title">${esc(item.title || item.workspaceId)}</h3><span class="kit-badge kit-badge-info code-id">${esc(item.status || "unknown")}</span></div><p class="ws-meta code-id">${esc(item.workspaceId)} · owner ${esc(item.owner || "unavailable")} · ${esc(item.status || "unknown")}</p><p class="ws-meta code-id">Revision ${esc(item.proposalRevision || "unavailable")} · source ${esc(item.sourceDigest || "unavailable")}</p></section>${section("tree", "Repository tree", `<div class="ws-tree-bar"><span class="ws-meta code-id">Directory ${esc(state.prefix || "root")}</span>${state.prefix ? '<button type="button" class="so-btn so-btn-sm" data-ws-action="root">Repository root</button>' : ""}</div>${state.tree.length ? `<ul class="ws-tree-list">${state.tree.map((entry) => `<li><button type="button" class="so-btn ws-tree-item code-id" data-ws-action="${["directory", "dir"].includes(entry.kind) ? "directory" : "file"}" data-path="${esc(entry.path)}" data-digest="${esc(entry.digest || "")}">${esc(entry.path)} · ${esc(entry.kind)}</button></li>`).join("")}</ul>` : '<p class="ws-muted">No directory entries are displayed.</p>'}${state.treeTruncated ? '<p class="ws-muted">First 256 directory entries shown; this is not a complete repository inventory.</p>' : ""}${state.cursor ? '<div class="ws-btn-row"><button type="button" class="so-btn" data-ws-action="more">Load next directory page</button></div>' : ""}`)}${section("source", "Source", fileMarkup())}${section("diff", "Workspace diff", diffMarkup(item))}${section("runtime", "Preview and logs", `<p class="ws-muted">Returned runtime status: <span class="code-id">${esc(state.runtime?.status || "unavailable")}</span> · origin: <span class="code-id">${esc(state.runtime?.origin || "unavailable")}</span>. A local preview is not a deployment or lease. Returned revision <span class="code-id">${esc(state.runtime?.proposalRevision || "unavailable")}</span> · artifact <span class="code-id">${esc(state.runtime?.artifactDigest || "unavailable")}</span>.</p><div class="ws-btn-row"><button type="button" class="so-btn" data-ws-action="start"${canWrite ? "" : " disabled"}>Stage preview start</button><button type="button" class="so-btn" data-ws-action="stop"${canWrite ? "" : " disabled"}>Stage preview stop</button><button type="button" class="so-btn" data-ws-action="logs">Refresh logs</button><button type="button" class="so-btn" data-ws-action="clear-logs"${canWrite ? "" : " disabled"}>Stage log clear</button></div><p class="ws-meta code-id">Log origin ${esc(state.logs?.origin || "unavailable")} · status ${esc(state.logs?.status || "unavailable")}</p><pre class="whitespace-pre-wrap code-id ws-code-block">${esc((state.logs?.lines || []).slice(0, 256).join("\n").slice(0, 65536))}</pre><p class="ws-muted">Bounded snapshot: up to 256 returned lines and 65,536 characters; not a continuous stream.</p>`)}${section(
            "lifecycle",
            "Review and lifecycle",
            `<p class="ws-muted">Signature status <span class="code-id">${esc(item.signatureStatus || "unavailable")}</span>; a download is not signature verification.</p><div class="ws-btn-row">${[
              ["fork", "Stage fork"],
              ["upgrade", "Stage metadata upgrade"],
              ["submit", "Stage separate review"],
            ]
              .map(
                ([action, label]) =>
                  `<button type="button" class="so-btn" data-ws-action="${action}"${canWrite ? "" : " disabled"}>${label}</button>`,
              )
              .join(
                "",
              )}<a class="so-btn" href="/v1/dev-workspaces/${encodeURIComponent(item.workspaceId)}/download">Download authorized snapshot</a></div><p class="ws-muted">Deletion is unavailable here until a governed deletion contract exists. The direct delete endpoint is not invoked.</p>`,
          )}${draftMarkup(item)}`
        : ""
    }</div>`;
    const form = host.querySelector("[data-ws-confirm]");
    if (form) {
      form.oninput = (event) => {
        if (event.target !== form.elements.namedItem("confirm"))
          form.elements.namedItem("confirm").checked = false;
        form.querySelector('[type="submit"]').disabled =
          state.busy ||
          !owner(selected()) ||
          !form.elements.namedItem("confirm").checked;
      };
      form.onsubmit = (event) => {
        event.preventDefault();
        void confirmAction(form);
      };
    }
  }
  async function refresh() {
    const token = readToken("index");
    try {
      const body = await json("/v1/dev-workspaces");
      if (!current(token)) return;
      if (!Array.isArray(body.items))
        throw new Error("Workspace index is unavailable.");
      const before = workspaceScope(selected(), identity);
      state.items = body.items.filter(
        (row) => row && typeof row.workspaceId === "string",
      );
      if (before !== workspaceScope(selected(), identity)) {
        selection++;
        clearReads();
      }
      state.error = "";
    } catch (error) {
      if (!current(token)) return;
      state.items = [];
      clearReads();
      state.error = error.message;
    }
    paint();
  }
  async function tree(prefix = "", cursor = "", append = false) {
    const token = readToken("tree");
    const page = await json(
      `/v1/dev-workspaces/${encodeURIComponent(token.id)}/tree?limit=50&prefix=${encodeURIComponent(prefix)}&cursor=${encodeURIComponent(cursor)}`,
    );
    if (!current(token)) return;
    if (!Array.isArray(page.entries))
      throw new Error("Directory page is unavailable.");
    const entries = append ? [...state.tree, ...page.entries] : page.entries;
    if (entries.some((entry) => !entry || typeof entry.path !== "string"))
      throw new Error("Directory entries are unavailable.");
    state.treeTruncated =
      entries.length > 256 ||
      (entries.length >= 256 && Boolean(page.nextCursor));
    state.tree = entries.slice(0, 256);
    state.prefix = prefix;
    state.cursor =
      state.tree.length < 256 && typeof page.nextCursor === "string"
        ? page.nextCursor
        : "";
  }
  async function panel(name) {
    const token = readToken(name);
    const item = selected();
    if (!item) return;
    if (name === "tree") await tree();
    if (name === "diff") {
      const diff = await json(
        `/v1/dev-workspaces/${encodeURIComponent(token.id)}/diff?revision=${encodeURIComponent(item.proposalRevision || "")}`,
      );
      if (!current(token)) return;
      if (!currentWorkspaceDiff(diff, item))
        throw new Error("Diff did not match the selected workspace revision.");
      state.diff = diff;
    }
    if (name === "runtime") {
      const runtime = await json(
        `/v1/dev-workspaces/${encodeURIComponent(token.id)}/runtime`,
      );
      if (!current(token)) return;
      if (runtime.workspaceId !== token.id)
        throw new Error("Runtime response did not match the workspace.");
      state.runtime = runtime;
      await logs();
    }
  }
  async function logs() {
    const token = readToken("logs");
    const page = await json(
      `/v1/dev-workspaces/${encodeURIComponent(token.id)}/logs?limit=100`,
    );
    if (!current(token)) return;
    if (!Array.isArray(page.lines))
      throw new Error("Log snapshot is unavailable.");
    state.logs = page;
  }
  async function click(event) {
    const button = event.target?.closest?.("[data-ws-action]");
    if (!button || button.disabled || state.busy || !getReady()) return;
    const action = button.dataset.wsAction;
    let token = snapshot();
    try {
      if (action === "refresh") {
        await refresh();
        return;
      }
      if (action === "open") {
        if (!state.items.some((row) => row.workspaceId === button.dataset.id))
          return;
        state.selected = button.dataset.id;
        selection++;
        token = snapshot();
        clearReads();
        state.open.tree = true;
        await tree();
      } else if (action === "toggle") {
        const name = button.dataset.panel;
        if (!Object.hasOwn(state.open, name)) return;
        state.open[name] = !state.open[name];
        if (state.open[name]) await panel(name);
      } else if (
        action === "directory" ||
        action === "root" ||
        action === "more"
      )
        await tree(
          action === "root"
            ? ""
            : action === "directory"
              ? button.dataset.path
              : state.prefix,
          action === "more" ? state.cursor : "",
          action === "more",
        );
      else if (action === "file") {
        state.file = null;
        state.open.source = true;
        paint();
        const read = readToken("file"),
          path = button.dataset.path,
          digest = button.dataset.digest;
        const file = await json(
          `/v1/dev-workspaces/${encodeURIComponent(read.id)}/file?path=${encodeURIComponent(path)}&revision=${encodeURIComponent(digest)}`,
        );
        if (!current(read)) return;
        if (file.path !== path || (digest && file.digest !== digest))
          throw new Error(
            "File response did not match the selected path and digest.",
          );
        state.file = file;
      } else if (action === "logs") await logs();
      else if (action === "cancel") state.draft = "";
      else if (
        ["start", "stop", "clear-logs", "fork", "upgrade", "submit"].includes(
          action,
        )
      ) {
        if (!owner(selected())) return;
        state.draft = action;
        state.error = "";
        state.message = "";
      }
    } catch (error) {
      if (token.host === host && token.mount === mount && identity === key())
        state.error = error.message;
    }
    paint();
  }
  async function confirmAction(form) {
    const item = selected(),
      action = state.draft,
      token = snapshot();
    if (
      state.busy ||
      !item ||
      !owner(item) ||
      !form.elements.namedItem("confirm")?.checked ||
      !action ||
      !current(token)
    )
      return;
    const value = (name) =>
      String(form.elements.namedItem(name)?.value || "").trim();
    let body = {};
    let suffix =
      action === "start"
        ? "/runtime"
        : action === "stop"
          ? "/runtime/stop"
          : action === "clear-logs"
            ? "/logs/clear"
            : "/" + action;
    if (action === "upgrade") {
      body = {
        sourceDigest: value("sourceDigest"),
        templateVersion: value("templateVersion"),
        toolVersions: value("toolVersions"),
      };
      if (Object.values(body).some((field) => !field)) {
        state.error = "Reviewed upgrade coordinates are required.";
        paint();
        return;
      }
    }
    if (action === "fork" || action === "submit") {
      body = {
        title: value("title"),
        ...(action === "submit"
          ? { description: value("description"), approve: false }
          : {}),
      };
      if (!body.title || (action === "submit" && !body.description)) {
        state.error = "A reviewed title and description are required.";
        paint();
        return;
      }
    }
    const scope = workspaceScope(item, identity),
      attempt = JSON.stringify([scope, action, body]);
    if (attempted.has(attempt)) {
      state.error =
        "This exact write was already attempted. Inspect existing records; automatic retry is disabled.";
      paint();
      return;
    }
    state.busy = true;
    paint();
    let sent = false,
      accepted = false;
    try {
      const fresh = await json(
        `/v1/dev-workspaces/${encodeURIComponent(item.workspaceId)}`,
      );
      if (
        !current(token) ||
        scope !== workspaceScope(fresh, identity) ||
        !owner(fresh)
      )
        throw new Error(
          "Workspace context changed before confirmation; inspect the current record again.",
        );
      attempted.add(attempt);
      sent = true;
      const receipt = await json(
        `/v1/dev-workspaces/${encodeURIComponent(item.workspaceId)}${suffix}`,
        "POST",
        body,
      );
      if (!current(token)) return;
      if (action === "submit") {
        if (
          !receipt?.changeRequestId ||
          !["PENDING_APPROVALS", "REJECTED"].includes(receipt.status)
        )
          throw new Error(
            "Submission receipt did not establish a separate pending or rejected review.",
          );
        state.message = `Review request ${receipt.changeRequestId} · ${receipt.status}. No approval or Plan execution was granted.`;
      } else if (action === "fork") {
        if (
          !receipt?.workspaceId ||
          receipt.workspaceId === item.workspaceId ||
          receipt.parentId !== item.workspaceId ||
          receipt.owner !== getIdentity()?.actor ||
          receipt.environmentId !== item.environmentId ||
          receipt.sourceDigest !== item.sourceDigest
        )
          throw new Error(
            "Fork receipt did not match the confirmed parent and owner.",
          );
        state.message = `Fork recorded: ${receipt.workspaceId}.`;
      } else if (action === "upgrade") {
        if (
          receipt?.workspaceId !== item.workspaceId ||
          receipt.owner !== item.owner ||
          receipt.environmentId !== item.environmentId ||
          !receipt.proposalRevision ||
          receipt.sourceDigest !== body.sourceDigest ||
          receipt.templateVersion !== body.templateVersion ||
          receipt.toolVersions !== body.toolVersions
        )
          throw new Error(
            "Upgrade metadata receipt did not match the reviewed coordinates.",
          );
        state.message =
          "Workspace metadata updated; file upgrades and deployment are not inferred.";
        state.items = state.items.map((row) =>
          row.workspaceId === item.workspaceId ? receipt : row,
        );
        selection++;
        clearReads();
      } else if (action === "start" || action === "stop") {
        if (receipt?.workspaceId !== item.workspaceId)
          throw new Error(
            "Runtime receipt did not match the selected workspace.",
          );
        state.runtime = receipt;
        const updated = await json(
          `/v1/dev-workspaces/${encodeURIComponent(item.workspaceId)}`,
        );
        if (!current(token)) return;
        if (
          updated?.workspaceId !== item.workspaceId ||
          updated.owner !== item.owner
        )
          throw new Error(
            "Workspace read after runtime action did not match the owner and target.",
          );
        state.items = state.items.map((row) =>
          row.workspaceId === item.workspaceId ? updated : row,
        );
        selection++;
        if (
          updated.proposalRevision !== item.proposalRevision ||
          updated.sourceDigest !== item.sourceDigest
        )
          clearReads();
        state.message = `Returned preview status ${receipt.status || "unknown"} · origin ${receipt.origin || "unavailable"}.`;
      } else
        state.message =
          "Log clear request acknowledged; other audit records are unchanged.";
      accepted = true;
      state.draft = "";
    } catch (error) {
      if (current(token))
        state.error =
          error.message +
          (sent
            ? " The write may have been recorded. Inspect records before another attempt; automatic retry is disabled."
            : "");
    } finally {
      state.busy = false;
      if (host?.isConnected && identity === key()) {
        if (!current(token) && !accepted)
          state.message =
            "An earlier workspace action finished after the view changed. Inspect current records; nothing is retried automatically.";
        paint();
      }
    }
  }
  return function mountDevWorkspaces(root, options = {}) {
    const nextIdentity = key();
    if (nextIdentity !== identity) {
      state.items = [];
      state.selected = "";
      clearReads();
      identity = nextIdentity;
    }
    mount++;
    selection++;
    state.error = "";
    state.message = "";
    state.draft = "";
    state.view = options.view || "list";
    if (Object.hasOwn(state.open, state.view)) state.open[state.view] = true;
    root.innerHTML = "<div data-workspace-root></div>";
    host = root.querySelector("[data-workspace-root]");
    onSelection(null);
    host.onclick = (event) => void click(event);
    void refresh();
  };
}

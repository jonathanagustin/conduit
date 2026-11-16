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
  "Workspace isolation must be configured; local previews run on the host.";
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
    return `<section class="so-panel p-3"><button type="button" class="so-btn" data-ws-action="toggle" data-panel="${id}" aria-expanded="${Boolean(state.open[id])}">${title}</button>${state.open[id] ? `<div class="mt-2">${body}</div>` : ""}</section>`;
  }
  function fileMarkup() {
    if (!state.file) return "<p>No selected file response is available.</p>";
    const file = state.file;
    const href = safeWorkspaceDownload(file.downloadPath, state.selected);
    return `<p>${esc(file.path)} · ${esc(file.digest || "digest unavailable")}${file.redacted ? " · redacted by Hub" : ""}</p>${file.binary || file.tooLarge ? `<p>Binary or oversized file; inline content is unavailable.</p>${href ? `<a href="${esc(href)}">Download authorized file view</a>` : ""}` : `<pre class="whitespace-pre-wrap code-id">${esc(String(file.content || "").slice(0, 65536))}</pre>${String(file.content || "").length > 65536 ? "<p>First 65,536 characters shown.</p>" : ""}`}`;
  }
  function diffMarkup(item) {
    if (!currentWorkspaceDiff(state.diff, item))
      return "<p>A current, matching workspace diff is unavailable.</p>";
    const files = Array.isArray(state.diff.files) ? state.diff.files : [];
    return `<p>Recorded diff for ${esc(state.diff.proposalRevision)}. Displaying it does not include it in a Plan.</p>${
      files.length
        ? files
            .slice(0, 128)
            .map(
              (file) =>
                `<article><h4>${esc(file.change)} ${esc(file.path)}</h4><pre class="whitespace-pre-wrap code-id">${esc(
                  (file.hunks || [])
                    .map((hunk) => (hunk.lines || []).join("\n"))
                    .join("\n")
                    .slice(0, 32768),
                )}</pre></article>`,
            )
            .join("")
        : "<p>No changed files were returned for this revision.</p>"
    }${files.length > 128 ? "<p>First 128 changed files shown.</p>" : ""}`;
  }
  function draftMarkup(item) {
    if (state.busy)
      return '<p role="status">Confirmed workspace action in progress. No automatic retries.</p>';
    const action = state.draft;
    if (!action) return "";
    const fields =
      action === "upgrade"
        ? '<label>Reviewed source digest <input name="sourceDigest" required></label><label>Template version <input name="templateVersion" required></label><label>Tool versions <input name="toolVersions" required></label>'
        : action === "fork" || action === "submit"
          ? `<label>Title <input name="title" required></label>${action === "submit" ? '<label>Review description <textarea name="description" required></textarea></label>' : ""}`
          : "";
    return `<form data-ws-confirm class="so-panel p-3 space-y-2"><h3>Review ${esc(action)} for ${esc(item.workspaceId)}</h3><p>Owner ${esc(item.owner)} · revision ${esc(item.proposalRevision || "unavailable")} · source ${esc(item.sourceDigest || "unavailable")}</p>${action === "upgrade" ? "<p>This endpoint updates recorded workspace metadata; it does not prove files were upgraded.</p>" : ""}${fields}<label><input type="checkbox" name="confirm"> Confirm this action for the displayed workspace and revision.</label><button type="submit" class="so-btn" disabled>Confirm workspace action</button><button type="button" class="so-btn" data-ws-action="cancel">Cancel</button></form>`;
  }
  function paint() {
    if (!host?.isConnected || identity !== key()) return;
    const item = selected();
    onSelection(item);
    const canWrite = owner(item) && !state.busy;
    host.innerHTML = `<div data-workspace-studio data-value-tier="CONDUIT_NATIVE" class="space-y-3"><section class="so-panel p-3"><h2>Developer workspaces</h2><button type="button" class="so-btn" data-engineer-studio-entry data-onclick="toggleEngineerWorkspace('studio')">Open engineer workspace</button><p>Hub records and authorized file views (${esc(WORKSPACE_ISOLATION_NOTICE)}; rootless builder, default-deny egress, zero cluster credentials). Source is read only here; a branch-backed editor and coding-agent bridge are separate features. No commit history is inferred.</p><button type="button" class="so-btn" data-ws-action="refresh"${state.busy || !getReady() ? " disabled" : ""}>Refresh workspaces</button>${state.error ? `<p role="alert">${esc(state.error)}</p>` : ""}${state.message ? `<p role="status">${esc(state.message)}</p>` : ""}${
      state.items.length
        ? `<ul>${state.items
            .slice(0, 256)
            .map(
              (row) =>
                `<li><button type="button" class="so-btn" data-ws-action="open" data-id="${esc(row.workspaceId)}"${state.busy || !getReady() ? " disabled" : ""}>${esc(row.title || row.workspaceId)} · ${esc(row.status || "unknown")}</button></li>`,
            )
            .join(
              "",
            )}</ul>${state.items.length > 256 ? "<p>First 256 visible workspaces shown.</p>" : ""}`
        : "<p>No visible workspace snapshot is available.</p>"
    }</section>${
      item
        ? `<section class="so-panel p-3" data-workspace-detail><h3>${esc(item.title || item.workspaceId)}</h3><p>${esc(item.workspaceId)} · owner ${esc(item.owner || "unavailable")} · ${esc(item.status || "unknown")}</p><p>Revision ${esc(item.proposalRevision || "unavailable")} · source ${esc(item.sourceDigest || "unavailable")}</p></section>${section("tree", "Repository tree", `<p>Directory ${esc(state.prefix || "root")}</p>${state.prefix ? '<button type="button" class="so-btn" data-ws-action="root">Repository root</button>' : ""}${state.tree.length ? `<ul>${state.tree.map((entry) => `<li><button type="button" class="so-btn" data-ws-action="${["directory", "dir"].includes(entry.kind) ? "directory" : "file"}" data-path="${esc(entry.path)}" data-digest="${esc(entry.digest || "")}">${esc(entry.path)} · ${esc(entry.kind)}</button></li>`).join("")}</ul>` : "<p>No directory entries are displayed.</p>"}${state.treeTruncated ? "<p>First 256 directory entries shown; this is not a complete repository inventory.</p>" : ""}${state.cursor ? '<button type="button" class="so-btn" data-ws-action="more">Load next directory page</button>' : ""}`)}${section("source", "Source", fileMarkup())}${section("diff", "Workspace diff", diffMarkup(item))}${section("runtime", "Preview and logs", `<p>Returned runtime status: ${esc(state.runtime?.status || "unavailable")} · origin: ${esc(state.runtime?.origin || "unavailable")}. A local preview is not a deployment or lease. Returned revision ${esc(state.runtime?.proposalRevision || "unavailable")} · artifact ${esc(state.runtime?.artifactDigest || "unavailable")}.</p><button type="button" class="so-btn" data-ws-action="start"${canWrite ? "" : " disabled"}>Stage preview start</button><button type="button" class="so-btn" data-ws-action="stop"${canWrite ? "" : " disabled"}>Stage preview stop</button><button type="button" class="so-btn" data-ws-action="logs">Refresh logs</button><button type="button" class="so-btn" data-ws-action="clear-logs"${canWrite ? "" : " disabled"}>Stage log clear</button><p>Log origin ${esc(state.logs?.origin || "unavailable")} · status ${esc(state.logs?.status || "unavailable")}</p><pre class="whitespace-pre-wrap code-id">${esc((state.logs?.lines || []).slice(0, 256).join("\n").slice(0, 65536))}</pre><p>Bounded snapshot: up to 256 returned lines and 65,536 characters; not a continuous stream.</p>`)}${section(
            "lifecycle",
            "Review and lifecycle",
            `<p>Signature status ${esc(item.signatureStatus || "unavailable")}; a download is not signature verification.</p>${[
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
              )}<a class="so-btn" href="/v1/dev-workspaces/${encodeURIComponent(item.workspaceId)}/download">Download authorized snapshot</a><p>Deletion is unavailable here until a governed deletion contract exists. The direct delete endpoint is not invoked.</p>`,
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

/* Developer workspace panel. Self-contained so a navigation restyle can mount it. */

function esc(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

async function readJSON(path) {
  const response = await fetch(path, { headers: { accept: "application/json" } });
  if (!response.ok) {
    const body = await response.text();
    throw new Error(body || `${response.status}`);
  }
  return response.json();
}

async function sendJSON(method, path, body) {
  const response = await fetch(path, {
    method,
    headers: { accept: "application/json", "content-type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `${response.status}`);
  }
  if (response.status === 204) return null;
  return response.json();
}

const state = {
  view: "list",
  selected: "",
  open: { tree: false, source: false, diff: false, runtime: false, lifecycle: false },
  paused: false,
  error: "",
  items: [],
  tree: [],
  file: null,
  diff: null,
  runtime: null,
  logs: [],
};

export function mountDevWorkspaces(root, options = {}) {
  state.view = options.view || state.view;
  if (options.view === "source") state.open.source = true;
  if (options.view === "diff") state.open.diff = true;
  if (options.view === "runtime") state.open.runtime = true;
  if (options.view === "lifecycle") state.open.lifecycle = true;
  root.innerHTML = `<div id="dev-workspace-root" class="space-y-3"></div>`;
  const host = root.querySelector("#dev-workspace-root");
  host.addEventListener("click", (event) => onClick(event, host));
  void refresh(host);
}

async function refresh(host) {
  try {
    const body = await readJSON("/v1/dev-workspaces");
    state.items = Array.isArray(body.items) ? body.items : [];
    state.error = "";
  } catch (error) {
    state.items = [];
    state.error = error instanceof Error ? error.message : "unavailable";
  }
  paint(host);
}

function paint(host) {
  const selected = state.items.find((item) => item.workspaceId === state.selected) || null;
  host.innerHTML = `
    <section class="so-panel p-3 space-y-2" data-region="workspace-list">
      <div class="flex items-center justify-between gap-2">
        <h2 class="text-[14px] font-semibold text-[#f0f5fa]">Workspaces</h2>
        <span class="text-[10px] uppercase tracking-[0.08em] text-[#7c8794]">${esc(state.items[0]?.valueTier || "CONDUIT_NATIVE")}</span>
      </div>
      ${state.error ? `<p class="text-[12px] text-[#e76a6e]" role="alert">${esc(state.error)}</p>` : ""}
      ${
        state.items.length
          ? `<ul class="space-y-1">${state.items
              .map(
                (item) => `<li><button type="button" data-action="open" data-id="${esc(item.workspaceId)}" class="so-nav-row w-full ${
                  item.workspaceId === state.selected ? "is-active" : ""
                }"><span class="truncate">${esc(item.title || item.workspaceId)}</span><span class="text-[10px] text-[#8b96a5]">${esc(item.status)}</span></button></li>`,
              )
              .join("")}</ul>`
          : `<p class="text-[12px] text-[#8b96a5]">No workspaces yet.</p>`
      }
    </section>
    ${selected ? detail(selected) : ""}
  `;
}

function detail(item) {
  return `
    <section class="so-panel p-3 space-y-3" data-region="workspace-detail">
      <header class="space-y-1">
        <h2 class="text-[15px] font-semibold text-[#ffffff]">${esc(item.title || item.workspaceId)}</h2>
        <p class="text-[12px] text-[#8b96a5]">${esc(item.workspaceId)} · ${esc(item.status)} · ${esc(item.owner || "")}</p>
        <p class="text-[11px] text-[#6d7885]">Revision ${esc(item.proposalRevision || "unknown")} · source ${esc(item.sourceDigest || "unknown")}</p>
      </header>
      ${section("tree", "Repository", treeBody())}
      ${section("source", "Source", sourceBody())}
      ${section("diff", "Diff", diffBody())}
      ${section("runtime", "Local runtime", runtimeBody(item))}
      ${section("lifecycle", "Fork and download", lifecycleBody(item))}
    </section>
  `;
}

function section(id, title, body) {
  const open = state.open[id];
  return `
    <div class="border border-[#1e252b] rounded-[6px]">
      <button type="button" data-action="toggle" data-panel="${id}" class="w-full text-left px-3 py-2 text-[12.5px] font-medium text-[#dce4ee]" aria-expanded="${open}">
        ${esc(title)}
      </button>
      ${open ? `<div class="px-3 pb-3 text-[12px] text-[#c5d0dc]">${body}</div>` : ""}
    </div>
  `;
}

function treeBody() {
  if (!state.tree.length) return `<p class="text-[#8b96a5]">Empty repository.</p>`;
  return `<ul class="space-y-1">${state.tree
    .map(
      (entry) =>
        `<li><button type="button" data-action="file" data-path="${esc(entry.path)}" data-revision="${esc(entry.digest || "")}" class="so-nav-row w-full"><span class="truncate">${esc(entry.path)}</span><span class="text-[10px] text-[#6d7885]">${esc(entry.kind)}</span></button></li>`,
    )
    .join("")}</ul>`;
}

function sourceBody() {
  if (!state.file) return `<p class="text-[#8b96a5]">Select a file.</p>`;
  if (state.file.binary || state.file.tooLarge) {
    return `<p>This file is not shown inline. <a href="${esc(state.file.downloadPath || "#")}">Download</a></p>`;
  }
  return `<p class="text-[10px] text-[#6d7885] mb-1">${esc(state.file.path)} · ${esc(state.file.digest)}</p><pre class="whitespace-pre-wrap code-id text-[11px]">${esc(state.file.content || "")}</pre>`;
}

function diffBody() {
  if (!state.diff) return `<p class="text-[#8b96a5]">No diff loaded.</p>`;
  const files = Array.isArray(state.diff.files) ? state.diff.files : [];
  if (!files.length) return `<p>Empty diff. Revision ${esc(state.diff.proposalRevision || "")}.</p>`;
  return files
    .map(
      (file) =>
        `<article class="mb-2"><h3 class="font-medium">${esc(file.change)} ${esc(file.path)}</h3><pre class="whitespace-pre-wrap code-id text-[11px]">${esc(
          (file.hunks || []).map((hunk) => (hunk.lines || []).join("\n")).join("\n"),
        )}</pre></article>`,
    )
    .join("");
}

function runtimeBody(item) {
  const runtime = state.runtime;
  return `
    <p>Status ${esc(runtime?.status || item.status)} · origin ${esc(runtime?.origin || "local-runtime")}</p>
    <div class="flex gap-2 mt-2">
      <button type="button" data-action="start" class="so-btn so-btn-sm">Start</button>
      <button type="button" data-action="stop" class="so-btn so-btn-sm">Stop</button>
      <button type="button" data-action="pause" class="so-btn so-btn-sm">${state.paused ? "Resume" : "Pause"}</button>
      <button type="button" data-action="clear-logs" class="so-btn so-btn-sm">Clear</button>
    </div>
    <pre class="mt-2 whitespace-pre-wrap code-id text-[11px]">${esc(state.logs.join("\n"))}</pre>
  `;
}

function lifecycleBody(item) {
  return `
    <p>Signature ${esc(item.signatureStatus || "unsigned")}. A download does not treat that status as valid.</p>
    <div class="flex flex-wrap gap-2 mt-2">
      <button type="button" data-action="fork" class="so-btn so-btn-sm">Fork</button>
      <button type="button" data-action="upgrade" class="so-btn so-btn-sm">Upgrade</button>
      <button type="button" data-action="submit" class="so-btn so-btn-sm">Submit change request</button>
      <a class="so-btn so-btn-sm" href="/v1/dev-workspaces/${esc(item.workspaceId)}/download">Download</a>
    </div>
  `;
}

async function onClick(event, host) {
  const target = event.target.closest("[data-action]");
  if (!target) return;
  const action = target.getAttribute("data-action");
  try {
    if (action === "open") {
      state.selected = target.getAttribute("data-id") || "";
      state.open.tree = true;
      await loadTree();
      paint(host);
      return;
    }
    if (action === "toggle") {
      const panel = target.getAttribute("data-panel");
      state.open[panel] = !state.open[panel];
      if (state.open[panel]) await loadPanel(panel);
      paint(host);
      return;
    }
    if (action === "file") {
      state.open.source = true;
      state.file = await readJSON(
        `/v1/dev-workspaces/${encodeURIComponent(state.selected)}/file?path=${encodeURIComponent(target.getAttribute("data-path") || "")}&revision=${encodeURIComponent(target.getAttribute("data-revision") || "")}`,
      );
      paint(host);
      return;
    }
    if (action === "start") {
      state.runtime = await sendJSON("POST", `/v1/dev-workspaces/${encodeURIComponent(state.selected)}/runtime`, {});
      await loadLogs();
    }
    if (action === "stop") {
      state.runtime = await sendJSON("POST", `/v1/dev-workspaces/${encodeURIComponent(state.selected)}/stop`, {});
      state.paused = true;
    }
    if (action === "pause") state.paused = !state.paused;
    if (action === "clear-logs") {
      await sendJSON("POST", `/v1/dev-workspaces/${encodeURIComponent(state.selected)}/logs/clear`, {});
      state.logs = [];
    }
    if (action === "fork") {
      await sendJSON("POST", `/v1/dev-workspaces/${encodeURIComponent(state.selected)}/fork`, { title: "Fork" });
      await refresh(host);
      return;
    }
    if (action === "upgrade") {
      await sendJSON("POST", `/v1/dev-workspaces/${encodeURIComponent(state.selected)}/upgrade`, {
        sourceDigest: "sha256:" + "ab".repeat(32),
        templateVersion: "next",
        toolVersions: "next",
      });
    }
    if (action === "submit") {
      await sendJSON("POST", `/v1/dev-workspaces/${encodeURIComponent(state.selected)}/submit`, {
        title: "Workspace review",
        description: "Submit for a separate reviewer",
        approve: false,
      });
    }
    state.error = "";
  } catch (error) {
    state.error = error instanceof Error ? error.message : "request failed";
  }
  await refresh(host);
}

async function loadPanel(panel) {
  if (!state.selected) return;
  if (panel === "tree") await loadTree();
  if (panel === "diff") {
    state.diff = await readJSON(`/v1/dev-workspaces/${encodeURIComponent(state.selected)}/diff`);
  }
  if (panel === "runtime") {
    state.runtime = await readJSON(`/v1/dev-workspaces/${encodeURIComponent(state.selected)}/runtime`);
    if (!state.paused) await loadLogs();
  }
}

async function loadTree() {
  const page = await readJSON(`/v1/dev-workspaces/${encodeURIComponent(state.selected)}/tree?limit=50`);
  state.tree = Array.isArray(page.entries) ? page.entries : [];
}

async function loadLogs() {
  const page = await readJSON(`/v1/dev-workspaces/${encodeURIComponent(state.selected)}/logs`);
  state.logs = Array.isArray(page.lines) ? page.lines : [];
}

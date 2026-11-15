import { selectedEnvironment } from "./release-environment-hub.js";
import { selectedPackagingRun } from "./packaging-pipeline.js";
import { runState } from "./packaging-runs.js";
/* Shared v3 surfaces. They render recorded Hub rows or an empty state. */

function esc(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

function statusTone(status) {
  const text = String(status || "").toLowerCase();
  if (/fail|block|deny|error|reject/.test(text)) return "err";
  if (/warn|pending|review|drift/.test(text)) return "warn";
  if (/pass|ok|allow|review|recorded|healthy|converged|applied/.test(text))
    return "ok";
  if (!text) return "muted";
  return "info";
}

export function statusBadge(status) {
  const label = status || "unknown";
  return `<span class="kit-badge kit-badge-${statusTone(label)}" data-status="${esc(label)}">${esc(label)}</span>`;
}

export function riskBadge(risk) {
  const tone =
    risk === "Critical" || risk === "High"
      ? "err"
      : risk === "Medium"
        ? "warn"
        : "ok";
  return `<span class="kit-badge kit-badge-${tone}" data-risk="${esc(risk)}">RISK: ${esc(String(risk || "").toUpperCase())}</span>`;
}

export function connectivityBadge(state) {
  const tone =
    state === "Disconnected" ? "err" : state === "Degraded" ? "warn" : "ok";
  const label = state || "not reported";
  return `<span class="kit-badge kit-badge-${state ? tone : "muted"}" data-conn="${esc(label)}">${esc(label.toUpperCase())}</span>`;
}

export function gateBadge(label, state) {
  const tone =
    state === "Blocked" ? "err" : state === "Pending" ? "warn" : "ok";
  return `<span class="kit-badge kit-badge-${tone}" data-gate="${esc(state)}">${esc(label)}: ${esc(String(state || "").toUpperCase())}</span>`;
}

export function objectHeader(header) {
  if (!header?.name) return "";
  const links = (header.links || [])
    .map(
      (link) =>
        `<button type="button" class="kit-btn kit-btn-sm" data-onclick="switchScreen('${esc(link.screen)}')">${esc(link.label)}</button>`,
    )
    .join("");
  const owner = header.owner ? esc(header.owner) : "not recorded";
  return `<header data-object-header class="kit-card kit-bar-${esc(header.tone || "info")} kit-header">
    <div class="kit-header-row">
      <div>
        <div class="kit-header-title">
          <span class="kit-badge kit-badge-muted">${esc(header.objectType || "Object")}</span>
          <h1>${esc(header.name)}</h1>
          ${statusBadge(header.status)}
        </div>
        <div class="kit-muted">Owner: <strong>${owner}</strong>${header.subtitle ? ` · ${esc(header.subtitle)}` : ""}</div>
      </div>
    </div>
    ${links ? `<div class="kit-links">${links}</div>` : ""}
  </header>`;
}

/**
 * @param {{ state: string, title: string, detail?: string, operator?: string }} args
 */
export function viewState({ state, title, detail, operator }) {
  const copy = {
    loading: "Reading the Hub.",
    empty: detail || "No records match this view.",
    error: detail || "The Hub did not answer. Nothing was changed.",
    disconnected: "The Hub did not answer. Nothing was changed.",
    denied: `${operator || "This operator"} does not have a role for this view.`,
  };
  return `<section class="kit-card kit-bar-warn kit-state" role="status" data-state="${esc(state)}">
    <div class="kit-state-title">${esc(String(state || "").toUpperCase())} — ${esc(title || "View")}</div>
    <p>${esc(copy[state] || detail || "")}</p>
  </section>`;
}

function numberOrNull(value) {
  if (value == null || value === "") return null;
  const number = Number(value);
  return Number.isFinite(number) ? number : null;
}

function clamp(value, min, max) {
  return Math.min(max, Math.max(min, value));
}

export function wavesFromRecords(records) {
  const pucks = [];
  const dependencies = [];
  for (const row of records || []) {
    const start = numberOrNull(row.startHourUtc ?? row.windowStartHour);
    if (start == null) continue;
    const windowStart = numberOrNull(row.windowStart) ?? 0;
    const windowEnd = numberOrNull(row.windowEnd) ?? 24;
    const duration = numberOrNull(row.durationHours) ?? 1;
    const id = String(row.id || row.runId || `wave-${pucks.length + 1}`);
    pucks.push({
      id,
      lane: String(
        row.lane || row.environment || row.toChannel || "unassigned",
      ),
      label: String(row.label || row.product || row.id || id),
      startHourUtc: start,
      durationHours: Math.max(1, duration),
      windowStart,
      windowEnd,
    });
    const after = row.dependsOn || row.after;
    if (after) dependencies.push([String(after), id]);
  }
  return { pucks, dependencies };
}

export function movePuck(pucks, id, startHour, durationHours) {
  return (pucks || []).map((puck) => {
    if (puck.id !== id) return puck;
    const duration =
      durationHours == null ? puck.durationHours : Math.max(1, durationHours);
    const latest = Math.max(puck.windowStart, puck.windowEnd - duration);
    const start = clamp(startHour, puck.windowStart, latest);
    return {
      ...puck,
      startHourUtc: start,
      durationHours: Math.min(duration, puck.windowEnd - start),
    };
  });
}

export function connectivityFromSpoke(state) {
  const text = String(state || "").toLowerCase();
  if (!text) return "";
  if (/offline|disconnect|unreachable/.test(text)) return "Disconnected";
  if (/degrad|drift|stale/.test(text)) return "Degraded";
  if (/online|healthy|ready|connected|reporting/.test(text)) return "Online";
  return "";
}

export function sitesFromEnvironments(rows) {
  return (rows || [])
    .map((row) => {
      const spoke = row.spoke || {};
      const id = row.environment?.id || row.id || "";
      const raw = spoke.state || row.reported?.state || "";
      return {
        id,
        health: raw || "not reported",
        connectivity: connectivityFromSpoke(raw),
        lastCheckIn: row.reported?.observedAt || spoke.observedAt || "",
        pendingSyncCount: Number(row.pendingSyncCount || 0),
      };
    })
    .filter((site) => site.id);
}

export function lineageFromHub(hub) {
  const nodes = [];
  const edges = [];
  const seen = new Set();
  const add = (id, title, kind, status) => {
    if (!id || seen.has(id)) return;
    seen.add(id);
    nodes.push({
      id,
      title,
      kind,
      status: status || "",
      subtitle: kind,
    });
  };
  for (const row of hub?.promotions || []) {
    if (row.fromChannel)
      add(`channel:${row.fromChannel}`, row.fromChannel, "channel", row.status);
    if (row.toChannel)
      add(`channel:${row.toChannel}`, row.toChannel, "channel", row.status);
    if (row.fromChannel && row.toChannel) {
      edges.push([`channel:${row.fromChannel}`, `channel:${row.toChannel}`]);
    }
  }
  for (const run of hub?.packaging || []) {
    if (run.runId)
      add(
        `run:${run.runId}`,
        run.runId,
        "packaging run",
        run.phase || run.outcome || "",
      );
  }
  return { nodes, edges };
}

export function diffRowsFromChanges(changes) {
  const rows = [];
  for (const change of changes || []) {
    const actor =
      typeof change.author === "string"
        ? change.author
        : change.author?.username || "";
    const ts = change.updatedAt || change.createdAt || "";
    for (const hunk of change.hunks || []) {
      rows.push({
        path: hunk.path || "",
        before: hunk.before || "",
        after: hunk.after || "",
        ts,
        actor,
        environment: change.environment || "",
      });
    }
    for (const entity of change.changes || []) {
      const diff = entity.diff || {};
      for (const [path, entry] of Object.entries(diff)) {
        if (!entry || typeof entry !== "object") continue;
        rows.push({
          path,
          before: entry.before ?? entry.Before ?? "",
          after: entry.after ?? entry.After ?? "",
          ts,
          actor,
          environment: change.environment || "",
        });
      }
    }
  }
  return rows;
}

export function recordedDependencies(promotions) {
  return (promotions || [])
    .filter((row) => row.fromChannel && row.toChannel)
    .map((row) => ({
      from: row.fromChannel,
      to: row.toChannel,
      label: [row.product, row.version, row.status].filter(Boolean).join(" · "),
    }));
}

function puckStyle(puck) {
  const left = (puck.startHourUtc / 24) * 100;
  const width = (puck.durationHours / 24) * 100;
  return `left:${left}%;width:${Math.max(width, 4)}%`;
}

export function ganttMarkup(pucks, dependencies, note) {
  if (!pucks.length) {
    const deps = (dependencies || [])
      .map(
        (edge) =>
          `<div class="kit-card kit-bar-info kit-pad">${esc(edge.from)} → ${esc(edge.to)}${edge.label ? ` · ${esc(edge.label)}` : ""}</div>`,
      )
      .join("");
    return `<section class="kit-panel kit-pad">
      <div class="kit-section">Staged wave rollout</div>
      ${viewState({ state: "empty", title: "Waves", detail: note || "No staged wave is recorded." })}
      ${deps ? `<div class="kit-stack">${deps}</div>` : ""}
    </section>`;
  }
  const rows = pucks
    .map(
      (puck) => `<div class="kit-gantt-row">
        <div class="kit-gantt-lane">${esc(puck.lane)}</div>
        <div class="kit-gantt-track">
          <button type="button" class="kit-gantt-puck" data-puck="${esc(puck.id)}" data-start="${esc(puck.startHourUtc)}" style="${puckStyle(puck)}">${esc(puck.label)}</button>
        </div>
      </div>`,
    )
    .join("");
  return `<section class="kit-panel kit-pad" data-gantt>
    <div class="kit-section">Staged wave rollout · ${pucks.length} waves</div>
    <p class="kit-muted">A drag stays on this screen. The Hub stores a window only when a wave API records it.</p>
    ${rows}
  </section>`;
}

export function lineageMarkup(graph) {
  const nodes = graph?.nodes || [];
  const edges = graph?.edges || [];
  if (!nodes.length) {
    return viewState({
      state: "empty",
      title: "Lineage",
      detail: "No dependency is recorded.",
    });
  }
  const width = Math.max(640, nodes.length * 160);
  const positioned = nodes.map((node, index) => ({
    ...node,
    x: 40 + (index % 4) * 160,
    y: 36 + Math.floor(index / 4) * 90,
  }));
  const byId = new Map(positioned.map((node) => [node.id, node]));
  const lines = edges
    .map(([from, to]) => {
      const start = byId.get(from);
      const end = byId.get(to);
      if (!start || !end) return "";
      return `<line x1="${start.x + 120}" y1="${start.y + 18}" x2="${end.x}" y2="${end.y + 18}" />`;
    })
    .join("");
  const marks = positioned
    .map(
      (node) => `<g data-node="${esc(node.id)}">
        <rect class="kit-node kit-node-${statusTone(node.status)}" x="${node.x}" y="${node.y}" width="128" height="36" rx="2" />
        <text x="${node.x + 8}" y="${node.y + 15}">${esc(node.title)}</text>
        <text class="kit-node-sub" x="${node.x + 8}" y="${node.y + 28}">${esc(node.kind)}</text>
      </g>`,
    )
    .join("");
  return `<section class="kit-panel kit-pad" data-lineage>
    <div class="kit-section">Lineage · ${nodes.length} recorded objects · ${edges.length} recorded links</div>
    <div class="kit-canvas"><svg width="${width}" height="${80 + Math.ceil(nodes.length / 4) * 90}" viewBox="0 0 ${width} ${80 + Math.ceil(nodes.length / 4) * 90}">${lines}${marks}</svg></div>
  </section>`;
}

export function connectivityMarkup(sites) {
  if (!sites.length) {
    return viewState({
      state: "empty",
      title: "Connectivity",
      detail: "No environment is recorded.",
    });
  }
  const cards = sites
    .map(
      (
        site,
      ) => `<button type="button" class="kit-card kit-card-interactive kit-bar-info kit-pad" data-onclick="switchScreen('s14-env-detail')">
        <div class="kit-header-row">
          <strong>${esc(site.id)}</strong>
          ${connectivityBadge(site.connectivity)}
        </div>
        <div class="kit-muted">${esc(site.health)} · Check-in ${esc(site.lastCheckIn || "not reported")} · ${esc(site.pendingSyncCount)} pending</div>
      </button>`,
    )
    .join("");
  return `<section class="kit-panel kit-pad"><div class="kit-section">Site connectivity</div><div class="kit-grid">${cards}</div></section>`;
}

export function diffTimeline(rows) {
  if (!rows.length) {
    return viewState({
      state: "empty",
      title: "Diff",
      detail: "No stored field diff is recorded.",
    });
  }
  const body = rows
    .map(
      (row) => `<tr>
        <td class="code-id">${esc(row.path)}</td>
        <td class="kit-del">${esc(row.before)}</td>
        <td class="kit-add">${esc(row.after)}</td>
        <td>${esc(row.environment || "")}</td>
        <td>${esc(row.actor || "")}</td>
      </tr>`,
    )
    .join("");
  return `<section class="kit-panel kit-pad">
    <div class="kit-section">Field diff</div>
    <table class="kit-table"><thead><tr><th>Field</th><th>Before</th><th>After</th><th>Environment</th><th>Actor</th></tr></thead><tbody>${body}</tbody></table>
  </section>`;
}

export function auditTimeline(entries) {
  const rows = (entries || []).slice(0, 8);
  if (!rows.length) {
    return viewState({
      state: "empty",
      title: "Timeline",
      detail: "No audit entry is recorded.",
    });
  }
  return `<section class="kit-stack">${rows
    .map(
      (entry) =>
        `<div class="kit-card kit-bar-info kit-pad"><strong>${esc(entry.sequence ?? "")} · ${esc(entry.action || "")}</strong> <span class="kit-muted">${esc(entry.actor || "")}</span></div>`,
    )
    .join("")}</section>`;
}

export function areaSupplement(hub, screen, tab) {
  const data = hub || {};
  if (screen === "area-releases" && tab === "promotion") {
    const waves = wavesFromRecords(data.promotions);
    return ganttMarkup(
      waves.pucks,
      recordedDependencies(data.promotions),
      "No staged wave window is recorded.",
    );
  }
  if (screen === "area-fleet" && (tab === "spokes" || tab === "drift")) {
    return connectivityMarkup(sitesFromEnvironments(data.environments));
  }
  if (screen === "area-packaging" && tab === "proposals") {
    return lineageMarkup(lineageFromHub(data));
  }
  if (screen === "area-audit") {
    return auditTimeline(data.audit);
  }
  return "";
}

/**
 * @param {any} hub
 * @param {string} screen
 * @param {{packagingRunId?: string, releaseEnvironmentId?: string, changeRequestId?: string}} selection
 */
export function areaObject(hub, screen, selection = {}) {
  const data = hub || {};
  if (screen === "area-packaging") {
    const run = selectedPackagingRun(data.packaging, selection.packagingRunId);
    if (!run?.runId) return null;
    return {
      name: run.runId,
      objectType: "PackagingRun",
      status: runState(run),
      owner: "",
      tone: {
        succeeded: "ok",
        review: "warn",
        running: "info",
        blocked: "warn",
        failed: "err",
      }[runState(run)],
      subtitle: run.outcome || run.phase || "",
      links: [{ label: "Releases", screen: "area-releases" }],
    };
  }
  if (screen === "area-releases") {
    const row = selectedEnvironment(
      data.environments,
      selection.releaseEnvironmentId,
    );
    const id = row?.environment?.id;
    if (!id) return null;
    return {
      name: id,
      objectType: "Environment",
      status: row.spoke?.state || "recorded",
      owner: "",
      tone: "info",
      subtitle: row.spoke?.agentId || "",
      links: [{ label: "Packaging", screen: "area-packaging" }],
    };
  }
  if (screen === "area-changes") {
    const change = selection.changeRequestId
      ? (data.changes || []).find((row) => row.id === selection.changeRequestId)
      : (data.changes || [])[0];
    if (!change?.id) return null;
    const author =
      typeof change.author === "string"
        ? change.author
        : change.author?.username;
    return {
      name: change.title || change.id,
      objectType: "ChangeRequest",
      status: change.status || "recorded",
      owner: author || "",
      tone: "warn",
      subtitle: change.id,
      links: [{ label: "Audit", screen: "area-audit" }],
    };
  }
  return null;
}

export function bindKit(root, store) {
  const frame = root.querySelector("[data-lineage]");
  if (frame) {
    let scale = 1;
    const svg = frame.querySelector("svg");
    frame.addEventListener(
      "wheel",
      (event) => {
        event.preventDefault();
        scale = Math.min(
          2.4,
          Math.max(0.6, scale + (event.deltaY > 0 ? -0.08 : 0.08)),
        );
        if (svg) svg.style.transform = `scale(${scale})`;
      },
      { passive: false },
    );
  }
  if (!store) return;
  root.querySelectorAll("[data-puck]").forEach((puckEl) => {
    puckEl.addEventListener("pointerdown", (event) => {
      const id = puckEl.getAttribute("data-puck");
      const origin = event.clientX;
      const start = Number(puckEl.getAttribute("data-start"));
      const move = (ev) => {
        store.pucks = movePuck(
          store.pucks,
          id,
          start + (ev.clientX - origin) / 18,
        );
        const next = store.pucks.find((puck) => puck.id === id);
        if (!next) return;
        puckEl.style.left = `${(next.startHourUtc / 24) * 100}%`;
        puckEl.style.width = `${Math.max((next.durationHours / 24) * 100, 4)}%`;
        puckEl.setAttribute("data-start", String(next.startHourUtc));
      };
      const up = () => {
        window.removeEventListener("pointermove", move);
        window.removeEventListener("pointerup", up);
      };
      window.addEventListener("pointermove", move);
      window.addEventListener("pointerup", up);
    });
  });
}

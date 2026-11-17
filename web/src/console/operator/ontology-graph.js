/* Native ontology metadata and authorized sample pages. */

import { ontologyCatalog, bounded } from "./ontology-explorer.js";
import { diffValue } from "./change-diff.js";
import { createConduitClient, fetchTransport } from "@conduit/ontology-client";

const transport = fetchTransport();
const client = createConduitClient({
  transport: async (path, init) => {
    const body = await transport(path, init);
    if (path.endsWith("/objectSets/loadObjects") && !Array.isArray(body?.data))
      throw new Error("Object page unavailable");
    return body;
  },
});

function esc(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

export function projectMetadata(body) {
  const types = ontologyCatalog(body)
    .types.slice(0, 256)
    .map((objectType) => {
      return {
        apiName: objectType.apiName,
        displayName: objectType.displayName || objectType.apiName,
        description: objectType.description || "",
        primaryKey: objectType.primaryKey || "",
        properties: Object.keys(objectType.properties || {}),
        links: (objectType.links || []).map((link) => ({
          apiName: link.apiName,
          rid: link.linkTypeRid,
          cardinality: link.cardinality,
        })),
      };
    });
  types.sort((left, right) => left.apiName.localeCompare(right.apiName));
  const byRid = new Map();
  for (const type of types) {
    for (const link of type.links) {
      if (!link.rid) continue;
      const sides = byRid.get(link.rid) || [];
      sides.push({ type: type.apiName, label: link.apiName });
      byRid.set(link.rid, sides);
    }
  }
  const edges = [];
  for (const sides of byRid.values()) {
    if (sides.length === 2 && sides[0].type !== sides[1].type) {
      edges.push({
        from: sides[0].type,
        to: sides[1].type,
        label: sides[0].label,
      });
    }
  }
  return { types, edges };
}

function layout(types) {
  const cx = 340;
  const cy = 210;
  const radius = types.length <= 1 ? 0 : 150;
  return types.map((type, index) => {
    const angle =
      -Math.PI / 2 + (index * 2 * Math.PI) / Math.max(types.length, 1);
    return {
      ...type,
      x: Math.round(cx + radius * Math.cos(angle)),
      y: Math.round(cy + radius * Math.sin(angle)),
    };
  });
}

const COLORS = [
  "var(--info-text)",
  "var(--ok-text)",
  "var(--info-text)",
  "var(--warn-text)",
  "var(--info-text)",
  "var(--warn-text)",
  "var(--ok-text)",
];

export function createOntologyGraph(ctx) {
  const state = ctx.state;

  let epoch = 0,
    objectEpoch = 0,
    identity = "";
  const nativeIdentity = () =>
    JSON.stringify([
      state.reviewOperatorActor || "",
      state.reviewOperatorIdentities || [],
    ]);
  async function load() {
    const stamp = ++epoch,
      actor = nativeIdentity();
    state.graphLoading = true;
    state.graphObjects = [];
    state.graphObjectNote = "Object page unavailable.";
    try {
      const body = await bounded(client.metadata());
      if (stamp !== epoch || actor !== nativeIdentity()) return;
      const projected = projectMetadata(body);
      state.graphLive = true;
      state.graphTypes = projected.types;
      state.graphEdges = projected.edges;
      state.graphNote = projected.types.length
        ? "Native metadata · first 256 types"
        : "No object types returned";
    } catch {
      if (stamp !== epoch || actor !== nativeIdentity()) return;
      state.graphLive = false;
      state.graphTypes = [];
      state.graphEdges = [];
      state.graphNote = "Metadata unavailable; no example data substituted";
    }
    if (!state.graphTypes.some((type) => type.apiName === state.graphSelected))
      state.graphSelected = state.graphTypes[0]?.apiName || "";
    await loadObjects();
    if (stamp !== epoch || actor !== nativeIdentity()) return;
    state.graphLoaded = true;
    state.graphLoading = false;
    ctx.render();
  }
  async function loadObjects() {
    const selected = state.graphSelected,
      stamp = ++objectEpoch,
      actor = nativeIdentity();
    state.graphObjects = [];
    state.graphObjectNote = "Loading authorized sample page…";
    if (!state.graphLive || !selected) {
      state.graphObjectNote = "No authorized page requested.";
      return;
    }
    try {
      const page = await bounded(
        client.objects(selected).fetchPage({ pageSize: 25 }),
      );
      if (
        stamp !== objectEpoch ||
        actor !== nativeIdentity() ||
        selected !== state.graphSelected
      )
        return;
      if (
        !Array.isArray(page.data) ||
        page.data.some(
          (row) => !row || typeof row !== "object" || Array.isArray(row),
        )
      )
        throw new Error("Invalid page");
      state.graphObjects = page.data.slice(0, 25);
      state.graphObjectNote = page.data.length
        ? "First 25 authorized sample rows; not a total count."
        : "Returned authorized page has no rows.";
    } catch {
      if (
        stamp !== objectEpoch ||
        actor !== nativeIdentity() ||
        selected !== state.graphSelected
      )
        return;
      state.graphObjects = [];
      state.graphObjectNote =
        "Object page unavailable; no zero-count inference.";
    }
  }

  function render(root) {
    const actor = nativeIdentity();
    if (actor !== identity) {
      identity = actor;
      epoch++;
      objectEpoch++;
      state.graphLoaded = false;
      state.graphLoading = false;
      state.graphTypes = [];
      state.graphEdges = [];
      state.graphObjects = [];
      state.graphNote = "Reading native session…";
    }
    if (state.hub?.loading !== false) {
      root.innerHTML = '<p role="status">Reading native session and Hub…</p>';
      return;
    }
    if (!state.graphLoaded && !state.graphLoading) {
      state.graphLoading = true;
      load();
    }
    const types = layout(state.graphTypes || []);
    const byName = new Map(types.map((type) => [type.apiName, type]));
    const selected = byName.get(state.graphSelected) || types[0];
    const edges = (state.graphEdges || [])
      .map((edge) => {
        const from = byName.get(edge.from);
        const to = byName.get(edge.to);
        if (!from || !to) return "";
        return `<line x1="${from.x}" y1="${from.y}" x2="${to.x}" y2="${to.y}" stroke="var(--border-subtle)" stroke-width="1.4"></line>
          <text x="${(from.x + to.x) / 2}" y="${(from.y + to.y) / 2 - 6}" fill="var(--text-2)" font-size="9" text-anchor="middle">${esc(edge.label)}</text>`;
      })
      .join("");
    const nodes = types
      .map((type, index) => {
        const on = selected && selected.apiName === type.apiName;
        const color = COLORS[index % COLORS.length];
        return `<g data-onclick="selectOntologyType(${esc(JSON.stringify(type.apiName))})" style="cursor:pointer">
          <circle cx="${type.x}" cy="${type.y}" r="${on ? 28 : 24}" fill="${on ? "var(--bg-1)" : "var(--bg-1)"}" stroke="${color}" stroke-width="${on ? 2.4 : 1.4}"></circle>
          <text x="${type.x}" y="${type.y + 4}" fill="var(--text-1)" font-size="9" text-anchor="middle">${esc(type.displayName)}</text>
        </g>`;
      })
      .join("");
    const rows = (state.graphObjects || [])
      .map((object) => {
        const cells = (selected?.properties || [])
          .slice(0, 4)
          .map((key) => `<td>${esc(diffValue(object[key], key))}</td>`);
        return `<tr><td class="font-semibold text-[var(--info-text)]">${esc(object[selected?.primaryKey] ?? object.__primaryKey ?? "")}</td>${cells.join("")}</tr>`;
      })
      .join("");
    const headers = ["Key", ...(selected?.properties || []).slice(0, 4)]
      .map((key) => `<th>${esc(key)}</th>`)
      .join("");
    root.innerHTML = `
      <div class="so-view-enter flex flex-col gap-3 min-h-0 h-full">
        <div class="flex items-center justify-between gap-3">
          <div>
            <div class="text-[15px] font-bold text-[var(--text-1)]">Ontology graph</div>
            <div class="text-[11px] text-[var(--text-2)] mt-0.5">${esc(selected?.description || "Object types linked by the catalog.")}</div>
          </div>
          <span class="bp6-compound-tag"><span class="bp6-compound-left">Catalog</span><span class="bp6-compound-right" style="background:var(--bg-2);color:var(--info-text)">${esc(state.graphNote || "Loading")}</span></span>
        </div>
        <div class="grid gap-3 min-h-0 flex-1" style="grid-template-columns:minmax(0,1.4fr) minmax(280px,0.8fr)">
          <div class="so-panel overflow-hidden min-h-[420px]">
            <svg viewBox="0 0 680 420" class="w-full h-full" role="img" aria-label="Ontology graph">
              ${edges}
              ${nodes}
            </svg>
          </div>
          <div class="so-panel overflow-hidden flex flex-col min-h-0">
            <div class="px-3 py-2 border-b border-[var(--border-subtle)] flex items-center justify-between">
              <span class="text-[13px] font-semibold text-[var(--text-1)]">${esc(selected?.displayName || "Objects")}</span>
              <span class="text-[10px] text-[var(--text-2)]">${esc(selected?.primaryKey || "")}</span>
            </div>
            <p role="status">${esc(state.graphObjectNote || "")}</p><div class="overflow-auto flex-1">
              <table class="so-table">
                <thead><tr>${headers}</tr></thead>
                <tbody>${rows || `<tr><td colspan="5" class="text-[var(--text-2)]">${esc(state.graphObjectNote || "Object page unavailable.")}</td></tr>`}</tbody>
              </table>
            </div>
          </div>
        </div>
      </div>`;
  }

  return {
    screens: [
      {
        id: "og01-ontology-graph",
        num: 33,
        short: "Ontology Graph",
        tabTitle: "Ontology Graph",
        hasSidebar: true,
        showKpis: false,
        subtitle: "ONTOLOGY GRAPH",
      },
    ],
    navItems: [
      { label: "Ontology Graph", icon: "globe", id: "og01-ontology-graph" },
    ],
    extendState() {
      Object.assign(state, {
        graphTypes: [],
        graphEdges: [],
        graphObjects: [],
        graphSelected: "Environment",
        graphLive: false,
        graphLoaded: false,
        graphLoading: false,
        graphNote: "",
      });
    },
    renderers: { "og01-ontology-graph": render },
    bind(target) {
      target.selectOntologyType = (apiName) => {
        if (!state.graphTypes.some((type) => type.apiName === apiName)) return;
        state.graphSelected = apiName;
        Promise.resolve(loadObjects()).then(() => ctx.render());
      };
      target.reloadOntologyGraph = () => {
        epoch++;
        objectEpoch++;
        state.graphLoaded = false;
        state.graphLoading = false;
        ctx.render();
      };
    },
  };
}

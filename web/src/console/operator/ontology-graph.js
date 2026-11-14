/* Ontology graph. Live rows come from the Hub catalog; fixtures are example data. */

import { createConduitClient } from "@conduit/ontology-client";

const client = createConduitClient();

const FIXTURE = {
  types: [
    {
      apiName: "Environment",
      displayName: "Environment",
      description: "A delivery environment and the generation it declares.",
      primaryKey: "id",
      properties: ["id", "generation", "connection", "releaseChannel"],
      count: 2,
    },
    {
      apiName: "Spoke",
      displayName: "Spoke",
      description: "The agent that reports an environment.",
      primaryKey: "id",
      properties: ["id", "environment"],
      count: 1,
    },
    {
      apiName: "WorkProfile",
      displayName: "Work profile",
      description: "The operator-authored profile of an environment.",
      primaryKey: "environmentId",
      properties: ["environmentId", "displayName", "ownerTeam"],
      count: 1,
    },
    {
      apiName: "ReleaseChannel",
      displayName: "Release channel",
      description: "A release channel declared for an environment.",
      primaryKey: "id",
      properties: ["id", "environment", "name"],
      count: 1,
    },
    {
      apiName: "Plan",
      displayName: "Plan",
      description: "A reviewed plan for one environment.",
      primaryKey: "id",
      properties: ["id", "environment", "status"],
      count: 1,
    },
    {
      apiName: "ChangeRequest",
      displayName: "Change request",
      description: "A governed change request.",
      primaryKey: "id",
      properties: ["id", "environment", "title", "status"],
      count: 1,
    },
    {
      apiName: "Artifact",
      displayName: "Artifact",
      description: "A chart or image referenced by a plan.",
      primaryKey: "id",
      properties: ["id", "product", "version", "digest"],
      count: 1,
    },
  ],
  edges: [
    { from: "Environment", to: "Spoke", label: "reportingSpoke" },
    { from: "Environment", to: "WorkProfile", label: "workProfile" },
    { from: "Environment", to: "ReleaseChannel", label: "releaseChannels" },
    { from: "Environment", to: "Plan", label: "plans" },
    { from: "Environment", to: "ChangeRequest", label: "changeRequests" },
    { from: "Plan", to: "Artifact", label: "artifacts" },
    { from: "Plan", to: "ChangeRequest", label: "changeRequest" },
  ],
  objects: {
    Environment: [
      {
        __primaryKey: "edge-west-02",
        id: "edge-west-02",
        generation: "18",
        releaseChannel: "edge",
      },
      {
        __primaryKey: "core-east-01",
        id: "core-east-01",
        generation: "12",
        releaseChannel: "stable",
      },
    ],
    Spoke: [
      {
        __primaryKey: "spoke-west-02",
        id: "spoke-west-02",
        environment: "edge-west-02",
      },
    ],
    WorkProfile: [
      {
        __primaryKey: "edge-west-02",
        displayName: "Edge west",
        ownerTeam: "fleet",
        environmentId: "edge-west-02",
      },
    ],
    ReleaseChannel: [
      {
        __primaryKey: "edge-west-02/edge",
        name: "edge",
        environment: "edge-west-02",
      },
    ],
    Plan: [
      {
        __primaryKey: "PLN-2026-0418",
        id: "PLN-2026-0418",
        environment: "edge-west-02",
        status: "AWAITING_APPROVAL",
      },
    ],
    ChangeRequest: [
      {
        __primaryKey: "CR-308",
        id: "CR-308",
        title: "Edge channel pin",
        status: "PENDING_APPROVALS",
        environment: "edge-west-02",
      },
    ],
    Artifact: [
      {
        __primaryKey: "sha256:example",
        digest: "sha256:example",
        product: "uds-core",
        version: "0.31.2",
      },
    ],
  },
};

function esc(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

function projectMetadata(body) {
  const types = Object.values(body.objectTypes || {}).map((entry) => {
    const objectType = entry.objectType || entry;
    return {
      apiName: objectType.apiName,
      displayName: objectType.displayName || objectType.apiName,
      description: objectType.description || "",
      primaryKey: objectType.primaryKey || "",
      properties: Object.keys(objectType.properties || {}),
      links: (entry.linkTypes || []).map((link) => ({
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
      const sides = byRid.get(link.rid) || [];
      sides.push({ type: type.apiName, label: link.apiName });
      byRid.set(link.rid, sides);
    }
  }
  const edges = [];
  for (const sides of byRid.values()) {
    if (sides.length >= 2 && sides[0].type !== sides[1].type) {
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
  "#4c90f0",
  "#34d399",
  "#b4a2f8",
  "#fbbf24",
  "#7cb4ff",
  "#f5a623",
  "#15b371",
];

export function createOntologyGraph(ctx) {
  const state = ctx.state;

  async function load() {
    state.graphLoading = true;
    try {
      const body = await client.metadata();
      const projected = projectMetadata(body);
      if (!projected.types.length) throw new Error("empty");
      state.graphLive = true;
      state.graphTypes = projected.types;
      state.graphEdges = projected.edges;
      state.graphNote = "Ontology API";
    } catch {
      state.graphLive = false;
      state.graphTypes = FIXTURE.types;
      state.graphEdges = FIXTURE.edges;
      state.graphNote = "Example data";
    }
    if (
      !state.graphTypes.some((type) => type.apiName === state.graphSelected)
    ) {
      state.graphSelected = state.graphTypes[0]?.apiName || "Environment";
    }
    await loadObjects();
    state.graphLoaded = true;
    state.graphLoading = false;
    ctx.render();
  }

  async function loadObjects() {
    const selected = state.graphSelected || "Environment";
    if (!state.graphLive) {
      state.graphObjects = FIXTURE.objects[selected] || [];
      return;
    }
    try {
      const page = await client.objects(selected).fetchPage({ pageSize: 25 });
      state.graphObjects = page.data;
    } catch {
      state.graphObjects = [];
    }
  }

  function render(root) {
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
        return `<line x1="${from.x}" y1="${from.y}" x2="${to.x}" y2="${to.y}" stroke="#2a3844" stroke-width="1.4"></line>
          <text x="${(from.x + to.x) / 2}" y="${(from.y + to.y) / 2 - 6}" fill="#6d7885" font-size="9" text-anchor="middle">${esc(edge.label)}</text>`;
      })
      .join("");
    const nodes = types
      .map((type, index) => {
        const on = selected && selected.apiName === type.apiName;
        const color = COLORS[index % COLORS.length];
        return `<g data-onclick="selectOntologyType('${esc(type.apiName)}')" style="cursor:pointer">
          <circle cx="${type.x}" cy="${type.y}" r="${on ? 28 : 24}" fill="${on ? "#17253a" : "#12181d"}" stroke="${color}" stroke-width="${on ? 2.4 : 1.4}"></circle>
          <text x="${type.x}" y="${type.y + 4}" fill="#f0f5fa" font-size="9" text-anchor="middle">${esc(type.displayName)}</text>
        </g>`;
      })
      .join("");
    const rows = (state.graphObjects || [])
      .map((object) => {
        const cells = (selected?.properties || [])
          .slice(0, 4)
          .map((key) => `<td>${esc(object[key] ?? "")}</td>`);
        return `<tr><td class="font-semibold text-[#5c9ce6]">${esc(object.__primaryKey || "")}</td>${cells.join("")}</tr>`;
      })
      .join("");
    const headers = ["Key", ...(selected?.properties || []).slice(0, 4)]
      .map((key) => `<th>${esc(key)}</th>`)
      .join("");
    root.innerHTML = `
      <div class="so-view-enter flex flex-col gap-3 min-h-0 h-full">
        <div class="flex items-center justify-between gap-3">
          <div>
            <div class="text-[15px] font-bold text-white">Ontology graph</div>
            <div class="text-[11px] text-[#8b96a5] mt-0.5">${esc(selected?.description || "Object types linked by the catalog.")}</div>
          </div>
          <span class="bp6-compound-tag"><span class="bp6-compound-left">Catalog</span><span class="bp6-compound-right" style="background:#1a2430;color:#7cb4ff">${esc(state.graphNote || "Loading")}</span></span>
        </div>
        <div class="grid gap-3 min-h-0 flex-1" style="grid-template-columns:minmax(0,1.4fr) minmax(280px,0.8fr)">
          <div class="so-panel overflow-hidden min-h-[420px]">
            <svg viewBox="0 0 680 420" class="w-full h-full" role="img" aria-label="Ontology graph">
              ${edges}
              ${nodes}
            </svg>
          </div>
          <div class="so-panel overflow-hidden flex flex-col min-h-0">
            <div class="px-3 py-2 border-b border-[#1c242c] flex items-center justify-between">
              <span class="text-[13px] font-semibold text-white">${esc(selected?.displayName || "Objects")}</span>
              <span class="text-[10px] text-[#8b96a5]">${esc(selected?.primaryKey || "")}</span>
            </div>
            <div class="overflow-auto flex-1">
              <table class="so-table">
                <thead><tr>${headers}</tr></thead>
                <tbody>${rows || `<tr><td colspan="5" class="text-[#8b96a5]">No objects of this type.</td></tr>`}</tbody>
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
        state.graphSelected = apiName;
        Promise.resolve(loadObjects()).then(() => ctx.render());
      };
      target.reloadOntologyGraph = () => {
        state.graphLoaded = false;
        state.graphLoading = false;
        ctx.render();
      };
    },
  };
}

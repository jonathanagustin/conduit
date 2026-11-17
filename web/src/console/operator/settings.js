/* Settings screens. Directory rows come from the Hub. Tokens and request shapes are examples. */
import {
  fetchPackagingRuns,
  packagingRunsTransport,
} from "../../api/packaging-runs.js";
import { capabilityTableMarkup } from "./session-capabilities.js";
import { createGovernanceEditor } from "./governance-editor.mjs";
import {
  installHubSession,
  signInAvailability,
} from "../../runtime/hub-session.js";

function esc(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

const EXAMPLE_TOKENS = [
  {
    id: "tok-example-1",
    name: "Workstation",
    scope: "read",
    expires: "8h",
    status: "Active",
  },
];

/** The tenant the Hub named in the page's bootstrap, or {} when absent. */
function consoleTenant() {
  try {
    const meta = document.querySelector(
      'meta[name="x-conduit-console-bootstrap"]',
    );
    const parsed = JSON.parse(meta?.getAttribute("content") || "{}");
    const tenant = parsed?.tenant || {};
    return {
      id: typeof tenant.id === "string" ? tenant.id : "",
      displayName:
        typeof tenant.displayName === "string" ? tenant.displayName : "",
    };
  } catch {
    return { id: "", displayName: "" };
  }
}

const SHORTCUTS = [
  ["Jump to a view", "Ctrl or Command K"],
  ["Close the open dialog", "Escape"],
  ["Run the focused command", "Enter"],
];

const DATA_SOURCES = [
  {
    id: "spoke-health",
    name: "Spoke health observations",
    endpoint: "POST /v1/environments/{environmentID}/health-observations",
    cadence: "Spoke HEALTH_POLL_INTERVAL, default 5s, refused below 5s",
    updates: "Installation health on the environment report",
    payload: {
      observations: [
        {
          installationId: "install-1",
          observedAt: "2026-11-14T00:00:00Z",
          details: {
            quorum: "1",
            nodes: [
              {
                nodeId: "node-1",
                productVersion: "1.2.3",
                result: "HEALTHY",
                containers: [
                  {
                    containerId: "app",
                    liveness: true,
                    readiness: true,
                    result: "HEALTHY",
                  },
                ],
              },
            ],
          },
        },
      ],
    },
  },
  {
    id: "spoke-info",
    name: "Spoke entity information",
    endpoint: "POST /api/v0/status/environments/{environmentId}/entities/info",
    cadence: "Posted as a full snapshot; the Hub does not poll for it",
    updates: "Reported entity identity, namespace, quorum, and nodes",
    payload: {
      entitiesInfo: [
        {
          entityType: "SERVICE",
          id: "widget",
          productGroup: "example",
          productName: "widget",
          k8sNamespace: "default",
          quorum: "1",
        },
      ],
    },
  },
  {
    id: "spoke-entity-health",
    name: "Spoke entity health",
    endpoint:
      "POST /api/v0/status/environments/{environmentId}/entities/health",
    cadence: "Posted independently of the information snapshot",
    updates: "Reported health and product version",
    payload: {
      entitiesHealth: [
        {
          nodeId: "node-1",
          productGroup: "example",
          productName: "widget",
          productVersion: "1.2.3",
          containerStatuses: [
            { containerId: "app", liveness: true, readiness: true },
          ],
        },
      ],
    },
  },
  {
    id: "plan-finish",
    name: "Plan completion report",
    endpoint: "POST /v1/plans/{planID}/finish",
    cadence: "Once, when the Spoke finishes a leased plan",
    updates: "Plan status and the environment report",
    payload: {
      status: "SUCCEEDED",
      entities: [
        {
          installationId: "install-1",
          product: "widget",
          version: "1.2.3",
          namespace: "default",
          health: "HEALTHY",
          rolloutStatus: "UP_TO_DATE",
          configStatus: "APPLIED",
          lifecycle: "INSTALLED",
          observedAt: "2026-11-14T00:00:00Z",
        },
      ],
    },
  },
  {
    id: "bundle-import",
    name: "Bundle import",
    endpoint: "POST /v1/bundles/import",
    cadence: "When an operator submits a bundle",
    updates: "Import receipt and a change request per target environment",
    payload: {
      bundle: {
        manifest: {
          apiVersion: "platform.delivery/bundle/v1alpha1",
          id: "bundle-1",
          pipelineId: "pipe-1",
          pipelineRevision: 1,
          sequence: 1,
          kind: "SNAPSHOT",
          createdAt: "2026-11-14T00:00:00Z",
          resources: [{ type: "ENVIRONMENT", id: "dev", digest: "sha256:abc" }],
          resourceTypes: ["ENVIRONMENT"],
          sourceFormatNote: "<manifest provenance note>",
        },
        manifestDigest: "sha256:def",
        signingKeyId: "key-1",
        signature: "<detached signature>",
      },
      requireSecurityPass: false,
      targetEnvironments: ["dev"],
    },
  },
  {
    id: "packaging-run",
    name: "Packaging run",
    endpoint:
      "POST /v1/packaging-runs/{runID}/rerun; worker start uses the same run identity",
    cadence: "Not periodic. A rerun or a worker start opens one run",
    updates:
      "Packaging-run phase, then a proposed plan when the handoff succeeds",
    payload: {
      decisionId: "decision-1",
      sourceDigest: "sha256:abc",
      proposalRevision: "",
      targetPlanRef: "environment/dev",
      templateVersion: "1",
      toolVersions: "helm 4",
    },
  },
];

async function readJSON(url) {
  const response = await fetch(url, {
    credentials: "same-origin",
    signal: AbortSignal.timeout(15000),
    headers: { accept: "application/json" },
  });
  if (!response.ok) throw new Error(String(response.status));
  return response.json();
}

export function createSettings(ctx) {
  const state = ctx.state;

  function note(live, label, native = false) {
    return `<span class="bp6-compound-tag"><span class="bp6-compound-left">${esc(label)}</span><span class="bp6-compound-right" style="background:#1a2430;color:#7cb4ff">${live ? "Hub" : native ? "Unavailable" : "Example data"}</span></span>`;
  }

  const identityKey = () =>
    JSON.stringify([
      state.reviewOperatorActor || "",
      state.reviewOperatorIdentities || [],
    ]);
  const governanceEditor = createGovernanceEditor({
    identityKey,
    canPerform: (operation, resource) => ctx.canPerform?.(operation, resource),
    render: () => ctx.render(),
    saved(kind, record) {
      const key = kind === "policy" ? "approvalPolicies" : "orgGrants";
      state[key] = [
        ...(state[key] || []).filter((row) => row.id !== record.id),
        record,
      ];
    },
  });
  function clearOrganization() {
    Object.assign(state, {
      orgTeams: [],
      orgGrants: [],
      approvalPolicies: [],
      signingKeys: [],
      sessionTeams: [],
      identityProviders: [],
      signInAvailable: false,
      orgTeamsLive: false,
      orgGrantsLive: false,
      approvalPoliciesLive: false,
      signingLive: false,
      identityAvailable: false,
      sessionAvailable: false,
      sessionName: "",
      authEnabled: false,
      hubReady: "unavailable",
    });
  }
  async function load() {
    if (state.settingsLoaded || state.settingsLoading) return;
    const snapshotIdentity = identityKey();
    state.settingsSnapshot = snapshotIdentity;
    state.settingsLoading = true;
    clearOrganization();
    try {
      const identity = await readJSON("/v1/operator-identity");
      if (typeof identity.enabled !== "boolean")
        throw new Error("identity unavailable");
      state.identityAvailable = true;
      state.authEnabled = identity.enabled;
      state.identityProviders = Array.isArray(identity.providers)
        ? identity.providers.filter(
            (provider) => provider && typeof provider === "object",
          )
        : [];
      state.signInAvailable = signInAvailability(identity).available;
    } catch {
      state.authEnabled = false;
      state.identityProviders = [];
      state.signInAvailable = false;
    }
    try {
      const body = await readJSON("/v1/teams");
      if (!Array.isArray(body.teams))
        throw new Error("team directory unavailable");
      state.orgTeamsLive = true;
      state.orgTeams = body.teams.map((team) => ({
        name: team.name,
        displayName: team.displayName || team.name,
        users: Array.isArray(team.memberUsers) ? team.memberUsers.length : 0,
        groups: Array.isArray(team.memberGroups) ? team.memberGroups.length : 0,
      }));
    } catch {
      state.orgTeamsLive = false;
      state.orgTeams = [];
    }
    try {
      const body = await readJSON("/v1/resource-grants");
      if (!Array.isArray(body.resourceGrants))
        throw new Error("grants unavailable");
      state.orgGrantsLive = true;
      state.orgGrants = body.resourceGrants;
    } catch {
      state.orgGrantsLive = false;
      state.orgGrants = [];
    }
    try {
      const body = await readJSON("/v1/approval-policies");
      if (
        !Array.isArray(body.approvalPolicies) ||
        body.approvalPolicies.some(
          (policy) => !policy || typeof policy.id !== "string",
        )
      )
        throw new Error("policies unavailable");
      state.approvalPolicies = body.approvalPolicies;
      state.approvalPoliciesLive = true;
    } catch {
      state.approvalPolicies = [];
      state.approvalPoliciesLive = false;
    }
    try {
      const ready = await readJSON("/health/ready");
      state.hubReady = ready?.status || "unknown";
    } catch (error) {
      state.hubReady = `unavailable (${error instanceof Error ? error.message : "no answer"})`;
    }
    try {
      const session = await readJSON("/v1/session");
      if (typeof session?.actor?.username !== "string")
        throw new Error("session identity unavailable");
      state.sessionAvailable = true;
      state.sessionName = session.effective?.username || session.actor.username;
      state.sessionTeams = Array.isArray(session?.effective?.teams)
        ? session.effective.teams
        : [];
      state.sessionDelegated = Boolean(session?.delegated);
    } catch {
      state.sessionAvailable = false;
    }
    try {
      const key = await readJSON("/v1/bundles/signing-key");
      if (key && typeof key.id === "string" && key.id !== "") {
        state.signingLive = true;
        state.signingKeys = [
          {
            id: key.id,
            algorithm: key.algorithm || "unavailable",
            publicKey: key.publicKey || "",
          },
        ];
      }
    } catch {
      state.signingLive = false;
    }
    if (snapshotIdentity !== identityKey()) {
      clearOrganization();
      state.settingsLoaded = false;
      state.settingsLoading = false;
      ctx.render();
      return;
    }
    state.settingsLoaded = true;
    state.settingsLoading = false;
    ctx.render();
  }

  let sourcesIdentity = "";
  let sourcesGeneration = 0;
  let sourcesPending = false;
  let sourcesLoaded = false;
  function ensureDataSources() {
    const identity = identityKey();
    if (identity !== sourcesIdentity) {
      sourcesIdentity = identity;
      sourcesGeneration++;
      sourcesPending = false;
      sourcesLoaded = false;
      state.dataSourceLive = {
        report: "Hub did not return a report",
        imports: "Hub did not return import receipts",
        packaging: "Hub did not return packaging runs",
      };
    }
    if (sourcesPending || sourcesLoaded) return;
    const generation = sourcesGeneration;
    sourcesPending = true;
    void loadDataSources().then((next) => {
      if (generation !== sourcesGeneration) return;
      sourcesPending = false;
      if (identity !== identityKey()) return;
      state.dataSourceLive = next;
      sourcesLoaded = true;
      ctx.render();
    });
  }

  async function loadDataSources() {
    const next = {
      report: "Hub did not return a report",
      imports: "Hub did not return import receipts",
      packaging: "Hub did not return packaging runs",
    };
    try {
      const catalog = await readJSON("/v1/environments?view=summary");
      const rows = Array.isArray(catalog.environments)
        ? catalog.environments
        : [];
      let newest = null;
      for (const row of rows) {
        const seen = row.reported && row.reported.observedAt;
        if (!seen) continue;
        if (!newest || seen > newest.seen) {
          newest = {
            seen,
            environment: row.environment && row.environment.id,
            entities: row.reported.entityCount,
          };
        }
      }
      next.report = newest
        ? `${newest.seen} on ${newest.environment || "an environment"} (${newest.entities} entities)`
        : "No reported observation on the environments this caller can read";
    } catch {
      next.report = "Hub did not return a report";
    }
    try {
      const body = await readJSON("/v1/bundle-imports");
      const imports = Array.isArray(body.imports) ? body.imports : [];
      if (!imports.length) {
        next.imports = "No import receipt";
      } else {
        const latest = imports.reduce((best, item) =>
          !best ||
          String(item.requestedAt || "") > String(best.requestedAt || "")
            ? item
            : best,
        );
        next.imports =
          `${latest.status || "recorded"} ${latest.requestedAt || ""} ${latest.bundleId || latest.id || ""}`.trim();
      }
    } catch {
      next.imports = "Hub did not return import receipts";
    }
    try {
      const body = await fetchPackagingRuns(
        packagingRunsTransport(window.location.origin),
      );
      const items = Array.isArray(body.items) ? body.items : [];
      if (!items.length) {
        next.packaging = "No packaging run";
      } else {
        const latest = items[0];
        next.packaging = `${latest.runId || "run"} phase ${latest.phase || "unknown"}${latest.outcome ? `, outcome ${latest.outcome}` : ""}`;
      }
    } catch {
      next.packaging = "Hub did not return packaging runs";
    }
    return next;
  }

  function sourceLastSeen(id) {
    const live = state.dataSourceLive || {};
    if (id === "bundle-import") return live.imports;
    if (id === "packaging-run") return live.packaging;
    return live.report;
  }

  function applyChrome() {
    const root = document.getElementById("operator-root");
    if (!root) return;
    root.classList.toggle(
      "is-comfortable",
      state.prefDensity === "comfortable",
    );
    root.classList.toggle("is-dim", state.prefTheme === "dim");
    let bar = document.getElementById("so-env-banner");
    const text = (state.environmentBanner || "").trim();
    if (!text) {
      if (bar) bar.remove();
      return;
    }
    if (!bar) {
      bar = document.createElement("div");
      bar.id = "so-env-banner";
      bar.className = "so-env-banner";
      root.insertBefore(bar, root.firstChild);
    }
    bar.textContent = text;
  }

  function segmented(options, current, handler) {
    return `<div role="radiogroup" class="bp6-segmented">${options
      .map(([value, label]) => {
        const on = current === value;
        return `<button type="button" role="radio" aria-checked="${on ? "true" : "false"}" data-onclick="${handler}('${value}')" class="bp6-seg-btn${on ? " is-selected" : ""}">${esc(label)}</button>`;
      })
      .join("")}</div>`;
  }

  function renderPreferences(root) {
    const tokens = state.prefTokens || [];
    root.innerHTML = `
      <div class="so-view-enter space-y-3">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div>
            <div class="text-[15px] font-bold text-white">Preferences</div>
            <div class="text-[11.5px] text-[#8b96a5]">Theme, density, clock, shortcuts, and personal tokens.</div>
          </div>
          ${note(false, "Tokens", !state.prefTokenExamples)}
        </div>
        <div class="grid gap-3 lg:grid-cols-2">
          <div class="so-panel p-3.5 space-y-3">
            <div class="text-[13px] font-semibold text-white">Appearance</div>
            <div class="flex flex-wrap items-center justify-between gap-2">
              <span class="text-[12px] text-[#dce4ee]">Theme</span>
              ${segmented(
                [
                  ["dark", "Dark"],
                  ["dim", "Dim"],
                ],
                state.prefTheme,
                "setPreferenceTheme",
              )}
            </div>
            <div class="flex flex-wrap items-center justify-between gap-2">
              <span class="text-[12px] text-[#dce4ee]">Density</span>
              ${segmented(
                [
                  ["compact", "Compact"],
                  ["comfortable", "Comfortable"],
                ],
                state.prefDensity,
                "setPreferenceDensity",
              )}
            </div>
            <div class="flex flex-wrap items-center justify-between gap-2">
              <span class="text-[12px] text-[#dce4ee]">Time zone</span>
              ${segmented(
                [
                  ["UTC", "UTC"],
                  ["local", "Local"],
                ],
                state.prefTimezone,
                "setPreferenceTimezone",
              )}
            </div>
          </div>
          <div class="so-panel p-3.5 space-y-2">
            <div class="text-[13px] font-semibold text-white">Keyboard shortcuts</div>
            ${SHORTCUTS.map(
              ([
                action,
                keys,
              ]) => `<div class="flex items-center justify-between gap-3 text-[12px]">
                <span class="text-[#dce4ee]">${esc(action)}</span>
                <kbd class="bp6-key-combo">${esc(keys)}</kbd>
              </div>`,
            ).join("")}
          </div>
        </div>
        <div class="so-panel p-3.5 space-y-3">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <div>
              <div class="text-[13px] font-semibold text-white">Personal API tokens</div>
              <div class="text-[11px] text-[#8b96a5]">This Hub does not issue personal tokens.</div>
            </div>
          </div>
          ${
            state.prefTokenExamples
              ? `
          <div role="status" class="so-warn-card p-2 text-[12px] text-[#eab854]">Example data. Changes here stay in this browser view and do not issue or revoke credentials.</div>
          <button type="button" class="so-btn" data-onclick="setTokenExampleMode(false)">Exit token example mode</button>
          <div class="flex flex-wrap gap-2 items-end">
            <label class="text-[11px] text-[#8b96a5]">Name
              <input id="pref-token-name" class="so-input mt-1" value="${esc(state.prefTokenDraft.name)}" data-oninput="state.prefTokenDraft.name=this.value" />
            </label>
            <label class="text-[11px] text-[#8b96a5]">Scope
              <select class="so-select mt-1" data-onchange="state.prefTokenDraft.scope=this.value">
                <option value="read" ${state.prefTokenDraft.scope === "read" ? "selected" : ""}>read</option>
                <option value="propose" ${state.prefTokenDraft.scope === "propose" ? "selected" : ""}>propose</option>
              </select>
            </label>
            <label class="text-[11px] text-[#8b96a5]">Expiry
              <select class="so-select mt-1" data-onchange="state.prefTokenDraft.expires=this.value">
                ${["8h", "24h", "7d", "30d"].map((value) => `<option value="${value}" ${state.prefTokenDraft.expires === value ? "selected" : ""}>${value}</option>`).join("")}
              </select>
            </label>
            <button type="button" class="so-btn so-btn-blue" data-onclick="createExampleToken()">Create example token</button>
          </div>
          <table class="so-table">
            <thead><tr><th>Name</th><th>Scope</th><th>Expiry</th><th>Status</th><th></th></tr></thead>
            <tbody>
              ${tokens
                .map(
                  (token) => `<tr>
                    <td class="font-semibold text-[#5c9ce6]">${esc(token.name)}</td>
                    <td>${esc(token.scope)}</td>
                    <td>${esc(token.expires)}</td>
                    <td>${esc(token.status)}</td>
                    <td>${token.status === "Revoked" ? "" : `<button type="button" class="so-btn so-btn-sm" data-onclick="revokeExampleToken('${esc(token.id)}')">Revoke example</button>`}</td>
                  </tr>`,
                )
                .join("")}
            </tbody>
          </table>
          `
              : `<button type="button" class="so-btn" data-onclick="setTokenExampleMode(true)">Show token example data</button>`
          }
        </div>
      </div>`;
  }

  function renderOrganization(root) {
    governanceEditor.synchronize();
    if (state.settingsSnapshot !== identityKey()) state.settingsLoaded = false;
    if (!state.settingsLoaded || state.settingsLoading) {
      void load();
      root.innerHTML =
        '<div class="so-panel p-3" role="status">Reading the current organization snapshot…</div>';
      return;
    }
    const tab = state.orgTab || "org";
    const tabs = [
      ["org", "Organizations"],
      ["teams", "Teams and roles"],
      ["policies", "Approval policies"],
      ["health", "Control plane"],
      ["identity", "Identity provider"],
      ["keys", "Signing keys"],
    ];
    const teams = state.orgTeams || [];
    const grants = state.orgGrants || [];
    const tenant = consoleTenant();
    let body = "";
    if (tab === "org") {
      body = `<section data-org-snapshot><h3>${esc(tenant.displayName || tenant.id || "This Hub")}</h3><p>${esc(tenant.id || "Tenant identity unavailable")}</p><p>Organization metadata and teams are read only. Policies and grants have separate confirmed editors; stored grants are not a complete effective-access calculation.</p><button type="button" class="so-btn" data-onclick="switchScreen('adm-platform-status')">Open platform measurements</button></section>`;
    } else if (tab === "teams") {
      body = `
        <div class="flex justify-end mb-2">${note(state.orgTeamsLive && state.orgGrantsLive, "Directory", true)}</div>
        <p data-org-directory-status>${state.orgTeamsLive ? `${teams.length} stored teams returned.` : "Team directory unavailable for this session."}</p>
        <table class="so-table">
          <thead><tr><th>Team</th><th>User memberships</th><th>Group memberships</th></tr></thead>
          <tbody>${teams.map((team) => `<tr><td class="font-semibold text-[#5c9ce6]">${esc(team.displayName || team.name)}</td><td>${esc(team.users)}</td><td>${esc(team.groups)}</td></tr>`).join("")}</tbody>
        </table>
        <div class="text-[13px] font-semibold text-white mt-3">Stored roles and explicit operations</div><p>${state.orgGrantsLive ? `${grants.length} stored grants returned.` : "Resource grants unavailable for this session."}</p>
        <table class="so-table">
          <thead><tr><th>Team</th><th>Resource</th><th>Roles</th><th>Operations</th></tr></thead>
          <tbody>${grants.map((grant) => `<tr><td>${esc(grant.team)}</td><td class="code-id">${esc(grant.resourceRid)}</td><td>${esc(grant.roles)}</td><td>${esc(grant.operations)}</td></tr>`).join("")}</tbody>
        </table>${governanceEditor.markup("grant", grants, state.orgGrantsLive)}`;
    } else if (tab === "policies") {
      const policies = state.approvalPolicies || [];
      body = policies.length
        ? `<div class="flex justify-end mb-2">${note(state.approvalPoliciesLive, "Policy", true)}</div>
          <table class="so-table">
            <thead><tr><th>Policy</th><th>Rule</th><th>State</th><th>Revision</th><th>Updated</th></tr></thead>
            <tbody>${policies
              .map(
                (policy) => `<tr>
                  <td><div class="font-semibold">${esc(policy.displayName || policy.id)}</div><div class="code-id text-[10.5px]">${esc(policy.id)}</div></td>
                  <td class="code-id text-[11px] break-all">${esc(policy.expression || "")}</td>
                  <td><span class="kit-badge kit-badge-${policy.enabled ? "ok" : "info"}">${typeof policy.enabled === "boolean" ? (policy.enabled ? "Enabled" : "Disabled") : "Unknown"}</span></td>
                  <td>${esc(policy.revision ?? "")}</td>
                  <td class="code-id text-[11px]">${esc(policy.updatedAt || "")}</td>
                </tr>`,
              )
              .join("")}</tbody>
          </table>
          `
        : `<div class="text-[12px] text-[#8b96a5]">${state.approvalPoliciesLive ? "No stored approval policies were returned; other review requirements can still apply." : "Hub did not return approval policies."}</div>`;
      body += governanceEditor.markup(
        "policy",
        policies,
        state.approvalPoliciesLive,
      );
    } else if (tab === "health") {
      const ready = state.hubReady || "unknown";
      body = `
        <div class="grid gap-3 lg:grid-cols-2">
          <div class="so-card p-3 space-y-1">
            <div class="text-[11px] uppercase tracking-[0.08em] text-[#8b96a5]">Hub readiness</div>
            <div><span class="kit-badge kit-badge-${ready === "ok" ? "ok" : "warn"}">${esc(ready)}</span></div>
            <div class="text-[11px] text-[#8b96a5]">From <span class="code-id">/health/ready</span> when this page loaded.</div>
          </div>
          <div class="so-card p-3 space-y-1">
            <div class="text-[11px] uppercase tracking-[0.08em] text-[#8b96a5]">Your session</div>
            <div class="text-[12px]">Signed in as <span class="code-id">${esc(state.sessionName || "unavailable")}</span>${state.sessionDelegated ? " (delegated)" : ""}</div>
            <div class="text-[11px] text-[#8b96a5]">Teams: ${esc((state.sessionTeams || []).join(", ") || (state.sessionAvailable ? "none recorded" : "unavailable"))}</div>
          </div>
        </div>
        ${capabilityTableMarkup(state.capabilities, esc)}`;
    } else if (tab === "identity") {
      const providers = state.identityProviders || [];
      body = !state.identityAvailable
        ? `<p>Identity provider metadata is unavailable.</p>`
        : state.authEnabled
          ? providers
              .map(
                (provider) => `<div class="so-card p-3">
                <div class="text-[12.5px] font-semibold text-white">${esc(provider.name || "Operator identity")}</div>
                <div class="text-[11px] text-[#8b96a5] mt-1">Issuer ${esc(provider.issuer || "not published")}</div>
                <div class="text-[11px] text-[#8b96a5]">Client ${esc(provider.clientId || "not published")}</div>
                <div class="text-[11px] text-[#6d7885] mt-2">Read only. This screen does not change the provider.</div>
              </div>`,
              )
              .join("")
          : `<div class="text-[12px] text-[#8b96a5]">No identity provider is published by this Hub.</div>`;
    } else {
      const keys = state.signingKeys || [];
      body = `
        <div class="flex justify-end mb-2">${note(state.signingLive, "Signer", true)}</div>
        ${
          keys.length
            ? keys
                .map(
                  (key) => `<div class="so-card p-3">
                    <div class="text-[12.5px] font-semibold text-white">${esc(key.id)}</div>
                    <div class="text-[11px] text-[#8b96a5]">${esc(key.algorithm)}</div>
                    <div class="code-id text-[10.5px] text-[#dce4ee] break-all mt-2">${esc(key.publicKey)}</div>
                  </div>`,
                )
                .join("")
            : `<div class="text-[12px] text-[#8b96a5]">Signing-key metadata is unavailable.</div>`
        }
        <div class="text-[11px] text-[#6d7885] mt-2">This is the published signing key, not the full trusted-key store.</div>`;
    }
    root.innerHTML = `
      <div class="so-view-enter space-y-3">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div>
            <div class="text-[15px] font-bold text-white">Organization</div>
            <div class="text-[11.5px] text-[#8b96a5]">Directory, roles, approval policies, control-plane health, the identity provider, and signing keys.</div>
          </div>
          ${segmented(tabs, tab, "setOrganizationTab")}<button type="button" class="so-btn" data-onclick="refreshOrganization()">Refresh snapshot</button>
        </div>
        <div class="so-panel p-3.5">${body}</div>
      </div>`;
    if (tab === "teams") governanceEditor.bind(root, "grant", grants);
    if (tab === "policies")
      governanceEditor.bind(root, "policy", state.approvalPolicies || []);
  }

  function renderSignIn(root) {
    if (!state.identityAvailable) {
      root.innerHTML = `<div class="so-panel p-3" role="status">${state.settingsLoading ? "Reading identity configuration…" : "Identity configuration is unavailable."}</div>`;
      return;
    }
    if (!state.authEnabled) {
      root.innerHTML = `
        <div class="so-view-enter so-panel p-4 max-w-[520px] mx-auto mt-6">
          <div class="text-[15px] font-bold text-white">Sign-in</div>
          <div class="text-[12px] text-[#8b96a5] mt-2">Sign-in is not enabled on this Hub.</div>
        </div>`;
      return;
    }
    const providers = state.identityProviders || [];
    const selected =
      providers.find((provider) => provider.issuer === state.signInIssuer) ||
      providers[0];
    root.innerHTML = `
      <div class="so-view-enter so-panel p-4 max-w-[520px] mx-auto mt-6 space-y-3">
        <div>
          <div class="text-[15px] font-bold text-white">Sign-in</div>
          <div class="text-[12px] text-[#8b96a5] mt-1">Choose a configured provider. This screen does not collect a password.</div>
        </div>
        <label class="block text-[11px] text-[#8b96a5]">Identity provider
          <select class="so-select mt-1 w-full" data-onchange="state.signInIssuer=this.value">
            ${providers.map((provider) => `<option value="${esc(provider.issuer)}" ${selected && selected.issuer === provider.issuer ? "selected" : ""}>${esc(provider.name || provider.issuer)}</option>`).join("")}
          </select>
        </label>
        ${
          state.signInAvailable
            ? `<div class="flex items-center gap-2 flex-wrap">
                <button type="button" class="so-btn so-btn-blue" data-sign-in data-onclick="beginOperatorSignIn()">Continue to the identity provider</button>
                ${state.sessionName ? `<button type="button" class="so-btn" data-sign-out data-onclick="endOperatorSession()">Sign out ${esc(state.sessionName)}</button>` : ""}
              </div>
              ${state.signOutFailed ? `<div class="text-[12px] text-[#f0a3a3]" role="alert">Sign-out did not complete. You are still signed in; try again.</div>` : ""}`
            : `<div class="text-[12px] text-[#8b96a5]" data-sign-in-unavailable>This Hub verifies operator tokens but does not run a browser sign-in. Reach it through your organization's access proxy, or ask the Conduit operator to configure the console's public address.</div>`
        }
      </div>`;
  }

  function renderDataSources(root) {
    ensureDataSources();
    const selected =
      DATA_SOURCES.find((source) => source.id === state.dataSourceId) ||
      DATA_SOURCES[0];
    const rows = DATA_SOURCES.map((source) => {
      const on = source.id === selected.id;
      const seen = sourceLastSeen(source.id);
      const live =
        Boolean(seen) &&
        !/Hub did not|No reported|No import|No packaging/.test(String(seen));
      return `<tr data-onclick="selectDataSource('${esc(source.id)}')" class="cursor-pointer${on ? " is-selected" : ""}">
        <td data-col="Source"><span class="font-semibold text-white">${esc(source.name)}</span></td>
        <td data-col="Endpoint" data-long="1" class="code-id text-[11px] text-[#dce4ee]">${esc(source.endpoint)}</td>
        <td data-col="Cadence" data-long="1" class="text-[11px] text-[#8b96a5]">${esc(source.cadence)}</td>
        <td data-col="Updates" data-long="1" class="text-[11px] text-[#dce4ee]">${esc(source.updates)}</td>
        <td data-col="Last seen" data-long="1">${live ? "" : `<span class="so-pill so-pill-amber">Example data</span> `}<span class="so-pill ${live ? "so-pill-green" : ""}">${esc(seen || "Not reported")}</span></td>
      </tr>`;
    }).join("");
    root.innerHTML = `
      <div class="so-view-enter space-y-3">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div>
            <div class="text-[15px] font-bold text-white">Data sources</div>
            <div class="text-[11.5px] text-[#8b96a5]">How this Hub receives data. Request shapes below are examples. Last seen is a Hub observation only when that read succeeds.</div>
          </div>
          ${note(false, "Request shape")}
        </div>
        <div class="so-panel overflow-hidden">
          <div class="so-table-scroll">
            <table class="so-table so-table-wide">
              <thead><tr><th>Source</th><th>Endpoint</th><th>Cadence</th><th>Updates</th><th>Last seen</th></tr></thead>
              <tbody>${rows}</tbody>
            </table>
          </div>
        </div>
        <div class="so-panel p-3.5 space-y-2">
          <div class="text-[13px] font-semibold text-white">${esc(selected.name)}</div>
          <div class="text-[12px] text-[#8b96a5]">${esc(selected.endpoint)}. ${esc(selected.cadence)}. Updates ${esc(selected.updates).charAt(0).toLowerCase()}${esc(selected.updates).slice(1)}.</div>
          <div class="text-[11px] text-[#eab854]">Example data. This screen does not accept a pasted document.</div>
          <pre class="code-id text-[11px] text-[#dce4ee] whitespace-pre-wrap break-words">${esc(JSON.stringify(selected.payload, null, 2))}</pre>
        </div>
        <div class="text-[11.5px] text-[#8b96a5]">Bundle import is an ordinary submission of a bundle envelope. The Hub records a receipt and opens a change request for each target environment.</div>
      </div>`;
  }

  return {
    screens: [
      {
        id: "adm01-user-prefs",
        num: 35,
        short: "Preferences",
        tabTitle: "Preferences",
        hasSidebar: true,
        showKpis: false,
        subtitle: "PREFERENCES",
      },
      {
        id: "adm02-org-control-panel",
        num: 36,
        short: "Organization",
        tabTitle: "Organization",
        hasSidebar: true,
        showKpis: false,
        subtitle: "ORGANIZATION",
      },
      {
        id: "adm03-auth-signin",
        num: 37,
        short: "Sign-in",
        tabTitle: "Sign-in",
        hasSidebar: true,
        showKpis: false,
        subtitle: "SIGN-IN",
      },
      {
        id: "adm04-data-sources",
        num: 38,
        short: "Data sources",
        tabTitle: "Data sources",
        hasSidebar: true,
        showKpis: false,
        subtitle: "DATA SOURCES",
      },
    ],
    navItems() {
      const items = [
        { label: "Preferences", icon: "settings", id: "adm01-user-prefs" },
        {
          label: "Organization",
          icon: "shield",
          id: "adm02-org-control-panel",
        },
        { label: "Data sources", icon: "cluster", id: "adm04-data-sources" },
      ];
      if (state.authEnabled)
        items.push({ label: "Sign-in", icon: "lock", id: "adm03-auth-signin" });
      return items;
    },
    extendState() {
      Object.assign(state, {
        prefTheme: "dark",
        prefDensity: "compact",
        prefTimezone: "UTC",
        prefTokenExamples: false,
        prefTokens: [],
        prefTokenDraft: { name: "", scope: "read", expires: "8h" },
        orgTab: "org",
        orgTeams: [],
        orgTeamsLive: false,
        orgGrants: [],
        orgGrantsLive: false,
        discoverUsers: true,
        discoverGroups: true,
        environmentBanner: "",
        identityProviders: [],
        authEnabled: false,
        signInAvailable: false,
        signOutFailed: false,
        signInIssuer: "",
        signingKeys: [],
        signingLive: false,
        settingsLoaded: false,
        settingsLoading: false,
        dataSourceId: "spoke-health",
        dataSourceLive: {
          report: "Hub did not return a report",
          imports: "Hub did not return import receipts",
          packaging: "Hub did not return packaging runs",
        },
      });
    },
    renderers: {
      "adm01-user-prefs": renderPreferences,
      "adm02-org-control-panel": renderOrganization,
      "adm03-auth-signin": renderSignIn,
      "adm04-data-sources": renderDataSources,
    },
    applyChrome,
    ensureLoaded() {
      if (!state.settingsLoaded && !state.settingsLoading) load();
    },
    bind(target) {
      target.refreshOrganization = () => {
        state.settingsLoaded = false;
        clearOrganization();
        const pending = load();
        ctx.render();
        return pending;
      };
      target.setPreferenceTheme = (value) => {
        state.prefTheme = value;
        ctx.render();
      };
      target.setPreferenceDensity = (value) => {
        state.prefDensity = value;
        ctx.render();
      };
      target.setPreferenceTimezone = (value) => {
        state.prefTimezone = value;
        ctx.render();
      };
      target.setOrganizationTab = (value) => {
        state.orgTab = value;
        ctx.render();
      };
      target.setEnvironmentBanner = (value) => {
        state.environmentBanner = value;
        applyChrome();
      };
      target.setTokenExampleMode = (enabled) => {
        state.prefTokenExamples = enabled === true;
        state.prefTokens = state.prefTokenExamples
          ? EXAMPLE_TOKENS.map((token) => ({ ...token }))
          : [];
        state.prefTokenDraft = { name: "", scope: "read", expires: "8h" };
        ctx.render();
      };
      target.createExampleToken = () => {
        if (!state.prefTokenExamples) return;
        const name = (state.prefTokenDraft.name || "").trim();
        if (!name) return;
        state.prefTokens = [
          {
            id: `tok-example-${state.prefTokens.length + 1}`,
            name,
            scope: state.prefTokenDraft.scope,
            expires: state.prefTokenDraft.expires,
            status: "Active",
          },
          ...state.prefTokens,
        ];
        state.prefTokenDraft.name = "";
        ctx.render();
      };
      target.revokeExampleToken = (id) => {
        if (!state.prefTokenExamples) return;
        state.prefTokens = state.prefTokens.map((token) =>
          token.id === id ? { ...token, status: "Revoked" } : token,
        );
        ctx.render();
      };
      target.selectDataSource = (id) => {
        state.dataSourceId = id;
        ctx.render();
      };
      // The Hub runs the sign-in: it holds the state, nonce, PKCE verifier
      // and client secret. The console only navigates to it, naming the view
      // to come back to.
      target.beginOperatorSignIn = () => {
        if (!state.signInAvailable) return;
        installHubSession(window).signIn();
      };
      target.endOperatorSession = () => {
        if (!state.signInAvailable) return;
        installHubSession(window)
          .signOut()
          .catch(() => {
            state.signOutFailed = true;
            ctx.render();
          });
      };
    },
  };
}

/* Settings screens. Directory rows come from the Hub. Tokens and request shapes are examples. */

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

const EXAMPLE_ORGS = [
  { id: "fleet-ops", name: "Fleet operations", members: 18, mode: "Internal" },
  { id: "partner-portal", name: "Partner portal", members: 6, mode: "Limited" },
];

const EXAMPLE_TEAMS = [
  { name: "release-managers", displayName: "Release managers", members: 4 },
  { name: "fleet-approvers", displayName: "Fleet approvers", members: 3 },
  { name: "security-review", displayName: "Security review", members: 2 },
];

const EXAMPLE_GRANTS = [
  {
    team: "release-managers",
    resourceRid: "environment/edge-west-02",
    roles: "operator",
    operations: "read, propose",
  },
  {
    team: "fleet-approvers",
    resourceRid: "environment/edge-west-02",
    roles: "approver",
    operations: "read, operate",
  },
];

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
    headers: { accept: "application/json" },
  });
  if (!response.ok) throw new Error(String(response.status));
  return response.json();
}

export function createSettings(ctx) {
  const state = ctx.state;

  function note(live, label) {
    return `<span class="bp6-compound-tag"><span class="bp6-compound-left">${esc(label)}</span><span class="bp6-compound-right" style="background:#1a2430;color:#7cb4ff">${live ? "Hub" : "Example data"}</span></span>`;
  }

  async function load() {
    if (state.settingsLoaded || state.settingsLoading) return;
    state.settingsLoading = true;
    try {
      const identity = await readJSON("/v1/operator-identity");
      state.authEnabled = Boolean(identity.enabled);
      state.identityProviders = Array.isArray(identity.providers)
        ? identity.providers
        : [];
    } catch {
      state.authEnabled = false;
      state.identityProviders = [];
    }
    try {
      const body = await readJSON("/v1/teams");
      const teams = Array.isArray(body.teams) ? body.teams : [];
      if (teams.length) {
        state.orgTeamsLive = true;
        state.orgTeams = teams.map((team) => ({
          name: team.name,
          displayName: team.displayName || team.name,
          members:
            (team.memberUsers || []).length + (team.memberGroups || []).length,
        }));
      }
    } catch {
      state.orgTeamsLive = false;
    }
    try {
      const body = await readJSON("/v1/resource-grants");
      const grants = Array.isArray(body.resourceGrants)
        ? body.resourceGrants
        : [];
      if (grants.length) {
        state.orgGrantsLive = true;
        state.orgGrants = grants.map((grant) => ({
          team: grant.team,
          resourceRid: grant.resourceRid,
          roles: (grant.roles || []).join(", "),
          operations: (grant.operations || []).join(", "),
        }));
      }
    } catch {
      state.orgGrantsLive = false;
    }
    try {
      const body = await readJSON("/v1/approval-policies");
      state.approvalPolicies = Array.isArray(body.approvalPolicies)
        ? body.approvalPolicies
        : [];
      state.approvalPoliciesLive = true;
    } catch {
      state.approvalPolicies = [];
      state.approvalPoliciesLive = false;
    }
    try {
      const ready = await readJSON("/health/ready");
      state.hubReady = ready?.status || "unknown";
    } catch (error) {
      state.hubReady = `not ready (${error instanceof Error ? error.message : "no answer"})`;
    }
    try {
      const session = await readJSON("/v1/session");
      state.sessionApplications = Array.isArray(session.applications)
        ? session.applications
        : [];
      state.sessionTeams = session?.effective?.teams || [];
      state.sessionDelegated = Boolean(session?.delegated);
    } catch {
      state.sessionApplications = [];
    }
    try {
      const key = await readJSON("/v1/bundles/signing-key");
      if (key && key.id) {
        state.signingLive = true;
        state.signingKeys = [
          {
            id: key.id,
            algorithm: key.algorithm || "Ed25519",
            publicKey: key.publicKey || "",
          },
        ];
      }
    } catch {
      state.signingLive = false;
    }
    await loadDataSources();
    state.settingsLoaded = true;
    state.settingsLoading = false;
    ctx.render();
  }

  async function loadDataSources() {
    state.dataSourceLive = {
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
      state.dataSourceLive.report = newest
        ? `${newest.seen} on ${newest.environment || "an environment"} (${newest.entities} entities)`
        : "No reported observation on the environments this caller can read";
    } catch {
      state.dataSourceLive.report = "Hub did not return a report";
    }
    try {
      const body = await readJSON("/v1/bundle-imports");
      const imports = Array.isArray(body.imports) ? body.imports : [];
      if (!imports.length) {
        state.dataSourceLive.imports = "No import receipt";
      } else {
        const latest = imports.reduce((best, item) =>
          !best ||
          String(item.requestedAt || "") > String(best.requestedAt || "")
            ? item
            : best,
        );
        state.dataSourceLive.imports =
          `${latest.status || "recorded"} ${latest.requestedAt || ""} ${latest.bundleId || latest.id || ""}`.trim();
      }
    } catch {
      state.dataSourceLive.imports = "Hub did not return import receipts";
    }
    try {
      const body = await readJSON("/v1/packaging-runs");
      const items = Array.isArray(body.items) ? body.items : [];
      if (!items.length) {
        state.dataSourceLive.packaging = "No packaging run";
      } else {
        const latest = items[0];
        state.dataSourceLive.packaging = `${latest.runId || "run"} phase ${latest.phase || "unknown"}${latest.outcome ? `, outcome ${latest.outcome}` : ""}`;
      }
    } catch {
      state.dataSourceLive.packaging = "Hub did not return packaging runs";
    }
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
          ${note(false, "Tokens")}
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
              <div class="text-[11px] text-[#8b96a5]">This Hub does not issue personal tokens. The rows below are example data.</div>
            </div>
          </div>
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
            <button type="button" class="so-btn so-btn-blue" data-onclick="createExampleToken()">Create token</button>
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
                    <td>${token.status === "Revoked" ? "" : `<button type="button" class="so-btn so-btn-sm" data-onclick="revokeExampleToken('${esc(token.id)}')">Revoke</button>`}</td>
                  </tr>`,
                )
                .join("")}
            </tbody>
          </table>
        </div>
      </div>`;
  }

  function renderOrganization(root) {
    const tab = state.orgTab || "org";
    const tabs = [
      ["org", "Organizations"],
      ["teams", "Teams and roles"],
      ["policies", "Approval policies"],
      ["health", "Control plane"],
      ["identity", "Identity provider"],
      ["keys", "Signing keys"],
    ];
    const teams = state.orgTeamsLive ? state.orgTeams : EXAMPLE_TEAMS;
    const grants = state.orgGrantsLive ? state.orgGrants : EXAMPLE_GRANTS;
    let body = "";
    if (tab === "org") {
      body = `
        <div class="grid gap-3 lg:grid-cols-2">
          <div class="space-y-2">
            ${EXAMPLE_ORGS.map(
              (
                org,
              ) => `<div class="so-card p-3 flex items-center justify-between gap-2">
                <div><div class="text-[12.5px] font-semibold text-white">${esc(org.name)}</div><div class="text-[11px] text-[#8b96a5]">${esc(org.mode)} · ${org.members} members</div></div>
                ${note(false, "Directory")}
              </div>`,
            ).join("")}
          </div>
          <div class="space-y-3">
            <label class="flex items-center gap-2 text-[12px] text-[#dce4ee]"><input type="checkbox" ${state.discoverUsers ? "checked" : ""} data-onchange="state.discoverUsers=this.checked" /> Let members find other people</label>
            <label class="flex items-center gap-2 text-[12px] text-[#dce4ee]"><input type="checkbox" ${state.discoverGroups ? "checked" : ""} data-onchange="state.discoverGroups=this.checked" /> Let members find groups</label>
            <label class="block text-[11px] text-[#8b96a5]">Environment banner
              <input class="so-input mt-1" value="${esc(state.environmentBanner)}" placeholder="Optional banner text" data-oninput="setEnvironmentBanner(this.value)" />
            </label>
            <div class="text-[11px] text-[#6d7885]">The banner is plain text. It does not assert a classification.</div>
          </div>
        </div>`;
    } else if (tab === "teams") {
      body = `
        <div class="flex justify-end mb-2">${note(state.orgTeamsLive && state.orgGrantsLive, "Directory")}</div>
        <table class="so-table">
          <thead><tr><th>Team</th><th>Members</th></tr></thead>
          <tbody>${teams.map((team) => `<tr><td class="font-semibold text-[#5c9ce6]">${esc(team.displayName || team.name)}</td><td>${esc(team.members)}</td></tr>`).join("")}</tbody>
        </table>
        <div class="text-[13px] font-semibold text-white mt-3">Roles</div>
        <table class="so-table">
          <thead><tr><th>Team</th><th>Resource</th><th>Roles</th><th>Operations</th></tr></thead>
          <tbody>${grants.map((grant) => `<tr><td>${esc(grant.team)}</td><td class="code-id">${esc(grant.resourceRid)}</td><td>${esc(grant.roles)}</td><td>${esc(grant.operations)}</td></tr>`).join("")}</tbody>
        </table>`;
    } else if (tab === "policies") {
      const policies = state.approvalPolicies || [];
      body = policies.length
        ? `<div class="flex justify-end mb-2">${note(state.approvalPoliciesLive, "Policy")}</div>
          <table class="so-table">
            <thead><tr><th>Policy</th><th>Rule</th><th>State</th><th>Revision</th><th>Updated</th></tr></thead>
            <tbody>${policies
              .map(
                (policy) => `<tr>
                  <td><div class="font-semibold">${esc(policy.displayName || policy.id)}</div><div class="code-id text-[10.5px]">${esc(policy.id)}</div></td>
                  <td class="code-id text-[11px] break-all">${esc(policy.expression || "")}</td>
                  <td><span class="kit-badge kit-badge-${policy.enabled ? "ok" : "info"}">${policy.enabled ? "Enabled" : "Disabled"}</span></td>
                  <td>${esc(policy.revision ?? "")}</td>
                  <td class="code-id text-[11px]">${esc(policy.updatedAt || "")}</td>
                </tr>`,
              )
              .join("")}</tbody>
          </table>
          <div class="text-[11px] text-[#8b96a5] mt-2">Read only. A policy change goes through the Hub's policy API and its own review.</div>`
        : `<div class="text-[12px] text-[#8b96a5]">${state.approvalPoliciesLive ? "No approval policy is configured." : "Hub did not return approval policies."}</div>`;
    } else if (tab === "health") {
      const apps = state.sessionApplications || [];
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
            <div class="text-[12px]">Signed in as <span class="code-id">${esc(state.operatorName || "Operator")}</span>${state.sessionDelegated ? " (delegated)" : ""}</div>
            <div class="text-[11px] text-[#8b96a5]">Teams: ${esc((state.sessionTeams || []).join(", ") || "none")}</div>
          </div>
        </div>
        <table class="so-table mt-3">
          <thead><tr><th>Application</th><th>State for this session</th></tr></thead>
          <tbody>${
            apps.length
              ? apps
                  .map(
                    (app) =>
                      `<tr><td class="code-id">${esc(app.id)}</td><td><span class="kit-badge kit-badge-${app.enabled ? "ok" : "info"}">${app.enabled ? "Enabled" : "Off"}</span></td></tr>`,
                  )
                  .join("")
              : `<tr><td colspan="2" class="text-[#8b96a5]">Hub did not return session applications.</td></tr>`
          }</tbody>
        </table>`;
    } else if (tab === "identity") {
      const providers = state.identityProviders || [];
      body = state.authEnabled
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
        <div class="flex justify-end mb-2">${note(state.signingLive, "Signer")}</div>
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
            : `<div class="text-[12px] text-[#8b96a5]">No signing key is published.</div>`
        }
        <div class="text-[11px] text-[#6d7885] mt-2">The trust list shown here is the key material this Hub publishes. Nothing else is added.</div>`;
    }
    root.innerHTML = `
      <div class="so-view-enter space-y-3">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div>
            <div class="text-[15px] font-bold text-white">Organization</div>
            <div class="text-[11.5px] text-[#8b96a5]">Directory, roles, approval policies, control-plane health, the identity provider, and signing keys.</div>
          </div>
          ${segmented(tabs, tab, "setOrganizationTab")}
        </div>
        <div class="so-panel p-3.5">${body}</div>
      </div>`;
  }

  function renderSignIn(root) {
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
          selected && selected.authorizationEndpoint
            ? `<button type="button" class="so-btn so-btn-blue" data-onclick="beginOperatorSignIn()">Continue to the identity provider</button>`
            : `<div class="text-[12px] text-[#8b96a5]">This Hub did not publish a browser sign-in address.</div>`
        }
      </div>`;
  }

  function renderDataSources(root) {
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
        prefTokens: EXAMPLE_TOKENS.map((token) => ({ ...token })),
        prefTokenDraft: { name: "", scope: "read", expires: "8h" },
        orgTab: "org",
        orgTeams: EXAMPLE_TEAMS,
        orgTeamsLive: false,
        orgGrants: EXAMPLE_GRANTS,
        orgGrantsLive: false,
        discoverUsers: true,
        discoverGroups: true,
        environmentBanner: "",
        identityProviders: [],
        authEnabled: false,
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
      target.createExampleToken = () => {
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
        state.prefTokens = state.prefTokens.map((token) =>
          token.id === id ? { ...token, status: "Revoked" } : token,
        );
        ctx.render();
      };
      target.selectDataSource = (id) => {
        state.dataSourceId = id;
        ctx.render();
      };
      target.beginOperatorSignIn = () => {
        const provider =
          (state.identityProviders || []).find(
            (item) => item.issuer === state.signInIssuer,
          ) || (state.identityProviders || [])[0];
        if (!provider || !provider.authorizationEndpoint) return;
        const url = new URL(provider.authorizationEndpoint);
        if (provider.clientId)
          url.searchParams.set("client_id", provider.clientId);
        url.searchParams.set("redirect_uri", `${location.origin}/console/`);
        url.searchParams.set("response_type", "code");
        url.searchParams.set("scope", "openid");
        location.assign(url.toString());
      };
    },
  };
}

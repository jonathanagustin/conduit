/* Settings screens. Live rows come from the Hub; tokens stay example data. */

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
    state.settingsLoaded = true;
    state.settingsLoading = false;
    ctx.render();
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
        return `<button type="button" role="radio" aria-checked="${on ? "true" : "false"}" onclick="${handler}('${value}')" class="bp6-seg-btn${on ? " is-selected" : ""}">${esc(label)}</button>`;
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
              <input id="pref-token-name" class="so-input mt-1" value="${esc(state.prefTokenDraft.name)}" oninput="state.prefTokenDraft.name=this.value" />
            </label>
            <label class="text-[11px] text-[#8b96a5]">Scope
              <select class="so-select mt-1" onchange="state.prefTokenDraft.scope=this.value">
                <option value="read" ${state.prefTokenDraft.scope === "read" ? "selected" : ""}>read</option>
                <option value="propose" ${state.prefTokenDraft.scope === "propose" ? "selected" : ""}>propose</option>
              </select>
            </label>
            <label class="text-[11px] text-[#8b96a5]">Expiry
              <select class="so-select mt-1" onchange="state.prefTokenDraft.expires=this.value">
                ${["8h", "24h", "7d", "30d"].map((value) => `<option value="${value}" ${state.prefTokenDraft.expires === value ? "selected" : ""}>${value}</option>`).join("")}
              </select>
            </label>
            <button type="button" class="so-btn so-btn-blue" onclick="createExampleToken()">Create token</button>
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
                    <td>${token.status === "Revoked" ? "" : `<button type="button" class="so-btn so-btn-sm" onclick="revokeExampleToken('${esc(token.id)}')">Revoke</button>`}</td>
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
            <label class="flex items-center gap-2 text-[12px] text-[#dce4ee]"><input type="checkbox" ${state.discoverUsers ? "checked" : ""} onchange="state.discoverUsers=this.checked" /> Let members find other people</label>
            <label class="flex items-center gap-2 text-[12px] text-[#dce4ee]"><input type="checkbox" ${state.discoverGroups ? "checked" : ""} onchange="state.discoverGroups=this.checked" /> Let members find groups</label>
            <label class="block text-[11px] text-[#8b96a5]">Environment banner
              <input class="so-input mt-1" value="${esc(state.environmentBanner)}" placeholder="Optional banner text" oninput="setEnvironmentBanner(this.value)" />
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
            <div class="text-[11.5px] text-[#8b96a5]">Directory, roles, the configured identity provider, and signing keys.</div>
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
          <select class="so-select mt-1 w-full" onchange="state.signInIssuer=this.value">
            ${providers.map((provider) => `<option value="${esc(provider.issuer)}" ${selected && selected.issuer === provider.issuer ? "selected" : ""}>${esc(provider.name || provider.issuer)}</option>`).join("")}
          </select>
        </label>
        ${
          selected && selected.authorizationEndpoint
            ? `<button type="button" class="so-btn so-btn-blue" onclick="beginOperatorSignIn()">Continue to the identity provider</button>`
            : `<div class="text-[12px] text-[#8b96a5]">This Hub did not publish a browser sign-in address.</div>`
        }
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
    ],
    navItems() {
      const items = [
        { label: "Preferences", icon: "settings", id: "adm01-user-prefs" },
        {
          label: "Organization",
          icon: "shield",
          id: "adm02-org-control-panel",
        },
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
      });
    },
    renderers: {
      "adm01-user-prefs": renderPreferences,
      "adm02-org-control-panel": renderOrganization,
      "adm03-auth-signin": renderSignIn,
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

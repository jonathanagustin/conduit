// CONDUIT-INVENTED: console.GovernanceEditors
// Displaced-by: An authoritative console workflow for native approval-policy and resource-grant edits.
// The native Hub remains the authority; review is confirmation, not approval.
const copy = (value) => JSON.parse(JSON.stringify(value));
const esc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
const title = (kind) =>
  kind === "policy" ? "approval policy" : "resource grant";
const strings = (value) =>
  Array.isArray(value) && value.every((item) => typeof item === "string");
const lines = (value) => [
  ...new Set(
    String(value)
      .split("\n")
      .map((item) => item.trim())
      .filter(Boolean),
  ),
];

function draftOf(kind, record = {}) {
  if (kind === "policy")
    return {
      id: record.id || "",
      displayName: record.displayName || "",
      expression: record.expression || "",
      enabled: record.enabled ?? true,
      variables: Object.entries(record.variables || {}).map(
        ([name, variable]) => ({ name, ...copy(variable) }),
      ),
    };
  return {
    resourceRid: record.resourceRid || "",
    team: record.team || "",
    relationship: record.relationship || "",
    roles: (record.roles || []).join("\n"),
    operations: (record.operations || []).join("\n"),
  };
}

export function governanceRequest(kind, draft, revision) {
  if (!Number.isSafeInteger(revision) || revision < 0)
    throw new Error("Reload the record before editing its revision.");
  if (kind === "policy") {
    if (
      !draft.id.trim() ||
      !draft.displayName.trim() ||
      !draft.expression.trim()
    )
      throw new Error("Policy ID, name and expression are required.");
    const variables = Object.fromEntries(
      draft.variables.map((variable) => {
        if (
          !variable.name.trim() ||
          !["STRING", "STRING_LIST"].includes(variable.type)
        )
          throw new Error("Every variable needs a name and a supported type.");
        return [
          variable.name,
          variable.type === "STRING"
            ? { type: variable.type, string: variable.string ?? "" }
            : { type: variable.type, list: variable.list || [] },
        ];
      }),
    );
    if (Object.keys(variables).length !== draft.variables.length)
      throw new Error("Variable names must be unique.");
    return {
      method: "PUT",
      path: `/v1/approval-policies/${encodeURIComponent(draft.id)}`,
      body: {
        id: draft.id,
        displayName: draft.displayName,
        expression: draft.expression,
        enabled: draft.enabled,
        variables,
        revision,
      },
    };
  }
  const body = {
    resourceRid: draft.resourceRid,
    team: draft.team,
    relationship: draft.relationship,
    roles: lines(draft.roles),
    operations: lines(draft.operations),
    revision,
  };
  if (
    !body.resourceRid.trim() ||
    !body.team.trim() ||
    !(body.relationship || body.roles.length || body.operations.length)
  )
    throw new Error(
      "Resource, team, and at least one relationship, role or operation are required.",
    );
  return { method: "POST", path: "/v1/resource-grants", body };
}

function validRecord(kind, record, body) {
  if (
    !record ||
    typeof record.id !== "string" ||
    !record.id ||
    record.revision !== body.revision + 1
  )
    return false;
  return kind === "policy"
    ? record.id === body.id &&
        typeof record.displayName === "string" &&
        typeof record.expression === "string" &&
        typeof record.enabled === "boolean"
    : record.resourceRid === body.resourceRid &&
        record.team === body.team &&
        strings(record.roles || []) &&
        strings(record.operations || []);
}

/** One editor survives background paints. Requests and responses stay bound to its identity. */
export function createGovernanceEditor({
  identityKey,
  canPerform,
  render,
  saved,
  fetchImpl = (...args) => fetch(...args),
}) {
  let edit = null;
  const synchronize = () => {
    if (edit && edit.identity !== identityKey()) edit = null;
  };
  const permission = (kind, draft) =>
    canPerform?.(
      "permissions:manage",
      kind === "policy" ? "platform/root" : draft.resourceRid,
    ) || {
      allowed: null,
      reason: "The Hub checks your authority when you save.",
    };
  const open = (kind, record) => {
    synchronize();
    if (edit && ["saving", "loading"].includes(edit.phase)) return;
    edit = {
      kind,
      identity: identityKey(),
      record: copy(record || {}),
      draft: draftOf(kind, record),
      phase: "editing",
      note: "",
      confirmation: null,
    };
    render();
  };
  const change = () => {
    const reviewing = edit.phase === "review";
    edit.confirmation = null;
    edit.note = "";
    edit.phase = "editing";
    if (reviewing) render();
  };
  const update = (key, value) => {
    synchronize();
    if (!edit || ["saving", "loading"].includes(edit.phase)) return;
    const match = /^variable\.(\d+)\.(name|type|string|list)(?:\.(\d+))?$/.exec(
      key,
    );
    let repaint = false;
    if (match) {
      const variable = edit.draft.variables[Number(match[1])];
      if (match[2] === "list") variable.list[Number(match[3])] = value;
      else variable[match[2]] = value;
      repaint = match[2] === "type";
    } else if (Object.hasOwn(edit.draft, key) && key !== "variables") {
      if (edit.record.id && ["id", "team", "resourceRid"].includes(key)) return;
      edit.draft[key] = value;
    }
    change();
    if (repaint) render();
  };
  const cancelReview = () => {
    synchronize();
    if (edit?.phase === "review") {
      edit.phase = "editing";
      edit.confirmation = null;
      render();
    }
  };
  const review = () => {
    synchronize();
    if (!edit || ["saving", "loading"].includes(edit.phase)) return;
    try {
      const request = governanceRequest(
        edit.kind,
        edit.draft,
        edit.record.id ? edit.record.revision : 0,
      );
      if (permission(edit.kind, edit.draft).allowed === false)
        throw new Error(
          "Your session cannot manage permissions on this target.",
        );
      edit.confirmation = {
        wire: JSON.stringify(request.body),
        path: request.path,
        method: request.method,
        draft: JSON.stringify(edit.draft),
      };
      edit.phase = "review";
      edit.note = "";
    } catch (error) {
      edit.note = error.message;
    }
    render();
  };
  const confirm = async () => {
    synchronize();
    if (!edit || edit.phase !== "review" || !edit.confirmation) return;
    const current = edit;
    const request = current.confirmation;
    if (
      request.draft !== JSON.stringify(current.draft) ||
      permission(current.kind, current.draft).allowed === false
    ) {
      current.phase = "editing";
      current.confirmation = null;
      current.note =
        "The draft or your authority changed. Review the changes again.";
      render();
      return;
    }
    current.phase = "saving";
    render();
    try {
      const response = await fetchImpl(request.path, {
        method: request.method,
        credentials: "same-origin",
        headers: {
          "content-type": "application/json",
          accept: "application/json",
        },
        body: request.wire,
      });
      const record = await response.json().catch(() => ({}));
      if (!response.ok)
        throw new Error(
          response.status === 409
            ? "This record changed on the Hub. Reload it before reviewing another update."
            : String(
                record.error ||
                  `The Hub refused the update (${response.status}).`,
              ),
        );
      if (!validRecord(current.kind, record, JSON.parse(request.wire)))
        throw new Error(
          "The Hub did not return the expected saved record. Reload to check its state.",
        );
      synchronize();
      if (edit !== current) return;
      current.record = copy(record);
      current.draft = draftOf(current.kind, record);
      current.phase = "saved";
      current.note = `The Hub saved ${title(current.kind)} ${record.id}, revision ${record.revision}.`;
      saved(current.kind, copy(record));
    } catch (error) {
      synchronize();
      if (edit !== current) return;
      current.phase = "failed";
      current.note = `${error.message} No retry was sent. Reload to check the current record before saving again.`;
    }
    if (edit === current) {
      current.confirmation = null;
      render();
    }
  };
  const reload = async () => {
    synchronize();
    if (!edit || edit.phase === "saving" || edit.phase === "loading") return;
    const current = edit;
    current.phase = "loading";
    current.confirmation = null;
    render();
    try {
      const path =
        current.kind === "policy"
          ? "/v1/approval-policies"
          : `/v1/resource-grants?resourceRid=${encodeURIComponent(current.draft.resourceRid)}`;
      const response = await fetchImpl(path, {
        credentials: "same-origin",
        headers: { accept: "application/json" },
      });
      if (!response.ok)
        throw new Error("The Hub did not return the current record.");
      const body = await response.json();
      const records =
        body[current.kind === "policy" ? "approvalPolicies" : "resourceGrants"];
      if (!Array.isArray(records))
        throw new Error("The Hub did not return the current records.");
      const record = records.find((row) =>
        current.kind === "policy"
          ? row.id === current.draft.id
          : row.resourceRid === current.draft.resourceRid &&
            row.team === current.draft.team,
      );
      if (!record)
        throw new Error(
          "The record is no longer listed. Close this editor and refresh the snapshot.",
        );
      synchronize();
      if (edit !== current) return;
      current.phase = "editing";
      open(current.kind, record);
    } catch (error) {
      synchronize();
      if (edit !== current) return;
      current.phase = "failed";
      current.note = error.message;
      render();
    }
  };

  function markup(kind, records, available) {
    synchronize();
    if (!available)
      return '<p class="kit-muted">Editing is unavailable until the Hub returns the records.</p>';
    const newPermission = permission(kind, draftOf(kind));
    const switchingBlocked = edit && ["saving", "loading"].includes(edit.phase);
    const button = (record, index) => {
      const allowed = permission(kind, draftOf(kind, record));
      return `<button type="button" class="kit-btn kit-btn-sm" data-governance-open="${index}"${switchingBlocked || allowed.allowed === false ? " disabled" : ""} title="${esc(allowed.reason)}">Edit ${esc(kind === "policy" ? record.displayName || record.id : `${record.team} on ${record.resourceRid}`)}</button>${allowed.allowed === false ? `<span class="kit-muted">${esc(allowed.reason)}</span>` : ""}`;
    };
    let html = `<section class="kit-stack" data-governance-kind="${kind}"><p class="kit-muted">Conduit-native ${title(kind)} management. The Hub checks your current authority and the record revision when you save.</p><div class="flex flex-wrap gap-2"><button type="button" class="kit-btn" data-governance-new${switchingBlocked || newPermission.allowed === false ? " disabled" : ""}>New ${title(kind)}</button>${records.map(button).join("")}</div>${newPermission.allowed === false ? `<p class="kit-muted">${esc(newPermission.reason)}</p>` : ""}`;
    if (!edit || edit.kind !== kind) return `${html}</section>`;
    const { draft, record, phase } = edit;
    const busy = phase === "saving" || phase === "loading";
    const fixed = Boolean(record.id);
    const input = (key, label, value, readonly = false, multiline = false) => {
      const id = `governance-field-${key}`;
      return `<div class="kit-stack"><label for="${id}">${label}</label>${multiline ? `<textarea id="${id}" class="kit-input" data-governance-field="${key}"${readonly ? " readonly" : ""}>${esc(value)}</textarea>` : `<input id="${id}" class="kit-input" data-governance-field="${key}" value="${esc(value)}"${readonly ? " readonly" : ""}>`}</div>`;
    };
    html += `<section class="kit-panel kit-pad kit-stack" data-governance-editor aria-label="Edit ${title(kind)}"><h3>${fixed ? "Edit" : "New"} ${title(kind)}</h3><p>Expected revision: ${esc(record.revision || 0)}. ${kind === "policy" ? "Policies apply across this Hub." : "The resource and team identify this grant."}</p><fieldset class="kit-stack"${busy ? " disabled" : ""}>`;
    if (kind === "policy") {
      html +=
        input("id", "Policy ID", draft.id, fixed) +
        input("displayName", "Policy name", draft.displayName) +
        input(
          "expression",
          "Review rule (CEL expression)",
          draft.expression,
          false,
          true,
        );
      html += `<label><input type="checkbox" data-governance-field="enabled"${draft.enabled ? " checked" : ""}> Enabled</label><h4>Policy variables</h4>`;
      draft.variables.forEach((variable, index) => {
        html += `<fieldset class="kit-stack" data-governance-variable="${index}"><legend>Variable ${index + 1}</legend>${input(`variable.${index}.name`, "Variable name", variable.name)}<label for="governance-type-${index}">Variable type</label><select id="governance-type-${index}" class="kit-input" data-governance-field="variable.${index}.type"><option value="STRING"${variable.type === "STRING" ? " selected" : ""}>Text</option><option value="STRING_LIST"${variable.type === "STRING_LIST" ? " selected" : ""}>Text list</option></select>`;
        if (variable.type === "STRING")
          html += input(
            `variable.${index}.string`,
            "Text value",
            variable.string || "",
            false,
            true,
          );
        else
          (variable.list || []).forEach((value, item) => {
            html += `${input(`variable.${index}.list.${item}`, `List value ${item + 1}`, value, false, true)}<button type="button" class="kit-btn kit-btn-sm" data-governance-remove-value="${index}.${item}">Remove list value ${item + 1}</button>`;
          });
        html += `${variable.type === "STRING_LIST" ? `<button type="button" class="kit-btn kit-btn-sm" data-governance-add-value="${index}">Add list value</button>` : ""}<button type="button" class="kit-btn kit-btn-sm" data-governance-remove-variable="${index}">Remove variable ${index + 1}</button></fieldset>`;
      });
      html +=
        '<button type="button" class="kit-btn kit-btn-sm" data-governance-add-variable>Add variable</button>';
    } else {
      html +=
        input("resourceRid", "Resource", draft.resourceRid, fixed) +
        input("team", "Team", draft.team, fixed) +
        input("roles", "Roles (one per line)", draft.roles, false, true) +
        input(
          "operations",
          "Operations (one per line)",
          draft.operations,
          false,
          true,
        );
      html += `<label for="governance-relationship">Ownership relationship</label><select id="governance-relationship" class="kit-input" data-governance-field="relationship">${[
        ["", "None"],
        ["OWNER", "Owner"],
        ["CO_OWNER", "Co-owner"],
      ]
        .map(
          ([value, label]) =>
            `<option value="${value}"${draft.relationship === value ? " selected" : ""}>${label}</option>`,
        )
        .join(
          "",
        )}</select><p class="kit-muted">Removing the entire grant is not supported here. Keep at least one relationship, role or operation.</p>`;
    }
    const allowed = permission(kind, draft);
    html += `</fieldset><p class="kit-muted">${esc(allowed.reason)}</p>${edit.note ? `<p data-governance-note role="${phase === "failed" ? "alert" : "status"}">${esc(edit.note)}</p>` : ""}`;
    if (phase === "review")
      html += `<p data-governance-review>Review these changes to ${esc(draft.id || `${draft.team} on ${draft.resourceRid}`)}. Saving changes who can review or manage resources; it does not approve a delivery.</p><button type="button" class="kit-btn kit-btn-primary" data-governance-confirm>Save ${kind === "policy" ? "policy" : "grant"}</button><button type="button" class="kit-btn" data-governance-cancel-review>Cancel</button>`;
    else
      html += `<button type="button" class="kit-btn kit-btn-primary" data-governance-review-changes${busy || phase === "failed" || allowed.allowed === false ? " disabled" : ""}>${phase === "saving" ? "Saving…" : "Review changes"}</button>`;
    html += `<button type="button" class="kit-btn" data-governance-reload${busy ? " disabled" : ""}>Reload and discard draft</button><button type="button" class="kit-btn" data-governance-close${busy ? " disabled" : ""}>Close editor</button></section></section>`;
    return html;
  }

  function bind(root, kind, records) {
    const host = root.querySelector?.(`[data-governance-kind="${kind}"]`);
    if (!host) return;
    const boundIdentity = identityKey();
    const guard = (handler) => (event) => {
      if (boundIdentity !== identityKey()) {
        synchronize();
        render();
        return;
      }
      handler(event);
    };
    const on = (selector, handler) =>
      host
        .querySelectorAll(selector)
        .forEach((node) => node.addEventListener("click", guard(handler)));
    on("[data-governance-new]", () => open(kind));
    on("[data-governance-open]", (event) =>
      open(kind, records[Number(event.currentTarget.dataset.governanceOpen)]),
    );
    on("[data-governance-review-changes]", review);
    on("[data-governance-confirm]", () => {
      void confirm();
    });
    on("[data-governance-reload]", () => {
      void reload();
    });
    on("[data-governance-close]", () => {
      synchronize();
      if (edit && ["saving", "loading"].includes(edit.phase)) return;
      edit = null;
      render();
    });
    on("[data-governance-cancel-review]", cancelReview);
    host.querySelectorAll("[data-governance-field]").forEach((node) =>
      node.addEventListener(
        "input",
        guard(() =>
          update(
            node.dataset.governanceField,
            node.type === "checkbox" ? node.checked : node.value,
          ),
        ),
      ),
    );
    on("[data-governance-add-variable]", () => {
      synchronize();
      if (!edit) return;
      edit.draft.variables.push({ name: "", type: "STRING", string: "" });
      change();
      render();
    });
    on("[data-governance-remove-variable]", (event) => {
      synchronize();
      if (!edit) return;
      edit.draft.variables.splice(
        Number(event.currentTarget.dataset.governanceRemoveVariable),
        1,
      );
      change();
      render();
    });
    on("[data-governance-add-value]", (event) => {
      synchronize();
      if (!edit) return;
      const variable =
        edit.draft.variables[
          Number(event.currentTarget.dataset.governanceAddValue)
        ];
      (variable.list ||= []).push("");
      change();
      render();
    });
    on("[data-governance-remove-value]", (event) => {
      synchronize();
      if (!edit) return;
      const [variable, item] = event.currentTarget.dataset.governanceRemoveValue
        .split(".")
        .map(Number);
      edit.draft.variables[variable].list.splice(item, 1);
      change();
      render();
    });
  }
  return {
    markup,
    bind,
    synchronize,
    open,
    update,
    review,
    confirm,
    cancelReview,
    reload,
  };
}

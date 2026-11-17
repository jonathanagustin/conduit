/*
 * Notification inbox, preference editor and contact editor
 * (docs/ui/contracts/notification-inbox.md). Every row is read from the Hub's
 * /v1/notifications routes; nothing here is example data.
 *
 * The console never names an owner: the Hub resolves the caller and the screen
 * reports the owner it resolved. A channel is always stated, because the Hub
 * refuses a request without one.
 */
import { CHANNELS, CONTACT_KINDS, EVENT_TYPES } from "./hub-vocabulary.js";
import { escapeHtml as esc } from "./palette.js";
import { readPref, writePref } from "./shell.js";

export const NOTIFICATIONS_SCREEN = "ntf-inbox";
export const DEFAULT_CHANNEL = "IN_APP";
/** The Hub's ceiling is 100 (application.MaximumLimit); the screen widens to it. */
export const INBOX_LIMITS = Object.freeze([25, 50, 100]);
const SEEN_KEY = "conduit.console.notifications.seenAt";

const TYPE_LABELS = Object.freeze({
  BLUE_GREEN_UPGRADE_FAILED: "Blue-green upgrade failed",
  CHANGE_REQUEST_PENDING_APPROVAL: "Change request pending approval",
  INSTALLED_PRODUCT_RELEASE_VULNERABILITY_UPDATE:
    "Installed release vulnerability update",
  LATEST_PRODUCT_RELEASE_VULNERABILITY_UPDATE:
    "Latest release vulnerability update",
  PLAN_FAILURES: "Plan failures",
  PRODUCT_IN_OPEN_ENDED_RECALL: "Product in open-ended recall",
  PRODUCT_RECALLS: "Product recalls",
  PRODUCT_RELEASE_PROMOTION_FAILED: "Release promotion failed",
  RBAC_CHANGE: "RBAC change",
  ROLE_CHANGE: "Role change",
});

/** A type this build has never heard of still shows the string the Hub sent. */
export const typeLabel = (type) => TYPE_LABELS[type] || String(type ?? "");

/** The channel to request: the one named when the Hub accepts it, else IN_APP. */
export function requestedChannel(value) {
  const channel = String(value ?? "")
    .trim()
    .toUpperCase();
  return CHANNELS.includes(channel) ? channel : DEFAULT_CHANNEL;
}

export function inboxPath(channel, limit) {
  return `/v1/notifications/inbox?channel=${encodeURIComponent(requestedChannel(channel))}&limit=${encodeURIComponent(limit)}`;
}

/** @returns {{channel: string, status: string, requested: number, limit: number, entries: any[], owner: any, error: string, httpStatus: number}} */
export function initialInbox(channel = DEFAULT_CHANNEL) {
  return {
    channel: requestedChannel(channel),
    status: "idle",
    requested: INBOX_LIMITS[0],
    limit: 0,
    entries: [],
    owner: null,
    error: "",
    httpStatus: 0,
  };
}

/**
 * The inbox read as a state machine. A failure never leaves rows behind and
 * never becomes an empty inbox, and it keeps the limit that was requested so a
 * retry does not widen the read.
 */
export function inboxReduce(inbox, action) {
  switch (action.type) {
    case "request": {
      const channel = requestedChannel(action.channel ?? inbox.channel);
      const sameChannel = channel === inbox.channel;
      return {
        ...inbox,
        channel,
        requested: action.limit ?? inbox.requested,
        // A quiet re-read of the same page keeps its rows on screen.
        status: action.quiet && inbox.status === "ready" ? "ready" : "loading",
        entries: sameChannel ? inbox.entries : [],
        limit: sameChannel ? inbox.limit : 0,
        error: "",
        httpStatus: 0,
      };
    }
    case "response": {
      const body = action.body;
      // Go encodes an empty slice as null; that is an empty inbox.
      const entries = body?.entries ?? [];
      if (!body || typeof body !== "object" || !Array.isArray(entries))
        return inboxReduce(inbox, {
          type: "failure",
          status: 502,
          message: "The Hub's inbox answer was not a list of entries.",
        });
      return {
        ...inbox,
        status: "ready",
        entries: entries.filter((entry) => entry && typeof entry === "object"),
        limit: Number.isInteger(body.limit) && body.limit > 0 ? body.limit : 0,
        owner: body.owner && typeof body.owner === "object" ? body.owner : null,
        error: "",
        httpStatus: 200,
      };
    }
    case "failure":
      return {
        ...inbox,
        status: action.status === 403 ? "refused" : "failed",
        entries: [],
        limit: 0,
        error: action.message || "The Hub did not answer.",
        httpStatus: action.status || 0,
      };
    default:
      return inbox;
  }
}

/** Which of the four screens the inbox is, plus the rows screen. */
export function inboxView(inbox) {
  if (inbox.status === "failed" || inbox.status === "refused")
    return inbox.status;
  if (inbox.status !== "ready") return "loading";
  return inbox.entries.length ? "rows" : "empty";
}

/**
 * Whether the page filled the limit the Hub applied. A filled page is a bound,
 * not the end of the inbox; an unfilled one is the whole inbox.
 */
export function inboxBound(inbox) {
  const bounded = inbox.limit > 0 && inbox.entries.length >= inbox.limit;
  const next = INBOX_LIMITS.find((limit) => limit > inbox.limit) || 0;
  if (!bounded) return { bounded: false, next: 0, text: "" };
  return {
    bounded: true,
    next,
    text: next
      ? `Showing ${inbox.entries.length}. This page filled the limit of ${inbox.limit} the Hub applied, so more may exist.`
      : `Showing ${inbox.entries.length}. This page filled the Hub's maximum of ${inbox.limit}; older entries may exist and this route does not page past them.`,
  };
}

const occurredAt = (entry) => {
  const time = Date.parse(entry?.event?.occurredAt ?? "");
  return Number.isFinite(time) ? time : 0;
};

/** The newest event time on a page, as the ISO string the Hub sent it in. */
export function newestOccurredAt(entries) {
  let newest = null;
  for (const entry of entries)
    if (!newest || occurredAt(entry) > occurredAt(newest)) newest = entry;
  return newest && occurredAt(newest) ? newest.event.occurredAt : "";
}

/**
 * An entry is new when it happened after the last time this browser showed the
 * inbox. No Hub route writes readAt today; when one does and the entry carries
 * a real time, the Hub's answer wins.
 */
export function isUnread(entry, seenAt) {
  const readAt = Date.parse(entry?.readAt ?? "");
  if (Number.isFinite(readAt) && readAt > 0) return false;
  const seen = Date.parse(seenAt || "");
  return occurredAt(entry) > (Number.isFinite(seen) ? seen : 0);
}

/**
 * The top-bar count. It is computed from one bounded page, so when every row
 * on a filled page is new the true count is at least that many and the label
 * says so rather than stating a number that would be wrong.
 */
export function unreadSummary(entries, seenAt, limit) {
  const count = entries.filter((entry) => isUnread(entry, seenAt)).length;
  const atLeast =
    count > 0 &&
    limit > 0 &&
    entries.length >= limit &&
    count === entries.length;
  return {
    count,
    atLeast,
    label: count ? `${count}${atLeast ? "+" : ""}` : "",
  };
}

export function initialPreferenceDraft(stored) {
  return {
    types: Array.isArray(stored?.types) ? [...stored.types] : [],
    allFutureTypes: stored?.allFutureTypes === true,
    contactId: typeof stored?.contactId === "string" ? stored.contactId : "",
  };
}

/** What an explicit save would change, against what the Hub last returned. */
export function preferenceDiff(stored, draft) {
  const before = initialPreferenceDraft(stored);
  const added = draft.types.filter((type) => !before.types.includes(type));
  const removed = before.types.filter((type) => !draft.types.includes(type));
  const allFutureTypes = before.allFutureTypes !== draft.allFutureTypes;
  const contactId = before.contactId !== draft.contactId;
  return {
    added,
    removed,
    allFutureTypes,
    contactId,
    // A preference that does not exist yet is always a change to save.
    changed:
      !stored ||
      added.length > 0 ||
      removed.length > 0 ||
      allFutureTypes ||
      contactId,
  };
}

/**
 * The PUT body. No owner (the Hub resolves the caller) and no revision (the
 * route rejects unknown fields). Known types are sent in vocabulary order;
 * a stored type this build does not know is carried through unchanged.
 * @returns {{resourceRid: string, channel: string, types: string[], allFutureTypes: boolean, contactId?: string}}
 */
export function preferencePayload(resourceRid, channel, draft) {
  const known = EVENT_TYPES.filter((type) => draft.types.includes(type));
  const carried = draft.types.filter((type) => !EVENT_TYPES.includes(type));
  const payload = {
    resourceRid: String(resourceRid ?? "").trim(),
    channel: requestedChannel(channel),
    types: [...known, ...new Set(carried)],
    allFutureTypes: draft.allFutureTypes === true,
  };
  if (draft.contactId) payload.contactId = draft.contactId;
  return payload;
}

/** Why the Hub would refuse this edit; empty when it would not. */
export function preferenceProblem(resourceRid, draft) {
  if (!String(resourceRid ?? "").trim())
    return "Name the resource this preference is about.";
  if (!draft.types.length && !draft.allFutureTypes)
    return "Enable at least one type or opt into future types. A preference that selects nothing can never deliver, and the Hub refuses it.";
  return "";
}

/** A save is refused locally when the stored revision moved under the editor. */
export function staleRevision(loaded, current) {
  const before = loaded ? Number(loaded.revision) || 0 : 0;
  const now = current ? Number(current.revision) || 0 : 0;
  return before === now
    ? ""
    : `This preference changed on the Hub after you loaded it (revision ${before || "none"} is now ${now || "none"}). Reload it and reapply your edit; nothing was saved.`;
}

export function contactPayload(draft) {
  return {
    kind: String(draft.kind ?? ""),
    address: String(draft.address ?? "").trim(),
    optIntoFutureResources: draft.optIntoFutureResources === true,
  };
}

export function contactProblem(draft) {
  if (!CONTACT_KINDS.includes(draft.kind))
    return "Choose EMAIL, SLACK or PAGERDUTY.";
  if (!String(draft.address ?? "").trim()) return "Enter an address.";
  return "";
}

async function hubRequest(method, path, body) {
  let response;
  try {
    response = await fetch(path, {
      method,
      headers:
        body === undefined
          ? { accept: "application/json" }
          : { accept: "application/json", "content-type": "application/json" },
      credentials: "same-origin",
      signal: AbortSignal.timeout(15000),
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    });
  } catch {
    return { ok: false, status: 0, message: "The Hub did not answer." };
  }
  const json = await response.json().catch(() => null);
  if (!response.ok)
    return {
      ok: false,
      status: response.status,
      message:
        typeof json?.error === "string" && json.error
          ? json.error
          : `The Hub answered ${response.status}.`,
    };
  return { ok: true, status: response.status, body: json };
}

const when = (value) => {
  const time = Date.parse(value ?? "");
  return Number.isFinite(time) && time > 0
    ? new Date(time).toISOString().replace(".000Z", "Z")
    : "";
};

const ownerText = (owner) =>
  owner?.id ? `${owner.kind || "USER"} ${owner.id}` : "not yet resolved";

function stateCard(kind, name, title, lines, action = "") {
  return `<section class="kit-card kit-state" role="${kind === "failed" || kind === "refused" ? "alert" : "status"}" data-${name}="${kind}">
    <div class="kit-state-title">${esc(title)}</div>
    ${lines.map((line) => `<p>${esc(line)}</p>`).join("")}
    ${action}
  </section>`;
}

function inboxMarkup(model, newSince) {
  const inbox = model.inbox;
  const view = inboxView(inbox);
  if (view === "loading")
    return stateCard("loading", "inbox", "Reading the inbox", [
      `Asking the Hub what it recorded for you on ${inbox.channel}.`,
    ]);
  const retry = `<button type="button" class="so-btn so-btn-sm" data-ntf-action="reload-inbox" data-key="reload-inbox">Retry</button>`;
  if (view === "failed")
    return stateCard(
      "failed",
      "inbox",
      "The inbox could not be read",
      [
        inbox.error,
        "This is a failed request, not an empty inbox. Nothing is known about what was recorded.",
      ],
      retry,
    );
  if (view === "refused")
    return stateCard("refused", "inbox", "The Hub refused this inbox read", [
      inbox.error,
    ]);
  if (view === "empty")
    return stateCard(
      "empty",
      "inbox",
      "No notifications",
      [
        `The Hub has recorded nothing for ${ownerText(inbox.owner)} on ${inbox.channel}.`,
        "An entry appears when a type you subscribed to is raised about a resource. Subscriptions are per resource, under Preferences.",
      ],
      `<button type="button" class="so-btn so-btn-sm" data-ntf-view="preferences" data-key="open-preferences">Open preferences</button>`,
    );
  const bound = inboxBound(inbox);
  const rows = inbox.entries
    .map((entry) => {
      const event = entry.event || {};
      const fresh =
        inbox.channel === DEFAULT_CHANNEL && isUnread(entry, newSince);
      return `<tr data-inbox-row="${esc(event.id)}">
        <td class="code-id text-[11px] whitespace-nowrap">${esc(when(event.occurredAt) || "time not recorded")}</td>
        <td><div>${esc(typeLabel(event.type))}${fresh ? ` <span class="kit-badge kit-badge-info" data-inbox-new>New</span>` : ""}</div><div class="code-id text-[10.5px]">${esc(event.type)}</div></td>
        <td class="code-id text-[11px] break-all">${esc(event.resourceRid)}</td>
        <td class="code-id text-[11px]">${esc(event.environment || "")}</td>
        <td>${esc(event.summary)}</td>
        <td class="code-id text-[11px]">${esc(event.actor || "")}</td>
      </tr>`;
    })
    .join("");
  return `<div class="so-panel p-3 space-y-2" data-inbox="rows">
    <table class="so-table">
      <thead><tr><th>Occurred</th><th>Type</th><th>Resource</th><th>Environment</th><th>Summary</th><th>Actor</th></tr></thead>
      <tbody>${rows}</tbody>
    </table>
    ${
      bound.bounded
        ? `<div role="status" data-inbox-bound class="so-warn-card p-2 text-[12px] flex items-center gap-2 flex-wrap"><span>${esc(bound.text)}</span>${bound.next ? `<button type="button" class="so-btn so-btn-sm" data-ntf-action="widen-inbox" data-key="widen-inbox">Show up to ${bound.next}</button>` : ""}</div>`
        : `<div class="text-[11px] text-[var(--text-2)]" data-inbox-complete>${inbox.entries.length} ${inbox.entries.length === 1 ? "entry" : "entries"}; the Hub returned fewer than its limit, so this is the whole inbox for ${esc(inbox.channel)}.</div>`
    }
  </div>`;
}

function preferenceEditor(model, decision) {
  const pref = model.pref;
  const draft = pref.draft;
  const unraised = pref.notYetEmitted.filter((type) =>
    draft.types.includes(type),
  );
  const carried = draft.types.filter((type) => !EVENT_TYPES.includes(type));
  const boxes = EVENT_TYPES.filter((type) => !unraised.includes(type))
    .map(
      (type) =>
        `<label class="flex items-start gap-2 text-[12px]"><input type="checkbox" data-ntf-type="${esc(type)}" data-key="type-${esc(type)}" ${draft.types.includes(type) ? "checked" : ""}/><span>${esc(typeLabel(type))} <span class="code-id text-[10.5px] text-[var(--text-2)]">${esc(type)}</span></span></label>`,
    )
    .join("");
  const silent = unraised
    .map(
      (type) =>
        `<li class="flex items-start gap-2 text-[12px]" data-type-not-emitted="${esc(type)}"><input type="checkbox" checked disabled aria-label="${esc(typeLabel(type))}, not yet emitted by this Hub"/><span>${esc(typeLabel(type))} <span class="code-id text-[10.5px] text-[var(--text-2)]">${esc(type)}</span><br/><span class="text-[var(--warn-text)]">Not yet emitted by this Hub. It is stored on your preference, and nothing in this build raises it, so silence on it does not mean nothing happened.</span></span><button type="button" class="so-btn so-btn-sm" data-ntf-remove-type="${esc(type)}" data-key="remove-${esc(type)}">Remove</button></li>`,
    )
    .join("");
  const diff = preferenceDiff(pref.stored, draft);
  const problem = preferenceProblem(pref.resourceRid, draft);
  const reason =
    decision.allowed === false
      ? decision.reason
      : problem || (diff.changed ? "" : "No unsaved change.");
  const changes = [
    ...diff.added.map((type) => `+ ${type}`),
    ...diff.removed.map((type) => `- ${type}`),
    ...(diff.allFutureTypes
      ? [`future types ${draft.allFutureTypes ? "on" : "off"}`]
      : []),
    ...(diff.contactId ? [`contact ${draft.contactId || "none"}`] : []),
  ];
  const contacts = model.contacts.rows;
  const contactIds = contacts.map((contact) => contact.id);
  const contactOptions = [
    `<option value="" ${draft.contactId ? "" : "selected"}>No selected contact</option>`,
    ...contacts.map(
      (contact) =>
        `<option value="${esc(contact.id)}" ${contact.id === draft.contactId ? "selected" : ""}>${esc(contact.kind)} ${esc(contact.address)}</option>`,
    ),
    ...(draft.contactId && !contactIds.includes(draft.contactId)
      ? [
          `<option value="${esc(draft.contactId)}" selected>${esc(draft.contactId)} (stored; not among your contacts)</option>`,
        ]
      : []),
  ].join("");
  return `<div class="so-panel p-3 space-y-3" data-preference-editor>
    <div class="text-[12px]">${
      pref.stored
        ? `Stored preference, revision <span class="code-id" data-preference-revision>${esc(pref.stored.revision)}</span>, owner <span class="code-id">${esc(ownerText(pref.stored.owner))}</span>, last updated <span class="code-id">${esc(when(pref.stored.updatedAt) || "not recorded")}</span>.`
        : `You are not subscribed to <span class="code-id">${esc(pref.resourceRid)}</span> on ${esc(model.inbox.channel)}. Saving creates the preference.`
    }</div>
    <fieldset class="space-y-1.5"><legend class="text-[11px] uppercase tracking-[0.08em] text-[var(--text-2)] mb-1">Event types</legend>${boxes}</fieldset>
    ${silent ? `<div><div class="text-[11px] uppercase tracking-[0.08em] text-[var(--text-2)] mb-1">Stored types nothing raises yet</div><ul class="space-y-1.5">${silent}</ul></div>` : ""}
    <p class="text-[11px] text-[var(--text-2)]">The Hub reports a type as not yet emitted only for types already saved on this preference, so a box above can still name a type nothing raises; the answer appears after a save.</p>
    ${carried.length ? `<p class="text-[12px]" data-carried-types>Stored types this console does not know are carried through a save unchanged: <span class="code-id">${esc(carried.join(", "))}</span></p>` : ""}
    <label class="flex items-start gap-2 text-[12px]"><input type="checkbox" data-ntf-future data-key="future" ${draft.allFutureTypes ? "checked" : ""}/><span>All future types. Include types a later Hub adds.</span></label>
    <label class="block text-[12px]">Contact
      <select class="so-select ml-2" data-ntf-contact data-key="pref-contact">${contactOptions}</select>
      ${model.contacts.status === "ready" ? "" : `<span class="text-[11px] text-[var(--text-2)] ml-2">Your contacts ${model.contacts.status === "loading" ? "are loading" : "could not be read"}.</span>`}
    </label>
    <div class="flex items-center gap-2 flex-wrap">
      <button type="button" class="so-btn so-btn-primary" data-ntf-action="save-preference" data-key="save-preference" ${reason || pref.saving ? "disabled" : ""} ${reason ? `title="${esc(reason)}"` : ""}>${pref.saving ? "Saving…" : "Save preference"}</button>
      <span class="text-[12px] ${decision.allowed === false || problem ? "text-[var(--warn-text)]" : "text-[var(--text-2)]"}" data-save-reason>${esc(reason || `Unsaved: ${changes.join(", ") || "new preference"}`)}</span>
    </div>
    ${pref.error ? `<div role="alert" data-preference-error class="so-warn-card p-2 text-[12px]">${esc(pref.error)}</div>` : ""}
    ${pref.saved ? `<div role="status" data-preference-saved class="text-[12px]">${esc(pref.saved)}</div>` : ""}
  </div>`;
}

function preferencesMarkup(model, decision) {
  const pref = model.pref;
  const form = `<form class="so-panel p-3 flex items-end gap-2 flex-wrap" data-ntf-form="load-preference">
    <label class="text-[12px] flex-1 min-w-[260px]">Resource
      <input class="so-input w-full mt-1" data-ntf-input="pref-rid" data-key="pref-rid" value="${esc(pref.ridDraft)}" placeholder="for example product/payments" autocomplete="off"/>
    </label>
    <button type="submit" class="so-btn" data-key="load-preference">Load preference</button>
    <p class="basis-full text-[11px] text-[var(--text-2)]">A preference is about one resource on one channel (${esc(model.inbox.channel)}). Reading or saving it needs resource:read on that resource.</p>
  </form>`;
  let body = "";
  if (pref.status === "idle")
    body = stateCard("idle", "preference", "No resource selected", [
      "Name a resource to read your preference for it.",
    ]);
  else if (pref.status === "loading")
    body = stateCard("loading", "preference", "Reading the preference", [
      `Asking the Hub for your preference on ${pref.resourceRid}.`,
    ]);
  else if (pref.status === "failed")
    body = stateCard(
      "failed",
      "preference",
      "The preference could not be read",
      [pref.error, "This is a failed request, not an absent preference."],
      `<button type="button" class="so-btn so-btn-sm" data-ntf-action="reload-preference" data-key="reload-preference">Retry</button>`,
    );
  else if (pref.status === "refused")
    body = stateCard(
      "refused",
      "preference",
      "The Hub refused this preference read",
      [pref.error],
    );
  else body = preferenceEditor(model, decision);
  return form + body;
}

function contactsMarkup(model) {
  const contacts = model.contacts;
  const draft = contacts.draft;
  const problem = contactProblem(draft);
  let list = "";
  if (contacts.status === "loading" || contacts.status === "idle")
    list = stateCard("loading", "contacts", "Reading your contacts", [
      "Asking the Hub for the destinations you hold.",
    ]);
  else if (contacts.status === "failed")
    list = stateCard(
      "failed",
      "contacts",
      "Your contacts could not be read",
      [contacts.error, "This is a failed request, not an empty list."],
      `<button type="button" class="so-btn so-btn-sm" data-ntf-action="reload-contacts" data-key="reload-contacts">Retry</button>`,
    );
  else if (contacts.status === "refused")
    list = stateCard(
      "refused",
      "contacts",
      "The Hub refused this contact read",
      [contacts.error],
    );
  else if (!contacts.rows.length)
    list = stateCard("empty", "contacts", "No contacts", [
      `The Hub holds no notification contact for ${ownerText(contacts.owner)}.`,
    ]);
  else
    list = `<div class="so-panel p-3" data-contacts="rows"><table class="so-table">
      <thead><tr><th>Kind</th><th>Address</th><th>Future resources</th><th>Delivery</th><th>Updated</th><th></th></tr></thead>
      <tbody>${contacts.rows
        .map(
          (contact) => `<tr data-contact-row="${esc(contact.kind)}">
          <td class="code-id">${esc(contact.kind)}</td>
          <td class="break-all">${esc(contact.address)}</td>
          <td>${contact.optIntoFutureResources ? "Opted in" : "No"}</td>
          <td>${contact.deliverable === false ? `<span class="kit-badge kit-badge-warn">Stored, never delivered</span><div class="text-[11px] text-[var(--text-2)]">This Hub has no transport for ${esc(contact.kind)}.</div>` : contact.deliverable === true ? `<span class="kit-badge kit-badge-ok">Deliverable</span>` : "Not reported"}</td>
          <td class="code-id text-[11px]">${esc(when(contact.updatedAt) || when(contact.createdAt))}</td>
          <td>${
            contacts.confirmDelete === contact.kind
              ? `<button type="button" class="so-btn so-btn-sm so-btn-danger" data-ntf-delete-confirm="${esc(contact.kind)}" data-key="delete-confirm-${esc(contact.kind)}" ${contacts.saving ? "disabled" : ""}>Confirm delete</button> <button type="button" class="so-btn so-btn-sm" data-ntf-delete-cancel data-key="delete-cancel">Keep</button>`
              : `<button type="button" class="so-btn so-btn-sm" data-ntf-delete="${esc(contact.kind)}" data-key="delete-${esc(contact.kind)}">Delete</button>`
          }</td>
        </tr>`,
        )
        .join("")}</tbody></table></div>`;
  const replaces = contacts.rows.some((contact) => contact.kind === draft.kind);
  return `${list}
  <form class="so-panel p-3 space-y-2" data-ntf-form="save-contact">
    <div class="text-[11px] uppercase tracking-[0.08em] text-[var(--text-2)]">Add or replace a contact</div>
    <div class="flex items-end gap-2 flex-wrap">
      <label class="text-[12px]">Kind
        <select class="so-select block mt-1" data-ntf-contact-kind data-key="contact-kind">${CONTACT_KINDS.map((kind) => `<option value="${kind}" ${kind === draft.kind ? "selected" : ""}>${kind}</option>`).join("")}</select>
      </label>
      <label class="text-[12px] flex-1 min-w-[240px]">Address
        <input class="so-input w-full mt-1" data-ntf-input="contact-address" data-key="contact-address" value="${esc(draft.address)}" autocomplete="off"/>
      </label>
      <button type="submit" class="so-btn so-btn-primary" data-key="save-contact" data-save-contact ${problem || contacts.saving ? "disabled" : ""}>${contacts.saving ? "Saving…" : replaces ? "Replace contact" : "Save contact"}</button>
    </div>
    <label class="flex items-start gap-2 text-[12px]"><input type="checkbox" data-ntf-contact-future data-key="contact-future" ${draft.optIntoFutureResources ? "checked" : ""}/><span>Use this contact for resources added later.</span></label>
    <p class="text-[11px] text-[var(--text-2)]">The Hub keeps one contact per kind for you; saving a kind you already hold replaces its address. A contact belongs to you rather than to a resource, so no resource grant applies: the Hub accepts a write only from its owner.</p>
    ${contacts.writeError ? `<div role="alert" data-contact-error class="so-warn-card p-2 text-[12px]">${esc(contacts.writeError)}</div>` : ""}
    ${contacts.saved ? `<div role="status" data-contact-saved class="text-[12px]">${esc(contacts.saved)}</div>` : ""}
  </form>`;
}

const VIEWS = Object.freeze([
  ["inbox", "Inbox"],
  ["preferences", "Preferences"],
  ["contacts", "Contacts"],
]);

/**
 * The whole screen for one model. Pure; every Hub value passes through esc.
 * @param {any} model
 * @param {{decision?: {allowed: boolean | null, reason: string}, newSince?: string}} [options]
 */
export function notificationsMarkup(model, { decision, newSince = "" } = {}) {
  const view = VIEWS.some(([id]) => id === model.view) ? model.view : "inbox";
  const owner =
    view === "contacts" ? model.contacts.owner : model.inbox.owner || null;
  const body =
    view === "inbox"
      ? inboxMarkup(model, newSince)
      : view === "preferences"
        ? preferencesMarkup(model, decision || { allowed: null, reason: "" })
        : contactsMarkup(model);
  return `<div class="space-y-3" data-notifications="${view}">
    <div class="kit-crumb">Notifications / ${VIEWS.find(([id]) => id === view)[1]}</div>
    <div class="kit-subnav">${VIEWS.map(([id, label]) => `<button type="button" data-ntf-view="${id}" data-key="view-${id}" class="kit-tab ${id === view ? "is-active" : ""}">${label}</button>`).join("")}</div>
    <div class="flex items-center gap-3 flex-wrap text-[12px]">
      ${
        view === "contacts"
          ? ""
          : `<label>Channel <select class="so-select ml-1" data-ntf-channel data-key="channel">${CHANNELS.map((channel) => `<option value="${channel}" ${channel === model.inbox.channel ? "selected" : ""}>${channel}</option>`).join("")}</select></label>`
      }
      <span class="text-[var(--text-2)]">Owner the Hub resolved: <span class="code-id" data-ntf-owner>${esc(ownerText(owner))}</span></span>
    </div>
    ${body}
  </div>`;
}

/** The top-bar control's text and accessible name for one badge state. */
export function indicatorView(badge) {
  if (badge.status === "failed")
    return {
      label: "?",
      title:
        "Notifications. The inbox could not be read, so the new count is unknown.",
    };
  if (badge.status !== "ready")
    return { label: "", title: "Notifications. Reading the inbox." };
  return {
    label: badge.label,
    title: badge.count
      ? `Notifications. ${badge.count}${badge.atLeast ? " or more" : ""} new in-app since you last opened the inbox in this browser.`
      : "Notifications. Nothing new in-app since you last opened the inbox in this browser.",
  };
}

/**
 * The screen's whole model before any Hub answer.
 * @returns {any}
 */
export function initialNotifications(channel = DEFAULT_CHANNEL) {
  return {
    view: "inbox",
    inbox: initialInbox(channel),
    badge: { status: "idle", count: 0, atLeast: false, label: "" },
    seenAt: "",
    pref: {
      ridDraft: "",
      resourceRid: "",
      status: "idle",
      stored: null,
      draft: initialPreferenceDraft(null),
      notYetEmitted: [],
      saving: false,
      error: "",
      saved: "",
    },
    contacts: {
      status: "idle",
      rows: [],
      owner: null,
      draft: { kind: "EMAIL", address: "", optIntoFutureResources: false },
      confirmDelete: "",
      saving: false,
      error: "",
      writeError: "",
      saved: "",
    },
  };
}

export function createNotifications({ state, render, capabilities }) {
  const initialChannel =
    typeof location === "undefined"
      ? DEFAULT_CHANNEL
      : requestedChannel(new URLSearchParams(location.search).get("channel"));
  const model = (state.notifications = initialNotifications(initialChannel));
  let seenHub;
  let visitNewSince = null;
  let inboxGeneration = 0;
  let prefGeneration = 0;
  let badgeEntries = [];
  let badgeLimit = 0;
  let badgeOwner = "";

  const visible = () => state.screen === NOTIFICATIONS_SCREEN;
  const seenKey = () => `${SEEN_KEY}:${badgeOwner}`;

  function paintIndicator() {
    if (typeof document === "undefined") return;
    let button = document.getElementById("notifications-indicator");
    if (!button) {
      const anchor = document.getElementById("settings-toggle");
      if (!anchor?.parentElement) return;
      button = document.createElement("button");
      button.type = "button";
      button.id = "notifications-indicator";
      button.className = "so-btn !h-[26px] !px-2";
      button.setAttribute(
        "data-onclick",
        `switchScreen('${NOTIFICATIONS_SCREEN}')`,
      );
      button.innerHTML = `<span>Inbox</span><span data-unread class="kit-badge kit-badge-info ml-1" hidden></span>`;
      anchor.parentElement.insertBefore(button, anchor);
    }
    const view = indicatorView(model.badge);
    button.title = view.title;
    button.setAttribute("aria-label", view.title);
    button.dataset.unreadState = model.badge.status;
    const count = button.querySelector("[data-unread]");
    count.textContent = view.label;
    count.hidden = view.label === "";
  }

  function summarize() {
    model.badge = {
      status: "ready",
      ...unreadSummary(badgeEntries, model.seenAt, badgeLimit),
    };
    paintIndicator();
  }

  /** Showing the in-app inbox is what makes its entries seen. */
  function markSeen(entries) {
    if (visitNewSince === null) visitNewSince = model.seenAt;
    const newest = newestOccurredAt(entries);
    if (newest && Date.parse(newest) > (Date.parse(model.seenAt) || 0)) {
      model.seenAt = newest;
      writePref(seenKey(), newest);
    }
    summarize();
  }

  /** The seen mark is kept per owner the Hub resolved, never per browser. */
  function adoptOwner(owner) {
    const id = String(owner?.id ?? "");
    if (id === badgeOwner && model.seenAt) return;
    badgeOwner = id;
    model.seenAt = String(readPref(seenKey(), "") || "");
  }

  async function pollBadge() {
    const result = await hubRequest(
      "GET",
      inboxPath(DEFAULT_CHANNEL, INBOX_LIMITS.at(-1)),
    );
    const page = inboxReduce(
      initialInbox(DEFAULT_CHANNEL),
      result.ok
        ? { type: "response", body: result.body }
        : { type: "failure", status: result.status, message: result.message },
    );
    if (page.status !== "ready") {
      model.badge = { status: "failed", count: 0, atLeast: false, label: "" };
      paintIndicator();
      return;
    }
    badgeEntries = page.entries;
    badgeLimit = page.limit;
    adoptOwner(page.owner);
    summarize();
  }

  async function loadInbox({ quiet = false, limit, channel } = {}) {
    const mine = ++inboxGeneration;
    model.inbox = inboxReduce(model.inbox, {
      type: "request",
      quiet,
      limit,
      channel,
    });
    if (!quiet) render();
    const result = await hubRequest(
      "GET",
      inboxPath(model.inbox.channel, model.inbox.requested),
    );
    if (mine !== inboxGeneration) return;
    model.inbox = inboxReduce(
      model.inbox,
      result.ok
        ? { type: "response", body: result.body }
        : { type: "failure", status: result.status, message: result.message },
    );
    if (
      model.inbox.status === "ready" &&
      model.inbox.channel === DEFAULT_CHANNEL &&
      visible() &&
      model.view === "inbox"
    ) {
      badgeEntries = model.inbox.entries;
      badgeLimit = model.inbox.limit;
      adoptOwner(model.inbox.owner);
      markSeen(model.inbox.entries);
    }
    render();
  }

  async function loadContacts() {
    const contacts = model.contacts;
    contacts.status = "loading";
    contacts.error = "";
    render();
    const result = await hubRequest("GET", "/v1/notifications/contacts");
    if (!result.ok) {
      contacts.status = result.status === 403 ? "refused" : "failed";
      contacts.rows = [];
      contacts.error = result.message;
    } else {
      contacts.status = "ready";
      contacts.rows = Array.isArray(result.body?.contacts)
        ? result.body.contacts.filter((row) => row && typeof row === "object")
        : [];
      contacts.owner = result.body?.owner || null;
    }
    render();
  }

  const preferencePath = () =>
    `/v1/notifications/preferences?resourceRid=${encodeURIComponent(model.pref.resourceRid)}&channel=${encodeURIComponent(model.inbox.channel)}`;

  function acceptPreference(stored) {
    const pref = model.pref;
    pref.status = "ready";
    pref.stored = stored;
    pref.draft = initialPreferenceDraft(stored);
    pref.notYetEmitted = Array.isArray(stored?.typesNotYetEmitted)
      ? stored.typesNotYetEmitted.filter((type) => typeof type === "string")
      : [];
  }

  async function loadPreference() {
    const pref = model.pref;
    const mine = ++prefGeneration;
    pref.resourceRid = pref.ridDraft.trim();
    pref.error = "";
    pref.saved = "";
    pref.stored = null;
    if (!pref.resourceRid) {
      pref.status = "idle";
      render();
      return;
    }
    pref.status = "loading";
    capabilities.watch([pref.resourceRid]);
    render();
    const result = await hubRequest("GET", preferencePath());
    if (mine !== prefGeneration) return;
    if (result.ok && result.body?.resourceRid !== pref.resourceRid) {
      pref.status = "failed";
      pref.error =
        "The Hub's answer did not describe the preference for this resource.";
    } else if (result.ok) acceptPreference(result.body);
    // 404 is a state, not a failure: there is no preference yet.
    else if (result.status === 404) acceptPreference(null);
    else {
      pref.status = result.status === 403 ? "refused" : "failed";
      pref.error = result.message;
    }
    render();
  }

  async function savePreference() {
    const pref = model.pref;
    if (pref.saving || pref.status !== "ready") return;
    if (preferenceProblem(pref.resourceRid, pref.draft)) return;
    if (
      capabilities.canPerform("resource:read", pref.resourceRid).allowed ===
      false
    )
      return;
    pref.saving = true;
    pref.error = "";
    pref.saved = "";
    render();
    // The route takes no revision, so the check is read-then-write here.
    const current = await hubRequest("GET", preferencePath());
    const stale =
      current.ok || current.status === 404
        ? staleRevision(pref.stored, current.ok ? current.body : null)
        : `The preference could not be re-read before saving, so nothing was saved. ${current.message}`;
    if (stale) {
      pref.saving = false;
      pref.error = stale;
      render();
      return;
    }
    const result = await hubRequest(
      "PUT",
      "/v1/notifications/preferences",
      preferencePayload(pref.resourceRid, model.inbox.channel, pref.draft),
    );
    pref.saving = false;
    if (!result.ok) {
      pref.error =
        result.status === 422
          ? `The Hub refused this preference: ${result.message}`
          : result.status === 409
            ? `The preference changed on the Hub; nothing was saved. ${result.message}`
            : `The preference was not saved. ${result.message}`;
    } else {
      acceptPreference(result.body);
      pref.saved = `Saved. The Hub stored revision ${result.body?.revision ?? "unknown"}.`;
    }
    render();
  }

  async function saveContact() {
    const contacts = model.contacts;
    if (contacts.saving || contactProblem(contacts.draft)) return;
    contacts.saving = true;
    contacts.writeError = "";
    contacts.saved = "";
    render();
    const result = await hubRequest(
      "PUT",
      "/v1/notifications/contacts",
      contactPayload(contacts.draft),
    );
    contacts.saving = false;
    if (!result.ok) {
      contacts.writeError =
        result.status === 422
          ? `The Hub refused this contact: ${result.message}`
          : `The contact was not saved. ${result.message}`;
      render();
      return;
    }
    contacts.saved = `Saved the ${contacts.draft.kind} contact.`;
    contacts.draft = { ...contacts.draft, address: "" };
    await loadContacts();
  }

  async function deleteContact(kind) {
    const contacts = model.contacts;
    if (contacts.saving) return;
    contacts.saving = true;
    contacts.writeError = "";
    contacts.saved = "";
    render();
    const result = await hubRequest(
      "DELETE",
      `/v1/notifications/contacts?kind=${encodeURIComponent(kind)}`,
    );
    contacts.saving = false;
    contacts.confirmDelete = "";
    if (!result.ok) {
      contacts.writeError = `The contact was not deleted. ${result.message}`;
      render();
      return;
    }
    contacts.saved = `Deleted the ${kind} contact.`;
    await loadContacts();
  }

  function openView(view) {
    model.view = view;
    if (view === "inbox") void loadInbox();
    else {
      if (model.contacts.status === "idle" || view === "contacts")
        void loadContacts();
      render();
    }
  }

  function setChannel(value) {
    const channel = requestedChannel(value);
    if (typeof history !== "undefined") {
      const url = new URL(location.href);
      url.searchParams.set("channel", channel);
      history.replaceState(null, "", url);
    }
    if (model.view === "preferences") {
      model.inbox = { ...initialInbox(channel) };
      void loadPreference();
    } else void loadInbox({ channel, limit: INBOX_LIMITS[0] });
  }

  function bindView(node) {
    node.addEventListener("click", (event) => {
      const target = event.target instanceof Element ? event.target : null;
      const view = target?.closest("[data-ntf-view]");
      if (view) return openView(view.getAttribute("data-ntf-view"));
      const action = target
        ?.closest("[data-ntf-action]")
        ?.getAttribute("data-ntf-action");
      if (action === "reload-inbox") return void loadInbox();
      if (action === "widen-inbox")
        return void loadInbox({
          quiet: true,
          limit: inboxBound(model.inbox).next || model.inbox.requested,
        });
      if (action === "reload-preference") return void loadPreference();
      if (action === "save-preference") return void savePreference();
      if (action === "reload-contacts") return void loadContacts();
      const remove = target?.closest("[data-ntf-remove-type]");
      if (remove) {
        const type = remove.getAttribute("data-ntf-remove-type");
        model.pref.draft.types = model.pref.draft.types.filter(
          (item) => item !== type,
        );
        return render();
      }
      const ask = target?.closest("[data-ntf-delete]");
      if (ask) {
        model.contacts.confirmDelete = ask.getAttribute("data-ntf-delete");
        return render();
      }
      if (target?.closest("[data-ntf-delete-cancel]")) {
        model.contacts.confirmDelete = "";
        return render();
      }
      const confirm = target?.closest("[data-ntf-delete-confirm]");
      if (confirm)
        return void deleteContact(
          confirm.getAttribute("data-ntf-delete-confirm"),
        );
      return undefined;
    });
    node.addEventListener("input", (event) => {
      const input = event.target instanceof Element ? event.target : null;
      const name = input?.getAttribute("data-ntf-input");
      if (name === "pref-rid") model.pref.ridDraft = input.value;
      if (name === "contact-address") {
        model.contacts.draft.address = input.value;
        const save = node.querySelector("[data-save-contact]");
        if (save)
          save.disabled =
            Boolean(contactProblem(model.contacts.draft)) ||
            model.contacts.saving;
      }
    });
    node.addEventListener("change", (event) => {
      const input = event.target instanceof Element ? event.target : null;
      if (!input) return;
      if (input.hasAttribute("data-ntf-channel"))
        return setChannel(input.value);
      const pref = model.pref;
      if (input.hasAttribute("data-ntf-type")) {
        const type = input.getAttribute("data-ntf-type");
        pref.draft.types = input.checked
          ? [...new Set([...pref.draft.types, type])]
          : pref.draft.types.filter((item) => item !== type);
      } else if (input.hasAttribute("data-ntf-future"))
        pref.draft.allFutureTypes = input.checked;
      else if (input.hasAttribute("data-ntf-contact"))
        pref.draft.contactId = input.value;
      else if (input.hasAttribute("data-ntf-contact-kind"))
        model.contacts.draft.kind = input.value;
      else if (input.hasAttribute("data-ntf-contact-future"))
        model.contacts.draft.optIntoFutureResources = input.checked;
      else return undefined;
      pref.saved = "";
      return render();
    });
    node.addEventListener("submit", (event) => {
      event.preventDefault();
      const form =
        event.target instanceof Element
          ? event.target.getAttribute("data-ntf-form")
          : "";
      if (form === "load-preference") void loadPreference();
      if (form === "save-contact") void saveContact();
    });
  }

  let painted = "";
  function renderScreen(root) {
    const decision =
      model.view === "preferences" && model.pref.resourceRid
        ? capabilities.canPerform("resource:read", model.pref.resourceRid)
        : { allowed: null, reason: "" };
    const html = notificationsMarkup(model, {
      decision,
      newSince: visitNewSince ?? model.seenAt,
    });
    // A background refresh that changes nothing must not rebuild a form the
    // operator is typing in.
    if (html === painted && root.querySelector("[data-notifications]")) return;
    const active = document.activeElement;
    const key = root.contains(active) ? active.getAttribute("data-key") : "";
    const caret =
      key && "selectionStart" in active
        ? [active.selectionStart, active.selectionEnd]
        : null;
    root.innerHTML = html;
    painted = html;
    bindView(root.firstElementChild);
    if (key) {
      const again = [...root.querySelectorAll("[data-key]")].find(
        (element) => element.getAttribute("data-key") === key,
      );
      again?.focus();
      if (again && caret && typeof again.setSelectionRange === "function") {
        try {
          again.setSelectionRange(caret[0], caret[1]);
        } catch {
          /* not a text control */
        }
      }
    }
    if (model.view === "inbox" && model.inbox.status === "idle")
      queueMicrotask(() => {
        if (model.inbox.status === "idle") void loadInbox();
      });
  }

  return {
    screens: [
      {
        id: NOTIFICATIONS_SCREEN,
        num: 60,
        short: "Notifications",
        tabTitle: "Notifications",
        hasSidebar: true,
        showKpis: false,
        subtitle: "NOTIFICATIONS",
      },
    ],
    navItems: () => [
      { label: "Notifications", icon: "inbox", id: NOTIFICATIONS_SCREEN },
    ],
    renderers: {
      "ntf-inbox": renderScreen,
    },
    /**
     * Runs on every shell render. No notification event reaches the console
     * on /v1/events, so the count is re-read when the Hub read model is
     * replaced: the console's existing refresh cycle.
     */
    sync() {
      paintIndicator();
      if (!visible()) visitNewSince = null;
      if (state.hub === seenHub) return;
      seenHub = state.hub;
      if (visible() && model.view === "inbox" && model.inbox.status === "ready")
        void loadInbox({ quiet: true });
      void pollBadge();
    },
  };
}

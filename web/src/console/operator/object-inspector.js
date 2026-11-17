// Object inspector: loads one ontology object with its links and renders it as tabbed HTML.
// The inspector only validates actions. It never applies or approves them.

import { ACTION_RISK, OBJECT_TYPES, riskOf } from "@conduit/ontology-client";
import { newestAuditEntries } from "./audit-order.js";

export const INSPECTOR_TABS = [
  { id: "props", label: "Properties" },
  { id: "links", label: "Links" },
  { id: "actions", label: "Actions" },
  { id: "timeline", label: "Timeline" },
  { id: "raw", label: "Raw" },
];

const TIMELINE_LIMIT = 20;

function defaultEsc(value) {
  return String(value ?? "").replace(/[&<>"']/g, (c) => {
    return {
      "&": "&amp;",
      "<": "&lt;",
      ">": "&gt;",
      '"': "&quot;",
      "'": "&#39;",
    }[c];
  });
}

function hasType(name) {
  return (
    typeof name === "string" &&
    Object.prototype.hasOwnProperty.call(OBJECT_TYPES, name)
  );
}

/** Maps a console area object to an ontology reference, or null when it has no ontology type. */
export function ontologyRef(areaObj) {
  if (!areaObj || !hasType(areaObj.objectType)) return null;
  const objectType = areaObj.objectType;
  const primaryKey =
    objectType === "ChangeRequest"
      ? (areaObj.id ?? areaObj.name)
      : areaObj.name;
  if (primaryKey === undefined || primaryKey === null || primaryKey === "")
    return null;
  return { objectType, primaryKey: String(primaryKey) };
}

function messageOf(err) {
  return err && typeof err.message === "string" && err.message
    ? err.message
    : String(err);
}

function itemOf(type, item) {
  const meta = OBJECT_TYPES[type];
  const primaryKey = item?.[meta.primaryKey] ?? item?.__primaryKey ?? "";
  const title = item?.[meta.titleProperty] ?? primaryKey;
  return { primaryKey: String(primaryKey), title: String(title) };
}

/** Loads the object and, for each declared link, the first linked objects. */
export async function loadInspector(client, ref, { linkLimit = 5 } = {}) {
  /** @type {{ ref: any, object: any, links: any[], error?: string }} */
  const model = { ref, object: undefined, links: [] };
  try {
    const set = client.objects(ref.objectType);
    model.object = await set.fetchOne(ref.primaryKey);
    const meta = OBJECT_TYPES[ref.objectType];
    for (const [link, def] of Object.entries(meta.links)) {
      const entry = {
        link,
        target: def.target,
        cardinality: def.cardinality,
        items: [],
        count: 0,
      };
      try {
        const page = await client
          .objects(ref.objectType)
          .where({ type: "eq", field: "__primaryKey", value: ref.primaryKey })
          .pivotTo(link)
          .fetchPage({ pageSize: linkLimit });
        const data = page.data ?? [];
        entry.items = data.map((item) => itemOf(def.target, item));
        entry.count = page.totalCount ?? data.length;
      } catch (err) {
        entry.items = [];
        entry.count = 0;
        entry.error = messageOf(err);
      }
      model.links.push(entry);
    }
  } catch (err) {
    model.error = messageOf(err);
  }
  return model;
}

/** Validates an action against the Hub for the inspected object. Nothing is executed. */
export async function validateFor(client, ref, actionName, object) {
  const environment =
    object?.environment ??
    (ref.objectType === "Environment" ? ref.primaryKey : "");
  try {
    return await client.actions.validate(actionName, {
      environment,
      targetKind: ref.objectType,
      targetId: ref.primaryKey,
      changeType: actionName,
    });
  } catch (err) {
    return { kind: "invalid", messages: {}, submission: messageOf(err) };
  }
}

function typeOf(model) {
  return model.ref?.objectType ?? model.object?.__apiName;
}

function keyOf(model) {
  const type = typeOf(model);
  const meta = hasType(type) ? OBJECT_TYPES[type] : undefined;
  const object = model.object;
  return String(
    object?.[meta?.primaryKey ?? "id"] ??
      object?.__primaryKey ??
      model.ref?.primaryKey ??
      "",
  );
}

function titleOf(model) {
  const type = typeOf(model);
  const meta = hasType(type) ? OBJECT_TYPES[type] : undefined;
  return String(model.object?.[meta?.titleProperty ?? "id"] ?? keyOf(model));
}

function renderValue(value) {
  if (value !== null && typeof value === "object") return JSON.stringify(value);
  return String(value ?? "");
}

function propsBody(model, esc, empty) {
  if (!model.object) return empty("No object is selected.");
  const rows = [
    ["Primary key", keyOf(model)],
    ["Title", titleOf(model)],
  ];
  for (const [key, value] of Object.entries(model.object)) {
    if (key.startsWith("__")) continue;
    rows.push([key, renderValue(value)]);
  }
  const body = rows
    .map(
      ([k, v]) =>
        `<tr><td class="kit-muted text-[12px] pr-3">${esc(k)}</td><td class="code-id text-[12px] text-[var(--text-1)]">${esc(v)}</td></tr>`,
    )
    .join("");
  return `<table class="kit-card w-full"><tbody>${body}</tbody></table>`;
}

function linksBody(model, esc, empty) {
  if (!model.links || model.links.length === 0)
    return empty("This object has no links.");
  return model.links
    .map((l) => {
      const items = l.items
        .map(
          (i) =>
            `<li><button type="button" class="kit-tab" data-inspect-type="${esc(l.target)}" data-inspect-key="${esc(i.primaryKey)}">${esc(i.title)}</button></li>`,
        )
        .join("");
      const err = l.error
        ? `<p class="kit-muted text-[12px]">${esc(l.error)}</p>`
        : "";
      return `<section class="kit-card"><h4 class="text-[12px] text-[var(--text-1)]">${esc(l.link)}</h4><p class="kit-muted text-[12px]">${esc(l.target)} · ${esc(l.cardinality)} · ${esc(l.count)}</p>${err}<ul>${items}</ul></section>`;
    })
    .join("");
}

function outcomeMarkup(outcome, esc) {
  const p = (text) => `<p class="text-[12px]">${esc(text)}</p>`;
  switch (outcome.kind) {
    case "invalid": {
      const items = Object.entries(outcome.messages ?? {}).map(
        ([param, msg]) =>
          `<li class="text-[12px]">${esc(param)}: ${esc(msg)}</li>`,
      );
      if (outcome.submission)
        items.push(`<li class="text-[12px]">${esc(outcome.submission)}</li>`);
      return `<div class="kit-card"><p class="text-[12px]">This action is not valid.</p><ul>${items.join("")}</ul></div>`;
    }
    case "confirm":
      return `<div class="kit-card">${p("Low risk: needs your confirmation before it runs.")}</div>`;
    case "change-request": {
      const id = outcome.changeRequestId
        ? `<p class="text-[12px]"><span class="code-id">${esc(outcome.changeRequestId)}</span></p>`
        : "";
      return `<div class="kit-card">${p("High risk: proposed as a change request. A different approver must approve it; you cannot approve your own request.")}${id}</div>`;
    }
    case "applied":
      return `<div class="kit-card">${p("Applied.")}</div>`;
    default:
      return "";
  }
}

function actionsBody(model, esc) {
  const rows = Object.keys(ACTION_RISK)
    .map((name) => {
      const badge =
        riskOf(name) === "low"
          ? `<span class="kit-badge kit-badge-info">Confirm</span>`
          : `<span class="kit-badge kit-badge-warn">Change request</span>`;
      return `<li class="text-[12px]"><span class="code-id text-[var(--text-1)]">${esc(name)}</span> ${badge} <button type="button" class="kit-tab" data-inspector-validate="${esc(name)}">Check</button></li>`;
    })
    .join("");
  const outcome = model.outcome ? outcomeMarkup(model.outcome, esc) : "";
  return `<ul>${rows}</ul>${outcome}`;
}

function timelineBody(model, esc, empty, audit, auditOrder) {
  const key = keyOf(model);
  const matching = (Array.isArray(audit) ? audit : []).filter((e) => {
    if (!key || !e) return false;
    if (String(e.resource ?? "").includes(key)) return true;
    try {
      return JSON.stringify(e).includes(key);
    } catch {
      return false;
    }
  });
  // Keep legacy ascending projections ordered by sequence on this display
  // copy. Descending projections already put their newest event at sequence 1.
  if (auditOrder !== "desc")
    matching.sort((a, b) => Number(a.sequence ?? 0) - Number(b.sequence ?? 0));
  const entries = newestAuditEntries(matching, auditOrder, TIMELINE_LIMIT);
  if (entries.length === 0) return empty("No audit entry names this object.");
  const rows = entries
    .map(
      (e) =>
        `<li class="text-[12px]"><span class="code-id text-[var(--text-1)]">${esc(e.sequence)}</span> <span class="kit-muted">${esc(e.action)} · ${esc(e.actor)} · ${esc(e.resource)} · ${esc(e.recordedAt)}</span></li>`,
    )
    .join("");
  return `<ul>${rows}</ul>`;
}

function rawBody(model, esc) {
  return `<pre class="code-id text-[12px]">${esc(JSON.stringify(model.object, null, 2))}</pre>`;
}

/** Renders the tab row and the body of the selected tab. */
/**
 * @param {any} model
 * @param {{ tab?: string, esc?: (v: unknown) => string, audit?: ReadonlyArray<any>, auditOrder?: string }} [options]
 */
export function inspectorMarkup(
  model,
  { tab = "props", esc = defaultEsc, audit = [], auditOrder = "asc" } = {},
) {
  model = model ?? {};
  const empty = (text) => `<p class="kit-muted text-[12px]">${esc(text)}</p>`;
  const active = INSPECTOR_TABS.some((t) => t.id === tab) ? tab : "props";
  const tabs = INSPECTOR_TABS.map(
    (t) =>
      `<button type="button" role="tab" aria-selected="${t.id === active}" class="kit-tab${t.id === active ? " is-active" : ""}" data-inspector-tab="${esc(t.id)}">${esc(t.label)}</button>`,
  ).join("");
  const parts = [
    `<div class="flex flex-wrap gap-1 mb-2" role="tablist" aria-label="Inspector">${tabs}</div>`,
  ];
  if (model.error) parts.push(empty(model.error));
  if (model.loading) {
    parts.push(empty("Loading…"));
    return parts.join("");
  }
  switch (active) {
    case "links":
      parts.push(linksBody(model, esc, empty));
      break;
    case "actions":
      parts.push(actionsBody(model, esc));
      break;
    case "timeline":
      parts.push(timelineBody(model, esc, empty, audit, auditOrder));
      break;
    case "raw":
      parts.push(rawBody(model, esc));
      break;
    default:
      parts.push(propsBody(model, esc, empty));
  }
  return parts.join("");
}

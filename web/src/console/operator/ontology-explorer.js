import { createConduitClient, fetchTransport } from "@conduit/ontology-client";
import { diffValue } from "./change-diff.js";
const esc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
export async function bounded(promise) {
  let timer;
  try {
    return await Promise.race([
      promise,
      new Promise((_, reject) => {
        timer = setTimeout(
          () => reject(new Error("Ontology read or validation timed out.")),
          15000,
        );
      }),
    ]);
  } finally {
    clearTimeout(timer);
  }
}
export function ontologyCatalog(body) {
  if (
    !body ||
    typeof body.objectTypes !== "object" ||
    body.objectTypes === null ||
    Array.isArray(body.objectTypes)
  )
    throw new Error("Ontology metadata is unavailable.");
  const types = Object.values(body.objectTypes)
    .map((entry) => {
      const type = entry?.objectType || entry;
      if (typeof type?.apiName !== "string" || !type.apiName)
        throw new Error("Ontology type metadata is invalid.");
      return {
        ...type,
        links: Array.isArray(entry.linkTypes) ? entry.linkTypes : [],
      };
    })
    .sort((a, b) => a.apiName.localeCompare(b.apiName));
  const actions =
    body.actionTypes &&
    typeof body.actionTypes === "object" &&
    !Array.isArray(body.actionTypes)
      ? Object.values(body.actionTypes).filter(
          (action) => action && typeof action.apiName === "string",
        )
      : [];
  return { types, actions };
}
export function objectKey(type, row) {
  const key = row?.[type?.primaryKey] ?? row?.__primaryKey;
  return typeof key === "string" || typeof key === "number" ? String(key) : "";
}
export function validationParameters(text) {
  if (text.length > 65536)
    throw new Error("Validation parameters exceed 65,536 characters.");
  const value = JSON.parse(text);
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new Error("Validation parameters must be a JSON object.");
  return value;
}
export function createOntologyExplorer({
  getIdentity,
  getReady = () => true,
  onInspect,
  onNavigate,
}) {
  const transport = fetchTransport();
  const client = createConduitClient({
    transport: async (path, init) => {
      const body = await transport(path, init);
      if (
        path.endsWith("/objectSets/loadObjects") &&
        !Array.isArray(body?.data)
      )
        throw new Error("Object page is unavailable.");
      return body;
    },
  });
  let generation = 0;
  return async function mount(root) {
    const token = ++generation,
      identity = JSON.stringify(getIdentity());
    const current = () =>
      token === generation &&
      root.isConnected &&
      identity === JSON.stringify(getIdentity());
    if (!getReady()) {
      root.innerHTML =
        '<p role="status">Reading the native session and Hub…</p>';
      return;
    }
    let catalog = { types: [], actions: [] },
      selected = "",
      page = [],
      next = "",
      history = [],
      pageToken = "",
      error = "",
      note = "",
      busy = false,
      objectGeneration = 0,
      actionGeneration = 0;
    const type = () =>
      catalog.types.find((item) => item.apiName === selected) || null;
    function paint() {
      if (!current()) return;
      const chosen = type();
      const properties =
        chosen?.properties && typeof chosen.properties === "object"
          ? Object.entries(chosen.properties)
          : [];
      root.innerHTML = `<div data-ontology-explorer data-value-tier="CONDUIT_NATIVE" class="space-y-3"><section class="so-panel p-3"><h2>Ontology registry and data preview</h2><p>Native schema metadata and authorized sample pages. Metadata does not grant object access or action permission. Schema/type publishing and entity-permission editing are not exposed here.</p><button type="button" class="so-btn" data-ont-open="og01-ontology-graph">Open relationship graph</button><button type="button" class="so-btn" data-ont-open="s01-intake">Open work intake</button><label>Object type <select data-ont-type aria-label="Ontology object type"><option value="">Choose a returned type</option>${catalog.types
        .slice(0, 256)
        .map(
          (item) =>
            `<option value="${esc(item.apiName)}"${item.apiName === selected ? " selected" : ""}>${esc(item.displayName || item.apiName)}</option>`,
        )
        .join(
          "",
        )}</select></label>${catalog.types.length > 256 ? "<p>First 256 returned types shown.</p>" : ""}${error ? `<p role="alert">${esc(error)}</p>` : ""}<p role="status">${esc(note)}</p></section>${
        chosen
          ? `<section class="so-panel p-3"><h3>${esc(chosen.displayName || chosen.apiName)} · ${esc(chosen.apiName)}</h3><p>${esc(chosen.description || "Description unavailable.")}</p><p>Primary key ${esc(chosen.primaryKey || "unavailable")} · status ${esc(chosen.status || "unavailable")}</p><h4>Declared properties</h4>${
              properties.length
                ? `<table class="so-table"><thead><tr><th>Property</th><th>Display name</th><th>Data type</th></tr></thead><tbody>${properties
                    .slice(0, 128)
                    .map(
                      ([name, property]) =>
                        `<tr><td>${esc(name)}</td><td>${esc(property?.displayName || name)}</td><td>${esc(property?.dataType?.type || "unavailable")}</td></tr>`,
                    )
                    .join(
                      "",
                    )}</tbody></table>${properties.length > 128 ? "<p>First 128 declared properties shown.</p>" : ""}`
                : "<p>No property schema is recorded.</p>"
            }<h4>Declared links</h4>${
              chosen.links.length
                ? `<ul>${chosen.links
                    .slice(0, 128)
                    .map(
                      (link) =>
                        `<li>${esc(link.apiName || "unnamed")} · ${esc(link.cardinality || "cardinality unavailable")} · ${esc(link.linkTypeRid || "identifier unavailable")}</li>`,
                    )
                    .join("")}</ul>`
                : "<p>No link declarations were returned.</p>"
            }<p>Per-object permissions are not established by schema metadata. The Hub authorizes each read.</p></section><section class="so-panel p-3" data-ontology-samples><h3>Authorized sample page</h3><p>${page.length} displayed rows; this is not a total object count.</p><button type="button" class="so-btn" data-ont-page="previous"${!history.length || busy ? " disabled" : ""}>Previous page</button><button type="button" class="so-btn" data-ont-page="next"${!next || busy ? " disabled" : ""}>Next page</button>${
              page.length
                ? `<ul>${page
                    .slice(0, 25)
                    .map(
                      (row, index) =>
                        `<li><button type="button" class="so-btn" data-ont-inspect="${index}"${objectKey(chosen, row) ? "" : " disabled"}>Inspect ${esc(objectKey(chosen, row) || "key unavailable")}</button><pre class="whitespace-pre-wrap code-id">${esc(diffValue(row, "sample"))}</pre></li>`,
                    )
                    .join("")}</ul>`
                : "<p>No sample rows are displayed. A failed read is not an empty ontology.</p>"
            }</section>`
          : ""
      }<section class="so-panel p-3"><h3>Action definitions · validate only</h3><p>Validation does not execute an action or store a change request. A valid result does not grant execution authority.</p><form data-ont-validation><label>Action <select name="action" aria-label="Ontology action"><option value="">Choose a returned action</option>${catalog.actions
        .slice(0, 256)
        .map(
          (action) =>
            `<option value="${esc(action.apiName)}">${esc(action.displayName || action.apiName)}</option>`,
        )
        .join(
          "",
        )}</select></label><label>JSON parameters <textarea name="parameters" aria-label="Ontology validation parameters" rows="5">{}</textarea></label><button type="submit" class="so-btn">Validate only</button><p role="status" data-ont-validation-result></p></form><div data-ont-action-schema></div></section></div>`;
      root.querySelector("[data-ontology-explorer]").dataset.identity =
        identity;
      const form = root.querySelector("[data-ont-validation]");
      const schema = root.querySelector("[data-ont-action-schema]");
      form.elements.namedItem("action").onchange = () => {
        actionGeneration++;
        form.querySelector("[data-ont-validation-result]").textContent = "";
        const action = catalog.actions.find(
          (item) => item.apiName === form.elements.namedItem("action").value,
        );
        schema.innerHTML = action
          ? `<p>${esc(action.description || "Description unavailable.")}</p><ul>${Object.entries(
              action.parameters || {},
            )
              .slice(0, 128)
              .map(
                ([name, parameter]) =>
                  `<li>${esc(name)} · ${esc(parameter?.dataType?.type || "type unavailable")} · required ${typeof parameter?.required === "boolean" ? (parameter.required ? "yes" : "no") : "unavailable"}</li>`,
              )
              .join("")}</ul>`
          : "";
      };
      form.elements.namedItem("parameters").oninput = () => {
        actionGeneration++;
        form.querySelector("[data-ont-validation-result]").textContent =
          "Parameters changed; validate the current draft again.";
      };
      form.onsubmit = async (event) => {
        event.preventDefault();
        const stamp = ++actionGeneration,
          name = form.elements.namedItem("action").value;
        if (!catalog.actions.some((action) => action.apiName === name)) return;
        const result = form.querySelector("[data-ont-validation-result]");
        const button = form.querySelector("button");
        try {
          const params = validationParameters(
            form.elements.namedItem("parameters").value,
          );
          button.disabled = true;
          result.textContent = "Validating only…";
          const outcome = await bounded(client.actions.validate(name, params));
          if (!current() || stamp !== actionGeneration || !form.isConnected)
            return;
          result.textContent =
            outcome.kind === "invalid"
              ? `Validation refused: ${JSON.stringify(outcome.errors || outcome).slice(0, 8000)}`
              : ["confirm", "change-request"].includes(outcome.kind) &&
                  outcome.stored !== true
                ? "Validation accepted. No action execution or change-request storage was requested."
                : "Unexpected validation result; no execution authority is inferred.";
        } catch (error) {
          if (current() && stamp === actionGeneration && form.isConnected)
            result.textContent = error.message;
        } finally {
          if (current() && form.isConnected) button.disabled = false;
        }
      };
    }
    async function objects(cursor = "") {
      const stamp = ++objectGeneration,
        chosen = type();
      if (!chosen) {
        page = [];
        next = "";
        busy = false;
        error = "";
        note = "Choose a returned type.";
        paint();
        return;
      }
      busy = true;
      page = [];
      next = "";
      note = "Loading the selected authorized object page…";
      error = "";
      paint();
      try {
        const result = await bounded(
          client.objects(chosen.apiName).fetchPage({
            pageSize: 25,
            ...(cursor ? { pageToken: cursor } : {}),
          }),
        );
        if (
          !current() ||
          stamp !== objectGeneration ||
          selected !== chosen.apiName
        )
          return;
        if (
          !Array.isArray(result.data) ||
          result.data.some(
            (row) => !row || typeof row !== "object" || Array.isArray(row),
          )
        )
          throw new Error("Object page is unavailable.");
        page = result.data.slice(0, 25);
        next =
          typeof result.nextPageToken === "string" ? result.nextPageToken : "";
        pageToken = cursor;
        note = page.length
          ? "Returned authorized sample rows; no total count is inferred."
          : "The returned authorized page has no rows.";
      } catch (failure) {
        if (current() && stamp === objectGeneration) {
          page = [];
          next = "";
          error = failure.message;
          note =
            "Object access or data is unavailable; no zero-count inference.";
        }
      } finally {
        if (current() && stamp === objectGeneration) {
          busy = false;
          paint();
        }
      }
    }
    root.onchange = (event) => {
      if (!event.target?.matches?.("[data-ont-type]")) return;
      selected = event.target.value;
      history = [];
      pageToken = "";
      actionGeneration++;
      void objects();
    };
    root.onclick = (event) => {
      const button = event.target?.closest?.("button");
      if (!button || button.disabled) return;
      if (button.dataset.ontOpen) {
        onNavigate(button.dataset.ontOpen);
        return;
      }
      if (button.hasAttribute("data-ont-inspect")) {
        const row = page[Number(button.dataset.ontInspect)],
          chosen = type();
        if (row && chosen && objectKey(chosen, row))
          onInspect(chosen.apiName, objectKey(chosen, row));
      }
      if (button.dataset.ontPage === "next" && next) {
        history.push(pageToken);
        void objects(next);
      }
      if (button.dataset.ontPage === "previous" && history.length) {
        void objects(history.pop());
      }
    };
    note = "Loading native ontology metadata…";
    paint();
    try {
      const body = await bounded(client.metadata());
      if (!current()) return;
      catalog = ontologyCatalog(body);
      note = catalog.types.length
        ? "Choose a returned type to inspect schema and an authorized sample page."
        : "The returned metadata contains no object types.";
      paint();
    } catch (failure) {
      if (current()) {
        error = failure.message;
        note =
          "Metadata unavailable; no example types or relationships substituted.";
        paint();
      }
    }
  };
}

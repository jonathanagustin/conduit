import { agent, renderer } from "./a2ui-validator.generated.js";
import { applyPatch, readPointer } from "./json-patch.js";

export const CATALOG_ID = "conduit:planning-forms/v1";
const clone = (value) => JSON.parse(JSON.stringify(value));
const esc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/gu,
    (char) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        char
      ],
  );
const patterns = new Set([
  "^[0-9]+$",
  "^[A-Za-z0-9_.-]+$",
  "^https://[A-Za-z0-9./_-]+$",
]);
function reject() {
  throw new Error("The agent form was refused by the approved catalog.");
}
function valueOf(value, model) {
  if (value && typeof value === "object") {
    if (Object.hasOwn(value, "@path")) {
      try {
        return readPointer(model, value["@path"]);
      } catch {
        return undefined;
      }
    }
    if (Object.hasOwn(value, "@call")) {
      const args = Object.fromEntries(
        Object.entries(value.args || {}).map(([key, item]) => [
          key,
          valueOf(item, model),
        ]),
      );
      switch (value["@call"]) {
        case "required":
          return (
            args.value !== null &&
            args.value !== undefined &&
            String(args.value).trim() !== ""
          );
        case "enum":
          return args.options.includes(args.value);
        case "pattern":
          return (
            patterns.has(args.pattern) &&
            typeof args.value === "string" &&
            args.value.length <= 4096 &&
            new RegExp(args.pattern, "u").test(args.value)
          );
        default:
          reject();
      }
    }
    if (Array.isArray(value)) return value.map((item) => valueOf(item, model));
    return Object.fromEntries(
      Object.entries(value).map(([key, item]) => [key, valueOf(item, model)]),
    );
  }
  return value;
}
function policy(value, depth = 0) {
  if (depth > 32) reject();
  if (!value || typeof value !== "object") return;
  for (const [key, child] of Object.entries(value)) {
    if (["__proto__", "constructor", "prototype"].includes(key)) reject();
    if (key === "@call" && !["required", "pattern", "enum"].includes(child))
      reject();
    if (key === "@catalogId" && child !== CATALOG_ID) reject();
    if (
      key === "catalogId" &&
      Object.hasOwn(value, "@call") &&
      child !== CATALOG_ID
    )
      reject();
    if (
      key === "extensions" &&
      Object.keys(child).some((name) => !/^[A-Za-z_][A-Za-z0-9_]*$/u.test(name))
    )
      reject();
    if (key === "@path") {
      if (
        typeof child !== "string" ||
        child.length > 512 ||
        !child.startsWith("/")
      )
        reject();
      // Reuse the pointer parser's prototype and escape checks.
      try {
        readPointer({}, child);
      } catch (error) {
        if (
          child
            .split("/")
            .some(
              (part) =>
                /~(?![01])/u.test(part) ||
                ["__proto__", "constructor", "prototype"].includes(
                  part.replace(/~1/g, "/").replace(/~0/g, "~"),
                ),
            )
        )
          reject();
      }
    }
    policy(child, depth + 1);
  }
}
function tree(components) {
  if (components.size > 64 || !components.has("root")) reject();
  const visited = new Set();
  function visit(id, depth) {
    if (depth > 8 || visited.has(id)) reject();
    const item = components.get(id);
    if (!item) reject();
    visited.add(id);
    for (const child of item.children || []) visit(child, depth + 1);
  }
  visit("root", 0);
  if (visited.size !== components.size) reject();
}
export class FormSurfaces {
  constructor() {
    this.surfaces = new Map();
    this.failures = [];
    this.notices = [];
  }
  accept(message) {
    try {
      if (JSON.stringify(message).length > 256 * 1024 || !agent(message))
        reject();
      policy(message);
      const operation = [
        "createSurface",
        "updateComponents",
        "updateDataModel",
        "deleteSurface",
      ].find((name) => message[name]);
      if (!operation) reject(); // No renderer or agent function RPC.
      const data = message[operation];
      if (
        typeof data.surfaceId !== "string" ||
        !data.surfaceId.length ||
        data.surfaceId.length > 256
      )
        reject();
      if (operation === "deleteSurface") {
        if (!this.surfaces.delete(data.surfaceId)) reject();
        return;
      }
      let surface;
      if (operation === "createSurface") {
        if (
          this.surfaces.has(data.surfaceId) ||
          this.surfaces.size >= 4 ||
          data.catalogId !== CATALOG_ID ||
          data.sendDataModel
        )
          reject();
        surface = {
          id: data.surfaceId,
          components: new Map(),
          model: clone(data.dataModel || {}),
          dirty: new Set(),
          errors: {},
          dismissed: new Set(),
        };
      } else {
        const old = this.surfaces.get(data.surfaceId);
        if (!old) reject();
        surface = {
          ...old,
          components: new Map(old.components),
          model: clone(old.model),
          dirty: new Set(old.dirty),
          errors: { ...old.errors },
          dismissed: new Set(old.dismissed),
        };
      }
      if (data.components) {
        const ids = new Set();
        for (const item of data.components) {
          if (ids.has(item.id)) reject();
          ids.add(item.id);
          surface.components.set(item.id, clone(item));
          surface.dismissed.delete(item.id);
        }
        tree(surface.components);
      }
      if (operation === "updateDataModel") {
        const path =
          data.path === "/" || data.path === undefined ? "" : data.path;
        if (
          [...surface.dirty].some(
            (dirty) =>
              path === "" ||
              path === dirty ||
              path.startsWith(dirty + "/") ||
              dirty.startsWith(path + "/"),
          )
        ) {
          this.notices = [
            ...this.notices,
            { code: "PREFILL_SKIPPED_DIRTY" },
          ].slice(-16);
          return;
        }
        surface.model = applyPatch(surface.model, [
          {
            op: data.value === null && path ? "remove" : "add",
            path,
            value: data.value,
          },
        ]);
      }
      if (
        !surface.model ||
        typeof surface.model !== "object" ||
        Array.isArray(surface.model) ||
        JSON.stringify(surface.model).length > 128 * 1024
      )
        reject();
      this.surfaces.set(surface.id, surface);
    } catch {
      this.failures.push({ code: "FORM_REFUSED" });
      this.failures = this.failures.slice(-16);
      reject();
    }
  }
  input(surfaceId, componentId, value, human = true) {
    const surface = this.surfaces.get(surfaceId);
    const item = surface?.components.get(componentId);
    if (
      !item?.value?.["@path"] ||
      !["TextField", "TextArea", "Select", "CheckBox", "RadioGroup"].includes(
        item.component,
      )
    )
      reject();
    if (
      item.component === "CheckBox"
        ? typeof value !== "boolean"
        : typeof value !== "string" || value.length > 4096
    )
      reject();

    surface.model = applyPatch(surface.model, [
      { op: "add", path: item.value["@path"], value },
    ]);
    if (human) surface.dirty.add(item.value["@path"]);
    delete surface.errors[item.id];
    surface.dismissed.add(item.id);
  }
  action(surfaceId, componentId) {
    const surface = this.surfaces.get(surfaceId);
    const item = surface?.components.get(componentId);
    if (item?.component !== "Button" || !item.action.event) reject();
    const errors = {};
    for (const field of surface.components.values()) {
      const remoteError =
        field.error && !surface.dismissed.has(field.id)
          ? valueOf(field.error, surface.model)
          : "";
      if (remoteError) {
        errors[field.id] = String(remoteError);
        continue;
      }
      if (
        field.options &&
        !field.options.includes(valueOf(field.value, surface.model))
      ) {
        errors[field.id] = "Choose one of the offered values.";
        continue;
      }
      for (const check of field["@checks"] || []) {
        if (!valueOf(check.condition, surface.model)) {
          errors[field.id] = check.message || "Review this value.";
          break;
        }
      }
    }
    surface.errors = errors;
    if (Object.keys(errors).length) return { errors, message: null };
    const event = item.action.event;
    const message = {
      version: "1.0",
      action: {
        name: event.name,
        surfaceId,
        sourceComponentId: componentId,
        timestamp: new Date().toISOString(),
        context: valueOf(event.context, surface.model),
      },
    };
    if (!renderer(message)) reject();
    return { errors: {}, message };
  }
  render(surfaceId, { planning = false } = {}) {
    const surface = this.surfaces.get(surfaceId);
    if (!surface) return "";
    if (!surface.components.size)
      return '<div role="status">Waiting for the agent form.</div>';
    const render = (id) => {
      const item = surface.components.get(id);
      const type = item.component;
      if (type === "Row" || type === "Column")
        return `<div class="kit-stack" data-a2ui-layout="${type}">${item.children.map(render).join("")}</div>`;
      if (type === "Text")
        return `<p>${esc(valueOf(item.text, surface.model))}</p>`;
      const attributes = `data-a2ui-surface="${esc(surfaceId)}" data-a2ui-component="${esc(id)}"`;
      if (type === "Button")
        return `<button type="button" class="kit-btn kit-btn-sm" ${attributes} data-a2ui-action${planning ? " data-plan-submit-answers" : ""}>${esc(item.label)}</button>`;
      const value = valueOf(item.value, surface.model);
      const fieldError =
        surface.errors[id] ||
        (!surface.dismissed.has(id) && item.error
          ? valueOf(item.error, surface.model)
          : "");
      const htmlId = `a2ui-${encodeURIComponent(surfaceId)}-${encodeURIComponent(id)}`;
      const errorId = htmlId + "-error";
      const native =
        planning && id.startsWith("question:")
          ? ` data-plan-answer="${esc(id.slice(9))}"`
          : "";
      const common = `id="${esc(htmlId)}" ${attributes}${native} aria-invalid="${Boolean(fieldError)}"${fieldError ? ` aria-describedby="${esc(errorId)}"` : ""}`;
      const label = `<label for="${esc(htmlId)}" class="plan-label">${esc(item.label)}</label>`;
      let field;
      if (type === "TextArea")
        field = `<textarea class="kit-input" ${common}>${esc(value)}</textarea>`;
      else if (type === "Select")
        field = `<select class="kit-select" ${common}><option value="">Choose one</option>${item.options.map((option) => `<option${option === value ? " selected" : ""}>${esc(option)}</option>`).join("")}</select>`;
      else if (type === "RadioGroup")
        field = `<fieldset ${common}><legend>${esc(item.label)}</legend>${item.options.map((option, n) => `<label><input type="radio" name="${esc(htmlId)}" id="${esc(htmlId)}-${n}" value="${esc(option)}" ${attributes}${option === value ? " checked" : ""}>${esc(option)}</label>`).join("")}</fieldset>`;
      else if (type === "CheckBox")
        field = `<input type="checkbox" ${common}${value === true ? " checked" : ""}>`;
      else
        field = `<input type="text" class="kit-input" ${common} value="${esc(value)}" autocomplete="off">`;
      return `<div>${type === "RadioGroup" ? "" : label}${field}${fieldError ? `<div role="alert" id="${esc(errorId)}">${esc(fieldError)}</div>` : ""}</div>`;
    };
    return `<section data-a2ui-form="${esc(surfaceId)}" class="kit-stack">${render("root")}</section>`;
  }
}

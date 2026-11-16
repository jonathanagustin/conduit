// Quick settings dialog (Ctrl/Cmd + ,): theme, density, and dock width, all
// kept in localStorage. Identity is read-only: the Hub session decides who the
// operator is, and the Hub refuses an approval from a change request's author.

import { DOCK_DEFAULT, readPref, writePref } from "./shell.js";

export const THEME_KEY = "conduit.console.theme";
export const DENSITY_KEY = "conduit.console.density";
export const DOCK_KEY = "conduit.console.dockWidth";
export const TIMEZONE_KEY = "conduit.console.timezone";

export const THEMES = [
  { id: "dark", label: "Dark", hint: "Low-glare panels for long sessions" },
  { id: "light", label: "Light", hint: "High-contrast daylight panels" },
];
export const DENSITIES = [
  { id: "standard", label: "Standard", hint: "32px rows" },
  { id: "compact", label: "Compact", hint: "26px rows" },
];
export const TIMEZONES = [
  { id: "utc", label: "UTC", hint: "24-hour ISO / UTC operator clock" },
  { id: "local", label: "Local", hint: "Browser local timezone" },
];

const esc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );

export function storedTheme() {
  const value = readPref(THEME_KEY, "dark");
  return value === "light" ? "light" : "dark";
}

export function storedDensity() {
  const value = readPref(DENSITY_KEY, "standard");
  return value === "compact" ? "compact" : "standard";
}

export function storedTimezone() {
  const value = readPref(TIMEZONE_KEY, "utc");
  return value === "local" ? "local" : "utc";
}

/** Applies the theme to the root and the top-bar toggle label. */
export function applyTheme(root, theme) {
  if (!root) return;
  root.setAttribute("data-theme", theme);
  const button = root.ownerDocument?.getElementById("theme-toggle");
  if (button) button.textContent = theme === "light" ? "Dark" : "Light";
}

export function applyDensity(root, density) {
  if (root) root.setAttribute("data-density", density);
}

export function applyTimezone(root, timezone) {
  if (root) root.setAttribute("data-timezone", timezone);
}

function pad(n) {
  return String(n).padStart(2, "0");
}

export function formatConsoleTimestamp(value, timezone = storedTimezone()) {
  if (value == null || value === "") return "";
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return "";
  if (timezone === "local") {
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())} Local`;
  }
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())} ${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())} UTC`;
}

function choice(group, options, current) {
  return options
    .map(
      (
        option,
      ) => `<button type="button" role="radio" aria-checked="${option.id === current}"
        class="kit-tab ${option.id === current ? "is-active" : ""}" data-setting="${group}" data-value="${option.id}">
        <span class="font-semibold">${esc(option.label)}</span>
        <span class="kit-muted block text-[10.5px]">${esc(option.hint)}</span>
      </button>`,
    )
    .join("");
}

/** Dialog HTML for the given values. */
export function settingsMarkup({
  theme,
  density,
  timezone = "utc",
  dockWidth,
  operatorName,
}) {
  return `<div class="kit-dialog-backdrop" data-settings-close></div>
  <div class="kit-dialog" role="dialog" aria-modal="true" aria-labelledby="settings-title">
    <div class="kit-dialog-head">
      <h2 id="settings-title">Settings</h2>
      <span class="kit-muted code-id text-[10px]">Ctrl/⌘ + ,</span>
      <button type="button" class="so-btn so-btn-sm" data-settings-close aria-label="Close settings">Close</button>
    </div>
    <section class="kit-dialog-section">
      <h3>Theme</h3>
      <div role="radiogroup" aria-label="Theme" class="kit-choice">${choice("theme", THEMES, theme)}</div>
    </section>
    <section class="kit-dialog-section">
      <h3>Density</h3>
      <div role="radiogroup" aria-label="Density" class="kit-choice">${choice("density", DENSITIES, density)}</div>
    </section>
    <section class="kit-dialog-section">
      <h3>Timestamps</h3>
      <div role="radiogroup" aria-label="Timestamps" class="kit-choice">${choice("timezone", TIMEZONES, timezone || "utc")}</div>
    </section>
    <section class="kit-dialog-section">
      <h3>Right dock</h3>
      <p class="kit-muted">Width ${esc(dockWidth)}px. Drag the dock edge to resize.</p>
      <button type="button" class="so-btn so-btn-sm" data-settings-reset-dock>Reset to ${DOCK_DEFAULT}px</button>
    </section>
    <section class="kit-dialog-section">
      <h3>Identity and approvals</h3>
      ${typeof operatorName === "string" && operatorName.trim() ? `<p>Signed in as <span class="code-id">${esc(operatorName.trim())}</span>. The Hub session sets this.</p>` : "<p>Native operator identity is unavailable.</p>"}
      <p class="kit-muted">A high-risk change needs an approver other than its author. The Hub refuses a self-approval and records the refusal in the audit chain.</p>
    </section>
  </div>`;
}

/**
 * ctx = { getRoot(), getOperatorName(), onDockReset(width), onChange() }
 */
export function createSettingsDialog(ctx) {
  let open = false;

  function host() {
    const root = ctx.getRoot();
    if (!root) return null;
    let el = root.querySelector("#settings-dialog-root");
    if (!el) {
      el = root.ownerDocument.createElement("div");
      el.id = "settings-dialog-root";
      root.appendChild(el);
      el.addEventListener("click", onClick);
    }
    return el;
  }

  function render() {
    const el = host();
    if (!el) return;
    if (!open) {
      el.innerHTML = "";
      return;
    }
    el.innerHTML = settingsMarkup({
      theme: storedTheme(),
      density: storedDensity(),
      timezone: storedTimezone(),
      dockWidth: readPref(DOCK_KEY, String(DOCK_DEFAULT)),
      operatorName: ctx.getOperatorName(),
    });
    const first = el.querySelector('[aria-checked="true"]');
    if (first && typeof first.focus === "function") first.focus();
  }

  function onClick(event) {
    const target = event.target instanceof Element ? event.target : null;
    if (!target) return;
    if (target.closest("[data-settings-close]")) return setOpen(false);
    if (target.closest("[data-settings-reset-dock]")) {
      writePref(DOCK_KEY, DOCK_DEFAULT);
      ctx.onDockReset?.(DOCK_DEFAULT);
      return render();
    }
    const option = target.closest("[data-setting]");
    if (!option) return;
    const value = option.getAttribute("data-value");
    const setting = option.getAttribute("data-setting");
    if (setting === "theme") {
      writePref(THEME_KEY, value);
      applyTheme(ctx.getRoot(), value);
    } else if (setting === "timezone") {
      writePref(TIMEZONE_KEY, value);
      applyTimezone(ctx.getRoot(), value);
    } else {
      writePref(DENSITY_KEY, value);
      applyDensity(ctx.getRoot(), value);
    }
    ctx.onChange?.();
    render();
  }

  function setOpen(next) {
    open = next;
    render();
  }

  /** Returns true when it handled the key. */
  function onKey(event) {
    if ((event.metaKey || event.ctrlKey) && event.key === ",") {
      event.preventDefault();
      setOpen(!open);
      return true;
    }
    if (open && event.key === "Escape") {
      setOpen(false);
      return true;
    }
    return false;
  }

  /** Applies stored theme, density, and timezone on mount. */
  function restore() {
    applyTheme(ctx.getRoot(), storedTheme());
    applyDensity(ctx.getRoot(), storedDensity());
    applyTimezone(ctx.getRoot(), storedTimezone());
  }

  return {
    open: () => setOpen(true),
    close: () => setOpen(false),
    toggle: () => setOpen(!open),
    isOpen: () => open,
    onKey,
    restore,
  };
}

// Quick settings dialog (Ctrl/Cmd + ,): theme, density, and dock width, all
// kept in localStorage. Identity is read-only: the Hub session decides who the
// operator is, and the Hub refuses an approval from a change request's author.

import { DOCK_DEFAULT, readPref, writePref } from "./shell.js";

export const THEME_KEY = "conduit.console.theme";
export const DENSITY_KEY = "conduit.console.density";
export const DOCK_KEY = "conduit.console.dockWidth";

export const THEMES = [
  { id: "dark", label: "Dark", hint: "Low-glare panels for long sessions" },
  { id: "light", label: "Light", hint: "High-contrast daylight panels" },
];
export const DENSITIES = [
  { id: "standard", label: "Standard", hint: "32px rows" },
  { id: "compact", label: "Compact", hint: "26px rows" },
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
export function settingsMarkup({ theme, density, dockWidth, operatorName }) {
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
      <h3>Right dock</h3>
      <p class="kit-muted">Width ${esc(dockWidth)}px. Drag the dock edge to resize.</p>
      <button type="button" class="so-btn so-btn-sm" data-settings-reset-dock>Reset to ${DOCK_DEFAULT}px</button>
    </section>
    <section class="kit-dialog-section">
      <h3>Identity and approvals</h3>
      <p>Signed in as <span class="code-id">${esc(operatorName || "Operator")}</span>. The Hub session sets this.</p>
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
    if (option.getAttribute("data-setting") === "theme") {
      writePref(THEME_KEY, value);
      applyTheme(ctx.getRoot(), value);
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

  /** Applies stored theme and density on mount. */
  function restore() {
    applyTheme(ctx.getRoot(), storedTheme());
    applyDensity(ctx.getRoot(), storedDensity());
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

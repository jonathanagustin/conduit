/* Only these renderers contain operational fixtures. Native views bypass this gate. */
const SCREENS = Object.freeze({
  "s03-saved-views": "Saved views",
  "s04-scope-plan": "Scope and plan",
  "s05-backlog": "Backlog",
  "s06-active-work": "Active work",
  "s13-fleet-overview": "Fleet overview",
  "rm01-env-page": "Legacy environment overview",
  "rm02-env-activity": "Environment activity",
});

export function isOperationalExample(screen) {
  return Object.hasOwn(SCREENS, screen);
}

/** A per-view, identity-bound choice; never persisted or selected by a failed read. */
export function createOperationalExamples({ currentScreen, identity, render }) {
  let active = "";
  let actor = "";
  function enabled(screen) {
    return active === screen && actor === identity();
  }
  function sync(screen) {
    if (active !== screen || actor !== identity()) {
      active = "";
      actor = "";
    }
  }
  function paint(root, screen, renderer) {
    if (!isOperationalExample(screen)) return false;
    sync(screen);
    if (!enabled(screen)) {
      root.innerHTML = `<section class="so-panel p-3 space-y-3" aria-label="${SCREENS[screen]} examples"><p>This view has no native operational data. Sample rows and controls are available in Example data mode.</p><button type="button" class="so-btn" data-example-enable>Show example data</button></section>`;
      root
        .querySelector("[data-example-enable]")
        .addEventListener("click", () => {
          if (currentScreen() !== screen) return;
          active = screen;
          actor = identity();
          render();
        });
      return true;
    }
    root.innerHTML = `<div class="space-y-3"><div role="status" class="so-warn-card p-3 space-y-2"><p>Example data — ${SCREENS[screen]}. These rows and controls are a local prototype, not this Hub's records or execution results.</p><button type="button" class="so-btn" data-example-exit>Exit example mode</button></div><div data-example-body></div></div>`;
    root.querySelector("[data-example-exit]").addEventListener("click", () => {
      active = "";
      actor = "";
      render();
    });
    renderer(root.querySelector("[data-example-body]"));
    return true;
  }
  return { enabled, sync, paint };
}

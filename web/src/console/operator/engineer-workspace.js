// Focus changes layout only. The assistant mount, runtime and Studio remain owned
// by their existing surfaces; no action is sent by entering or leaving focus.
export function createEngineerFocus(root) {
  const backgrounds = [
    root.querySelector("#left-nav"),
    root.querySelector("#main-workspace")?.parentElement,
  ].filter(Boolean);
  const previous = new Map();
  let active = false;
  return {
    set(enabled) {
      if (active === enabled) return;
      active = enabled;
      root.dataset.engineerFocus = String(enabled);
      for (const element of backgrounds) {
        if (enabled) {
          previous.set(element, element.inert);
          element.inert = true;
          element.setAttribute("data-engineer-background", "");
        } else {
          element.inert = previous.get(element);
          element.removeAttribute("data-engineer-background");
        }
      }
      if (!enabled) previous.clear();
    },
    dispose() {
      this.set(false);
      delete root.dataset.engineerFocus;
    },
  };
}

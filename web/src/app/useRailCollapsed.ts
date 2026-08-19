import { useCallback, useEffect, useState } from "react";

/** The collapsed/expanded state of the NavigationRail, resolved from the
 * operator's choice and the viewport.
 *
 * The choice is plain component state, deliberately: the console has no
 * client-state convention to persist through (no store, no settings surface),
 * and the rulebook's default for the browser archetype is the labeled rail, so
 * a fresh session should open expanded rather than restore a narrower one. If a
 * settings surface later gives the console a place to keep preferences, this is
 * the one line that changes.
 *
 * The viewport is the other input. The rulebook's progressive subtraction
 * collapses the category rail at ~720-800px, so below the break the rail is
 * forced to the icon state and the toggle is withdrawn -- expanding a 230px rail
 * onto a 390px viewport would take two thirds of the screen the work needs. The
 * force is OR-ed over the choice, so an operator who collapsed the rail on a
 * wide screen still sees it collapsed on a narrow one, and one who left it
 * expanded has it collapsed for them until the viewport can hold it again. */
const NARROW_QUERY = "(max-width: 800px)";

export interface RailCollapse {
  /** The effective state the rail renders. */
  collapsed: boolean;
  /** Flip the operator's choice, or `undefined` when the state is forced by the
   * viewport and cannot be changed. */
  toggle: (() => void) | undefined;
}

function readNarrow(): boolean {
  if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
    return false;
  }
  return window.matchMedia(NARROW_QUERY).matches;
}

export function useRailCollapsed(): RailCollapse {
  const [narrow, setNarrow] = useState(readNarrow);
  const [chosenCollapsed, setChosenCollapsed] = useState(false);

  useEffect(() => {
    if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
      return;
    }
    const media = window.matchMedia(NARROW_QUERY);
    const onChange = () => setNarrow(media.matches);
    onChange();
    media.addEventListener("change", onChange);
    return () => media.removeEventListener("change", onChange);
  }, []);

  const toggle = useCallback(() => setChosenCollapsed((value) => !value), []);

  return {
    collapsed: narrow || chosenCollapsed,
    toggle: narrow ? undefined : toggle,
  };
}

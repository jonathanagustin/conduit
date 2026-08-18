import { type ReactNode, useState } from "react";
import { Button } from "@blueprintjs/core";

import {
  DEFAULT_FACET_LIMIT,
  type FacetGroup,
  boundFacetValues,
  facetBarWidth,
  facetsAreEmpty,
  largestFacetCount,
} from "./facets.js";
import "./facet-rail.css";

export interface FacetRailProps {
  /** Names the rail for a screen reader; a page may hold more than one. */
  readonly ariaLabel: string;
  readonly groups: readonly FacetGroup[];
  /** What each count is over, in the screen's own words. A count is only
   * readable next to the scope it was taken over, and a rail that leaves that
   * to the reader's assumption is a rail that can be read as the wrong thing. */
  readonly caption: ReactNode;
  /** Shown instead of the groups when there is nothing to summarise, so an
   * empty fleet reads as an empty fleet rather than as a rail of zeroes. */
  readonly empty: ReactNode;
  readonly limit?: number;
  /** Called with the value the Hub spells, and whether it is being turned on.
   * The rail owns no state but which groups are expanded: the selection lives
   * in the URL, where the rest of the screen's filters already live. */
  readonly onToggle: (key: string, value: string, select: boolean) => void;
}

/** A faceted filter rail: headed groups of selectable values, each with its
 * count and a bar proportional to the largest count in its group.
 *
 * This component knows no vocabulary. It is given groups and reports clicks,
 * which is what lets it sit in `components/` -- a second index screen wanting
 * the same rail must not have to import it out of somebody's feature, because
 * `scripts/verify-console-boundaries.mjs` forbids that and would be right to.
 * Everything specific to a screen -- which dimensions exist, what they are
 * called, which query parameter each writes -- is supplied by the caller. */
export function FacetRail({
  ariaLabel,
  groups,
  caption,
  empty,
  limit = DEFAULT_FACET_LIMIT,
  onToggle,
}: FacetRailProps) {
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(new Set());
  const toggleExpanded = (key: string) =>
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });

  return (
    <nav className="console-facet-rail" aria-label={ariaLabel} data-testid="facet-rail">
      <p className="console-facet-rail__caption">{caption}</p>
      {facetsAreEmpty(groups) ? (
        <p className="console-facet-rail__empty">{empty}</p>
      ) : (
        groups
          .filter((group) => group.values.length > 0)
          .map((group) => {
            const open = expanded.has(group.key);
            const { shown, hidden } = boundFacetValues(group.values, open ? 0 : limit);
            const largest = largestFacetCount(group.values);
            return (
              <section
                key={group.key}
                className="console-facet-rail__group"
                aria-labelledby={`facet-${group.key}`}
                data-testid={`facet-${group.key}`}
              >
                <h2 className="console-facet-rail__heading" id={`facet-${group.key}`}>
                  {group.heading}
                </h2>
                <ul className="console-facet-rail__values">
                  {shown.map((value) => (
                    <li key={value.value}>
                      <button
                        type="button"
                        className="console-facet-rail__value"
                        aria-pressed={value.selected}
                        onClick={() => onToggle(group.key, value.value, !value.selected)}
                      >
                        <span className="console-facet-rail__label">{value.value}</span>
                        <span className="console-facet-rail__count">{value.count}</span>
                        {/* The bar restates the count and is hidden from
                          assistive technology, which already has the number. */}
                        <span className="console-facet-rail__bar" aria-hidden="true">
                          <span style={{ width: `${facetBarWidth(value.count, largest)}%` }} />
                        </span>
                      </button>
                    </li>
                  ))}
                </ul>
                {hidden > 0 || open ? (
                  <Button
                    variant="minimal"
                    size="small"
                    className="console-facet-rail__more"
                    icon={open ? "chevron-up" : "chevron-down"}
                    // The count is in the label because a bound that does not
                    // say what it left out is indistinguishable from the end of
                    // the list.
                    text={open ? "Show fewer" : `${hidden} more`}
                    onClick={() => toggleExpanded(group.key)}
                  />
                ) : null}
              </section>
            );
          })
      )}
    </nav>
  );
}

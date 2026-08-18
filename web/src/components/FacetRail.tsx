import { type HTMLAttributes, type ReactNode, useState } from "react";
import { Button, Classes, Collapse, ProgressBar, Section, SectionCard, Tag } from "@blueprintjs/core";

import {
  DEFAULT_FACET_LIMIT,
  type FacetGroup,
  type FacetValue,
  boundFacetValues,
  facetBarValue,
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

/** `Section` renders an `H6` by default, which would jump five levels below the
 * page's `H1`. The console already corrects this the same way in OverviewPage:
 * the heading level is the document's, not the component's. */
function SectionHeading(props: HTMLAttributes<HTMLElement>) {
  return <h2 {...props} />;
}

/** A faceted filter rail: headed groups of selectable values, each with its
 * count and a bar proportional to the largest count in its group.
 *
 * Composed rather than drawn. A group is a Blueprint `Section`/`SectionCard`,
 * which is the panel this console already uses eleven times over; a value is a
 * `Button` whose selected state is Blueprint's own `active`; the count is a
 * `Tag`; the bar is a static `ProgressBar`; and the bounded tail is a
 * `Collapse`. What is left in facet-rail.css is the rail's place on the page and
 * the one thing Blueprint does not compose -- a button whose content is a label
 * that takes the slack with a count and a bar pinned after it.
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
      <p className={`console-facet-rail__note ${Classes.TEXT_MUTED} ${Classes.TEXT_SMALL}`}>
        {caption}
      </p>
      {facetsAreEmpty(groups) ? (
        <p className={`console-facet-rail__note ${Classes.TEXT_MUTED}`}>{empty}</p>
      ) : (
        groups
          .filter((group) => group.values.length > 0)
          .map((group) => {
            const open = expanded.has(group.key);
            const { shown, overflow } = boundFacetValues(group.values, limit);
            const largest = largestFacetCount(group.values);
            const row = (value: FacetValue) => (
              <FacetButton
                key={value.value}
                value={value}
                largest={largest}
                onClick={() => onToggle(group.key, value.value, !value.selected)}
              />
            );
            return (
              <Section
                key={group.key}
                compact
                className="console-facet-rail__group"
                titleRenderer={SectionHeading}
                title={group.heading}
                data-testid={`facet-${group.key}`}
              >
                <SectionCard padded={false}>
                  {shown.map(row)}
                  {/* Collapsed, not discarded. The control below names how many
                      are behind it, so a bound list never reads as the end of
                      the list. */}
                  <Collapse isOpen={open}>{overflow.map(row)}</Collapse>
                  {overflow.length > 0 ? (
                    <Button
                      variant="minimal"
                      size="small"
                      fill
                      alignText="start"
                      icon={open ? "chevron-up" : "chevron-down"}
                      text={open ? "Show fewer" : `${overflow.length} more`}
                      onClick={() => toggleExpanded(group.key)}
                    />
                  ) : null}
                </SectionCard>
              </Section>
            );
          })
      )}
    </nav>
  );
}

function FacetButton({
  value,
  largest,
  onClick,
}: {
  value: FacetValue;
  largest: number;
  onClick: () => void;
}) {
  return (
    <Button
      className="console-facet-rail__value"
      variant="minimal"
      size="small"
      fill
      alignText="start"
      active={value.selected}
      aria-pressed={value.selected}
      onClick={onClick}
    >
      {/* A Product coordinate is longer than any rail column, so the label
          ellipsises and carries its full value as a title -- truncating a value
          the reader cannot recover is the same defect as dropping one. */}
      <span className="console-facet-rail__label" title={value.value}>
        {value.value}
      </span>
      <Tag minimal round>
        {value.count}
      </Tag>
      {/* The bar restates the count, so it is hidden from assistive technology,
          which already has the number from the Tag. */}
      <ProgressBar
        className="console-facet-rail__bar"
        value={facetBarValue(value.count, largest)}
        stripes={false}
        animate={false}
        intent={value.selected ? "primary" : "none"}
        aria-hidden
      />
    </Button>
  );
}

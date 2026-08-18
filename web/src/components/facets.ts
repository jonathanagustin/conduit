/** The arithmetic behind a faceted filter rail, kept apart from the component
 * that draws it so it can be unit-tested and mutated without a browser.
 *
 * None of this decides what a count means -- that is the Hub's, and
 * docs/ui/contracts/fleet-facets.md states it. What is decided here is the order
 * values appear in, how many of them are shown before the list is bounded, and
 * what a bar's length is a proportion of. Each of those has a wrong answer that
 * looks right: an order that hides the value you came for, a bound that drops
 * values without saying so, and a bar measured against the wrong denominator so
 * every bar is nearly full. */

export interface FacetValue {
  /** The value as the Hub spells it. This is what goes in the URL, so it is
   * never a display string. */
  readonly value: string;
  readonly count: number;
  readonly selected: boolean;
}

export interface FacetGroup {
  /** The query parameter this group selects on. The Hub serves each facet under
   * the name of the parameter that filters it, so this is one string rather than
   * a facet name and a separate URL key that could disagree. */
  readonly key: string;
  readonly heading: string;
  readonly values: readonly FacetValue[];
}

/** How many values a group shows before the rest are folded behind a control
 * that names how many they are. The 2020 Apollo capture bounds its INSTALLATION
 * facet at ten with "64 more" below it; the number itself is a judgement, the
 * bounding is not -- an open-vocabulary dimension has no upper size. */
export const DEFAULT_FACET_LIMIT = 8;

/** Order a dimension's counts for display.
 *
 * Largest first, because a facet is read to find where the fleet actually is.
 * Ties break on `vocabulary` order -- the console's declared status list, which
 * runs from the calm value to the alarming one -- and then alphabetically, so
 * two values with the same count never swap places between renders.
 *
 * `selected` is always present in the result even when the Hub sent no count
 * for it. A selected value with no count is a real state: the facets exclude
 * their own dimension, so a selection that matches nothing still has to be
 * visible, or it cannot be undone from the rail that applied it. */
export function orderFacetValues(
  counts: Readonly<Record<string, number>>,
  selected: string | undefined,
  vocabulary: readonly string[] = [],
): FacetValue[] {
  const values = new Map<string, number>();
  for (const [value, count] of Object.entries(counts)) {
    if (count > 0) values.set(value, count);
  }
  if (selected !== undefined && selected !== "" && !values.has(selected)) values.set(selected, 0);

  const rank = (value: string) => {
    const index = vocabulary.indexOf(value);
    // A value the console has never heard of sorts after the ones it declares,
    // rather than before them or in place of one of them. The Hub's
    // vocabularies grow, and a new status must still be selectable.
    return index === -1 ? vocabulary.length : index;
  };
  return [...values.entries()]
    .map(([value, count]) => ({ value, count, selected: value === selected }))
    .sort((left, right) => {
      if (left.count !== right.count) return right.count - left.count;
      if (rank(left.value) !== rank(right.value)) return rank(left.value) - rank(right.value);
      return left.value.localeCompare(right.value);
    });
}

export interface BoundedFacet {
  readonly shown: readonly FacetValue[];
  /** How many values the bound left out. Never inferred by the caller from
   * lengths: the count is what the "more" control has to say, and a control
   * that folds values away without naming how many is a truncation the reader
   * cannot see. */
  readonly hidden: number;
}

/** Bound a group to `limit` values, keeping a selected value visible.
 *
 * A selected value below the bound would otherwise disappear behind the "more"
 * control the moment it was chosen, leaving a rail showing a filter that is
 * applied and no way to lift it. Keeping it costs at most one extra row,
 * because a dimension carries at most one selection. */
export function boundFacetValues(
  values: readonly FacetValue[],
  limit: number = DEFAULT_FACET_LIMIT,
): BoundedFacet {
  if (limit <= 0 || values.length <= limit) return { shown: values, hidden: 0 };
  const head = new Set(values.slice(0, limit));
  const shown = values.filter((value) => head.has(value) || value.selected);
  return { shown, hidden: values.length - shown.length };
}

/** The largest count in a group, which is what a bar is a proportion of. */
export function largestFacetCount(values: readonly FacetValue[]): number {
  return values.reduce((largest, value) => (value.count > largest ? value.count : largest), 0);
}

/** A bar's width as a percentage of the group's largest count.
 *
 * Against the largest count rather than the group's total, so the bars compare
 * the values in one dimension against each other -- which is the comparison the
 * rail is for. Against a total, a dimension with many values would render every
 * bar as a sliver and stop distinguishing anything. The count is printed beside
 * the bar in either case; the bar is the secondary signal, as the status tags
 * treat colour. */
export function facetBarWidth(count: number, largest: number): number {
  if (largest <= 0 || count <= 0) return 0;
  return Math.min(100, (100 * count) / largest);
}

/** Whether a rail has anything at all to show. An empty rail must read as an
 * empty fleet, not as every count being zero. */
export function facetsAreEmpty(groups: readonly FacetGroup[]): boolean {
  return groups.every((group) => group.values.length === 0);
}

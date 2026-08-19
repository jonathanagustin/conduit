import { type FacetGroup, orderFacetValues } from "../../components/index.js";
import {
  CONFIG,
  HEALTH,
  LIFECYCLE,
  ROLLOUT,
  type FleetFacets,
  type FleetQuery,
} from "../../api/types.js";

/** The fleet's dimensions, in the order the rail shows them.
 *
 * `key` is a FleetQuery field, a `facets` key and a URL parameter at the same
 * time, which is deliberate: three names for one dimension is how a rail comes
 * to count one thing and filter another. `vocabulary` is only a tie-break for
 * equal counts, and is the console's declared status list -- held to the Hub's
 * Go consts in both directions by scripts/verify-console-contract.mjs, because
 * a list that invented a value would offer a filter that matches nothing and
 * one that dropped a real value would order it last for no reason.
 *
 * Environment and Product have no declared vocabulary because they have no
 * closed one: they are whatever this deployment installs, which is the case the
 * bounded list and its "N more" control exist for. */
const DIMENSIONS: readonly {
  key: keyof FleetFacets & keyof FleetQuery;
  heading: string;
  vocabulary: readonly string[];
}[] = [
  { key: "environment", heading: "Environment", vocabulary: [] },
  { key: "product", heading: "Product", vocabulary: [] },
  { key: "health", heading: "Health", vocabulary: HEALTH },
  { key: "rolloutStatus", heading: "Rollout", vocabulary: ROLLOUT },
  { key: "configStatus", heading: "Config", vocabulary: CONFIG },
  { key: "lifecycle", heading: "Lifecycle", vocabulary: LIFECYCLE },
];

/** Turn the Hub's facet counts into rail groups. Exported for its own unit
 * test: the mapping from a dimension to the parameter it writes is the part
 * that has to be right, and a browser test proving one click works would not
 * prove the other five write the parameters they claim to. */
export function fleetFacetGroups(facets: FleetFacets, query: FleetQuery): FacetGroup[] {
  return DIMENSIONS.map((dimension) => ({
    key: dimension.key,
    heading: dimension.heading,
    values: orderFacetValues(facets[dimension.key] ?? {}, query[dimension.key], dimension.vocabulary),
  }));
}

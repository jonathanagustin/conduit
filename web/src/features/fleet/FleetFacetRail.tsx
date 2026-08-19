import { FacetRail } from "../../components/index.js";
import type { FleetFacets, FleetQuery } from "../../api/types.js";
import { fleetFacetGroups } from "./facets.js";

export interface FleetFacetRailProps {
  readonly facets: FleetFacets;
  readonly query: FleetQuery;
  /** The same setter the rest of the screen's filters use, so a facet click and
   * a filter change are one mechanism and land in one place: the URL. */
  readonly onFilter: (key: keyof FleetQuery, value: string | undefined) => void;
}

export function FleetFacetRail({ facets, query, onFilter }: FleetFacetRailProps) {
  return (
    <FacetRail
      ariaLabel="Fleet filters"
      groups={fleetFacetGroups(facets, query)}
      // Said in full because the counts are not the counts in the table, and a
      // reader who assumes they are will read "HEALTHY 40" beside four rows and
      // conclude the table is broken.
      caption={
        <>
          Counts are installations matching the other filters. Each dimension
          ignores its own selection, so a count says how many rows you would get
          by choosing it.
        </>
      }
      empty="No installations to summarise."
      onToggle={(key, value, select) =>
        onFilter(key as keyof FleetQuery, select ? value : undefined)
      }
    />
  );
}

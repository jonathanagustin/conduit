import { Tag } from "@blueprintjs/core";
import type { FleetSummary } from "../../api/types.js";
import "./summary-bar.css";

/** The headline above the table: how many installations the current filters
 * match, and how many of those have stopped reporting.
 *
 * The per-health tags this used to carry have moved to the facet rail. They had
 * to move rather than be duplicated: `summary` describes the result and
 * `facets` describes what each filter would give, so the moment a health is
 * selected the two counts for HEALTHY differ -- and two numbers of the same name
 * meaning different things on one screen is worse than either alone. What is
 * left here only ever describes the result, which is what a headline should do.
 *
 * Stale is always present, because a stale count of zero is information. */
export function SummaryBar({ summary }: { summary: FleetSummary }) {
  return (
    <div className="console-summary-bar" role="status" aria-label="Fleet summary">
      <Tag large minimal>
        {summary.total} installations
      </Tag>
      <Tag large minimal intent={summary.stale > 0 ? "warning" : "none"}>
        STALE {summary.stale}
      </Tag>
    </div>
  );
}

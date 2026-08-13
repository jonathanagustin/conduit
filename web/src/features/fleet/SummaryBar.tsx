import { Tag } from "@blueprintjs/core";
import type { FleetSummary } from "../../api/types.js";
import "./summary-bar.css";

/** The counts strip above the table. Only non-zero buckets are shown, so the
 * strip stays readable as the vocabulary grows, but Total and Stale are always
 * present -- a stale count of zero is information. */
export function SummaryBar({ summary }: { summary: FleetSummary }) {
  const buckets = Object.entries(summary.health)
    .filter(([, count]) => count > 0)
    .sort(([left], [right]) => left.localeCompare(right));
  return (
    <div className="console-summary-bar" role="status" aria-label="Fleet summary">
      <Tag large minimal>
        {summary.total} installations
      </Tag>
      {buckets.map(([status, count]) => (
        <Tag key={status} large minimal>
          {status} {count}
        </Tag>
      ))}
      <Tag large minimal intent={summary.stale > 0 ? "warning" : "none"}>
        STALE {summary.stale}
      </Tag>
    </div>
  );
}

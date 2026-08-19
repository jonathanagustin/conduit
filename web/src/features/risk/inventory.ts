import {
  findingScore,
  severityTier,
  type ActiveFindingRow,
  type SeverityTier,
} from "../../api/catalog.js";

/** Pure ordering and filtering for the cross-Release vulnerability inventory,
 * kept out of the component so the rules are unit-pinned rather than only
 * exercised through the browser. */

/** Inventory rows worst-first by the score the Hub would give each finding, so
 * the table's order is the order `ReleaseRiskScore` is built from. Ties fall
 * back to Product, then version, then CVE id, so the list is stable between
 * renders and between the fan-out's arrival order. */
export function orderInventoryRows(rows: readonly ActiveFindingRow[]): ActiveFindingRow[] {
  return [...rows].sort((left, right) => {
    const byScore = findingScore(right.finding) - findingScore(left.finding);
    if (byScore !== 0) {
      return byScore;
    }
    const byProduct = left.product.localeCompare(right.product);
    if (byProduct !== 0) {
      return byProduct;
    }
    const byVersion = left.version.localeCompare(right.version);
    return byVersion !== 0 ? byVersion : left.finding.id.localeCompare(right.finding.id);
  });
}

/** The severity tiers actually present in the inventory, in worst-first order,
 * so the filter offers only the tiers there is something to filter to. */
export function inventoryTiers(rows: readonly ActiveFindingRow[]): SeverityTier[] {
  const present = new Set<SeverityTier>(rows.map((row) => severityTier(row.finding)));
  return (["CRITICAL", "HIGH", "MEDIUM", "LOW", "NONE"] as const).filter((tier) =>
    present.has(tier),
  );
}

/** Rows in one tier, or all rows when no tier is selected. */
export function filterInventoryByTier(
  rows: readonly ActiveFindingRow[],
  tier: SeverityTier | undefined,
): ActiveFindingRow[] {
  if (tier === undefined) {
    return [...rows];
  }
  return rows.filter((row) => severityTier(row.finding) === tier);
}

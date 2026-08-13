import type { FleetInstallation } from "../../api/types.js";

/** Free-text matching for the fleet table.
 *
 * Extracted from the page so it can be tested and mutated directly. The Hub has
 * no free-text parameter, so this is the console's own behaviour and is exactly
 * the kind of logic that looks obviously right and is easy to get subtly wrong:
 * which fields are searched, and whether the match is case-insensitive. */
export function matchesSearch(item: FleetInstallation, search: string): boolean {
  const needle = search.trim().toLowerCase();
  if (needle === "") {
    return true;
  }
  return (
    item.installationId.toLowerCase().includes(needle) ||
    item.environment.toLowerCase().includes(needle) ||
    item.product.toLowerCase().includes(needle)
  );
}

export function filterFleet(
  installations: FleetInstallation[],
  search: string,
): FleetInstallation[] {
  return installations.filter((item) => matchesSearch(item, search));
}

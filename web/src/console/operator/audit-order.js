// Preserve returned projection order for proof checks. Display order is a
// separate copy, so an ascending read and a descending read both show recent
// recorded events first without changing hashed sequence/predecessor inputs.
export function newestAuditEntries(entries, order = "asc", limit) {
  const rows = Array.isArray(entries) ? entries.slice() : [];
  if (order !== "desc") rows.reverse();
  return limit === undefined ? rows : rows.slice(0, limit);
}

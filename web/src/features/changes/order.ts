import type { ChangeRequest } from "../../api/resources.js";

/** Inbox ordering.
 *
 * Requests awaiting approval come first because they are the only ones that
 * need a person; everything else is history, ordered by recency. Extracted from
 * the page so the rule can be tested and mutated on its own -- a comparator is
 * the classic place where an inverted sign passes every screenshot. */
export function orderInbox(requests: ChangeRequest[]): ChangeRequest[] {
  const waiting = (status: string) => (status === "PENDING_APPROVALS" ? 0 : 1);
  return [...requests].sort((left, right) => {
    const byUrgency = waiting(left.status) - waiting(right.status);
    return byUrgency !== 0 ? byUrgency : right.updatedAt.localeCompare(left.updatedAt);
  });
}

/** How many reviewer requirements nobody has satisfied yet. */
export function outstandingRequirements(request: ChangeRequest): number {
  return request.requirements.filter(
    (requirement) => (requirement.satisfiedBy ?? []).length === 0,
  ).length;
}

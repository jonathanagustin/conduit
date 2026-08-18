import { reviewBody, type ChangeRequest, type ReviewAction } from "../../api/resources.js";

/** Which review actions the Hub will actually admit, for this viewer and this
 * request.
 *
 * Cancel and Reopen are not review decisions and the Hub does not treat them as
 * such: it admits only the request's own author, and only from the statuses each
 * transition can legally leave. The console offered all four to everyone, which
 * made two permanently non-functional buttons -- and because every action was
 * gated on a rationale, a reviewer was first made to type one and then handed a
 * 403.
 *
 * The status sets are transcribed from the Go rather than inferred:
 *
 *   - Reopen: GovernanceService.Reopen in internal/controlplane/governance.go
 *     refuses any principal but the author, then refuses any status but
 *     REJECTED, CANCELLED or ERROR.
 *   - Cancel: the POST /v1/change-requests/{id}/cancel handler in
 *     internal/controlplane/http.go checks the author and admits only
 *     PENDING_APPROVALS or APPROVED, and GovernanceService.Cancel repeats both
 *     checks under the record's write lock.
 *
 * Approve and Reject stay unconditional. Their gate is principalCanReview --
 * team membership evaluated against each outstanding requirement, including
 * teams reached through grants -- which the session projection does not answer.
 * Guessing it here would hide a decision from a reviewer the Hub would have
 * admitted, and a missing approval button on a production change is the worse
 * of the two failures. */
export const CANCELLABLE_STATUSES: readonly string[] = ["PENDING_APPROVALS", "APPROVED"];
export const REOPENABLE_STATUSES: readonly string[] = ["REJECTED", "CANCELLED", "ERROR"];

/** Whether the viewer is the principal the Hub will compare against `author`.
 *
 * The actor, not the effective principal. The proposing routes pass the
 * resolved principal itself as the author (`governance.ProposeChanges(...,
 * principal, ...)` in http.go), and Reopen and Cancel compare
 * `request.Author.Username` to `principal.Username` -- neither side is put
 * through EffectivePrincipal. Under delegation both are the acting service
 * account's name rather than the person's, so reading `effective` here would
 * hide Reopen from the only principal the Hub admits for it.
 *
 * An unknown viewer -- the session is still loading, or could not be read --
 * matches nobody, because an empty username is not an author. */
export function isAuthor(request: ChangeRequest, viewer: string): boolean {
  return viewer !== "" && request.author.username === viewer;
}

export function canCancel(request: ChangeRequest, viewer: string): boolean {
  return isAuthor(request, viewer) && CANCELLABLE_STATUSES.includes(request.status);
}

export function canReopen(request: ChangeRequest, viewer: string): boolean {
  return isAuthor(request, viewer) && REOPENABLE_STATUSES.includes(request.status);
}

/** Whether an action carries the rationale the form collects.
 *
 * Derived from the wire body rather than restated, so the screen and the
 * request cannot disagree: reopen is the one action with no field to put a
 * rationale in, because requireEmptyBody refuses any body at all (http.go POST
 * /v1/change-requests/{id}/reopen). The console used to demand a rationale for
 * reopening under a placeholder promising it was "recorded against your name",
 * and then drop it on the floor -- the text was never sent, on success or on
 * failure. Adding a rationale field to the Hub is the other way to honour that
 * promise; until someone does, the screen must stop making it. */
export function carriesRationale(action: ReviewAction): boolean {
  return reviewBody(action, "") !== undefined;
}

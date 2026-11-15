# Native change review, diff and constraint views

The existing Change Requests area hosts Requests (`rm07-change-requests`),
Approvals (`s16-approvals-board`), Diff (`s10-diff`) and Constraint gate
(`rm04-constraints`). No second approval path is introduced.

The approvals board groups the visible `/v1/plans` index by actual status.
`RECORDED` plans have a separate non-executable column, and unknown statuses are
retained in an Other column. Cards show recorded approval-row counts and required
counts when provided, without deriving quorum, signatures, percentages or an
execution decision. Selecting a card opens that actual Plan in the existing
inspector. Example targets, crypto roots, convergence percentages and gate toggles
are removed from these affected views.

Request review retains status filters, reason-required approve/reject and explicit
confirmation through the same existing Hub endpoints. Selection is persisted
before rendering; an explicit missing ID never falls back to another request.
A filter change clears the draft. A request revision, scope, evaluation,
requirement, label or signed-in-principal change clears both reason and decision.
Editing the reason removes the prepared confirmation. The shared header and
inspector context follow the same selected request. The old area-wide diff strip
is removed so another request's fields do not precede the selected detail.

Review identity comes only from the Hub session's effective/actor username, not
the operator display preference or its fallback label. Unknown identity or author
and self-review have no action controls. The UI does not predict resource-role
permission: Hub authorization, current policy, status and no-self-approval checks
remain authoritative at the existing write endpoint. Pending writes disable the
form and selection; confirming binds to the current visible request context.

The detail and Diff view render the selected request's supplied `changes[].diff`
side by side, sorted by field path. Numbered JSON lines refer to this display,
not source-file locations. Missing sides stay “not recorded”; values are not
invented from arbitrary request bodies. Displays are bounded to 128 entries,
8 KiB per side and 80 lines, with truncation labels. Known sensitive field names
are masked at the path and nested-key level; this is a presentation filter, not
a substitute for API authorization or a general secret classifier. A fixed
allowlist of recorded request fields also exposes native Entity window times and
type for review without dumping opaque request payloads.

Required reviewers and policy outcomes come from actual request requirements and
evaluations. The contract has no risk rating, so risk is explicitly not recorded.
The read-only constraint gate shows that evidence and the no-self-approval rule,
then links back to the existing review flow. It does not compute a maintenance,
artifact, signature or deployment grant. These are Conduit extensions over native
Hub records, not private protocol compatibility claims.

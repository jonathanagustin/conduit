# Workspace idle cleanup failures

The existing developer-workspace idle scan runs when workspace records are
listed; this change does not introduce a background scheduler. For eligible
running or starting records at the idle deadline, cleanup must confirm process
stop and pod deletion before recording the workspace as stopped. An adapter's
explicit not-found result is idempotent absence. An unavailable cluster is not
absence, and the recorded fallback cannot confirm deletion of a real pod.

The scan records `workspace.reap_requested` before cleanup and
`workspace.compute_reclaimed` after confirmed cleanup, under the system subject
`system:workspace-reaper`. Receipts identify the workspace, owner, idle reason
and retained working copy. Failed cleanup records a denied outcome. Adapter
error text is not copied into the audit reason. A missing/failing audit sink
prevents an unaudited attempt or a successful stopped transition. A persistence
failure after cleanup leaves the record eligible for retry; its compute receipt
still describes the confirmed cleanup, not a successful catalog update. A scan
returns previously completed IDs together with a later error.

The local process adapter respects cancellation, propagates kill errors, and
requires the supervisor's exit notification before reporting stopped. A missing
process handle, cancelled wait or bounded one-second wait timeout returns an
error. A later confirmed retry can finish the stopped transition.

Cleanup never calls catalog deletion or file deletion, pushes a branch, expires
a volume or claims work has been pushed. There are no new Kubernetes verbs,
credentials, namespaces or cluster-wide operations. The existing pod deletion
remains namespace-scoped. A no-cluster recorded creation can no longer stand in
for confirmed primary deletion; when cluster access is unavailable, cleanup
remains pending rather than claiming resources were reclaimed.

This is a correction to the registered Conduit-native developer workspace
behavior (`devworkspace.WorkspaceRecord`), not a private Apollo lifecycle claim.
The broader #635 requirements remain open: the #618 branch-backed session
lifecycle, transition deadlines, orphan inventory, durable retention/volume
cleanup, scheduling and corresponding least-privilege integration. The current
in-memory catalog and scan do not establish restart-safe orphan discovery or
multi-replica synchronization. These changes do not close #635.

Verification covers stop/delete/unavailable-cluster errors, inaccurate runtime
status, missing dependencies, audit and persistence failures, retry, retained
files, cancellation, deadline boundaries, partial progress and observed process
exit. All process commands in these tests belong to their test runtime; no real
workspace or cluster cleanup is performed by this verification.

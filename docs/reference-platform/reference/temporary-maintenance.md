# Temporary Entity maintenance windows

Sources and exact reviewed hashes are indexed in `review-ledger.tsv`. The
Experimental CLI parent/create pages establish one request per unique AEID,
cross-Environment submission, one common start/end interval, positive duration
at most 3600h, approval waiting consuming that interval, default no-downtime,
reason/ticket retention, dry-run and stop-on-partial-failure behavior. They do
not establish a private override API, output schema or Agent protocol.

`conduit entity maintenancewindowoverride create AEID... --duration 2h --reason
"CAB-364"` (alias `entity mwo create`) submits native proposals. `--downtime`
permits downtime; without it, work requiring downtime stays blocked.
`--compliance-ticket` is retained on each Change Request. Normal approval policy
can require review or auto-approve; the CLI itself grants no approval. The
native CLI requires a reason and JSON/YAML output before any mutation. A partial
result prints already-created requests and identifies the failed target, exits
unsuccessfully and leaves later targets unsubmitted. An uncertain network
response is not proof that no request was created: inspect existing requests
before repeating a submission.

The CLI accepts the bounded Conduit `aeid:ENVIRONMENT:helm-chart:ENTITY` shape;
private or other Entity kinds are not inferred.

The native batch route is `POST /v1/temporary-maintenance-overrides`. Its body
names Environment/Entity coordinates, duration, reason, ticket, downtime and
dry-run. The server computes one common start, deduplicates in order and checks
propose authority for each Environment. Each individual creation and its audit
are transactional on memory/PostgreSQL. FileRepository serializes visibility on
its instance but is not a multi-file crash transaction. A batch intentionally
retains successful earlier submissions if a later one fails.

The current approved request is the durable grant. Its original fixed interval,
Entity/installation/Product identity and type are sealed into the native target
identity. A pending, rejected, held, cancelled, malformed or expired request
cannot open a window. Approval never extends an end time; approving an already
expired request does not revive it. No Environment-wide permanent override is
written, and unrelated Entities do not inherit the grant.

## Native future-window extension

The Windows console can send optional RFC3339 `startsAt` and
`expectedChangeRequestId`. These are Conduit-native extensions, not a recovered
Apollo scheduler or a change to the CLI's submission-time default. An explicit
start must be nonzero and not precede server submission time; its end must remain
within 3600 hours of submission. Duration stays positive and at most 3600 hours.
The exact original interval remains sealed through preview, submission and review.
A future approved request cannot open admission before its start.

Preview binding requires one Entity and an explicit start. Within the existing
repository boundary, submission recomputes the native request identity against
the current Entity/installation/Product mapping and refuses a different expected
ID before persistence. Policy is still evaluated normally at submission; preview
is not approval. The console requires preview, an unchanged draft and explicit
confirmation, and does not automatically retry an uncertain write.

`TestScheduledMaintenancePreviewBindingAndFutureAdmission` covers stable preview,
pre-start admission and fixed intervals on available repository implementations.
`TestScheduledMaintenanceRefusesChangedPreviewTarget` covers mapping changes before
persistence. HTTP and console tests cover strict fields, receipt binding, draft
editing and the existing governed proposal route.

## Agent admission and upgrade order

Upgrade the Hub before upgrading Agents. New Agents use
`POST /v1/environments/{environmentID}/maintenance-admission/leases` and
`POST /v1/plans/{planID}/maintenance-admission/start`; they fail closed if these
routes are absent. Both authenticate the Agent using the existing verified
identity carrier (or the explicitly enabled development headers). Start also
requires matching Environment, Agent lease ownership and opaque token.

Within one repository serialization boundary, admission reads currently
approved requests and projects matching, active windows into a copy of the
Plan. Lease/start recheck the schedule at their transition time. Admission audit records bind the Plan and current request IDs before acknowledgment;
audit failure rolls back the transition on transactional repositories. The stored
Plan and desired-state snapshot retain their original maintenance constraints.
The ordinary lease route therefore remains restricted to the base schedule,
including when an older Hub/Agent reads the records. A projection is not a new
Plan approval: the workload still needs its normal approval, suppression and
rollout checks. Imperative commands retain their separate explicit policy;
optimizer Plans retain their own admission boundary.

The updated Worker checks maintenance before shared material mutations and
each forward step. Helm checks again before registry/image preparation and the
actual uninstall/restart/upgrade command, including after potentially slow
rendering. End times are exclusive. Work already admitted before the end may
continue to completion; expiry does not undo an in-flight Kubernetes operation.
Rollback remains available after expiry. A held request is rechecked at start;
its status is not polled synchronously during an admitted Helm operation.

`TestTemporaryMaintenanceCLIRealHubPartialResultsAndRestart` exercises the real
CLI/Hub proposal, approval, restart, native lease and scoped start. Core tests
cover memory/file/PostgreSQL, approval delay, end-time boundaries, ordinary
lease isolation, held requests and audit rollback. Agent tests advance time
after Helm rendering and verify that no upgrade command runs after expiry.

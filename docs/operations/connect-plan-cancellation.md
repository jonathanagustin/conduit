# Native Plan cancellation over Connect

`conduit.plans.v1.PlanCancellationService/CancelPlan` dual-serves the existing
`POST /v1/plans/{planID}/cancel` command. Both call `CancelPlanAsOperator`;
Connect adds no cancellation, suppression or approval authority. Existing
console and CLI cancellation callers remain REST. No automatic retry or REST
fallback is introduced.

## Authority and request

Authentication runs before protobuf decoding, with the original carrier retained
through native governance. A governed Plan can be cancelled by its Change Request
author. An ungoverned Plan requires Operate on its Environment. Creating a
suppression additionally requires Operate, including for a governed author.
Delegation must itself be permitted; a represented administrator is not a
bootstrap administrator. Cookie-authenticated POSTs retain the existing origin
and CSRF requirements. Production does not accept development identity headers.

The request names `plan_id`, `reason`, `create_suppression`,
`suppression_duration_seconds` and `installation_id`. Payloads are bounded to
1 MiB. Duration uses int64 on the wire and must fit the native integer type.
The existing native command validates reason, status, requested installation and
suppression duration. Zero requests the native 30-minute suppression default;
explicit suppression durations must be between one minute and 30 days. Duration
is otherwise ignored by native cancellation, as in REST.

The successful result contains a Plan summary, the recorded cancellation
subject/reason/timestamp and the linked Change Request ID. It is not a full Plan
detail or a suppression object. Revision remains uint64; timestamps preserve
nanoseconds. Execution lease tokens and complete desired/execution state are
not represented. Invalid UTF-8 in native strings is replaced for protobuf output.

## Sequential effects and errors

The native command cancels the Plan, then its linked Change Request, then creates
any requested suppression. These effects are sequential. A later error can
follow an already persisted cancellation. Inspect authoritative state before any
manual recovery; an error is not permission to replay a command.

Authorization failures map to permission denied, missing Plans to not found,
native invalid transitions to failed precondition, conflicts to aborted, and
request validation failures to invalid argument. Internal diagnostic strings are
not returned. Error status does not establish whether earlier effects persisted.

If a valid native cancellation result cannot be serialized, an internal error
carries `PlanCancellationOutcome` with `command_completed: true`. This reports
that the native command completed; it is not a retry permit. Invalid or unbound
native results do not receive that evidence. Absent outcome details mean unknown,
not proof of no writes. Context cancellation reaches the native command and does
not imply that previously persisted effects were rolled back.

Only the exact registered POST procedure is mounted. It is a write operation;
Plan read services, packaging services and unknown methods cannot cancel a Plan.
This is a Conduit-native transport contract, not private protocol compatibility.

## Explicit CLI transport

`conduit plan cancel PLAN_ID --reason "reviewed" --write-transport connect`
sends one generated command and emits `conduit.plan-cancellation/v1`, kind
`PlanCancellationReceipt`. JSON and YAML contain the scoped Plan summary,
recorded cancellation and linked Change Request ID. CSV and table include the
receipt version/kind, Plan coordinates/status, cancellation subject/reason and
exact revision. This is not the full REST Plan or an Agent stop acknowledgement.

Omitting `--write-transport`, or selecting `rest`, retains the existing full Plan
output. The CLI retains its existing reason-only cancellation capability; it does
not add suppression flags. Existing console cancellation remains REST.

The Connect caller retains configured TLS, bearer authentication, timeout and
context cancellation. It refuses redirects and never retries, reads a substitute
Plan or replays over REST after failure. Successful receipts must match the
requested Plan, cancelled status and normalized reason, with valid timestamps
and native integer counts. Explicit Connect selection cannot use a REST-only
injected service.

Only one completed-outcome detail matching the requested Plan is accepted as
completion evidence. Missing, duplicated, malformed, false or unbound evidence
remains unknown. Error strings omit raw server and transport diagnostics. Inspect
native state before manual recovery; neither a failure nor context cancellation
establishes that no effect occurred.

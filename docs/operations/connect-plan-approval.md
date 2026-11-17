# Governed Plan approval over Connect

`conduit.plans.v1.PlanApprovalService/ApprovePlan` dual-serves the existing
`POST /v1/plans/{planID}/approvals` command. This is a Conduit-native governed
write, not a read encoded as POST. Existing REST callers remain supported. It adds no
automatic retry, fallback, publication or deployment authority.

## One native command

Both adapters call `ApprovePlanAsOperator`. It retains the original resolved
credential carrier and the entire existing sequence:

1. Read the named Plan.
2. For a linked Change Request, apply the existing reviewer requirements,
   independent-review rules, duplicate handling and audit through Governance.
   A policy-eligible reviewer does not also need Environment Operate.
3. For a Plan without a Change Request, require Environment Operate through
   the existing resource decision and audit, then call Lifecycle approval.
   Authentication alone is insufficient. Bootstrap authority is evaluated on
   the original carrier, not the represented person.
4. Call the configured `AfterPlanApproved` sequencer only once the native result
   is approved. A partial approval does not start execution.

Production authentication precedes decoding and store access. The existing
explicit insecure development mode retains its native local-developer fallback;
it is not production authentication. Only the registered POST method is served.
The per-message request bound is 1 MiB, corresponding to the native JSON body
bound; encoding and envelope sizes differ between transports.

The request carries only Plan ID and rationale. Identity and approval authority
come from native authentication and governance, never from message fields.
Rationale trimming, state transitions, approval subjects, revisions and audit
decisions retain native meaning. ProtoJSON follows generated-schema handling;
it is not a substitute for the native decision. REST stays mounted.

## Result and failure semantics

Success returns the existing Plan summary plus recorded approvals and the
linked Change Request ID. Revisions remain uint64; counts remain int64;
timestamps retain nanoseconds. No lease token, full desired-state payload or
execution credential can be represented. The result is the approval snapshot,
not a promise that an asynchronous sequencer has not since advanced the Plan.

Native 401/403/404 map to `unauthenticated`/`permission_denied`/`not_found`.
Duplicate 409 maps to `already_exists`; invalid state or missing rationale 409
maps to `failed_precondition`; typed validation/digest failures map to
`invalid_argument`. Wire HTTP codes differ for some Connect categories.
Oversized messages fail `resource_exhausted` before the command runs. Unexpected
backend diagnostics are sanitized.

Approval may already be stored when sequencing fails. REST preserves its
existing 502 result with Plan ID; Connect returns `unavailable` with a typed
`PlanApprovalOutcome` detail naming the Plan, `approval_recorded: true` and
`handoff_failed: true`. A result that cannot be represented after a successful
command returns `internal` with recorded outcome and `handoff_failed: false`.
Missing outcome details mean an unknown outcome, not proof that no write
occurred. Inspect stored status and repair/reconcile sequencing; do not replay
approval through REST or retry it automatically. Duplicate approval remains a
native refusal, not a recovery mechanism.

## Operator console

The active delivery panel and approvals board use the generated approval client,
as does the legacy console helper. The operator still reviews the exact Plan ID
and rationale, then confirms one command. The confirmation shows the Connect
procedure and message actually sent. Reject and cancel retain their REST routes.

Successful approval displays the returned summary status. Its versioned
`conduit.plan-approval/v1` result retains the generated receipt; it does not
invent a full desired-state document. An approval response failure preserves
the client's recorded/unknown and handoff-failed outcome information. The panel
does not label a recorded approval refused, and blocks another approval until
the operator explicitly reloads current Plan state. Reloading performs reads;
it never repeats the approval. Navigation or a background repaint does not
clear the blocked state. There is no automatic REST fallback or command retry.

## Evidence and cutover

Generated Go clients exercise the real HTTP endpoint. Hub tests compare native
and Connect authorization, stored Plan/Change Request state, audit decisions,
partial/duplicate/state failures, delegated carriers and independent review.
They verify sequencer calls, recorded-outcome details on handoff failure and no
REST replay. These are local fixture checks, not production approval evidence.

Together with existing index/detail read parity, this supplies the representative
governed POST required by #394. Other route families and caller migrations remain
under #391–#394; the CLI rollout items in #394 remain open. The [cutover plan](connect-cutover.md) retains REST and its
deprecation/removal criteria; general live updates remain SSE.

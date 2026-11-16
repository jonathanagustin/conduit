# CLI Plan approval over Connect

`conduit plan approve PLAN_ID --rationale 'reviewed target and digest'` keeps its
existing REST default and full Plan document. Explicit `--write-transport rest`
selects that same rollback path. Other Plan commands are unchanged.

`--write-transport connect` selects the generated `PlanApprovalService/ApprovePlan`
command and a different, explicitly versioned output contract:

Illustrative partial-approval receipt:

```json
{
  "apiVersion": "conduit.plan-approval/v1",
  "kind": "PlanApprovalReceipt",
  "plan": {
    "id": "plan-id", "environment": "dev", "status": "AWAITING_APPROVAL",
    "stepCount": 1, "approvalCount": 1, "requiredApprovals": 2,
    "createdAt": "2026-11-16T10:00:00Z",
    "updatedAt": "2026-11-16T10:01:00Z", "revision": 42
  },
  "approvals": [{
    "subject": "reviewer", "rationale": "reviewed target and digest",
    "createdAt": "2026-11-16T10:01:00Z"
  }],
  "changeRequestId": "linked-request"
}
```

An absent linked request is omitted. This is a receipt projection, not a full Plan or an execution
credential. It may record a partial approval and leave the Plan awaiting review;
it is not evidence that execution ran. JSON/YAML include the summary and reviews;
CSV/table provide a versioned one-row summary. Revisions remain exact in every
format, including above 2^53. Unknown transport/output selections and missing
rationale are refused before sending a command.

The HTTP adapter uses the existing configured identity, trust roots and timeout.
No insecure identity mode is added. The native command still enforces reviewer
policy, author exclusion, Environment Operate and durable audit rules. Bootstrap
administrator status does not bypass an independent reviewer-team requirement.

The Connect command does not follow HTTP redirects, retry an ambiguous write, fetch a
replacement Plan document or replay over REST. A failed command must be inspected
against native Plan state before an explicit human retry. In particular:

- A matching, unique native error detail can establish that approval was recorded,
  including when sequencing handoff subsequently failed. That error is preserved
  rather than reported as a successful receipt.
- Missing, conflicting, malformed or wrong-Plan details leave the outcome unknown.
  They are never proof that no approval was recorded.
- A lost reply or rejected/corrupt response leaves the outcome unknown. No partial
  receipt is written to stdout. Raw server/transport diagnostic messages are not
  included in the displayed command failure.

Existing in-process/REST-only ports cannot silently stand in for the generated
command. The optional operator receipt port reports unavailable instead.

Verification uses native in-memory Hub policy/state/audit fixtures, partial and
duplicate reviews, failed handoff, a reply lost after commit, TLS bearer identity,
timeout/cancellation, corrupt evidence, output formats and command redirects.
No production Plan is approved by the tests. REST stays enabled without a
deprecation date; remaining CLI migrations are tracked in #394.

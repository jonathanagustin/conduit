# Organization governance editors

The mounted console edits native approval policies and resource grants through
their existing Hub APIs. This is a Conduit extension, registered as
`console.GovernanceEditors`; it claims no private Apollo workflow.

Policy writes use `PUT /v1/approval-policies/{id}` and require the Hub's existing
administrator predicate. Grant writes use `POST /v1/resource-grants` and require
`permissions:manage` on the exact resource. Session capabilities are presentation
hints; every write is authorized again by the Hub. Selection of a Space conveys
no additional permission and does not narrow Hub-wide policy administration.

Editing preserves the returned revision and complete typed variable values.
Existing grant resource/team coordinates and policy IDs cannot be retargeted.
Roles and operations remain separate lists. There is no invented delete route,
automatic overwrite, or second-person approval of these administrative writes.

Review captures an immutable payload. Cancel sends nothing. A changed draft or
observed authenticated identity invalidates confirmation. Duplicate confirmation
does not send another request. An identity change also suppresses late responses
and discards the editor; background paints otherwise retain the draft.
While saving or reloading, all New/Edit controls are disabled and the controller
refuses replacement, including a different row or record type. The identity
check runs first, so a newly observed identity cannot inherit the old editor or
its pending response.

Only a successful matching Hub record at the expected next revision is reported
as saved. A conflict, unavailable response, or ambiguous transport failure keeps
the draft and reports uncertainty without retrying. Reload explicitly discards
the draft and obtains the current record before another review. A failed audit
can follow a persisted grant change, so a failed request never proves no change
occurred.

Unit tests constrain payloads and lifecycle transitions. The focused live browser
test uses a real Hub to edit both records, check persisted revisions and typed
values, and observe a restricted identity's server refusal. Synthetic fixtures
establish native behavior only, not production identity-provider qualification.

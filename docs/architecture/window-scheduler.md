# Reviewed Entity maintenance proposals

The existing Releases Windows tab hosts a draft editor beside selected-environment
evidence. Targets are Entity names from visible Plan steps in that environment.
The Hub rebinds each Entity to its current installation and Product; a stale
client index cannot create authority for an absent target.

A draft states an explicit UTC start, duration in whole minutes, optional downtime,
reason and ticket. Pointer movement shifts its start; the end handle changes its
duration. Arrow keys shift by 15 minutes and Shift + arrow resizes. Editing only
changes the draft. Recorded calendar bars and existing requests are not edited.
No weekly-rule evaluation, blackout span or dependency timing is inferred, and
there is no claim that the draft is a valid complete rollout appointment.

Preview uses the existing `POST /v1/temporary-maintenance-overrides` with
`dryRun:true`. Its `CONDUIT_NATIVE` receipt must match the exact Entity,
environment, type and interval. Any edit invalidates preview and confirmation.
Creating the request requires explicit confirmation, sends the same fields with
`dryRun:false` and the expected preview request ID, and calls no approval, lease,
start or publication endpoint. Current Hub policy can require separate approval
or apply its existing automatic policy. A preview itself never grants approval.

The optional `startsAt` and `expectedChangeRequestId` are native extensions.
Submission checks an explicit start against server time and bounds the full end
to 3600 hours from submission. Preview binding requires a single Entity and an
explicit start. Within the existing repository boundary, the server reconstructs
the request identity from current scope and refuses a stale identity before
creating a request. Omitting the fields preserves existing CLI behavior.
Approved future windows still fail admission before their start and at their
exclusive end. Immutable Plans and ordinary lease semantics are unchanged.

The UI validates the receipt after submission too, prevents a second write
attempt for the same draft within the mounted form, and never automatically
retries an uncertain response. An uncertain result directs the user to inspect
change requests. Navigating away does not mean an in-flight submission failed.
The backend's existing stable native request identity remains the durable
idempotency mechanism. All displayed fields are escaped.

This is part of #553. Resolved rollout appointments, automatic snapping from
complete policy, blackout interval projection and dependency-time arrows still
need implementation; this form does not claim those capabilities.

# Console ontology

Typed definitions live in `web/src/console/ontology.ts`. The diagram in
`conduit-ontology-erd.png` shows the same objects and the link cardinalities.
Identifiers drawn on the diagram are example data.

## Objects

| Type | Status values |
| --- | --- |
| Work profile | ACTIVE, UPGRADE_AVAILABLE, ARCHIVED, DELETING |
| Workspace | ACTIVE, BUILDING, DRIFTED, GATED, IDLE, ERROR |
| Runtime | RUNNING, STOPPED, STARTING, FAILED |
| Story | SCOPING, READY, IN_PROGRESS, PAUSED, BLOCKED, DONE |
| Change request | DRAFT, IN_REVIEW, CHANGES_REQUESTED, APPROVED, INCLUDED_IN_PLAN, CLOSED |
| Packaging run | QUEUED, RUNNING, STAGED, SUCCEEDED, FAILED, CANCELED |
| Step | PENDING, RUNNING, SUCCEEDED, FAILED, SKIPPED. Stages: fetch, scan, sign, package, publish, propose_plan |
| Artifact | UNSIGNED, SIGNED, PUBLISHED, REVOKED |
| Plan | PROPOSED, AWAITING_APPROVAL, APPROVED, ROLLING_OUT, SUCCEEDED, REJECTED |
| Requirement | PASS, BLOCKED, PENDING |
| Approval | PENDING, APPROVED, REJECTED, EXPIRED |
| Environment | ONLINE, DRIFT_DETECTED, DEGRADED, AWAITING_BUNDLE, DISCONNECTED |
| Spoke | CONNECTED, STORE_AND_FORWARD, DIODE_ONE_WAY, COURIER_OFFLINE, DEGRADED |
| Installation | SYNCED, DRIFTED, OUTDATED, INSTALLING, FAILED |
| Reported state | CURRENT, STALE, UNVERIFIED |
| Audit event | RECORDED, VERIFIED |
| Saved view | PERSONAL, SHARED, SYSTEM_LOCKED |

A gate verdict is GO, CAUTION, or NO_GO.

## Links

| From | To | Cardinality | Meaning |
| --- | --- | --- | --- |
| Work profile | Story | 1 to many | tracks |
| Work profile | Workspace | 1 to 1 | owns |
| Work profile | Saved view | 0 to many | filters |
| Workspace | Change request | 1 to many | authors |
| Story | Packaging run | 1 to many | triggers |
| Packaging run | Step | 1 to many | steps |
| Packaging run | Artifact | 1 to 1 | produces |
| Packaging run | Plan | 1 to 0 or 1 | proposes |
| Plan | Requirement | 1 to many | requires |
| Requirement | Approval | 1 to many | signed |
| Change request | Plan | 0 or 1 | included |
| Environment | Spoke | 1 to 1 | routes |
| Environment | Installation | 1 to many | installs |
| Installation | Reported state | 1 to 1 | current state |
| Work profile | Environment | 0 to many | targets |
| Any object | Audit event | 1 to 0 or many | recorded |

## Actions

Roles, blocking rules, and audit events are the `CONDUIT_ACTIONS` rows.

| Action | Object | Roles | Blocked when | Audit event |
| --- | --- | --- | --- | --- |
| fork | Work profile | Developer, Operator, Profile Owner | status is DELETING | work_profile.forked |
| upgrade | Work profile | Operator, Profile Owner | status is not UPGRADE_AVAILABLE, or the target artifact is not SIGNED | work_profile.upgrade_staged |
| download_bundle | Work profile | Operator, Profile Owner | the latest packaging run has no SIGNED artifact | work_profile.bundle_downloaded |
| archive | Work profile | Profile Owner | a linked plan is ROLLING_OUT | work_profile.archived |
| delete | Work profile | Profile Owner | a plan is ROLLING_OUT, or the confirmation string is not the profile id | work_profile.deleted |
| approve_plan | Plan | Release Approver | a requirement is BLOCKED or PENDING, or the caller already signed | plan.approved |
| reject_plan | Plan | Release Approver, Security Admin | the plan is SUCCEEDED or REJECTED | plan.rejected |
| rerun_from_step | Packaging run | Operator, Developer | the run is RUNNING, or the linked plan is ROLLING_OUT | packaging_run.rerun_from_step |
| cancel_run | Packaging run | Operator, Developer | the run is not QUEUED, RUNNING, or STAGED | packaging_run.canceled |

The packaging page calls the Hub cancel route when the read model carries a
source digest and a target plan reference. Rerun stays disabled with that
reason until the read model also carries a template version and tool versions,
which the current status payload does not.

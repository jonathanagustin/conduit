# Console ontology

Typed definitions live in `web/src/console/ontology.ts`. The diagram in
`conduit-ontology-erd.png` shows the earlier core objects. Identifiers drawn
on the diagram are example data. Release-management types below are additions
on that core.

## Objects

| Type | Status values |
| --- | --- |
| Work profile | ACTIVE, UPGRADE_AVAILABLE, DEPRECATED, ARCHIVED, DELETING |
| Workspace | ACTIVE, BUILDING, DRIFTED, GATED, IDLE, ERROR |
| Runtime | RUNNING, STOPPED, STARTING, FAILED |
| Story | SCOPING, READY, IN_PROGRESS, PAUSED, BLOCKED, DONE |
| Change request | DRAFT, IN_REVIEW, CHANGES_REQUESTED, APPROVED, INCLUDED_IN_PLAN, CLOSED |
| Packaging run | QUEUED, RUNNING, STAGED, SUCCEEDED, FAILED, CANCELED |
| Step | PENDING, RUNNING, SUCCEEDED, FAILED, SKIPPED. Stages: fetch, scan, sign, package, publish, propose_plan |
| Artifact | UNSIGNED, SIGNED, PUBLISHED, REVOKED |
| Plan | PROPOSED, AWAITING_APPROVAL, APPROVED, ROLLING_OUT, SUCCEEDED, FAILED, REJECTED |
| Requirement | PASS, BLOCKED, PENDING |
| Approval | PENDING, APPROVED, REJECTED, EXPIRED |
| Environment | ONLINE, DRIFT_DETECTED, DEGRADED, AWAITING_BUNDLE, DISCONNECTED |
| Spoke | CONNECTED, STORE_AND_FORWARD, DIODE_ONE_WAY, COURIER_OFFLINE, DEGRADED |
| Installation | SYNCED, DRIFTED, OUTDATED, INSTALLING, FAILED |
| Reported state | CURRENT, STALE, UNVERIFIED |
| Audit event | RECORDED, VERIFIED |
| Saved view | PERSONAL, SHARED, SYSTEM_LOCKED |
| Release channel | ACTIVE, PAUSED, FROZEN, DEPRECATED |
| Command | ISSUED, RECEIVED, RUNNING, SUCCEEDED, FAILED, EXPIRED |
| Override | ACTIVE, SCHEDULED, EXPIRED, REVOKED |

A gate verdict is GO, CAUTION, or NO_GO. Connection modes are ONLINE_MTLS,
STORE_AND_FORWARD, OPTICAL_DIODE, and COURIER_BUNDLE. Plan kinds are UPGRADE,
INSTALL, CONFIG_CHANGE, ROLLBACK, and UNINSTALL.

Release channel groups signed versions and the rules for promoting between
channels. Command is an imperative instruction, separate from a Plan. Override
is a time-bounded exception to a maintenance window, an upgrade suppression,
or a channel pin. Environment carries connection mode, channel, labels,
owners, maintenance windows, and health counts. Plan carries ordered tasks and
a dispatch timeline. Artifact carries an SBOM summary and bundle entries.
Change request carries the base and proposed configuration text.

## Links

| From | To | Cardinality | Meaning |
| --- | --- | --- | --- |
| Work profile | Packaging run | 1 to many | triggers |
| Work profile | Release channel | many to many | publishes versions to |
| Release channel | Release channel | 0 or 1 to many | promotes from |
| Release channel | Environment | 1 to many | governs upgrades for |
| Packaging run | Step | 1 to many | executes |
| Packaging run | Artifact | 1 to many | produces |
| Packaging run | Plan | 1 to 0 or 1 | proposes |
| Change request | Plan | many to many | included in |
| Plan | Requirement | 1 to many | gated by |
| Plan | Approval | 1 to many | collects |
| Plan | Environment | many to many | targets |
| Spoke | Environment | 1 to many | relays to |
| Environment | Installation | 1 to many | hosts |
| Environment | Reported state | 1 to 1 | reports current |
| Environment | Command | 1 to many | executes |
| Environment | Override | 1 to many | governed by |

## Actions

Roles, blocking rules, and audit events are the `CONDUIT_ACTIONS` rows.
`execute_plan`, `promote_release_manual`, and `include_change_in_plan` also
carry constraint rows for the maintenance window, approval quorum, unsigned
artifact, channel requirements, and a plan already rolling out.

| Action | Object | Roles | Blocked when | Audit event |
| --- | --- | --- | --- | --- |
| execute_plan | Plan | Operator, Release Approver | window closed without an override, quorum missing, artifact unsigned, or another plan is rolling out | plan.execution_dispatched |
| promote_release_manual | Release channel | Release Approver, Profile Owner | artifact unsigned, channel labels unmet, quorum missing, or a plan is rolling out | release_channel.manual_promoted |
| include_change_in_plan | Change request | Operator, Developer | the change is not approved, or the target plan is already rolling out | change_request.included_in_plan |
| download_bundle | Artifact | Operator, Profile Owner | the artifact is unsigned or the SBOM has an unresolved critical finding | artifact.bundle_downloaded |
| rerun_packaging_run | Packaging run | Operator, Developer | the run is running, or the linked plan is rolling out | packaging_run.rerun_requested |
| cancel_packaging_run | Packaging run | Operator, Developer | the run is not queued, running, or staged | packaging_run.canceled |
| fork | Work profile | Developer, Operator, Profile Owner | status is DELETING | work_profile.forked |
| upgrade | Work profile | Operator, Profile Owner | status is not UPGRADE_AVAILABLE, or the target artifact is not SIGNED | work_profile.upgrade_staged |
| archive | Work profile | Profile Owner | a linked plan is ROLLING_OUT | work_profile.archived |
| delete | Work profile | Profile Owner | a plan is ROLLING_OUT, or the confirmation string is not the profile id | work_profile.deleted |
| approve_plan | Plan | Release Approver | a requirement is BLOCKED or PENDING, or the caller already signed | plan.approved |
| reject_plan | Plan | Release Approver, Security Admin | the plan is SUCCEEDED or REJECTED | plan.rejected |
| rerun_from_step | Packaging run | Operator, Developer | the run is RUNNING, or the linked plan is ROLLING_OUT | packaging_run.rerun_from_step |
| cancel_run | Packaging run | Operator, Developer | the run is not QUEUED, RUNNING, or STAGED | packaging_run.canceled |

The packaging read model can cancel a run when it carries a source digest and
a target plan reference. Rerun from the status payload stays disabled until
that payload also carries a template version and tool versions.

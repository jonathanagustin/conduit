/**
 * Conduit Governed Software Delivery Ontology (v1.0)
 * 15 Core Object Types + 1 Justified Screen Support Type (SavedView)
 */

export type WorkProfileStatus = 'ACTIVE' | 'UPGRADE_AVAILABLE' | 'ARCHIVED' | 'DELETING';
export type WorkspaceStatus = 'ACTIVE' | 'BUILDING' | 'DRIFTED' | 'GATED' | 'IDLE' | 'ERROR';
export type RuntimeStatus = 'RUNNING' | 'STOPPED' | 'STARTING' | 'FAILED';
export type StoryStatus = 'SCOPING' | 'READY' | 'IN_PROGRESS' | 'PAUSED' | 'BLOCKED' | 'DONE';
export type ChangeRequestStatus = 'DRAFT' | 'IN_REVIEW' | 'CHANGES_REQUESTED' | 'APPROVED' | 'INCLUDED_IN_PLAN' | 'CLOSED';
export type PackagingRunStatus = 'QUEUED' | 'RUNNING' | 'STAGED' | 'SUCCEEDED' | 'FAILED' | 'CANCELED';
export type StepStageKind = 'fetch' | 'scan' | 'sign' | 'package' | 'publish' | 'propose_plan';
export type StepStatus = 'PENDING' | 'RUNNING' | 'SUCCEEDED' | 'FAILED' | 'SKIPPED';
export type ArtifactStatus = 'UNSIGNED' | 'SIGNED' | 'PUBLISHED' | 'REVOKED';
export type PlanStatus = 'PROPOSED' | 'AWAITING_APPROVAL' | 'APPROVED' | 'ROLLING_OUT' | 'SUCCEEDED' | 'REJECTED';
export type GateVerdict = 'GO' | 'CAUTION' | 'NO_GO';
export type RequirementStatus = 'PASS' | 'BLOCKED' | 'PENDING';
export type ApprovalStatus = 'PENDING' | 'APPROVED' | 'REJECTED' | 'EXPIRED';
export type EnvironmentStatus = 'ONLINE' | 'DRIFT_DETECTED' | 'DEGRADED' | 'AWAITING_BUNDLE' | 'DISCONNECTED';
export type SpokeStatus = 'CONNECTED' | 'STORE_AND_FORWARD' | 'DIODE_ONE_WAY' | 'COURIER_OFFLINE' | 'DEGRADED';
export type InstallationStatus = 'SYNCED' | 'DRIFTED' | 'OUTDATED' | 'INSTALLING' | 'FAILED';
export type ReportedStateStatus = 'CURRENT' | 'STALE' | 'UNVERIFIED';
export type AuditEventStatus = 'RECORDED' | 'VERIFIED';
export type SavedViewStatus = 'PERSONAL' | 'SHARED' | 'SYSTEM_LOCKED';

export interface OntologyActionSpec {
  actionId: string;
  objectType: string;
  label: string;
  allowedRoles: string[];
  blockingRule: string;
  emitsAuditEvent: string;
}

export const CONDUIT_ACTIONS: OntologyActionSpec[] = [
  { actionId: 'fork', objectType: 'Work profile', label: 'Fork Work Profile', allowedRoles: ['Developer', 'Operator', 'Profile Owner'], blockingRule: 'Blocked if Work profile status is DELETING', emitsAuditEvent: 'work_profile.forked' },
  { actionId: 'upgrade', objectType: 'Work profile', label: 'Upgrade Work Profile', allowedRoles: ['Operator', 'Profile Owner'], blockingRule: 'Blocked unless status is UPGRADE_AVAILABLE and target Artifact is SIGNED', emitsAuditEvent: 'work_profile.upgrade_staged' },
  { actionId: 'download_bundle', objectType: 'Work profile', label: 'Download Bundle', allowedRoles: ['Operator', 'Profile Owner'], blockingRule: 'Blocked if latest Packaging run has no SIGNED Artifact', emitsAuditEvent: 'work_profile.bundle_downloaded' },
  { actionId: 'archive', objectType: 'Work profile', label: 'Archive Work Profile', allowedRoles: ['Profile Owner'], blockingRule: 'Blocked while any linked Plan is ROLLING_OUT', emitsAuditEvent: 'work_profile.archived' },
  { actionId: 'delete', objectType: 'Work profile', label: 'Delete Work Profile', allowedRoles: ['Profile Owner'], blockingRule: 'Blocked while any Plan is ROLLING_OUT or confirmation string != profileId', emitsAuditEvent: 'work_profile.deleted' },
  { actionId: 'approve_plan', objectType: 'Plan', label: 'Approve Plan', allowedRoles: ['Release Approver (@fleet-approvers)'], blockingRule: 'Blocked while any Requirement is BLOCKED/PENDING or caller already signed', emitsAuditEvent: 'plan.approved' },
  { actionId: 'reject_plan', objectType: 'Plan', label: 'Reject Plan', allowedRoles: ['Release Approver (@fleet-approvers)', 'Security Admin'], blockingRule: 'Blocked if Plan is SUCCEEDED or REJECTED', emitsAuditEvent: 'plan.rejected' },
  { actionId: 'rerun_from_step', objectType: 'Packaging run', label: 'Rerun From Step', allowedRoles: ['Operator', 'Developer'], blockingRule: 'Blocked while Packaging run is RUNNING or linked Plan is ROLLING_OUT', emitsAuditEvent: 'packaging_run.rerun_from_step' },
  { actionId: 'cancel_run', objectType: 'Packaging run', label: 'Cancel Packaging Run', allowedRoles: ['Operator', 'Developer'], blockingRule: 'Blocked unless Packaging run is QUEUED, RUNNING, or STAGED', emitsAuditEvent: 'packaging_run.canceled' },
];

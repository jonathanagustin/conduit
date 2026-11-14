/**
 * Conduit Governed Software Delivery & Release-Management Ontology (v2.0)
 *
 * Existing Core Types (14 prompt-grounded + 2 workspace support types):
 *   Work profile, Packaging run, Step, Artifact, Plan, Requirement, Approval,
 *   Environment, Spoke, Installation, Reported state, Change request,
 *   Audit event, Saved view (+ Workspace, Story).
 *
 * Justified Release-Management Extensions (v2.0):
 * 1. New Type `Release channel` (`ReleaseChannel`): Groups signed Work profile versions by stability tier and governs channel-to-channel promotion criteria and Environment subscriptions.
 * 2. New Type `Command` (`OperationalCommand`): Models imperative hub-to-agent diagnostic and operational commands separately from declarative convergence Plans in Environment Activity.
 * 3. New Type `Override` (`MaintenanceOverride`): Models time-bounded maintenance window exceptions, upgrade suppressions, and channel pins surfaced in Activity and Constraint remediation.
 * 4. Extended Fields on `Environment`: Added `connectionMode`, `releaseChannelId`, `labels`, `owners`, `maintenanceWindows`, and `healthCounts` for the Environment header, proportional health bar, and property cards.
 * 5. Extended Fields on `Plan`: Added `planType`, `orderedTasks`, and `dispatchTimeline` (`issuedAt`, `receivedAt`, `failedAt`, `rawOutput`) for Activity filtering and failed-plan diagnostics.
 * 6. Extended Fields on `Artifact` & `Change request`: Added `sbomSummary`, `bundleEntries`, and `compressedDeltaBytes` on Artifact for disconnected Bundle preview, and `baseConfigYaml` / `proposedConfigYaml` on Change request for side-by-side diffs.
 * 7. Structured `ConstraintRuleSpec` on `OntologyActionSpec`: Parses `blockingRule` into machine-evaluable constraint rows (maintenance window, missing approval, unsigned artifact, Plan rolling out) with status, reason, and remediation links.
 */

export type WorkProfileStatus = 'ACTIVE' | 'UPGRADE_AVAILABLE' | 'DEPRECATED' | 'ARCHIVED' | 'DELETING';
export type RuntimeStatus = 'RUNNING' | 'STOPPED' | 'STARTING' | 'FAILED';
export type WorkspaceStatus = 'ACTIVE' | 'BUILDING' | 'DRIFTED' | 'GATED' | 'IDLE' | 'ERROR';
export type StoryStatus = 'SCOPING' | 'READY' | 'IN_PROGRESS' | 'PAUSED' | 'BLOCKED' | 'DONE';
export type ChangeRequestStatus = 'DRAFT' | 'IN_REVIEW' | 'CHANGES_REQUESTED' | 'APPROVED' | 'INCLUDED_IN_PLAN' | 'CLOSED';
export type PackagingRunStatus = 'QUEUED' | 'RUNNING' | 'STAGED' | 'SUCCEEDED' | 'FAILED' | 'CANCELED';
export type StepStageKind = 'fetch' | 'scan' | 'sign' | 'package' | 'publish' | 'propose_plan';
export type StepStatus = 'PENDING' | 'RUNNING' | 'SUCCEEDED' | 'FAILED' | 'SKIPPED';
export type ArtifactStatus = 'UNSIGNED' | 'SIGNED' | 'PUBLISHED' | 'REVOKED';
export type PlanStatus = 'PROPOSED' | 'AWAITING_APPROVAL' | 'APPROVED' | 'ROLLING_OUT' | 'SUCCEEDED' | 'FAILED' | 'REJECTED';
export type PlanKind = 'UPGRADE' | 'INSTALL' | 'CONFIG_CHANGE' | 'ROLLBACK' | 'UNINSTALL';
export type RequirementStatus = 'PASS' | 'BLOCKED' | 'PENDING';
export type ApprovalStatus = 'PENDING' | 'APPROVED' | 'REJECTED' | 'EXPIRED';
export type EnvironmentStatus = 'ONLINE' | 'DRIFT_DETECTED' | 'DEGRADED' | 'AWAITING_BUNDLE' | 'DISCONNECTED';
export type ConnectionMode = 'ONLINE_MTLS' | 'STORE_AND_FORWARD' | 'OPTICAL_DIODE' | 'COURIER_BUNDLE';
export type SpokeStatus = 'CONNECTED' | 'STORE_AND_FORWARD' | 'DIODE_ONE_WAY' | 'COURIER_OFFLINE' | 'DEGRADED';
export type InstallationStatus = 'SYNCED' | 'DRIFTED' | 'OUTDATED' | 'INSTALLING' | 'FAILED';
export type ReportedStateStatus = 'CURRENT' | 'STALE' | 'UNVERIFIED';
export type AuditEventStatus = 'RECORDED' | 'VERIFIED';
export type SavedViewStatus = 'PERSONAL' | 'SHARED' | 'SYSTEM_LOCKED';

/** New v2.0 Status Sets */
export type ReleaseChannelStatus = 'ACTIVE' | 'PAUSED' | 'FROZEN' | 'DEPRECATED';
export type OperationalCommandStatus = 'ISSUED' | 'RECEIVED' | 'RUNNING' | 'SUCCEEDED' | 'FAILED' | 'EXPIRED';
export type MaintenanceOverrideStatus = 'ACTIVE' | 'SCHEDULED' | 'EXPIRED' | 'REVOKED';

export type BlockingRuleCategory =
  | 'MAINTENANCE_WINDOW'
  | 'MISSING_APPROVAL'
  | 'UNSIGNED_ARTIFACT'
  | 'PLAN_ROLLING_OUT'
  | 'CHANNEL_REQUIREMENT';

export interface ConstraintRuleSpec {
  ruleId: string;
  category: BlockingRuleCategory;
  label: string;
  ruleExpression: string;
  defaultRemediationLabel: string;
  defaultRemediationRoute: string;
}

export interface OntologyActionSpec {
  actionId: string;
  objectType: OntologyObjectTypeName;
  label: string;
  allowedRoles: string[];
  /** Human-readable canonical rule expression; drives ConstraintList alongside constraintRules */
  blockingRule: string;
  constraintRules: ConstraintRuleSpec[];
  emitsAuditEvent: string;
}

export type OntologyObjectTypeName =
  | 'Work profile'
  | 'Workspace'
  | 'Story'
  | 'Packaging run'
  | 'Step'
  | 'Artifact'
  | 'Plan'
  | 'Requirement'
  | 'Approval'
  | 'Environment'
  | 'Spoke'
  | 'Installation'
  | 'Reported state'
  | 'Change request'
  | 'Audit event'
  | 'Saved view'
  | 'Release channel'
  | 'Command'
  | 'Override';

export interface OntologyObjectDefinition {
  objectType: OntologyObjectTypeName;
  primaryKey: string;
  keyProperties: string[];
  statusSet: readonly string[];
  definition: string;
  addedOrExtendedJustification?: string;
}

export interface OntologyLinkDefinition {
  linkId: string;
  fromType: OntologyObjectTypeName;
  toType: OntologyObjectTypeName;
  verb: string;
  cardinality: '1:1' | '1:N' | '0..1:N' | '1:0..1' | 'M:N';
  addedJustification?: string;
}

export const CONDUIT_OBJECT_TYPES: OntologyObjectDefinition[] = [
  {
    objectType: 'Work profile',
    primaryKey: 'profileId',
    keyProperties: ['profileId', 'name', 'currentVersion', 'targetVersion', 'defaultChannelId', 'ownerGroup'],
    statusSet: ['ACTIVE', 'UPGRADE_AVAILABLE', 'DEPRECATED', 'DELETING'],
    definition: 'Governed software product definition binding charts, policy baselines, and target Environment cohorts.',
  },
  {
    objectType: 'Packaging run',
    primaryKey: 'runId',
    keyProperties: ['runId', 'profileId', 'version', 'triggerKind', 'durationSec', 'startedAt'],
    statusSet: ['QUEUED', 'RUNNING', 'STAGED', 'SUCCEEDED', 'FAILED', 'CANCELED'],
    definition: 'Deterministic six-stage build, vulnerability scan, signing, and bundle assembly pipeline execution.',
  },
  {
    objectType: 'Step',
    primaryKey: 'stepId',
    keyProperties: ['stepId', 'runId', 'stageKind', 'ordinal', 'durationSec', 'exitCode'],
    statusSet: ['PENDING', 'RUNNING', 'SUCCEEDED', 'FAILED', 'SKIPPED'],
    definition: 'Individual stage execution within a Packaging run emitting structured timestamped logs.',
  },
  {
    objectType: 'Artifact',
    primaryKey: 'artifactId',
    keyProperties: ['artifactId', 'runId', 'name', 'digestSha256', 'sizeBytes', 'compressedDeltaBytes', 'signerKeyId', 'sbomSummary', 'bundleEntries'],
    statusSet: ['UNSIGNED', 'SIGNED', 'PUBLISHED', 'REVOKED'],
    definition: 'Immutable release bundle, differential delta, SPDX SBOM, or provenance attestation produced by a Packaging run.',
    addedOrExtendedJustification: 'Added sbomSummary, bundleEntries, and compressedDeltaBytes to support pre-download Bundle preview for disconnected sites.',
  },
  {
    objectType: 'Plan',
    primaryKey: 'planId',
    keyProperties: ['planId', 'runId', 'planType', 'targetVersion', 'orderedTasks', 'dispatchTimeline', 'waveCount'],
    statusSet: ['PROPOSED', 'AWAITING_APPROVAL', 'APPROVED', 'ROLLING_OUT', 'SUCCEEDED', 'FAILED', 'REJECTED'],
    definition: 'Governed unit of convergence work delivered from Hub to Environment agents once all Requirements and Approvals pass.',
    addedOrExtendedJustification: 'Added planType, orderedTasks, FAILED status, and dispatchTimeline (issuedAt, receivedAt, failedAt, rawOutput) for Activity filtering and failed-plan diagnostics.',
  },
  {
    objectType: 'Requirement',
    primaryKey: 'requirementId',
    keyProperties: ['requirementId', 'planId', 'category', 'reason', 'remediationRoute'],
    statusSet: ['PASS', 'BLOCKED', 'PENDING'],
    definition: 'Machine-evaluated pre-condition gating Plan approval, promotion, or agent execution.',
  },
  {
    objectType: 'Approval',
    primaryKey: 'approvalId',
    keyProperties: ['approvalId', 'planId', 'reviewerHandle', 'roleGroup', 'signatureDigest', 'decidedAt'],
    statusSet: ['PENDING', 'APPROVED', 'REJECTED', 'EXPIRED'],
    definition: 'Cryptographically attributed human sign-off or rejection recorded against a Plan or manual promotion.',
  },
  {
    objectType: 'Environment',
    primaryKey: 'environmentId',
    keyProperties: ['environmentId', 'spokeId', 'connectionMode', 'releaseChannelId', 'k8sVersion', 'labels', 'owners', 'maintenanceWindows', 'healthCounts'],
    statusSet: ['ONLINE', 'DRIFT_DETECTED', 'DEGRADED', 'AWAITING_BUNDLE', 'DISCONNECTED'],
    definition: 'Managed remote or disconnected Kubernetes cluster reporting state and executing Plans via a Spoke.',
    addedOrExtendedJustification: 'Added connectionMode, releaseChannelId, labels, owners, maintenanceWindows, and healthCounts for Environment page tabs and proportional health bar.',
  },
  {
    objectType: 'Spoke',
    primaryKey: 'spokeId',
    keyProperties: ['spokeId', 'name', 'transportKind', 'spoolDepthMb', 'boundEnvironmentCount'],
    statusSet: ['CONNECTED', 'STORE_AND_FORWARD', 'DIODE_ONE_WAY', 'COURIER_OFFLINE', 'DEGRADED'],
    definition: 'Regional relay, one-way optical diode, or courier transfer boundary linking the Hub to Environments.',
  },
  {
    objectType: 'Installation',
    primaryKey: 'installationId',
    keyProperties: ['installationId', 'environmentId', 'entityName', 'desiredVersion', 'reportedVersion', 'channelId', 'driftCount'],
    statusSet: ['SYNCED', 'DRIFTED', 'OUTDATED', 'INSTALLING', 'FAILED'],
    definition: 'Managed workload chart or platform component installed on a specific Environment.',
  },
  {
    objectType: 'Reported state',
    primaryKey: 'reportedStateId',
    keyProperties: ['reportedStateId', 'environmentId', 'snapshotDigest', 'driftItems', 'observedAt'],
    statusSet: ['CURRENT', 'STALE', 'UNVERIFIED'],
    definition: 'Signed point-in-time inventory and resource diff reported back from an Environment agent.',
  },
  {
    objectType: 'Change request',
    primaryKey: 'changeRequestId',
    keyProperties: ['changeRequestId', 'profileId', 'targetEnvironmentId', 'author', 'baseConfigYaml', 'proposedConfigYaml', 'linkedPlanId'],
    statusSet: ['DRAFT', 'IN_REVIEW', 'CHANGES_REQUESTED', 'APPROVED', 'INCLUDED_IN_PLAN', 'CLOSED'],
    definition: 'Reviewed configuration override, channel subscription edit, or chart parameter diff.',
    addedOrExtendedJustification: 'Added targetEnvironmentId, baseConfigYaml, and proposedConfigYaml for side-by-side configuration diffing and Plan inclusion.',
  },
  {
    objectType: 'Audit event',
    primaryKey: 'eventId',
    keyProperties: ['eventId', 'actor', 'actionId', 'targetObjectType', 'targetObjectId', 'reason', 'occurredAt'],
    statusSet: ['RECORDED', 'VERIFIED'],
    definition: 'Immutable append-only governance ledger record emitted by every state-mutating action.',
  },
  {
    objectType: 'Saved view',
    primaryKey: 'savedViewId',
    keyProperties: ['savedViewId', 'name', 'targetObjectType', 'filterExpression', 'owner'],
    statusSet: ['PERSONAL', 'SHARED', 'SYSTEM_LOCKED'],
    definition: 'Persisted facet filter query pinned for rapid operator fleet triage.',
  },
  {
    objectType: 'Release channel',
    primaryKey: 'channelId',
    keyProperties: ['channelId', 'name', 'tierOrder', 'currentVersion', 'upstreamChannelId', 'requiredLabels', 'minSoakHours', 'soakHealthThreshold'],
    statusSet: ['ACTIVE', 'PAUSED', 'FROZEN', 'DEPRECATED'],
    definition: 'Stability ring grouping approved versions for subscribed Environments and governing stage-to-stage promotion.',
    addedOrExtendedJustification: 'New type required by Environment release-channel assignment (Pattern 1) and channel-to-channel Promotion pipeline DAG (Pattern 6).',
  },
  {
    objectType: 'Command',
    primaryKey: 'commandId',
    keyProperties: ['commandId', 'environmentId', 'targetEntity', 'commandKind', 'issuedBy', 'durationSec', 'startedAt'],
    statusSet: ['ISSUED', 'RECEIVED', 'RUNNING', 'SUCCEEDED', 'FAILED', 'EXPIRED'],
    definition: 'Imperative one-shot operational or diagnostic instruction dispatched to an Environment agent.',
    addedOrExtendedJustification: 'New type required by Environment Activity subnav (Pattern 2: Plans, Commands, Overrides).',
  },
  {
    objectType: 'Override',
    primaryKey: 'overrideId',
    keyProperties: ['overrideId', 'environmentId', 'overrideKind', 'scopeEntity', 'reason', 'createdBy', 'expiresAt'],
    statusSet: ['ACTIVE', 'SCHEDULED', 'EXPIRED', 'REVOKED'],
    definition: 'Time-bounded exception overriding maintenance windows, suppressing upgrades, or pinning an entity channel.',
    addedOrExtendedJustification: 'New type required by Environment Activity subnav (Pattern 2) and Constraint remediation links (Pattern 4).',
  },
  {
    objectType: 'Workspace',
    primaryKey: 'workspaceId',
    keyProperties: ['workspaceId', 'profileId', 'environmentId', 'branch', 'owner'],
    statusSet: ['ACTIVE', 'BUILDING', 'DRIFTED', 'GATED', 'IDLE', 'ERROR'],
    definition: 'Isolated operator authoring sandbox bound to a Work profile and target Environment.',
  },
  {
    objectType: 'Story',
    primaryKey: 'storyId',
    keyProperties: ['storyId', 'profileId', 'priority', 'linkedPlanId'],
    statusSet: ['SCOPING', 'READY', 'IN_PROGRESS', 'PAUSED', 'BLOCKED', 'DONE'],
    definition: 'Scoped delivery work item tracking tasks from change authoring through packaging.',
  },
];

export const CONDUIT_LINKS: OntologyLinkDefinition[] = [
  { linkId: 'profile_runs', fromType: 'Work profile', toType: 'Packaging run', verb: 'triggers', cardinality: '1:N' },
  { linkId: 'profile_channels', fromType: 'Work profile', toType: 'Release channel', verb: 'publishes versions to', cardinality: 'M:N', addedJustification: 'Links Work profile versions to stability Release channels.' },
  { linkId: 'channel_upstream', fromType: 'Release channel', toType: 'Release channel', verb: 'promotes from', cardinality: '0..1:N', addedJustification: 'Defines the channel-to-channel DAG edges in PromotionGraph.' },
  { linkId: 'channel_environments', fromType: 'Release channel', toType: 'Environment', verb: 'governs upgrades for', cardinality: '1:N', addedJustification: 'Subscribes Environments to a default Release channel.' },
  { linkId: 'run_steps', fromType: 'Packaging run', toType: 'Step', verb: 'executes', cardinality: '1:N' },
  { linkId: 'run_artifacts', fromType: 'Packaging run', toType: 'Artifact', verb: 'produces', cardinality: '1:N' },
  { linkId: 'run_plan', fromType: 'Packaging run', toType: 'Plan', verb: 'proposes', cardinality: '1:0..1' },
  { linkId: 'cr_plan', fromType: 'Change request', toType: 'Plan', verb: 'included in', cardinality: 'M:N' },
  { linkId: 'plan_requirements', fromType: 'Plan', toType: 'Requirement', verb: 'gated by', cardinality: '1:N' },
  { linkId: 'plan_approvals', fromType: 'Plan', toType: 'Approval', verb: 'collects', cardinality: '1:N' },
  { linkId: 'plan_environments', fromType: 'Plan', toType: 'Environment', verb: 'targets', cardinality: 'M:N' },
  { linkId: 'spoke_environments', fromType: 'Spoke', toType: 'Environment', verb: 'relays to', cardinality: '1:N' },
  { linkId: 'env_installations', fromType: 'Environment', toType: 'Installation', verb: 'hosts', cardinality: '1:N' },
  { linkId: 'env_reported_state', fromType: 'Environment', toType: 'Reported state', verb: 'reports current', cardinality: '1:1' },
  { linkId: 'env_commands', fromType: 'Environment', toType: 'Command', verb: 'executes', cardinality: '1:N', addedJustification: 'Associates imperative operator Commands with an Environment.' },
  { linkId: 'env_overrides', fromType: 'Environment', toType: 'Override', verb: 'governed by', cardinality: '1:N', addedJustification: 'Associates active maintenance and suppression Overrides with an Environment.' },
];

export const CONDUIT_ACTIONS: OntologyActionSpec[] = [
  {
    actionId: 'execute_plan',
    objectType: 'Plan',
    label: 'Execute Release Plan',
    allowedRoles: ['Operator', 'Release Approver'],
    blockingRule:
      'Blocked if maintenance window is closed without active Override; OR missing required Approval quorum; OR any linked Artifact is UNSIGNED; OR a conflicting Plan is currently ROLLING_OUT on target Environments',
    constraintRules: [
      {
        ruleId: 'rule-maint-window',
        category: 'MAINTENANCE_WINDOW',
        label: 'Maintenance window open',
        ruleExpression: 'environment.windowOpen == true || environment.activeOverride != null',
        defaultRemediationLabel: 'Create maintenance override',
        defaultRemediationRoute: '/environments/edge-west-02/activity?subview=overrides',
      },
      {
        ruleId: 'rule-missing-approval',
        category: 'MISSING_APPROVAL',
        label: 'Required approvals (2/2)',
        ruleExpression: 'plan.approvals.approvedCount >= plan.approvals.requiredCount',
        defaultRemediationLabel: 'Request second approver sign-off',
        defaultRemediationRoute: '/promotions?dialog=manual-promote',
      },
      {
        ruleId: 'rule-unsigned-artifact',
        category: 'UNSIGNED_ARTIFACT',
        label: 'All bundle artifacts signed',
        ruleExpression: 'plan.artifacts.every(a => a.status == SIGNED || a.status == PUBLISHED)',
        defaultRemediationLabel: 'Inspect bundle signatures & SBOM',
        defaultRemediationRoute: '/bundles/uds-core-0.31.2/preview',
      },
      {
        ruleId: 'rule-plan-rolling-out',
        category: 'PLAN_ROLLING_OUT',
        label: 'No conflicting Plan rolling out',
        ruleExpression: 'targetEnvironments.concurrentRollingOutPlans == 0',
        defaultRemediationLabel: 'Inspect active rollout PLN-2026-0412',
        defaultRemediationRoute: '/environments/edge-west-02/activity?subview=plans',
      },
    ],
    emitsAuditEvent: 'plan.execution_dispatched',
  },
  {
    actionId: 'promote_release_manual',
    objectType: 'Release channel',
    label: 'Promote Release to Channel',
    allowedRoles: ['Release Approver', 'Profile Owner'],
    blockingRule:
      'Blocked if target Artifact is UNSIGNED; OR channel label requirements are unmet; OR missing required Approval quorum; OR conflicting Plan is ROLLING_OUT',
    constraintRules: [
      {
        ruleId: 'promo-unsigned',
        category: 'UNSIGNED_ARTIFACT',
        label: 'Release bundle & SBOM signed',
        ruleExpression: 'release.signatureVerified && release.sbomCriticalCount == 0',
        defaultRemediationLabel: 'Verify bundle signatures',
        defaultRemediationRoute: '/bundles/uds-core-0.31.2/preview',
      },
      {
        ruleId: 'promo-approval',
        category: 'MISSING_APPROVAL',
        label: 'Channel promotion quorum (2/2)',
        ruleExpression: 'promotion.approvals >= 2',
        defaultRemediationLabel: 'Add channel approver signature',
        defaultRemediationRoute: '/promotions?dialog=manual-promote',
      },
      {
        ruleId: 'promo-window',
        category: 'MAINTENANCE_WINDOW',
        label: 'Upstream soak threshold met',
        ruleExpression: 'upstreamChannel.soakHours >= targetChannel.minSoakHours',
        defaultRemediationLabel: 'Review soak telemetry on staging',
        defaultRemediationRoute: '/releases/0.31.2',
      },
      {
        ruleId: 'promo-rolling',
        category: 'PLAN_ROLLING_OUT',
        label: 'No active recall or conflicting rollout',
        ruleExpression: 'channel.activeConflictingRollouts == 0',
        defaultRemediationLabel: 'Inspect active Plans',
        defaultRemediationRoute: '/environments/edge-west-02/activity?subview=plans',
      },
    ],
    emitsAuditEvent: 'release_channel.manual_promoted',
  },
  {
    actionId: 'include_change_in_plan',
    objectType: 'Change request',
    label: 'Include in Plan',
    allowedRoles: ['Developer', 'Operator', 'Profile Owner'],
    blockingRule:
      'Blocked unless Change request status is APPROVED and target Plan is not ROLLING_OUT',
    constraintRules: [
      {
        ruleId: 'cr-approved',
        category: 'MISSING_APPROVAL',
        label: 'Change request approved',
        ruleExpression: 'changeRequest.status == APPROVED',
        defaultRemediationLabel: 'Complete config diff review',
        defaultRemediationRoute: '/change-requests/CR-308',
      },
      {
        ruleId: 'cr-plan-mutable',
        category: 'PLAN_ROLLING_OUT',
        label: 'Target Plan is mutable (not rolling out)',
        ruleExpression: 'targetPlan.status != ROLLING_OUT',
        defaultRemediationLabel: 'Select proposed Plan PLN-2026-0421',
        defaultRemediationRoute: '/plans/PLN-2026-0421',
      },
    ],
    emitsAuditEvent: 'change_request.included_in_plan',
  },
  {
    actionId: 'download_bundle',
    objectType: 'Artifact',
    label: 'Download Bundle',
    allowedRoles: ['Operator', 'Profile Owner'],
    blockingRule:
      'Blocked if Artifact status is UNSIGNED or SBOM has unresolved critical vulnerabilities',
    constraintRules: [
      {
        ruleId: 'dl-signed',
        category: 'UNSIGNED_ARTIFACT',
        label: 'Hardware signature & SLSA provenance verified',
        ruleExpression: 'artifact.status == SIGNED || artifact.status == PUBLISHED',
        defaultRemediationLabel: 'Re-run signing step',
        defaultRemediationRoute: '/packaging-runs/RUN-8842',
      },
    ],
    emitsAuditEvent: 'artifact.bundle_downloaded',
  },
  {
    actionId: 'rerun_packaging_run',
    objectType: 'Packaging run',
    label: 'Rerun Packaging Run',
    allowedRoles: ['Operator', 'Developer'],
    blockingRule: 'Blocked while Packaging run is RUNNING or linked Plan is ROLLING_OUT',
    constraintRules: [
      {
        ruleId: 'rerun-not-rolling',
        category: 'PLAN_ROLLING_OUT',
        label: 'Linked Plan not currently rolling out',
        ruleExpression: 'linkedPlan.status != ROLLING_OUT',
        defaultRemediationLabel: 'View linked Plan status',
        defaultRemediationRoute: '/plans/PLN-2026-0416',
      },
    ],
    emitsAuditEvent: 'packaging_run.rerun_requested',
  },
  {
    actionId: 'cancel_packaging_run',
    objectType: 'Packaging run',
    label: 'Cancel Packaging Run',
    allowedRoles: ['Operator', 'Developer'],
    blockingRule: 'Blocked unless Packaging run is QUEUED, RUNNING, or STAGED',
    constraintRules: [],
    emitsAuditEvent: 'packaging_run.canceled',
  },
  { actionId: 'fork', objectType: 'Work profile', label: 'Fork Work Profile', allowedRoles: ['Developer', 'Operator', 'Profile Owner'], blockingRule: 'Blocked if Work profile status is DELETING', constraintRules: [], emitsAuditEvent: 'work_profile.forked' },
  { actionId: 'upgrade', objectType: 'Work profile', label: 'Upgrade Work Profile', allowedRoles: ['Operator', 'Profile Owner'], blockingRule: 'Blocked unless status is UPGRADE_AVAILABLE and target Artifact is SIGNED', constraintRules: [], emitsAuditEvent: 'work_profile.upgrade_staged' },
  { actionId: 'archive', objectType: 'Work profile', label: 'Archive Work Profile', allowedRoles: ['Profile Owner'], blockingRule: 'Blocked while any linked Plan is ROLLING_OUT', constraintRules: [], emitsAuditEvent: 'work_profile.archived' },
  { actionId: 'delete', objectType: 'Work profile', label: 'Delete Work Profile', allowedRoles: ['Profile Owner'], blockingRule: 'Blocked while any Plan is ROLLING_OUT or confirmation string != profileId', constraintRules: [], emitsAuditEvent: 'work_profile.deleted' },
  { actionId: 'approve_plan', objectType: 'Plan', label: 'Approve Plan', allowedRoles: ['Release Approver'], blockingRule: 'Blocked while any Requirement is BLOCKED or PENDING or the caller already signed', constraintRules: [], emitsAuditEvent: 'plan.approved' },
  { actionId: 'reject_plan', objectType: 'Plan', label: 'Reject Plan', allowedRoles: ['Release Approver', 'Security Admin'], blockingRule: 'Blocked if Plan is SUCCEEDED or REJECTED', constraintRules: [], emitsAuditEvent: 'plan.rejected' },
  { actionId: 'rerun_from_step', objectType: 'Packaging run', label: 'Rerun From Step', allowedRoles: ['Operator', 'Developer'], blockingRule: 'Blocked while Packaging run is RUNNING or linked Plan is ROLLING_OUT', constraintRules: [], emitsAuditEvent: 'packaging_run.rerun_from_step' },
  { actionId: 'cancel_run', objectType: 'Packaging run', label: 'Cancel Packaging Run', allowedRoles: ['Operator', 'Developer'], blockingRule: 'Blocked unless Packaging run is QUEUED, RUNNING, or STAGED', constraintRules: [], emitsAuditEvent: 'packaging_run.canceled' },
];

export interface ConstraintEvalInput {
  maintenanceWindowOpen: boolean;
  hasActiveWindowOverride: boolean;
  approvalsRecorded: number;
  approvalsRequired: number;
  allArtifactsSigned: boolean;
  unsignedArtifactName?: string;
  conflictingRollingOutPlanId?: string | null;
}

export interface EvaluatedConstraintRow {
  ruleId: string;
  category: BlockingRuleCategory;
  label: string;
  ruleClauseFromOntology: string;
  status: 'PASS' | 'BLOCKED';
  reason: string;
  fixLabel: string;
  fixRoute: string;
}

/**
 * Evaluates an OntologyActionSpec's `blockingRule` and `constraintRules` into
 * concrete UI rows for `ConstraintList`.
 */
export function evaluateActionConstraints(
  action: OntologyActionSpec,
  ctx: ConstraintEvalInput
): EvaluatedConstraintRow[] {
  const clauses = action.blockingRule.split(';').map((s) => s.trim());
  return action.constraintRules.map((rule, idx) => {
    const clause = clauses[idx] ?? action.blockingRule;
    switch (rule.category) {
      case 'MAINTENANCE_WINDOW': {
        const pass = ctx.maintenanceWindowOpen || ctx.hasActiveWindowOverride;
        return {
          ruleId: rule.ruleId,
          category: rule.category,
          label: rule.label,
          ruleClauseFromOntology: clause,
          status: pass ? 'PASS' : 'BLOCKED',
          reason: pass
            ? ctx.hasActiveWindowOverride
              ? 'Active Override OVR-119 permits execution outside recurring UTC window.'
              : 'Environment maintenance window (02:00-05:00 UTC) is open.'
            : 'Current UTC time (21:48Z) is outside scheduled window 02:00-05:00 UTC and no Override is active.',
          fixLabel: rule.defaultRemediationLabel,
          fixRoute: rule.defaultRemediationRoute,
        };
      }
      case 'MISSING_APPROVAL': {
        const pass = ctx.approvalsRecorded >= ctx.approvalsRequired;
        return {
          ruleId: rule.ruleId,
          category: rule.category,
          label: rule.label,
          ruleClauseFromOntology: clause,
          status: pass ? 'PASS' : 'BLOCKED',
          reason: pass
            ? `Quorum satisfied (${ctx.approvalsRecorded}/${ctx.approvalsRequired} @fleet-approvers signed).`
            : `${ctx.approvalsRecorded} of ${ctx.approvalsRequired} required @fleet-approvers approvals recorded; awaiting second reviewer.`,
          fixLabel: rule.defaultRemediationLabel,
          fixRoute: rule.defaultRemediationRoute,
        };
      }
      case 'UNSIGNED_ARTIFACT': {
        const pass = ctx.allArtifactsSigned;
        return {
          ruleId: rule.ruleId,
          category: rule.category,
          label: rule.label,
          ruleClauseFromOntology: clause,
          status: pass ? 'PASS' : 'BLOCKED',
          reason: pass
            ? 'All release tarballs, differential chunks, and SPDX SBOMs carry verified hardware signatures.'
            : `Artifact ${ctx.unsignedArtifactName ?? 'telemetry-collector-0.9.4.tgz'} is UNSIGNED (missing cosign digest attestation).`,
          fixLabel: rule.defaultRemediationLabel,
          fixRoute: rule.defaultRemediationRoute,
        };
      }
      case 'PLAN_ROLLING_OUT':
      default: {
        const pass = !ctx.conflictingRollingOutPlanId;
        return {
          ruleId: rule.ruleId,
          category: rule.category,
          label: rule.label,
          ruleClauseFromOntology: clause,
          status: pass ? 'PASS' : 'BLOCKED',
          reason: pass
            ? 'Zero conflicting Plans are currently ROLLING_OUT across target Environments.'
            : `Plan ${ctx.conflictingRollingOutPlanId} is currently ROLLING_OUT across shared target Environments (14/19 converged).`,
          fixLabel: rule.defaultRemediationLabel,
          fixRoute: rule.defaultRemediationRoute,
        };
      }
    }
  });
}

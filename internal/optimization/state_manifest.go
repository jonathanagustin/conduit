package optimization

import "sort"

// StateManifestStatus is deliberately separate from acquisition coverage. A
// field can be named in the system contract while its owning provider is not
// yet admitted to this Platform process.
type StateManifestStatus string

const (
	StateManifestAvailable StateManifestStatus = "AVAILABLE"
	StateManifestUnknown   StateManifestStatus = "UNKNOWN"
)

// StateManifestEntry is the field-level authority contract used to prevent a
// small executable registry from being mistaken for whole-system knowledge.
// Unknown entries are first-class: they may not be used as facts or silently
// replaced by estimates from another plane.
type StateManifestEntry struct {
	ID                   string              `json:"id"`
	Plane                StatePlane          `json:"plane"`
	Authority            string              `json:"authority"`
	RequirementID        string              `json:"requirementId"`
	CallID               string              `json:"callId,omitempty"`
	Extractor            string              `json:"extractor,omitempty"`
	MaximumStalenessSecs int                 `json:"maximumStalenessSeconds"`
	Status               StateManifestStatus `json:"status"`
	Reason               string              `json:"reason,omitempty"`
}

// PlatformStateRequirementManifest enumerates the minimum cross-plane fields
// needed by the optimizer. Only the four entries backed by the fixture-safe
// Platform Hub adapter are currently AVAILABLE; provider-owned and unmodeled
// fields remain explicit UNKNOWN until their read contract is admitted.
func PlatformStateRequirementManifest() []StateManifestEntry {
	entries := []StateManifestEntry{
		{ID: "desired.environment", Plane: StateDesired, Authority: "platform-hub", RequirementID: "platform.desired.environment", CallID: "hub.environment", Extractor: "PlatformHubFactExtractor", MaximumStalenessSecs: 30, Status: StateManifestAvailable},
		{ID: "desired.product-release-coordinates", Plane: StateDesired, Authority: "platform-hub", RequirementID: "platform.desired.product-release-coordinates", MaximumStalenessSecs: 300, Status: StateManifestUnknown, Reason: "product and release inventory is not yet bound to a fixture-safe extractor"},
		{ID: "desired.installations-and-policies", Plane: StateDesired, Authority: "platform-hub", RequirementID: "platform.desired.installations-and-policies", MaximumStalenessSecs: 300, Status: StateManifestUnknown, Reason: "installation, profile, update-policy, and maintenance projections are not yet one admitted read"},
		{ID: "admitted.plan", Plane: StateAdmitted, Authority: "platform-hub", RequirementID: "platform.admitted.plan", CallID: "hub.plan", Extractor: "PlatformHubFactExtractor", MaximumStalenessSecs: 5, Status: StateManifestAvailable},
		{ID: "admitted.policy-and-artifact-evidence", Plane: StateAdmitted, Authority: "platform-hub", RequirementID: "platform.admitted.policy-and-artifact-evidence", MaximumStalenessSecs: 30, Status: StateManifestUnknown, Reason: "approval-policy, grants, scanner, signature, and promotion evidence need a joined read contract"},
		{ID: "admitted.capacity-and-security-constraints", Plane: StateAdmitted, Authority: "environment-provider", RequirementID: "provider.admitted.capacity-and-security-constraints", MaximumStalenessSecs: 30, Status: StateManifestUnknown, Reason: "provider admission behavior is external and has no authorized capture"},
		{ID: "executing.plan", Plane: StateExecuting, Authority: "platform-hub", RequirementID: "platform.executing.plan", CallID: "hub.plan", Extractor: "PlatformHubFactExtractor", MaximumStalenessSecs: 5, Status: StateManifestAvailable},
		{ID: "executing.workload-and-data-transactions", Plane: StateExecuting, Authority: "environment-provider", RequirementID: "provider.executing.workload-and-data-transactions", MaximumStalenessSecs: 15, Status: StateManifestUnknown, Reason: "workload, queue, job, and data-transaction join keys are not yet acquired as one bounded read set"},
		{ID: "observed.reported-state", Plane: StateObserved, Authority: "platform-hub", RequirementID: "platform.observed.reported-state", CallID: "hub.reported-state", Extractor: "PlatformHubFactExtractor", MaximumStalenessSecs: 5, Status: StateManifestAvailable},
		{ID: "observed.fleet-health-and-drift", Plane: StateObserved, Authority: "platform-hub", RequirementID: "platform.observed.fleet-health-and-drift", MaximumStalenessSecs: 30, Status: StateManifestUnknown, Reason: "fleet, health-target, reconciliation-lag, and drift projections are not yet bound to the registry"},
		{ID: "observed.network-realization", Plane: StateObserved, Authority: "environment-provider", RequirementID: "network.flow.aggregate", CallID: "hubble.flows", Extractor: "HubbleFlowExtractor", MaximumStalenessSecs: 60, Status: StateManifestUnknown, Reason: "candidate provider call exists but no authorized live network capture has been admitted"},
		{ID: "observed.capacity-realization", Plane: StateObserved, Authority: "environment-provider", RequirementID: "capacity.nodes.aggregate", CallID: "kubernetes.nodes", Extractor: "KubernetesNodeExtractor", MaximumStalenessSecs: 30, Status: StateManifestUnknown, Reason: "candidate provider call exists but node authority is external to Platform"},
		{ID: "billed.invoice-and-adjustments", Plane: StateBilled, Authority: "provider-billing", RequirementID: "provider.billing.plan-cost", CallID: "provider-billing.invoice", Extractor: "ProviderInvoiceExtractor", MaximumStalenessSecs: 86400, Status: StateManifestUnknown, Reason: "invoice/export authorization and immutable revision are not present"},
		{ID: "billed.allocation-residual", Plane: StateBilled, Authority: "provider-billing", RequirementID: "provider.billing.allocation-residual", MaximumStalenessSecs: 86400, Status: StateManifestUnknown, Reason: "usage, OpenCost, invoice, credits, and shared allocation reconciliation is not yet bound"},
		{ID: "experienced.journey-outcomes", Plane: StateExperienced, Authority: "experience-telemetry", RequirementID: "experience.journey.aggregate", CallID: "experience.journeys", Extractor: "JourneyTelemetryExtractor", MaximumStalenessSecs: 300, Status: StateManifestUnknown, Reason: "privacy-safe journey evidence is a candidate read and has no authorized live capture"},
		{ID: "experienced.business-value", Plane: StateExperienced, Authority: "business-outcome-provider", RequirementID: "experience.business-value", MaximumStalenessSecs: 86400, Status: StateManifestUnknown, Reason: "revenue, support, retention, and causal outcome sources are not owned by Platform"},
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries
}

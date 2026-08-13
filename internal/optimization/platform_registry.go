package optimization

import (
	"errors"
	"strings"
)

// PlatformStateAcquisitionRegistry is the route-tested, read-only acquisition
// surface for state owned by this repository. It deliberately excludes billing,
// user experience, network fulfillment, and physical capacity, which remain
// provider-owned gaps below.
func PlatformStateAcquisitionRegistry() ([]StateRequirement, []AcquisitionCall) {
	requirements := []StateRequirement{
		{ID: "platform.desired.environment", Plane: StateDesired, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 30},
		{ID: "platform.admitted.plan", Plane: StateAdmitted, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 5},
		{ID: "platform.executing.plan", Plane: StateExecuting, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 5},
		{ID: "platform.observed.reported-state", Plane: StateObserved, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 5},
	}
	calls := []AcquisitionCall{
		{ID: "hub.environment", Source: "platform-hub", Method: "GET", Endpoint: "/v1/environments/{environmentID}", Consistency: "repository-read", CostUnits: 1, Observations: []FieldObservation{{Requirement: "platform.desired.environment", Plane: StateDesired, Fidelity: FidelityExact, MaximumStalenessSeconds: 0}}},
		{ID: "hub.plan", Source: "platform-hub", Method: "GET", Endpoint: "/v1/plans/{planID}", Consistency: "repository-read", CostUnits: 1, Observations: []FieldObservation{{Requirement: "platform.admitted.plan", Plane: StateAdmitted, Fidelity: FidelityExact, MaximumStalenessSeconds: 0}, {Requirement: "platform.executing.plan", Plane: StateExecuting, Fidelity: FidelityExact, MaximumStalenessSeconds: 0}}},
		{ID: "hub.reported-state", Source: "platform-hub", Method: "GET", Endpoint: "/v1/environments/{environmentID}/reported-state", Consistency: "exact-spoke-report", CostUnits: 1, Observations: []FieldObservation{{Requirement: "platform.observed.reported-state", Plane: StateObserved, Fidelity: FidelityExact, MaximumStalenessSeconds: 0}}},
	}
	return requirements, calls
}

// ResolvePlatformStateAcquisition binds the route-only registry to one exact
// Environment and Plan. Keeping discovery outside this function makes the
// dependency explicit: a caller must already hold authorized, immutable
// coordinates before any acquisition adapter can be invoked.
func ResolvePlatformStateAcquisition(environmentID, planID string) ([]StateRequirement, []AcquisitionCall, error) {
	if !validPlatformHubPathID(environmentID) || !validPlatformHubPathID(planID) {
		return nil, nil, errors.New("platform state acquisition requires bounded environment and plan path identifiers")
	}
	requirements, calls := PlatformStateAcquisitionRegistry()
	for index := range calls {
		calls[index].Endpoint = strings.ReplaceAll(calls[index].Endpoint, "{environmentID}", environmentID)
		calls[index].Endpoint = strings.ReplaceAll(calls[index].Endpoint, "{planID}", planID)
	}
	return requirements, calls, nil
}

// ExternalStateAcquisitionRegistry is a candidate-read manifest for provider-
// owned planes. It makes the missing domains mechanically selectable while
// keeping their behavior and authorization outside this repository's
// Platform-owned registry. A call is only a read intent; it is not evidence
// that the provider route exists or that a tenant has authorized it.
func ExternalStateAcquisitionRegistry() ([]StateRequirement, []AcquisitionCall) {
	requirements := []StateRequirement{
		{ID: "provider.billing.plan-cost", Plane: StateBilled, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 86400},
		{ID: "experience.journey.aggregate", Plane: StateExperienced, MinimumFidelity: FidelityAggregated, MaximumStalenessSeconds: 300},
		{ID: "network.flow.aggregate", Plane: StateObserved, MinimumFidelity: FidelityAggregated, MaximumStalenessSeconds: 60},
		{ID: "capacity.nodes.aggregate", Plane: StateObserved, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 30},
	}
	calls := []AcquisitionCall{
		{ID: "provider-billing.invoice", Source: "provider-billing", Method: "GET", Endpoint: "/invoices?planId={planID}", Consistency: "finalized", CostUnits: 5, Observations: []FieldObservation{{Requirement: "provider.billing.plan-cost", Plane: StateBilled, Fidelity: FidelityExact, MaximumStalenessSeconds: 86400}}},
		{ID: "experience.journeys", Source: "experience-telemetry", Method: "GET", Endpoint: "/journeys?environmentId={environmentID}", Consistency: "windowed", CostUnits: 4, Observations: []FieldObservation{{Requirement: "experience.journey.aggregate", Plane: StateExperienced, Fidelity: FidelityAggregated, MaximumStalenessSeconds: 300}}},
		{ID: "hubble.flows", Source: "hubble", Method: "GET", Endpoint: "/flows?environmentId={environmentID}", Consistency: "windowed", CostUnits: 3, Observations: []FieldObservation{{Requirement: "network.flow.aggregate", Plane: StateObserved, Fidelity: FidelityAggregated, MaximumStalenessSeconds: 60}}},
		{ID: "kubernetes.nodes", Source: "kubernetes", Method: "GET", Endpoint: "/api/v1/nodes?environmentId={environmentID}", Consistency: "resource-version", CostUnits: 2, Observations: []FieldObservation{{Requirement: "capacity.nodes.aggregate", Plane: StateObserved, Fidelity: FidelityExact, MaximumStalenessSeconds: 30}}},
	}
	return requirements, calls
}

// ResolveExternalStateAcquisition binds candidate provider reads to already
// authorized immutable coordinates. It performs no discovery and no network
// call; provider adapters must still enforce their own exact route contract.
func ResolveExternalStateAcquisition(environmentID, planID string) ([]StateRequirement, []AcquisitionCall, error) {
	if !validPlatformHubPathID(environmentID) || !validPlatformHubPathID(planID) {
		return nil, nil, errors.New("external state acquisition requires bounded environment and plan identifiers")
	}
	requirements, calls := ExternalStateAcquisitionRegistry()
	for index := range calls {
		calls[index].Endpoint = strings.ReplaceAll(calls[index].Endpoint, "{environmentID}", environmentID)
		calls[index].Endpoint = strings.ReplaceAll(calls[index].Endpoint, "{planID}", planID)
	}
	return requirements, calls, nil
}

func validPlatformHubPathID(value string) bool {
	if value == "" || len(value) > 64 || value == "." || value == ".." {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || strings.ContainsRune("-_.:", character) {
			continue
		}
		return false
	}
	return true
}

type ExternalStateGap struct {
	ID                 string     `json:"id"`
	Plane              StatePlane `json:"plane"`
	RequiredProvenance string     `json:"requiredProvenance"`
	Reason             string     `json:"reason"`
}

func PlatformExternalStateGaps() []ExternalStateGap {
	return []ExternalStateGap{
		{ID: "provider.billing", Plane: StateBilled, RequiredProvenance: "provider invoice/export schema and immutable revision", Reason: "billing is provider-owned"},
		{ID: "user.experience", Plane: StateExperienced, RequiredProvenance: "consented product outcome or RUM schema and collector revision", Reason: "the control plane does not observe user experience"},
		{ID: "network.fulfillment", Plane: StateObserved, RequiredProvenance: "environment-owned network telemetry contract and collector revision", Reason: "network infrastructure is environment-owned"},
		{ID: "capacity.fulfillment", Plane: StateObserved, RequiredProvenance: "environment scheduler/provider capacity API and immutable response digest", Reason: "capacity fulfillment is environment-owned"},
	}
}

package optimization

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

const (
	SubstrateObservationAPIVersion = "optimization.platform.example/substrate-observation/v1alpha3"
	ObservationFidelityExact       = "EXACT"
)

// ProviderObservationCall is a credential-free receipt for one read-only
// provider call. Request and response bodies remain with the provider owner;
// their exact digests make the normalized observation independently traceable.
type ProviderObservationCall struct {
	ID                string `json:"id"`
	Provider          string `json:"provider"`
	Operation         string `json:"operation"`
	ObservedAt        string `json:"observedAt"`
	SourceRevision    string `json:"sourceRevision"`
	RequestDigest     string `json:"requestDigest"`
	ResponseDigest    string `json:"responseDigest"`
	CollectorIdentity string `json:"collectorIdentity"`
	Fidelity          string `json:"fidelity"`
}

type SubstrateCapacityBinding struct {
	AvailableCallID  string         `json:"availableCallId"`
	ProjectionCallID string         `json:"projectionCallId"`
	Available        CapacityVector `json:"available"`
	ReadyNodes       int64          `json:"readyNodes"`
	NodesAfterChange int64          `json:"nodesAfterChange"`
}

type SubstrateStorageBinding struct {
	CallID            string                `json:"callId"`
	ObservedNodeCount int64                 `json:"observedNodeCount"`
	Nodes             []NodeStorageEvidence `json:"nodes"`
}

type SubstrateDependencyBinding struct {
	CallID       string `json:"callId"`
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Required     bool   `json:"required"`
	Status       string `json:"status"`
	DefectOrigin string `json:"defectOrigin"`
}

type SubstrateObservationSnapshot struct {
	APIVersion              string                       `json:"apiVersion"`
	Kind                    string                       `json:"kind"`
	ID                      string                       `json:"id"`
	SnapshotAt              string                       `json:"snapshotAt"`
	MaximumStalenessSeconds int64                        `json:"maximumStalenessSeconds"`
	Calls                   []ProviderObservationCall    `json:"calls"`
	Capacity                SubstrateCapacityBinding     `json:"capacity"`
	Storage                 SubstrateStorageBinding      `json:"storage"`
	Dependencies            []SubstrateDependencyBinding `json:"dependencies"`
}

func NewSubstrateObservationSnapshot(snapshot SubstrateObservationSnapshot) (SubstrateObservationSnapshot, error) {
	snapshot.APIVersion = SubstrateObservationAPIVersion
	snapshot.Kind = "SubstrateObservation"
	snapshot.ID = ""
	snapshot.Calls = append([]ProviderObservationCall(nil), snapshot.Calls...)
	snapshot.Dependencies = append([]SubstrateDependencyBinding(nil), snapshot.Dependencies...)
	snapshot.Storage.Nodes = append([]NodeStorageEvidence(nil), snapshot.Storage.Nodes...)
	sort.Slice(snapshot.Calls, func(i, j int) bool { return snapshot.Calls[i].ID < snapshot.Calls[j].ID })
	sort.Slice(snapshot.Dependencies, func(i, j int) bool { return snapshot.Dependencies[i].ID < snapshot.Dependencies[j].ID })
	sort.Slice(snapshot.Storage.Nodes, func(i, j int) bool {
		return snapshot.Storage.Nodes[i].NodeID < snapshot.Storage.Nodes[j].NodeID
	})
	if err := validateSubstrateObservation(snapshot); err != nil {
		return SubstrateObservationSnapshot{}, err
	}
	id, err := substrateObservationID(snapshot)
	if err != nil {
		return SubstrateObservationSnapshot{}, err
	}
	snapshot.ID = id
	return snapshot, nil
}

func VerifySubstrateObservationSnapshot(snapshot SubstrateObservationSnapshot) error {
	if snapshot.APIVersion != SubstrateObservationAPIVersion || snapshot.Kind != "SubstrateObservation" || !validContentID(snapshot.ID, "sobs_") {
		return errors.New("unsupported or invalid substrate observation snapshot")
	}
	replayed, err := NewSubstrateObservationSnapshot(snapshot)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(replayed.Calls, snapshot.Calls) || !reflect.DeepEqual(replayed.Dependencies, snapshot.Dependencies) || !reflect.DeepEqual(replayed.Storage.Nodes, snapshot.Storage.Nodes) {
		return errors.New("substrate observation sets are not in canonical order")
	}
	if replayed.ID != snapshot.ID {
		return fmt.Errorf("substrate observation id does not match content: expected %s", replayed.ID)
	}
	return nil
}

func CompileSubstratePreflight(snapshot SubstrateObservationSnapshot, demand RolloutCapacityDemand, requireNetworkPolicyCoverage, requireNetworkTelemetryCoverage bool) (SubstratePreflightRequest, error) {
	if err := VerifySubstrateObservationSnapshot(snapshot); err != nil {
		return SubstratePreflightRequest{}, err
	}
	calls := make(map[string]ProviderObservationCall, len(snapshot.Calls))
	used := make(map[string]bool, len(snapshot.Calls))
	for _, call := range snapshot.Calls {
		calls[call.ID] = call
	}
	capacityCall, ok := calls[snapshot.Capacity.AvailableCallID]
	if !ok {
		return SubstratePreflightRequest{}, errors.New("substrate capacity references an absent provider call")
	}
	used[capacityCall.ID] = true
	projectionCall, ok := calls[snapshot.Capacity.ProjectionCallID]
	if !ok {
		return SubstratePreflightRequest{}, errors.New("substrate capacity projection references an absent provider call")
	}
	used[projectionCall.ID] = true
	storageCall, ok := calls[snapshot.Storage.CallID]
	if !ok {
		return SubstratePreflightRequest{}, errors.New("substrate storage references an absent provider call")
	}
	used[storageCall.ID] = true
	request := SubstratePreflightRequest{
		SnapshotAt: snapshot.SnapshotAt, MaximumStalenessSeconds: snapshot.MaximumStalenessSeconds,
		Capacity: CapacityEvidence{
			Available: snapshot.Capacity.Available, ReadyNodes: snapshot.Capacity.ReadyNodes,
			NodesAfterChange: snapshot.Capacity.NodesAfterChange, ObservedAt: capacityCall.ObservedAt,
			Source: capacityCall.Provider + "/" + capacityCall.Operation, SourceRevision: capacityCall.SourceRevision,
			EvidenceDigest:       capacityCall.ResponseDigest,
			ProjectionObservedAt: projectionCall.ObservedAt, ProjectionSource: projectionCall.Provider + "/" + projectionCall.Operation,
			ProjectionSourceRevision: projectionCall.SourceRevision, ProjectionEvidenceDigest: projectionCall.ResponseDigest,
			Storage: StorageEvidence{
				ObservedNodeCount: snapshot.Storage.ObservedNodeCount, Nodes: append([]NodeStorageEvidence(nil), snapshot.Storage.Nodes...),
				ObservedAt: storageCall.ObservedAt, Source: storageCall.Provider + "/" + storageCall.Operation,
				SourceRevision: storageCall.SourceRevision, EvidenceDigest: storageCall.ResponseDigest,
			},
		},
		Demand: demand, RequireNetworkPolicyCoverage: requireNetworkPolicyCoverage, RequireNetworkTelemetryCoverage: requireNetworkTelemetryCoverage,
	}
	for _, binding := range snapshot.Dependencies {
		call, exists := calls[binding.CallID]
		if !exists {
			return SubstratePreflightRequest{}, fmt.Errorf("dependency %q references an absent provider call", binding.ID)
		}
		used[call.ID] = true
		request.Dependencies = append(request.Dependencies, SubstrateDependencyEvidence{
			ID: binding.ID, Kind: binding.Kind, Required: binding.Required, Status: binding.Status,
			DefectOrigin: binding.DefectOrigin, ObservedAt: call.ObservedAt, Source: call.Provider + "/" + call.Operation,
			SourceRevision: call.SourceRevision, EvidenceDigest: call.ResponseDigest,
		})
	}
	for _, call := range snapshot.Calls {
		if !used[call.ID] {
			return SubstratePreflightRequest{}, fmt.Errorf("provider call %q is not bound to any preflight fact", call.ID)
		}
	}
	if _, err := AssessSubstratePreflight(request); err != nil {
		return SubstratePreflightRequest{}, err
	}
	return request, nil
}

func validateSubstrateObservation(snapshot SubstrateObservationSnapshot) error {
	snapshotAt, err := time.Parse(time.RFC3339, snapshot.SnapshotAt)
	if err != nil {
		return fmt.Errorf("substrate observation snapshotAt must be RFC3339: %w", err)
	}
	if snapshot.MaximumStalenessSeconds < 0 {
		return errors.New("substrate observation maximumStalenessSeconds must be non-negative")
	}
	if len(snapshot.Calls) == 0 {
		return errors.New("substrate observation requires provider calls")
	}
	seenCalls := map[string]ProviderObservationCall{}
	for _, call := range snapshot.Calls {
		if !validEvidenceCoordinate(call.ID) || !validEvidenceCoordinate(call.Provider) || !validEvidenceCoordinate(call.Operation) || !validObservationText(call.CollectorIdentity) {
			return errors.New("provider call requires safe id, provider, operation, and collectorIdentity coordinates")
		}
		if _, exists := seenCalls[call.ID]; exists {
			return fmt.Errorf("duplicate provider call %q", call.ID)
		}
		seenCalls[call.ID] = call
		if call.Fidelity != ObservationFidelityExact || !validObservationText(call.SourceRevision) || !validDigest(call.RequestDigest) || !validDigest(call.ResponseDigest) {
			return fmt.Errorf("provider call %q requires EXACT fidelity, sourceRevision, and sha256 request/response digests", call.ID)
		}
		if err := validateObservationTime("provider call "+call.ID, call.ObservedAt, snapshotAt, snapshot.MaximumStalenessSeconds); err != nil {
			return err
		}
	}
	if !validEvidenceCoordinate(snapshot.Capacity.AvailableCallID) || !validEvidenceCoordinate(snapshot.Capacity.ProjectionCallID) {
		return errors.New("substrate capacity requires safe availableCallId and projectionCallId coordinates")
	}
	if snapshot.Capacity.AvailableCallID == snapshot.Capacity.ProjectionCallID {
		return errors.New("observed capacity and post-change projection require distinct provider calls")
	}
	if _, exists := seenCalls[snapshot.Capacity.AvailableCallID]; !exists {
		return errors.New("substrate capacity references an absent provider call")
	}
	if _, exists := seenCalls[snapshot.Capacity.ProjectionCallID]; !exists {
		return errors.New("substrate capacity projection references an absent provider call")
	}
	if !validEvidenceCoordinate(snapshot.Storage.CallID) {
		return errors.New("substrate storage requires a safe callId coordinate")
	}
	if snapshot.Storage.CallID == snapshot.Capacity.AvailableCallID || snapshot.Storage.CallID == snapshot.Capacity.ProjectionCallID {
		return errors.New("runtime storage evidence requires a distinct provider call")
	}
	storageCall, exists := seenCalls[snapshot.Storage.CallID]
	if !exists {
		return errors.New("substrate storage references an absent provider call")
	}
	if err := validateStorageEvidence(StorageEvidence{
		ObservedNodeCount: snapshot.Storage.ObservedNodeCount, Nodes: snapshot.Storage.Nodes,
		ObservedAt: storageCall.ObservedAt, Source: storageCall.Provider + "/" + storageCall.Operation,
		SourceRevision: storageCall.SourceRevision, EvidenceDigest: storageCall.ResponseDigest,
	}, snapshotAt, snapshot.MaximumStalenessSeconds); err != nil {
		return err
	}
	if err := validateVector("substrate observation capacity.available", snapshot.Capacity.Available); err != nil {
		return err
	}
	if snapshot.Capacity.ReadyNodes < 0 || snapshot.Capacity.NodesAfterChange < 0 {
		return errors.New("substrate observation node counts must be non-negative")
	}
	usedCalls := map[string]bool{snapshot.Capacity.AvailableCallID: true, snapshot.Capacity.ProjectionCallID: true, snapshot.Storage.CallID: true}
	seenDependencies := map[string]struct{}{}
	for _, dependency := range snapshot.Dependencies {
		if !validEvidenceCoordinate(dependency.CallID) || !validEvidenceCoordinate(dependency.ID) {
			return errors.New("substrate dependency binding requires safe callId and id coordinates")
		}
		if _, exists := seenDependencies[dependency.ID]; exists {
			return fmt.Errorf("duplicate substrate dependency binding %q", dependency.ID)
		}
		seenDependencies[dependency.ID] = struct{}{}
		call, exists := seenCalls[dependency.CallID]
		if !exists {
			return fmt.Errorf("dependency %q references an absent provider call", dependency.ID)
		}
		usedCalls[dependency.CallID] = true
		if err := validateDependency(SubstrateDependencyEvidence{
			ID: dependency.ID, Kind: dependency.Kind, Required: dependency.Required, Status: dependency.Status,
			DefectOrigin: dependency.DefectOrigin, ObservedAt: call.ObservedAt, Source: call.Provider + "/" + call.Operation,
			SourceRevision: call.SourceRevision, EvidenceDigest: call.ResponseDigest,
		}, snapshotAt, snapshot.MaximumStalenessSeconds); err != nil {
			return err
		}
	}
	for _, call := range snapshot.Calls {
		if !usedCalls[call.ID] {
			return fmt.Errorf("provider call %q is not bound to any preflight fact", call.ID)
		}
	}
	return nil
}

func validObservationText(value string) bool {
	if value == "" || len(value) > 256 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func substrateObservationID(snapshot SubstrateObservationSnapshot) (string, error) {
	snapshot.ID = ""
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return "", fmt.Errorf("encode substrate observation identity: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return "sobs_" + hex.EncodeToString(digest[:]), nil
}

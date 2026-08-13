package optimization

import (
	"os"
	"strings"
	"testing"
)

func TestCompileSubstrateObservationBindsEveryFactToExactCall(t *testing.T) {
	snapshot, err := NewSubstrateObservationSnapshot(validSubstrateObservation())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Calls[0].ID != "aws-cni" || snapshot.Dependencies[0].ID != "cni-permission" {
		t.Fatalf("snapshot is not canonical: %#v", snapshot)
	}
	if snapshot.APIVersion != "optimization.platform.example/substrate-observation/v1alpha3" {
		t.Fatalf("apiVersion = %q", snapshot.APIVersion)
	}
	request, err := CompileSubstratePreflight(snapshot, validSubstratePreflight().Demand, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if request.Capacity.Source != "kubernetes/list-nodes" || request.Capacity.EvidenceDigest != digest('c') {
		t.Fatalf("capacity = %#v", request.Capacity)
	}
	if len(request.Dependencies) != 4 || request.Dependencies[0].Source != "aws/simulate-cni-permissions" || request.Dependencies[0].EvidenceDigest != digest('b') {
		t.Fatalf("dependencies = %#v", request.Dependencies)
	}
	assessment, err := AssessSubstratePreflight(request)
	if err != nil || !assessment.Eligible {
		t.Fatalf("assessment = %#v, error = %v", assessment, err)
	}
}

func TestSubstrateObservationRejectsLegacyVersionWithoutStorageAdmission(t *testing.T) {
	snapshot, err := NewSubstrateObservationSnapshot(validSubstrateObservation())
	if err != nil {
		t.Fatal(err)
	}
	snapshot.APIVersion = "optimization.platform.example/substrate-observation/v1alpha1"
	if err := VerifySubstrateObservationSnapshot(snapshot); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("legacy version error = %v", err)
	}
}

func TestSubstrateObservationRejectsUnboundAndAbsentCalls(t *testing.T) {
	tests := []struct {
		name string
		edit func(*SubstrateObservationSnapshot)
		want string
	}{
		{"unbound", func(snapshot *SubstrateObservationSnapshot) {
			snapshot.Calls = append(snapshot.Calls, providerCall("unused", "aws", "describe-vpcs", 'd'))
		}, "not bound"},
		{"capacity absent", func(snapshot *SubstrateObservationSnapshot) { snapshot.Capacity.AvailableCallID = "absent" }, "capacity references an absent"},
		{"projection absent", func(snapshot *SubstrateObservationSnapshot) { snapshot.Capacity.ProjectionCallID = "absent" }, "capacity projection references an absent"},
		{"projection conflated", func(snapshot *SubstrateObservationSnapshot) {
			snapshot.Capacity.ProjectionCallID = snapshot.Capacity.AvailableCallID
		}, "require distinct provider calls"},
		{"storage absent", func(snapshot *SubstrateObservationSnapshot) { snapshot.Storage.CallID = "absent" }, "storage references an absent"},
		{"storage conflated", func(snapshot *SubstrateObservationSnapshot) {
			snapshot.Storage.CallID = snapshot.Capacity.AvailableCallID
		}, "storage evidence requires a distinct"},
		{"dependency absent", func(snapshot *SubstrateObservationSnapshot) {
			for index := range snapshot.Dependencies {
				if snapshot.Dependencies[index].ID == "network-policy" {
					snapshot.Dependencies[index].CallID = "absent"
				}
			}
		}, "dependency \"network-policy\" references an absent"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := validSubstrateObservation()
			test.edit(&snapshot)
			if _, err := NewSubstrateObservationSnapshot(snapshot); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestSubstrateObservationRejectsNonExactStaleAndTamperedEvidence(t *testing.T) {
	tests := []struct {
		name string
		edit func(*SubstrateObservationSnapshot)
		want string
	}{
		{"estimated", func(snapshot *SubstrateObservationSnapshot) { snapshot.Calls[0].Fidelity = "ESTIMATED" }, "EXACT fidelity"},
		{"stale", func(snapshot *SubstrateObservationSnapshot) { snapshot.Calls[0].ObservedAt = "2026-08-09T11:58:00Z" }, "maximum staleness"},
		{"bad response digest", func(snapshot *SubstrateObservationSnapshot) { snapshot.Calls[0].ResponseDigest = "latest" }, "request/response digests"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := validSubstrateObservation()
			test.edit(&snapshot)
			if _, err := NewSubstrateObservationSnapshot(snapshot); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}

	snapshot, err := NewSubstrateObservationSnapshot(validSubstrateObservation())
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Capacity.ReadyNodes++
	if err := VerifySubstrateObservationSnapshot(snapshot); err == nil || !strings.Contains(err.Error(), "id does not match") {
		t.Fatalf("tamper error = %v", err)
	}
}

func TestPinnedSubstrateObservationFixtureCompiles(t *testing.T) {
	encoded, err := os.ReadFile("testdata/substrate-observation.json")
	if err != nil {
		t.Fatal(err)
	}
	var snapshot SubstrateObservationSnapshot
	if err := decodeStoredJSON(encoded, &snapshot); err != nil {
		t.Fatal(err)
	}
	if err := VerifySubstrateObservationSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := CompileSubstratePreflight(snapshot, validSubstratePreflight().Demand, true, true); err != nil {
		t.Fatal(err)
	}
}

func validSubstrateObservation() SubstrateObservationSnapshot {
	return SubstrateObservationSnapshot{
		SnapshotAt: "2026-08-09T12:00:00Z", MaximumStalenessSeconds: 60,
		Calls: []ProviderObservationCall{
			providerCall("kubernetes-capacity", "kubernetes", "list-nodes", 'c'),
			providerCall("node-pool-projection", "aws", "simulate-node-pool-change", 'd'),
			providerCall("compute-offering", "aws", "resolve-eligible-instance-types", '9'),
			providerCall("network-policy", "kubernetes", "evaluate-network-policy", 'a'),
			providerCall("network-telemetry", "hubble", "verify-flow-ingestion", 'e'),
			providerCall("aws-cni", "aws", "simulate-cni-permissions", 'b'),
			providerCall("node-storage", "kubernetes", "get-node-storage-stats", '8'),
		},
		Capacity: SubstrateCapacityBinding{AvailableCallID: "kubernetes-capacity", ProjectionCallID: "node-pool-projection", Available: CapacityVector{PodSlots: 10, MilliCPU: 10000, MemoryBytes: 20_000_000_000, IPAddresses: 10, EphemeralStorageBytes: 40_000_000_000}, ReadyNodes: 3, NodesAfterChange: 3},
		Storage: SubstrateStorageBinding{CallID: "node-storage", ObservedNodeCount: 3, Nodes: []NodeStorageEvidence{
			{NodeID: "node-a", Status: DiskPressureFalse, FreeBytes: 10_000_000_000, FreeBytesObserved: true, FreeInodes: 1_000_000, FreeInodesObserved: true},
			{NodeID: "node-b", Status: DiskPressureFalse, FreeBytes: 10_000_000_000, FreeBytesObserved: true, FreeInodes: 1_000_000, FreeInodesObserved: true},
			{NodeID: "node-c", Status: DiskPressureFalse, FreeBytes: 10_000_000_000, FreeBytesObserved: true, FreeInodes: 1_000_000, FreeInodesObserved: true},
		}},
		Dependencies: []SubstrateDependencyBinding{
			{CallID: "compute-offering", ID: "compute-offering", Kind: "COMPUTE_OFFERING", Required: true, Status: DependencyHealthy, DefectOrigin: DefectUnknown},
			{CallID: "network-policy", ID: "network-policy", Kind: "NETWORK_POLICY", Required: true, Status: DependencyHealthy, DefectOrigin: DefectUnknown},
			{CallID: "network-telemetry", ID: "network-telemetry", Kind: "NETWORK_TELEMETRY", Required: true, Status: DependencyHealthy, DefectOrigin: DefectUnknown},
			{CallID: "aws-cni", ID: "cni-permission", Kind: "CNI_PERMISSION", Required: true, Status: DependencyHealthy, DefectOrigin: DefectUnknown},
		},
	}
}

func providerCall(id, provider, operation string, response byte) ProviderObservationCall {
	return ProviderObservationCall{ID: id, Provider: provider, Operation: operation, ObservedAt: "2026-08-09T11:59:30Z", SourceRevision: "cluster-r43", RequestDigest: digest('f'), ResponseDigest: digest(response), CollectorIdentity: "collector:substrate", Fidelity: ObservationFidelityExact}
}

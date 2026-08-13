package optimization

import (
	"math"
	"strings"
	"testing"
)

func TestSubstratePreflightAccountsForSurgeDaemonSetsAndPDBs(t *testing.T) {
	request := validSubstratePreflight()
	request.Capacity.Available.PodSlots = 4
	request.Demand.Additional.PodSlots = 3
	request.Demand.DaemonSetPerNewNode.PodSlots = 1
	request.Demand.NewNodes = 2
	request.Demand.Storage.Placements[0].WorkloadBytes = 2_200_000_000
	request.Demand.ConcurrentEvictions = 2
	request.Demand.DisruptionsAllowed = 1

	assessment, err := AssessSubstratePreflight(request)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Eligible {
		t.Fatalf("assessment = %#v", assessment)
	}
	joined := strings.Join(assessment.BlockingReasons, "\n")
	for _, expected := range []string{"pod-slots requires 5 but only 4", "concurrent evictions 2 exceed disruptions allowed 1"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("reasons %q do not contain %q", joined, expected)
		}
	}
}

func TestSubstratePreflightRejectsEphemeralStoragePressure(t *testing.T) {
	request := validSubstratePreflight()
	request.Capacity.Available.EphemeralStorageBytes = 4_000_000_000
	request.Demand.Additional.EphemeralStorageBytes = 3_500_000_000
	request.Demand.DaemonSetPerNewNode.EphemeralStorageBytes = 300_000_000
	request.Demand.NewNodes = 2
	request.Demand.Storage.Placements[0].WorkloadBytes = 4_100_000_000

	assessment, err := AssessSubstratePreflight(request)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Eligible || !strings.Contains(strings.Join(assessment.BlockingReasons, "\n"),
		"ephemeral-storage-bytes requires 4100000000 but only 4000000000 is available") {
		t.Fatalf("assessment = %#v", assessment)
	}
}

func TestSubstratePreflightBlocksDiskPressureAndUnknownCondition(t *testing.T) {
	for _, test := range []struct {
		name   string
		status string
		want   string
	}{{"pressure", DiskPressureTrue, "node node-b reports DiskPressure"}, {"unknown", DiskPressureUnknown, "node node-b DiskPressure condition is UNKNOWN"}} {
		t.Run(test.name, func(t *testing.T) {
			request := validSubstratePreflight()
			request.Capacity.Storage.Nodes[1].Status = test.status
			assessment, err := AssessSubstratePreflight(request)
			if err != nil {
				t.Fatal(err)
			}
			if assessment.Eligible || !strings.Contains(strings.Join(assessment.BlockingReasons, "\n"), test.want) {
				t.Fatalf("assessment = %#v", assessment)
			}
		})
	}
}

func TestSubstratePreflightFailsClosedWithoutDiskInodeAndArtifactEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SubstratePreflightRequest)
		want   string
	}{
		{"conditions", func(r *SubstratePreflightRequest) { r.Capacity.Storage.Nodes = nil }, "one DiskPressure/free-space observation"},
		{"inodes", func(r *SubstratePreflightRequest) { r.Capacity.Storage.Nodes[0].FreeInodesObserved = false }, "missing free bytes or free inodes"},
		{"images", func(r *SubstratePreflightRequest) { r.Demand.Storage.Images = nil }, "image artifact size evidence"},
		{"image size", func(r *SubstratePreflightRequest) { r.Demand.Storage.Images[0].UnpackedBytes = 0 }, "positive compressedBytes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := validSubstratePreflight()
			test.mutate(&request)
			if _, err := AssessSubstratePreflight(request); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestSubstratePreflightAccountsForImagesReservesHeadroomAndInodesOnce(t *testing.T) {
	request := validSubstratePreflight()
	request.Capacity.Storage.Nodes[0].FreeBytes = 8_799_999_999
	request.Capacity.Storage.Nodes[0].FreeInodes = 619_999
	assessment, err := AssessSubstratePreflight(request)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(assessment.BlockingReasons, "\n")
	// 2.1GB workload (including one DaemonSet amplification) + 0.7GB image
	// transfer/unpack + 1GB writable/log reserve + 5GB retained headroom.
	if assessment.Eligible || !strings.Contains(joined, "node-storage-free-bytes/node-a requires 8800000000") || !strings.Contains(joined, "node-storage-free-inodes/node-a requires 620000") {
		t.Fatalf("assessment = %#v", assessment)
	}
	request.Demand.Storage.Images = append(request.Demand.Storage.Images, request.Demand.Storage.Images[0])
	if _, err := AssessSubstratePreflight(request); err == nil || !strings.Contains(err.Error(), "duplicate image storage evidence") {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestSubstratePreflightRejectsStrandedNodeStorageDespiteAggregateHeadroom(t *testing.T) {
	request := validSubstratePreflight()
	request.Capacity.Storage.Nodes[0].FreeBytes = 1_000_000_000
	request.Capacity.Storage.Nodes[1].FreeBytes = 100_000_000_000
	request.Capacity.Storage.Nodes[2].FreeBytes = 100_000_000_000
	assessment, err := AssessSubstratePreflight(request)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Eligible || !strings.Contains(strings.Join(assessment.BlockingReasons, "\n"), "node-storage-free-bytes/node-a requires 8800000000 but only 1000000000 is available") {
		t.Fatalf("assessment = %#v", assessment)
	}
}

func TestSubstratePreflightRequiresExactNonDuplicatedPlacementProjection(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SubstratePreflightRequest)
		want   string
	}{
		{"workload total", func(r *SubstratePreflightRequest) { r.Demand.Storage.Placements[0].WorkloadBytes++ }, "do not equal peak rollout and DaemonSet demand"},
		{"image repeated on node", func(r *SubstratePreflightRequest) {
			r.Demand.Storage.Placements[0].ImageDigests = append(r.Demand.Storage.Placements[0].ImageDigests, digest('1'))
		}, "repeats image"},
		{"copy count", func(r *SubstratePreflightRequest) { r.Demand.Storage.Images[0].Copies = 2 }, "do not match 1 projected node placements"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := validSubstratePreflight()
			test.mutate(&request)
			if _, err := AssessSubstratePreflight(request); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestSubstratePreflightTreatsObservedZeroAsIneligibleNotMissing(t *testing.T) {
	request := validSubstratePreflight()
	request.Capacity.Storage.Nodes[0].FreeBytes = 0
	request.Capacity.Storage.Nodes[0].FreeInodes = 0
	assessment, err := AssessSubstratePreflight(request)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(assessment.BlockingReasons, "\n")
	if assessment.Eligible || !strings.Contains(joined, "only 0 is available") {
		t.Fatalf("assessment = %#v", assessment)
	}
}

func TestSubstratePreflightFailsClosedForMissingAndUnhealthyDependencies(t *testing.T) {
	request := validSubstratePreflight()
	request.RequireNetworkPolicyCoverage = true
	request.Dependencies = []SubstrateDependencyEvidence{
		dependency("registry", "REGISTRY", DependencyHealthy, DefectUnknown),
		dependency("aws-cni-permission", "CNI_PERMISSION", DependencyUnhealthy, DefectLatent),
	}
	assessment, err := AssessSubstratePreflight(request)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(assessment.BlockingReasons, "\n")
	if assessment.Eligible || !strings.Contains(joined, "aws-cni-permission is UNHEALTHY (LATENT defect)") || !strings.Contains(joined, "healthy network policy coverage evidence is missing") {
		t.Fatalf("assessment = %#v", assessment)
	}
}

func TestSubstratePreflightRejectsStaleFutureAndUnpinnedEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SubstratePreflightRequest)
		want   string
	}{
		{"stale", func(r *SubstratePreflightRequest) { r.Capacity.ObservedAt = "2026-08-09T11:58:00Z" }, "exceeds maximum staleness"},
		{"stale projection", func(r *SubstratePreflightRequest) { r.Capacity.ProjectionObservedAt = "2026-08-09T11:58:00Z" }, "capacity projection evidence exceeds maximum staleness"},
		{"future", func(r *SubstratePreflightRequest) { r.Dependencies[0].ObservedAt = "2026-08-09T12:00:01Z" }, "newer than snapshot"},
		{"digest", func(r *SubstratePreflightRequest) { r.Dependencies[0].EvidenceDigest = "latest" }, "sha256 evidenceDigest"},
		{"projection digest", func(r *SubstratePreflightRequest) { r.Capacity.ProjectionEvidenceDigest = "latest" }, "sha256 projectionEvidenceDigest"},
		{"storage digest", func(r *SubstratePreflightRequest) { r.Capacity.Storage.EvidenceDigest = "latest" }, "storage evidence requires"},
		{"artifact request digest", func(r *SubstratePreflightRequest) { r.Demand.Storage.Images[0].RequestDigest = "latest" }, "EXACT read-only receipt"},
		{"artifact stale", func(r *SubstratePreflightRequest) { r.Demand.Storage.Images[0].ObservedAt = "2026-08-09T11:58:00Z" }, "image sha256:1111111111111111111111111111111111111111111111111111111111111111 evidence exceeds maximum staleness"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := validSubstratePreflight()
			test.mutate(&request)
			if _, err := AssessSubstratePreflight(request); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestSubstratePreflightRejectsMissingEphemeralStorageEvidence(t *testing.T) {
	request := validSubstratePreflight()
	request.Capacity.Available.EphemeralStorageBytes = 0
	if _, err := AssessSubstratePreflight(request); err == nil || !strings.Contains(err.Error(), "capacity.available.ephemeralStorageBytes must be positive") {
		t.Fatalf("capacity error = %v", err)
	}

	request = validSubstratePreflight()
	request.Demand.Additional.EphemeralStorageBytes = 0
	if _, err := AssessSubstratePreflight(request); err == nil || !strings.Contains(err.Error(), "demand.additional.ephemeralStorageBytes must be positive") {
		t.Fatalf("demand error = %v", err)
	}
}

func TestSubstratePreflightRejectsOverflow(t *testing.T) {
	request := validSubstratePreflight()
	request.Demand.DaemonSetPerNewNode.MemoryBytes = math.MaxInt64
	request.Demand.NewNodes = 2
	if _, err := AssessSubstratePreflight(request); err == nil || !strings.Contains(err.Error(), "overflows") {
		t.Fatalf("error = %v", err)
	}
}

func TestSubstratePreflightRequiresAcceptanceWindow(t *testing.T) {
	request := validSubstratePreflight()
	request.Demand.AcceptanceWindowSecs = 0
	if _, err := AssessSubstratePreflight(request); err == nil || !strings.Contains(err.Error(), "positive acceptance window") {
		t.Fatalf("error = %v", err)
	}
}

func TestSubstratePreflightSeparatesNetworkEnforcementAndTelemetryRequirements(t *testing.T) {
	for _, test := range []struct {
		name                            string
		requirePolicy, requireTelemetry bool
		wantPolicy, wantTelemetry       bool
	}{
		{"neither", false, false, false, false},
		{"policy only", true, false, true, false},
		{"telemetry only", false, true, false, true},
		{"both", true, true, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := validSubstratePreflight()
			request.Dependencies = request.Dependencies[:1]
			request.RequireNetworkPolicyCoverage = test.requirePolicy
			request.RequireNetworkTelemetryCoverage = test.requireTelemetry
			assessment, err := AssessSubstratePreflight(request)
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(assessment.BlockingReasons, "\n")
			policyMissing := strings.Contains(joined, "network policy coverage evidence is missing")
			telemetryMissing := strings.Contains(joined, "network telemetry coverage evidence is missing")
			if policyMissing != test.wantPolicy || telemetryMissing != test.wantTelemetry || assessment.Eligible != (!test.wantPolicy && !test.wantTelemetry) {
				t.Fatalf("assessment = %#v", assessment)
			}
		})
	}
}

func TestSubstratePreflightEligibleWithCompleteHealthyEvidence(t *testing.T) {
	assessment, err := AssessSubstratePreflight(validSubstratePreflight())
	if err != nil {
		t.Fatal(err)
	}
	if !assessment.Eligible || len(assessment.BlockingReasons) != 0 || len(assessment.CapacityChecks) != 7 {
		t.Fatalf("assessment = %#v", assessment)
	}
}

func TestSubstratePreflightBlocksProviderIneligibleComputeOffering(t *testing.T) {
	request := validSubstratePreflight()
	request.Dependencies = append(request.Dependencies, dependency("requested-instance-type", "COMPUTE_OFFERING", DependencyUnhealthy, DefectIntroduced))
	assessment, err := AssessSubstratePreflight(request)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Eligible || !strings.Contains(strings.Join(assessment.BlockingReasons, "\n"), "requested-instance-type is UNHEALTHY (INTRODUCED defect)") {
		t.Fatalf("assessment = %#v", assessment)
	}
}

func TestSubstratePreflightBlocksMissingNamespaceDeliveryRBAC(t *testing.T) {
	request := validSubstratePreflight()
	request.Dependencies = append(request.Dependencies,
		dependency("plane-connection-delivery-rbac", "NAMESPACE_RBAC", DependencyUnhealthy, DefectLatent))
	assessment, err := AssessSubstratePreflight(request)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Eligible || !strings.Contains(strings.Join(assessment.BlockingReasons, "\n"),
		"plane-connection-delivery-rbac is UNHEALTHY (LATENT defect)") {
		t.Fatalf("assessment = %#v", assessment)
	}
}

func validSubstratePreflight() SubstratePreflightRequest {
	return SubstratePreflightRequest{
		SnapshotAt: "2026-08-09T12:00:00Z", MaximumStalenessSeconds: 60,
		Capacity: CapacityEvidence{
			Available:  CapacityVector{PodSlots: 10, MilliCPU: 10_000, MemoryBytes: 10_000_000_000, IPAddresses: 10, EphemeralStorageBytes: 20_000_000_000},
			ReadyNodes: 3, NodesAfterChange: 4, ObservedAt: "2026-08-09T11:59:30Z", Source: "scheduler",
			SourceRevision: "cluster-r43", EvidenceDigest: digest('a'),
			ProjectionObservedAt: "2026-08-09T11:59:30Z", ProjectionSource: "autoscaler-simulation",
			ProjectionSourceRevision: "cluster-r43", ProjectionEvidenceDigest: digest('c'),
			Storage: StorageEvidence{
				ObservedNodeCount: 3,
				Nodes: []NodeStorageEvidence{
					{NodeID: "node-a", Status: DiskPressureFalse, FreeBytes: 10_000_000_000, FreeBytesObserved: true, FreeInodes: 1_000_000, FreeInodesObserved: true},
					{NodeID: "node-b", Status: DiskPressureFalse, FreeBytes: 10_000_000_000, FreeBytesObserved: true, FreeInodes: 1_000_000, FreeInodesObserved: true},
					{NodeID: "node-c", Status: DiskPressureFalse, FreeBytes: 10_000_000_000, FreeBytesObserved: true, FreeInodes: 1_000_000, FreeInodesObserved: true},
				},
				ObservedAt: "2026-08-09T11:59:30Z", Source: "kubelet/stats-summary", SourceRevision: "cluster-r43", EvidenceDigest: digest('d'),
			},
		},
		Demand: RolloutCapacityDemand{
			Strategy:            RolloutBlueGreen,
			Additional:          CapacityVector{PodSlots: 2, MilliCPU: 2000, MemoryBytes: 2_000_000_000, IPAddresses: 2, EphemeralStorageBytes: 2_000_000_000},
			DaemonSetPerNewNode: CapacityVector{PodSlots: 1, MilliCPU: 100, MemoryBytes: 100_000_000, IPAddresses: 1, EphemeralStorageBytes: 100_000_000},
			NewNodes:            1, MinimumReadyNodes: 3, ConcurrentEvictions: 1, DisruptionsAllowed: 1, AcceptanceWindowSecs: 300,
			Storage: StorageDemand{
				Images: []ImageStorageDemand{{Digest: digest('1'), CompressedBytes: 200_000_000, UnpackedBytes: 500_000_000, UnpackedInodes: 20_000, Copies: 1, Source: "registry/manifest", SourceRevision: "release-r7", ObservedAt: "2026-08-09T11:59:30Z", RequestDigest: digest('3'), EvidenceDigest: digest('2'), CollectorIdentity: "collector:registry", Fidelity: ObservationFidelityExact}},
				Placements: []NodeStorageDemand{{
					NodeID: "node-a", WorkloadBytes: 2_100_000_000, ImageDigests: []string{digest('1')},
					WritableLayerReserveBytes: 500_000_000, WritableLayerReserveInodes: 50_000,
					LogReserveBytes: 500_000_000, LogReserveInodes: 50_000,
					MinimumRemainingBytes: 5_000_000_000, MinimumRemainingInodes: 500_000,
				}},
			},
		},
		Dependencies: []SubstrateDependencyEvidence{
			dependency("cni", "CNI", DependencyHealthy, DefectUnknown),
			dependency("network-policy", "NETWORK_POLICY", DependencyHealthy, DefectUnknown),
			dependency("network-telemetry", "NETWORK_TELEMETRY", DependencyHealthy, DefectUnknown),
		},
		RequireNetworkPolicyCoverage: true, RequireNetworkTelemetryCoverage: true,
	}
}

func dependency(id, kind, status, origin string) SubstrateDependencyEvidence {
	return SubstrateDependencyEvidence{ID: id, Kind: kind, Required: true, Status: status, DefectOrigin: origin, ObservedAt: "2026-08-09T11:59:30Z", Source: "provider", SourceRevision: "r43", EvidenceDigest: digest('b')}
}

func digest(character byte) string { return "sha256:" + strings.Repeat(string(character), 64) }

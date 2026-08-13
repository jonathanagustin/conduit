package optimization

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	DependencyHealthy   = "HEALTHY"
	DependencyUnhealthy = "UNHEALTHY"
	DependencyUnknown   = "UNKNOWN"

	DefectLatent     = "LATENT"
	DefectIntroduced = "INTRODUCED"
	DefectUnknown    = "UNKNOWN"

	RolloutRolling         = "ROLLING"
	RolloutBlueGreen       = "BLUE_GREEN"
	RolloutReplacementPool = "REPLACEMENT_POOL"
)

// CapacityVector uses exact integral Kubernetes scheduling units. It deliberately
// keeps pod/IP slots and ephemeral storage separate and never treats zero or
// missing capacity as free.
type CapacityVector struct {
	PodSlots              int64 `json:"podSlots"`
	MilliCPU              int64 `json:"milliCpu"`
	MemoryBytes           int64 `json:"memoryBytes"`
	IPAddresses           int64 `json:"ipAddresses"`
	EphemeralStorageBytes int64 `json:"ephemeralStorageBytes"`
}

type CapacityEvidence struct {
	Available                CapacityVector  `json:"available"`
	ReadyNodes               int64           `json:"readyNodes"`
	NodesAfterChange         int64           `json:"nodesAfterChange"`
	ObservedAt               string          `json:"observedAt"`
	Source                   string          `json:"source"`
	SourceRevision           string          `json:"sourceRevision"`
	EvidenceDigest           string          `json:"evidenceDigest"`
	ProjectionObservedAt     string          `json:"projectionObservedAt"`
	ProjectionSource         string          `json:"projectionSource"`
	ProjectionSourceRevision string          `json:"projectionSourceRevision"`
	ProjectionEvidenceDigest string          `json:"projectionEvidenceDigest"`
	Storage                  StorageEvidence `json:"storage"`
}

const (
	DiskPressureFalse   = "FALSE"
	DiskPressureTrue    = "TRUE"
	DiskPressureUnknown = "UNKNOWN"
)

type NodeStorageEvidence struct {
	NodeID             string `json:"nodeId"`
	Status             string `json:"diskPressureStatus"`
	FreeBytes          int64  `json:"freeBytes"`
	FreeBytesObserved  bool   `json:"freeBytesObserved"`
	FreeInodes         int64  `json:"freeInodes"`
	FreeInodesObserved bool   `json:"freeInodesObserved"`
}

// StorageEvidence is runtime filesystem evidence, deliberately separate from
// scheduler allocatable/request headroom. It covers every observed node's
// DiskPressure condition plus current free bytes and inodes.
type StorageEvidence struct {
	ObservedNodeCount int64                 `json:"observedNodeCount"`
	Nodes             []NodeStorageEvidence `json:"nodes"`
	ObservedAt        string                `json:"observedAt"`
	Source            string                `json:"source"`
	SourceRevision    string                `json:"sourceRevision"`
	EvidenceDigest    string                `json:"evidenceDigest"`
}

// ImageStorageDemand records one exact OCI manifest's transfer, unpacked, and
// inode demand. Copies is the projected peak count of node-local copies.
type ImageStorageDemand struct {
	Digest            string `json:"digest"`
	CompressedBytes   int64  `json:"compressedBytes"`
	UnpackedBytes     int64  `json:"unpackedBytes"`
	UnpackedInodes    int64  `json:"unpackedInodes"`
	Copies            int64  `json:"copies"`
	Source            string `json:"source"`
	SourceRevision    string `json:"sourceRevision"`
	ObservedAt        string `json:"observedAt"`
	RequestDigest     string `json:"requestDigest"`
	EvidenceDigest    string `json:"evidenceDigest"`
	CollectorIdentity string `json:"collectorIdentity"`
	Fidelity          string `json:"fidelity"`
}

type NodeStorageDemand struct {
	NodeID                     string   `json:"nodeId"`
	WorkloadBytes              int64    `json:"workloadBytes"`
	ImageDigests               []string `json:"imageDigests"`
	WritableLayerReserveBytes  int64    `json:"writableLayerReserveBytes"`
	WritableLayerReserveInodes int64    `json:"writableLayerReserveInodes"`
	LogReserveBytes            int64    `json:"logReserveBytes"`
	LogReserveInodes           int64    `json:"logReserveInodes"`
	MinimumRemainingBytes      int64    `json:"minimumRemainingBytes"`
	MinimumRemainingInodes     int64    `json:"minimumRemainingInodes"`
}

// StorageDemand keeps scheduler requests, image transfer/unpack, writable
// layers, logs, and post-rollout headroom explicit so none silently substitutes
// for another.
type StorageDemand struct {
	Images     []ImageStorageDemand `json:"images"`
	Placements []NodeStorageDemand  `json:"placements"`
}

type RolloutCapacityDemand struct {
	Strategy string `json:"strategy"`
	// Additional is the complete incremental demand at peak overlap, including
	// blue/green or ordinary rollout surge, beyond the currently running fleet.
	Additional           CapacityVector `json:"additional"`
	DaemonSetPerNewNode  CapacityVector `json:"daemonSetPerNewNode"`
	NewNodes             int64          `json:"newNodes"`
	MinimumReadyNodes    int64          `json:"minimumReadyNodes"`
	ConcurrentEvictions  int64          `json:"concurrentEvictions"`
	DisruptionsAllowed   int64          `json:"disruptionsAllowed"`
	AcceptanceWindowSecs int64          `json:"acceptanceWindowSeconds"`
	Storage              StorageDemand  `json:"storage"`
}

type SubstrateDependencyEvidence struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Required       bool   `json:"required"`
	Status         string `json:"status"`
	DefectOrigin   string `json:"defectOrigin"`
	ObservedAt     string `json:"observedAt"`
	Source         string `json:"source"`
	SourceRevision string `json:"sourceRevision"`
	EvidenceDigest string `json:"evidenceDigest"`
}

type SubstratePreflightRequest struct {
	SnapshotAt                      string                        `json:"snapshotAt"`
	MaximumStalenessSeconds         int64                         `json:"maximumStalenessSeconds"`
	Capacity                        CapacityEvidence              `json:"capacity"`
	Demand                          RolloutCapacityDemand         `json:"demand"`
	Dependencies                    []SubstrateDependencyEvidence `json:"dependencies"`
	RequireNetworkPolicyCoverage    bool                          `json:"requireNetworkPolicyCoverage"`
	RequireNetworkTelemetryCoverage bool                          `json:"requireNetworkTelemetryCoverage"`
}

type CapacityCheck struct {
	Resource  string `json:"resource"`
	Available int64  `json:"available"`
	Required  int64  `json:"required"`
	Remaining int64  `json:"remaining"`
	Passed    bool   `json:"passed"`
}

type SubstratePreflightAssessment struct {
	Eligible        bool                          `json:"eligible"`
	CapacityChecks  []CapacityCheck               `json:"capacityChecks"`
	BlockingReasons []string                      `json:"blockingReasons"`
	Dependencies    []SubstrateDependencyEvidence `json:"dependencies"`
}

// AssessSubstratePreflight is advisory and deterministic. It evaluates pinned
// provider evidence but never mutates infrastructure, approves a change, or
// turns missing evidence into health.
func AssessSubstratePreflight(request SubstratePreflightRequest) (SubstratePreflightAssessment, error) {
	snapshotAt, err := time.Parse(time.RFC3339, request.SnapshotAt)
	if err != nil {
		return SubstratePreflightAssessment{}, fmt.Errorf("snapshotAt must be RFC3339: %w", err)
	}
	if request.MaximumStalenessSeconds < 0 {
		return SubstratePreflightAssessment{}, errors.New("maximumStalenessSeconds must be non-negative")
	}
	if err := validateCapacityEvidence(request.Capacity, snapshotAt, request.MaximumStalenessSeconds); err != nil {
		return SubstratePreflightAssessment{}, err
	}
	if err := validateStorageEvidence(request.Capacity.Storage, snapshotAt, request.MaximumStalenessSeconds); err != nil {
		return SubstratePreflightAssessment{}, err
	}
	if err := validateDemand(request.Demand, snapshotAt, request.MaximumStalenessSeconds); err != nil {
		return SubstratePreflightAssessment{}, err
	}

	dependencies := append([]SubstrateDependencyEvidence(nil), request.Dependencies...)
	sort.Slice(dependencies, func(i, j int) bool { return dependencies[i].ID < dependencies[j].ID })
	seen := map[string]struct{}{}
	reasons := []string{}
	networkPolicyHealthy := false
	networkTelemetryHealthy := false
	for _, dependency := range dependencies {
		if err := validateDependency(dependency, snapshotAt, request.MaximumStalenessSeconds); err != nil {
			return SubstratePreflightAssessment{}, err
		}
		if _, exists := seen[dependency.ID]; exists {
			return SubstratePreflightAssessment{}, fmt.Errorf("duplicate dependency %q", dependency.ID)
		}
		seen[dependency.ID] = struct{}{}
		if dependency.Kind == "NETWORK_POLICY" && dependency.Status == DependencyHealthy {
			networkPolicyHealthy = true
		}
		if dependency.Kind == "NETWORK_TELEMETRY" && dependency.Status == DependencyHealthy {
			networkTelemetryHealthy = true
		}
		if dependency.Required && dependency.Status != DependencyHealthy {
			reasons = append(reasons, fmt.Sprintf("required dependency %s is %s (%s defect)", dependency.ID, dependency.Status, dependency.DefectOrigin))
		}
	}
	if request.RequireNetworkPolicyCoverage && !networkPolicyHealthy {
		reasons = append(reasons, "required healthy network policy coverage evidence is missing")
	}
	if request.RequireNetworkTelemetryCoverage && !networkTelemetryHealthy {
		reasons = append(reasons, "required healthy network telemetry coverage evidence is missing")
	}

	required, err := peakDemand(request.Demand)
	if err != nil {
		return SubstratePreflightAssessment{}, err
	}
	checks := capacityChecks(request.Capacity.Available, required)
	storageChecks, err := storageCapacityChecks(request.Capacity.Storage, request.Demand)
	if err != nil {
		return SubstratePreflightAssessment{}, err
	}
	checks = append(checks, storageChecks...)
	for _, node := range request.Capacity.Storage.Nodes {
		switch node.Status {
		case DiskPressureTrue:
			reasons = append(reasons, fmt.Sprintf("node %s reports DiskPressure", node.NodeID))
		case DiskPressureUnknown:
			reasons = append(reasons, fmt.Sprintf("node %s DiskPressure condition is UNKNOWN", node.NodeID))
		}
	}
	for _, check := range checks {
		if !check.Passed {
			reasons = append(reasons, fmt.Sprintf("%s requires %d but only %d is available", check.Resource, check.Required, check.Available))
		}
	}
	if request.Capacity.NodesAfterChange < request.Demand.MinimumReadyNodes {
		reasons = append(reasons, fmt.Sprintf("ready nodes after change requires %d but only %d is predicted", request.Demand.MinimumReadyNodes, request.Capacity.NodesAfterChange))
	}
	if request.Demand.ConcurrentEvictions > request.Demand.DisruptionsAllowed {
		reasons = append(reasons, fmt.Sprintf("concurrent evictions %d exceed disruptions allowed %d", request.Demand.ConcurrentEvictions, request.Demand.DisruptionsAllowed))
	}
	sort.Strings(reasons)
	return SubstratePreflightAssessment{Eligible: len(reasons) == 0, CapacityChecks: checks, BlockingReasons: reasons, Dependencies: dependencies}, nil
}

func validateCapacityEvidence(e CapacityEvidence, snapshotAt time.Time, maximumStaleness int64) error {
	if err := validateVector("capacity.available", e.Available); err != nil {
		return err
	}
	if e.Available.EphemeralStorageBytes == 0 {
		return errors.New("capacity.available.ephemeralStorageBytes must be positive")
	}
	if e.ReadyNodes < 0 || e.NodesAfterChange < 0 {
		return errors.New("capacity node counts must be non-negative")
	}
	if strings.TrimSpace(e.Source) == "" || strings.TrimSpace(e.SourceRevision) == "" || !validDigest(e.EvidenceDigest) {
		return errors.New("capacity evidence requires source, sourceRevision, and sha256 evidenceDigest")
	}
	if strings.TrimSpace(e.ProjectionSource) == "" || strings.TrimSpace(e.ProjectionSourceRevision) == "" || !validDigest(e.ProjectionEvidenceDigest) {
		return errors.New("capacity evidence requires projectionSource, projectionSourceRevision, and sha256 projectionEvidenceDigest")
	}
	if err := validateObservationTime("capacity", e.ObservedAt, snapshotAt, maximumStaleness); err != nil {
		return err
	}
	return validateObservationTime("capacity projection", e.ProjectionObservedAt, snapshotAt, maximumStaleness)
}

func validateStorageEvidence(e StorageEvidence, snapshotAt time.Time, maximumStaleness int64) error {
	if e.ObservedNodeCount <= 0 || int64(len(e.Nodes)) != e.ObservedNodeCount {
		return errors.New("storage evidence requires one DiskPressure/free-space observation for every observed node")
	}
	seen := map[string]struct{}{}
	for _, node := range e.Nodes {
		if !validEvidenceCoordinate(node.NodeID) {
			return errors.New("storage DiskPressure condition requires a safe nodeId")
		}
		if _, exists := seen[node.NodeID]; exists {
			return fmt.Errorf("duplicate storage evidence for node %q", node.NodeID)
		}
		seen[node.NodeID] = struct{}{}
		if !node.FreeBytesObserved || !node.FreeInodesObserved {
			return fmt.Errorf("node %q storage evidence is missing free bytes or free inodes", node.NodeID)
		}
		if node.FreeBytes < 0 || node.FreeInodes < 0 {
			return fmt.Errorf("node %q storage availability must be non-negative", node.NodeID)
		}
		if node.Status != DiskPressureFalse && node.Status != DiskPressureTrue && node.Status != DiskPressureUnknown {
			return fmt.Errorf("node %q has invalid DiskPressure status %q", node.NodeID, node.Status)
		}
	}
	if strings.TrimSpace(e.Source) == "" || strings.TrimSpace(e.SourceRevision) == "" || !validDigest(e.EvidenceDigest) {
		return errors.New("storage evidence requires source, sourceRevision, and sha256 evidenceDigest")
	}
	return validateObservationTime("storage", e.ObservedAt, snapshotAt, maximumStaleness)
}

func validateDemand(d RolloutCapacityDemand, snapshotAt time.Time, maximumStaleness int64) error {
	if d.Strategy != RolloutRolling && d.Strategy != RolloutBlueGreen && d.Strategy != RolloutReplacementPool {
		return fmt.Errorf("rollout strategy %q is invalid", d.Strategy)
	}
	if err := validateVector("demand.additional", d.Additional); err != nil {
		return err
	}
	if err := validateVector("demand.daemonSetPerNewNode", d.DaemonSetPerNewNode); err != nil {
		return err
	}
	if d.Additional.EphemeralStorageBytes == 0 {
		return errors.New("demand.additional.ephemeralStorageBytes must be positive")
	}
	if d.NewNodes < 0 || d.MinimumReadyNodes < 0 || d.ConcurrentEvictions < 0 || d.DisruptionsAllowed < 0 || d.AcceptanceWindowSecs < 0 {
		return errors.New("rollout counts and acceptance window must be non-negative")
	}
	if (d.Strategy == RolloutBlueGreen || d.Strategy == RolloutReplacementPool) && d.AcceptanceWindowSecs == 0 {
		return fmt.Errorf("%s rollout requires a positive acceptance window", d.Strategy)
	}
	peak, err := peakDemand(d)
	if err != nil {
		return err
	}
	if err := validateStorageDemand(d.Storage, peak.EphemeralStorageBytes, snapshotAt, maximumStaleness); err != nil {
		return err
	}
	return nil
}

func validateStorageDemand(d StorageDemand, peakWorkloadBytes int64, snapshotAt time.Time, maximumStaleness int64) error {
	if len(d.Images) == 0 {
		return errors.New("storage demand requires image artifact size evidence")
	}
	if len(d.Placements) == 0 {
		return errors.New("storage demand requires projected per-node placements")
	}
	seen := map[string]struct{}{}
	for _, image := range d.Images {
		if !validDigest(image.Digest) || image.CompressedBytes <= 0 || image.UnpackedBytes <= 0 || image.UnpackedInodes <= 0 || image.Copies <= 0 {
			return errors.New("image storage evidence requires digest and positive compressedBytes, unpackedBytes, unpackedInodes, and copies")
		}
		if _, exists := seen[image.Digest]; exists {
			return fmt.Errorf("duplicate image storage evidence for %s", image.Digest)
		}
		seen[image.Digest] = struct{}{}
		if strings.TrimSpace(image.Source) == "" || strings.TrimSpace(image.SourceRevision) == "" || !validDigest(image.RequestDigest) || !validDigest(image.EvidenceDigest) || !validObservationText(image.CollectorIdentity) || image.Fidelity != ObservationFidelityExact {
			return fmt.Errorf("image %s requires an EXACT read-only receipt with source, sourceRevision, collectorIdentity, and sha256 request/evidence digests", image.Digest)
		}
		if err := validateObservationTime("image "+image.Digest, image.ObservedAt, snapshotAt, maximumStaleness); err != nil {
			return err
		}
	}
	placementNodes := map[string]struct{}{}
	imageCopies := map[string]int64{}
	placedWorkload := int64(0)
	for _, placement := range d.Placements {
		if !validEvidenceCoordinate(placement.NodeID) || placement.WorkloadBytes < 0 {
			return errors.New("storage placement requires a safe nodeId and non-negative workloadBytes")
		}
		if _, exists := placementNodes[placement.NodeID]; exists {
			return fmt.Errorf("duplicate storage placement for node %q", placement.NodeID)
		}
		placementNodes[placement.NodeID] = struct{}{}
		if placement.WritableLayerReserveBytes <= 0 || placement.WritableLayerReserveInodes <= 0 || placement.LogReserveBytes <= 0 || placement.LogReserveInodes <= 0 {
			return fmt.Errorf("storage placement %s requires positive writable-layer and log byte/inode reserves", placement.NodeID)
		}
		if placement.MinimumRemainingBytes <= 0 || placement.MinimumRemainingInodes <= 0 {
			return fmt.Errorf("storage placement %s requires positive minimum post-rollout byte and inode headroom", placement.NodeID)
		}
		var ok bool
		placedWorkload, ok = checkedAdd(placedWorkload, placement.WorkloadBytes)
		if !ok {
			return errors.New("projected per-node workload storage overflows int64")
		}
		seenOnNode := map[string]struct{}{}
		for _, digest := range placement.ImageDigests {
			if _, exists := seen[digest]; !exists {
				return fmt.Errorf("storage placement %s references unknown image %s", placement.NodeID, digest)
			}
			if _, exists := seenOnNode[digest]; exists {
				return fmt.Errorf("storage placement %s repeats image %s", placement.NodeID, digest)
			}
			seenOnNode[digest] = struct{}{}
			imageCopies[digest]++
		}
	}
	if placedWorkload != peakWorkloadBytes {
		return fmt.Errorf("projected per-node workload bytes %d do not equal peak rollout and DaemonSet demand %d", placedWorkload, peakWorkloadBytes)
	}
	for _, image := range d.Images {
		if imageCopies[image.Digest] != image.Copies {
			return fmt.Errorf("image %s copies %d do not match %d projected node placements", image.Digest, image.Copies, imageCopies[image.Digest])
		}
	}
	return nil
}

func validateDependency(d SubstrateDependencyEvidence, snapshotAt time.Time, maximumStaleness int64) error {
	if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.Kind) == "" {
		return errors.New("dependency requires id and kind")
	}
	if d.Status != DependencyHealthy && d.Status != DependencyUnhealthy && d.Status != DependencyUnknown {
		return fmt.Errorf("dependency %q has invalid status %q", d.ID, d.Status)
	}
	if !containsString([]string{"CNI", "CNI_PERMISSION", "COMPUTE_OFFERING", "CSI", "GIT_CREDENTIAL", "IDENTITY", "NAMESPACE_RBAC", "NETWORK_CONNECTIVITY", "NETWORK_POLICY", "NETWORK_TELEMETRY", "REGISTRY", "SECRET_SYNC", "SPIFFE"}, d.Kind) {
		return fmt.Errorf("dependency %q has invalid kind %q", d.ID, d.Kind)
	}
	if d.DefectOrigin != DefectLatent && d.DefectOrigin != DefectIntroduced && d.DefectOrigin != DefectUnknown {
		return fmt.Errorf("dependency %q has invalid defectOrigin %q", d.ID, d.DefectOrigin)
	}
	if d.Status != DependencyUnhealthy && d.DefectOrigin != DefectUnknown {
		return fmt.Errorf("dependency %q can classify defectOrigin only when unhealthy", d.ID)
	}
	if strings.TrimSpace(d.Source) == "" || strings.TrimSpace(d.SourceRevision) == "" || !validDigest(d.EvidenceDigest) {
		return fmt.Errorf("dependency %q requires source, sourceRevision, and sha256 evidenceDigest", d.ID)
	}
	return validateObservationTime("dependency "+d.ID, d.ObservedAt, snapshotAt, maximumStaleness)
}

func containsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func validateObservationTime(label, raw string, snapshotAt time.Time, maximumStaleness int64) error {
	observedAt, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return fmt.Errorf("%s observedAt must be RFC3339: %w", label, err)
	}
	if observedAt.After(snapshotAt) {
		return fmt.Errorf("%s evidence is newer than snapshot", label)
	}
	if snapshotAt.Sub(observedAt) > time.Duration(maximumStaleness)*time.Second {
		return fmt.Errorf("%s evidence exceeds maximum staleness", label)
	}
	return nil
}

func validateVector(label string, v CapacityVector) error {
	if v.PodSlots < 0 || v.MilliCPU < 0 || v.MemoryBytes < 0 || v.IPAddresses < 0 || v.EphemeralStorageBytes < 0 {
		return fmt.Errorf("%s values must be non-negative", label)
	}
	return nil
}

func peakDemand(d RolloutCapacityDemand) (CapacityVector, error) {
	daemon, err := multiplyVector(d.DaemonSetPerNewNode, d.NewNodes)
	if err != nil {
		return CapacityVector{}, err
	}
	return addVectors(d.Additional, daemon)
}

func multiplyVector(v CapacityVector, factor int64) (CapacityVector, error) {
	return checkedVector(
		func(value int64) (int64, bool) { return checkedMultiply(value, factor) }, v,
	)
}

func addVectors(a, b CapacityVector) (CapacityVector, error) {
	values := [5]int64{}
	left := [5]int64{a.PodSlots, a.MilliCPU, a.MemoryBytes, a.IPAddresses, a.EphemeralStorageBytes}
	right := [5]int64{b.PodSlots, b.MilliCPU, b.MemoryBytes, b.IPAddresses, b.EphemeralStorageBytes}
	for i := range values {
		if left[i] > int64(^uint64(0)>>1)-right[i] {
			return CapacityVector{}, errors.New("peak capacity demand overflows int64")
		}
		values[i] = left[i] + right[i]
	}
	return CapacityVector{PodSlots: values[0], MilliCPU: values[1], MemoryBytes: values[2], IPAddresses: values[3], EphemeralStorageBytes: values[4]}, nil
}

func checkedVector(operation func(int64) (int64, bool), v CapacityVector) (CapacityVector, error) {
	values := [5]int64{v.PodSlots, v.MilliCPU, v.MemoryBytes, v.IPAddresses, v.EphemeralStorageBytes}
	for i := range values {
		var ok bool
		values[i], ok = operation(values[i])
		if !ok {
			return CapacityVector{}, errors.New("peak capacity demand overflows int64")
		}
	}
	return CapacityVector{PodSlots: values[0], MilliCPU: values[1], MemoryBytes: values[2], IPAddresses: values[3], EphemeralStorageBytes: values[4]}, nil
}

func checkedMultiply(value, factor int64) (int64, bool) {
	if value == 0 || factor == 0 {
		return 0, true
	}
	maximum := int64(^uint64(0) >> 1)
	if value > maximum/factor {
		return 0, false
	}
	return value * factor, true
}

func capacityChecks(available, required CapacityVector) []CapacityCheck {
	checks := []CapacityCheck{
		capacityCheck("cpu-millicores", available.MilliCPU, required.MilliCPU),
		capacityCheck("ephemeral-storage-bytes", available.EphemeralStorageBytes, required.EphemeralStorageBytes),
		capacityCheck("ip-addresses", available.IPAddresses, required.IPAddresses),
		capacityCheck("memory-bytes", available.MemoryBytes, required.MemoryBytes),
		capacityCheck("pod-slots", available.PodSlots, required.PodSlots),
	}
	return checks
}

func storageCapacityChecks(available StorageEvidence, demand RolloutCapacityDemand) ([]CapacityCheck, error) {
	nodes := make(map[string]NodeStorageEvidence, len(available.Nodes))
	for _, node := range available.Nodes {
		nodes[node.NodeID] = node
	}
	images := make(map[string]ImageStorageDemand, len(demand.Storage.Images))
	for _, image := range demand.Storage.Images {
		images[image.Digest] = image
	}
	placements := append([]NodeStorageDemand(nil), demand.Storage.Placements...)
	sort.Slice(placements, func(i, j int) bool { return placements[i].NodeID < placements[j].NodeID })
	checks := make([]CapacityCheck, 0, len(placements)*2)
	for _, placement := range placements {
		node, exists := nodes[placement.NodeID]
		if !exists {
			return nil, fmt.Errorf("storage placement %s has no node storage evidence", placement.NodeID)
		}
		bytesRequired := placement.WorkloadBytes
		inodesRequired := int64(0)
		for _, digest := range placement.ImageDigests {
			image := images[digest]
			imageBytes, ok := checkedAdd(image.CompressedBytes, image.UnpackedBytes)
			if !ok {
				return nil, errors.New("peak image storage demand overflows int64")
			}
			bytesRequired, ok = checkedAdd(bytesRequired, imageBytes)
			if !ok {
				return nil, errors.New("peak image storage demand overflows int64")
			}
			inodesRequired, ok = checkedAdd(inodesRequired, image.UnpackedInodes)
			if !ok {
				return nil, errors.New("peak image inode demand overflows int64")
			}
		}
		var ok bool
		for _, reserve := range []int64{placement.WritableLayerReserveBytes, placement.LogReserveBytes, placement.MinimumRemainingBytes} {
			bytesRequired, ok = checkedAdd(bytesRequired, reserve)
			if !ok {
				return nil, errors.New("peak storage demand overflows int64")
			}
		}
		for _, reserve := range []int64{placement.WritableLayerReserveInodes, placement.LogReserveInodes, placement.MinimumRemainingInodes} {
			inodesRequired, ok = checkedAdd(inodesRequired, reserve)
			if !ok {
				return nil, errors.New("peak inode demand overflows int64")
			}
		}
		checks = append(checks,
			capacityCheck("node-storage-free-bytes/"+placement.NodeID, node.FreeBytes, bytesRequired),
			capacityCheck("node-storage-free-inodes/"+placement.NodeID, node.FreeInodes, inodesRequired),
		)
	}
	return checks, nil
}

func checkedAdd(left, right int64) (int64, bool) {
	maximum := int64(^uint64(0) >> 1)
	if left < 0 || right < 0 || left > maximum-right {
		return 0, false
	}
	return left + right, true
}

func capacityCheck(resource string, available, required int64) CapacityCheck {
	remaining := available - required
	return CapacityCheck{Resource: resource, Available: available, Required: required, Remaining: remaining, Passed: remaining >= 0}
}

func validDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, character := range value[len("sha256:"):] {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return false
		}
	}
	return true
}

// Package solverprotocol contains the transport-free external optimizer wire
// contract and deterministic model compiler. It deliberately imports no
// parent optimization or networking package so a static stdin/stdout shim does
// not inherit control-plane transport closure.
package solverprotocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"time"
)

const (
	EvidenceAPIVersion              = "optimization.platform.example/v1alpha1"
	SolverAPIVersion                = "optimization.platform.example/v1alpha2"
	ExternalSolverProcessAPIVersion = "optimization.platform.example/external-solver-process/v1alpha3"
	AllocationBackendCapability     = "allocation.makespan.v1"
	AllocationModelRevision         = "allocation.makespan.v1"
	maximumSolverMemoryBytes        = int64(1 << 50)
	maxItems                        = 2_000
	maxResources                    = 256
	maxEligibilityEdges             = 100_000
)

type SolverTermination string

const (
	SolverOptimal    SolverTermination = "OPTIMAL"
	SolverFeasible   SolverTermination = "FEASIBLE"
	SolverInfeasible SolverTermination = "INFEASIBLE"
	SolverUnbounded  SolverTermination = "UNBOUNDED"
	SolverTimeLimit  SolverTermination = "TIME_LIMIT"
	SolverError      SolverTermination = "ERROR"
	SolverUnknown    SolverTermination = "UNKNOWN"
)

type Snapshot struct {
	ID         string `json:"id"`
	Source     string `json:"source"`
	Revision   string `json:"revision"`
	CapturedAt string `json:"capturedAt"`
}

type Objective struct {
	Primary   string   `json:"primary"`
	Secondary []string `json:"secondary,omitempty"`
}

type ConstraintEvidence struct {
	ID         string `json:"id"`
	Source     string `json:"source"`
	Revision   string `json:"revision"`
	ObservedAt string `json:"observedAt"`
}

type WorkItem struct {
	ID                  string           `json:"id"`
	Demand              float64          `json:"demand"`
	Eligible            []string         `json:"eligible,omitempty"`
	EstimatedCostMicros map[string]int64 `json:"estimatedCostMicros,omitempty"`
}

type Resource struct {
	ID       string  `json:"id"`
	Capacity float64 `json:"capacity"`
}

type CostModel struct {
	Currency string `json:"currency"`
	Source   string `json:"source"`
	AsOf     string `json:"asOf"`
}

type Problem struct {
	Items        []WorkItem `json:"items"`
	Resources    []Resource `json:"resources"`
	CostModel    *CostModel `json:"costModel,omitempty"`
	MakespanUnit string     `json:"makespanUnit,omitempty"`
}

type RecommendationRequest struct {
	APIVersion  string               `json:"apiVersion"`
	Kind        string               `json:"kind"`
	Snapshot    Snapshot             `json:"snapshot"`
	Objective   Objective            `json:"objective"`
	Constraints []ConstraintEvidence `json:"constraints"`
	Problem     Problem              `json:"problem"`
}

type SolverSelection struct {
	PreferredBackend    string   `json:"preferredBackend"`
	AuthorizedFallbacks []string `json:"authorizedFallbacks,omitempty"`
}

type SolverDeterminism struct {
	Seed    uint64 `json:"seed"`
	Workers int    `json:"workers"`
}

type SolverLimits struct {
	TimeLimitMilliseconds int64 `json:"timeLimitMilliseconds"`
	MemoryLimitBytes      int64 `json:"memoryLimitBytes"`
}

type SolverModel struct {
	Name                  string `json:"name"`
	Revision              string `json:"revision"`
	Digest                string `json:"digest"`
	HardConstraintsDigest string `json:"hardConstraintsDigest"`
	ObjectiveDigest       string `json:"objectiveDigest"`
}

type SolverRequest struct {
	APIVersion     string                `json:"apiVersion"`
	Kind           string                `json:"kind"`
	ID             string                `json:"id"`
	Recommendation RecommendationRequest `json:"recommendation"`
	Model          SolverModel           `json:"model"`
	Capability     string                `json:"capability"`
	Selection      SolverSelection       `json:"selection"`
	Determinism    SolverDeterminism     `json:"determinism"`
	Limits         SolverLimits          `json:"limits"`
}

type SolverObjectiveProof struct {
	Objective string `json:"objective"`
	Unit      string `json:"unit"`
	Incumbent string `json:"incumbent"`
	BestBound string `json:"bestBound"`
	Gap       string `json:"gap"`
}

type SolverBackendReportedEvidence struct {
	Status     string                 `json:"status"`
	Objectives []SolverObjectiveProof `json:"objectives,omitempty"`
}

type ExternalSolverAssignmentChoice struct {
	Item     string `json:"item"`
	Resource string `json:"resource"`
}

type ExternalSolverProcessResponse struct {
	APIVersion              string                            `json:"apiVersion"`
	Kind                    string                            `json:"kind"`
	RequestID               string                            `json:"requestId"`
	ModelDigest             string                            `json:"modelDigest"`
	Termination             SolverTermination                 `json:"termination"`
	BackendReportedEvidence *SolverBackendReportedEvidence    `json:"backendReportedEvidence,omitempty"`
	Assignments             *[]ExternalSolverAssignmentChoice `json:"assignments,omitempty"`
	Explanation             string                            `json:"explanation,omitempty"`
}

// NewSolverRequest is the transport-free constructor used by separately
// built solver shims and their conformance fixtures. It owns the same
// canonicalization and derived digest rules that VerifySolverRequest checks;
// callers cannot supply or override any derived identity.
func NewSolverRequest(
	recommendation RecommendationRequest,
	selection SolverSelection,
	determinism SolverDeterminism,
	limits SolverLimits,
) (SolverRequest, error) {
	recommendation = canonicalRecommendation(recommendation)
	if err := validateRecommendation(recommendation); err != nil {
		return SolverRequest{}, err
	}
	if _, _, _, err := validateProblem(recommendation.Problem); err != nil {
		return SolverRequest{}, err
	}
	modelDigest, err := digestJSON(recommendation.Problem)
	if err != nil {
		return SolverRequest{}, err
	}
	hardConstraints := struct {
		Evidence  []ConstraintEvidence `json:"evidence"`
		Items     []WorkItem           `json:"items"`
		Resources []Resource           `json:"resources"`
	}{recommendation.Constraints, recommendation.Problem.Items, recommendation.Problem.Resources}
	constraintsDigest, err := digestJSON(hardConstraints)
	if err != nil {
		return SolverRequest{}, err
	}
	objectiveDigest, err := digestJSON(recommendation.Objective)
	if err != nil {
		return SolverRequest{}, err
	}
	request := SolverRequest{
		APIVersion:     SolverAPIVersion,
		Kind:           "SolverRequest",
		Recommendation: recommendation,
		Model: SolverModel{
			Name:                  "allocation.makespan",
			Revision:              AllocationModelRevision,
			Digest:                modelDigest,
			HardConstraintsDigest: constraintsDigest,
			ObjectiveDigest:       objectiveDigest,
		},
		Capability:  AllocationBackendCapability,
		Selection:   selection,
		Determinism: determinism,
		Limits:      limits,
	}
	digest, err := digestJSON(request)
	if err != nil {
		return SolverRequest{}, err
	}
	request.ID = "solreq_" + strings.TrimPrefix(digest, "sha256:")
	if err := VerifySolverRequest(request); err != nil {
		return SolverRequest{}, err
	}
	return request, nil
}

// VerifySolverRequest independently validates canonical ordering, every
// derived digest, and the content-addressed request identity.
func VerifySolverRequest(request SolverRequest) error {
	if request.APIVersion != SolverAPIVersion || request.Kind != "SolverRequest" {
		return errors.New("unsupported solver request type")
	}
	if request.Model.Name != "allocation.makespan" || request.Model.Revision != AllocationModelRevision || request.Capability != AllocationBackendCapability {
		return errors.New("unsupported solver model or capability")
	}
	if !validDigest(request.Model.Digest) || !validDigest(request.Model.HardConstraintsDigest) || !validDigest(request.Model.ObjectiveDigest) {
		return errors.New("solver model requires exact SHA-256 evidence digests")
	}
	if !validSolverToken(request.Selection.PreferredBackend) || request.Determinism.Workers < 1 || request.Determinism.Workers > 256 {
		return errors.New("preferred backend and bounded positive worker count are required")
	}
	if request.Limits.TimeLimitMilliseconds < 1 || request.Limits.TimeLimitMilliseconds > int64((24*time.Hour)/time.Millisecond) || request.Limits.MemoryLimitBytes < 1 || request.Limits.MemoryLimitBytes > maximumSolverMemoryBytes {
		return errors.New("positive bounded solver time and memory limits are required")
	}
	seenBackends := map[string]bool{request.Selection.PreferredBackend: true}
	for _, fallback := range request.Selection.AuthorizedFallbacks {
		if !validSolverToken(fallback) || seenBackends[fallback] {
			return errors.New("solver fallback list contains an empty or duplicate backend")
		}
		seenBackends[fallback] = true
	}
	canonical := canonicalRecommendation(request.Recommendation)
	if !reflect.DeepEqual(canonical, request.Recommendation) {
		return errors.New("solver request recommendation is not canonical")
	}
	if err := validateRecommendation(canonical); err != nil {
		return err
	}
	if _, _, _, err := validateProblem(canonical.Problem); err != nil {
		return err
	}
	modelDigest, err := digestJSON(canonical.Problem)
	if err != nil || modelDigest != request.Model.Digest {
		return errors.New("solver request model digest does not match content")
	}
	hardConstraints := struct {
		Evidence  []ConstraintEvidence `json:"evidence"`
		Items     []WorkItem           `json:"items"`
		Resources []Resource           `json:"resources"`
	}{canonical.Constraints, canonical.Problem.Items, canonical.Problem.Resources}
	constraintsDigest, err := digestJSON(hardConstraints)
	if err != nil || constraintsDigest != request.Model.HardConstraintsDigest {
		return errors.New("solver request constraints digest does not match content")
	}
	objectiveDigest, err := digestJSON(canonical.Objective)
	if err != nil || objectiveDigest != request.Model.ObjectiveDigest {
		return errors.New("solver request objective digest does not match content")
	}
	candidate := request
	candidate.ID = ""
	digest, err := digestJSON(candidate)
	if err != nil || request.ID != "solreq_"+strings.TrimPrefix(digest, "sha256:") {
		return errors.New("solver request identity does not match content")
	}
	return nil
}

func validateRecommendation(request RecommendationRequest) error {
	if request.APIVersion != EvidenceAPIVersion || request.Kind != "RecommendationRequest" {
		return errors.New("unsupported recommendation request type")
	}
	if !validContentID(request.Snapshot.ID, "snap_") || request.Snapshot.Source == "" || request.Snapshot.Revision == "" {
		return errors.New("snapshot id, source, and revision are required")
	}
	captured, err := time.Parse(time.RFC3339, request.Snapshot.CapturedAt)
	if err != nil {
		return fmt.Errorf("snapshot capturedAt must be RFC3339: %w", err)
	}
	if request.Objective.Primary != "minimize_makespan" {
		return fmt.Errorf("unsupported primary objective %q", request.Objective.Primary)
	}
	seenSecondary := make(map[string]bool, len(request.Objective.Secondary))
	for _, objective := range request.Objective.Secondary {
		if objective != "minimize_estimated_cost" {
			return fmt.Errorf("unsupported secondary objective %q", objective)
		}
		if seenSecondary[objective] {
			return fmt.Errorf("duplicate secondary objective %q", objective)
		}
		seenSecondary[objective] = true
	}
	for index, constraint := range request.Constraints {
		if constraint.ID == "" || constraint.Source == "" || constraint.Revision == "" {
			return errors.New("constraint id, source, and revision are required")
		}
		if index > 0 && request.Constraints[index-1].ID == constraint.ID {
			return fmt.Errorf("duplicate constraint %q", constraint.ID)
		}
		observed, parseErr := time.Parse(time.RFC3339, constraint.ObservedAt)
		if parseErr != nil || observed.After(captured) {
			return fmt.Errorf("constraint %q has invalid observation time", constraint.ID)
		}
	}
	return nil
}

func canonicalRecommendation(request RecommendationRequest) RecommendationRequest {
	request.Objective.Secondary = append([]string(nil), request.Objective.Secondary...)
	sort.Strings(request.Objective.Secondary)
	request.Constraints = append([]ConstraintEvidence(nil), request.Constraints...)
	sort.Slice(request.Constraints, func(i, j int) bool { return request.Constraints[i].ID < request.Constraints[j].ID })
	request.Problem.Items = append([]WorkItem(nil), request.Problem.Items...)
	sort.Slice(request.Problem.Items, func(i, j int) bool { return request.Problem.Items[i].ID < request.Problem.Items[j].ID })
	for index := range request.Problem.Items {
		request.Problem.Items[index].Eligible = append([]string(nil), request.Problem.Items[index].Eligible...)
		sort.Strings(request.Problem.Items[index].Eligible)
	}
	request.Problem.Resources = append([]Resource(nil), request.Problem.Resources...)
	sort.Slice(request.Problem.Resources, func(i, j int) bool { return request.Problem.Resources[i].ID < request.Problem.Resources[j].ID })
	return request
}

func validateProblem(problem Problem) ([]WorkItem, []Resource, map[string][]int, error) {
	if problem.MakespanUnit != "" && problem.MakespanUnit != "seconds" {
		return nil, nil, nil, errors.New("makespanUnit must be seconds when specified")
	}
	if len(problem.Items) > maxItems || len(problem.Resources) > maxResources {
		return nil, nil, nil, errors.New("problem exceeds item or resource limit")
	}
	items := append([]WorkItem(nil), problem.Items...)
	resources := append([]Resource(nil), problem.Resources...)
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	sort.Slice(resources, func(i, j int) bool { return resources[i].ID < resources[j].ID })
	resourceIndex := make(map[string]int, len(resources))
	for index, resource := range resources {
		if resource.ID == "" || resource.Capacity <= 0 || math.IsNaN(resource.Capacity) || math.IsInf(resource.Capacity, 0) {
			return nil, nil, nil, fmt.Errorf("resource %q must have a finite positive capacity", resource.ID)
		}
		if _, exists := resourceIndex[resource.ID]; exists {
			return nil, nil, nil, fmt.Errorf("duplicate resource %q", resource.ID)
		}
		resourceIndex[resource.ID] = index
	}
	seenItems := make(map[string]bool, len(items))
	eligible := make(map[string][]int, len(items))
	edgeCount, maximumEstimatedTotal := 0, int64(0)
	hasCosts := false
	for _, item := range items {
		if item.ID == "" || item.Demand <= 0 || math.IsNaN(item.Demand) || math.IsInf(item.Demand, 0) || seenItems[item.ID] {
			return nil, nil, nil, fmt.Errorf("item %q must be unique with finite positive demand", item.ID)
		}
		seenItems[item.ID] = true
		maximumItemCost := int64(0)
		for resource, cost := range item.EstimatedCostMicros {
			if _, exists := resourceIndex[resource]; !exists || cost < 0 {
				return nil, nil, nil, fmt.Errorf("item %q has an invalid resource price", item.ID)
			}
			hasCosts = true
			maximumItemCost = max(maximumItemCost, cost)
		}
		if maximumItemCost > math.MaxInt64-maximumEstimatedTotal {
			return nil, nil, nil, errors.New("estimated cost total overflows micro-units")
		}
		maximumEstimatedTotal += maximumItemCost
		allowed := item.Eligible
		if len(allowed) == 0 {
			allowed = make([]string, len(resources))
			for index := range resources {
				allowed[index] = resources[index].ID
			}
		}
		seenEligible := make(map[int]bool, len(allowed))
		for _, resource := range allowed {
			index, exists := resourceIndex[resource]
			if !exists {
				return nil, nil, nil, fmt.Errorf("item %q names unknown resource %q", item.ID, resource)
			}
			seenEligible[index] = true
		}
		for index := range seenEligible {
			eligible[item.ID] = append(eligible[item.ID], index)
		}
		sort.Ints(eligible[item.ID])
		edgeCount += len(eligible[item.ID])
		if len(eligible[item.ID]) == 0 || edgeCount > maxEligibilityEdges {
			return nil, nil, nil, fmt.Errorf("item %q has invalid eligibility", item.ID)
		}
	}
	if len(items) > 0 && len(resources) == 0 {
		return nil, nil, nil, errors.New("work requires at least one resource")
	}
	if hasCosts {
		if problem.CostModel == nil || !validCurrency(problem.CostModel.Currency) || problem.CostModel.Source == "" {
			return nil, nil, nil, errors.New("estimated costs require a valid cost model")
		}
		if _, err := time.Parse(time.RFC3339, problem.CostModel.AsOf); err != nil {
			return nil, nil, nil, errors.New("cost model asOf must be RFC3339")
		}
	}
	return items, resources, eligible, nil
}

func digestJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func validDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, character := range strings.TrimPrefix(value, "sha256:") {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return false
		}
	}
	return true
}

func validContentID(value, prefix string) bool {
	if len(value) != len(prefix)+64 || !strings.HasPrefix(value, prefix) {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, prefix))
	return err == nil
}

func validCurrency(value string) bool {
	return len(value) == 3 && value[0] >= 'A' && value[0] <= 'Z' && value[1] >= 'A' && value[1] <= 'Z' && value[2] >= 'A' && value[2] <= 'Z'
}

func validSolverToken(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || strings.ContainsRune("._-+/:", character) {
			continue
		}
		return false
	}
	return true
}

package solverprotocol

import (
	"errors"
	"fmt"
	"math"
)

const (
	HiGHSCompiledModelAPIVersion       = "optimization.platform.example/highs-model/v1alpha1"
	MinimumHiGHSModelCoefficient       = 1e-12
	MaximumHiGHSExactInteger     int64 = 1<<53 - 1
)

type HiGHSColumnKind string

const (
	HiGHSBinaryColumn     HiGHSColumnKind = "binary"
	HiGHSContinuousColumn HiGHSColumnKind = "continuous"
)

type HiGHSCompiledModel struct {
	APIVersion                     string                           `json:"apiVersion"`
	Kind                           string                           `json:"kind"`
	RequestID                      string                           `json:"requestId"`
	ModelDigest                    string                           `json:"modelDigest"`
	Seed                           uint64                           `json:"seed"`
	Workers                        int                              `json:"workers"`
	Columns                        []HiGHSColumn                    `json:"columns"`
	Rows                           []HiGHSRow                       `json:"rows"`
	Objectives                     []HiGHSObjective                 `json:"objectives"`
	ConstructiveAssignments        []ExternalSolverAssignmentChoice `json:"constructiveAssignments"`
	ConstructiveMakespanUpperBound float64                          `json:"constructiveMakespanUpperBound"`
	CostNormalization              *HiGHSCostNormalization          `json:"costNormalization,omitempty"`
}

type HiGHSColumn struct {
	Name       string          `json:"name"`
	Kind       HiGHSColumnKind `json:"kind"`
	Item       string          `json:"item,omitempty"`
	Resource   string          `json:"resource,omitempty"`
	LowerBound float64         `json:"lowerBound"`
	UpperBound float64         `json:"upperBound"`
}

type HiGHSRow struct {
	Name          string             `json:"name"`
	Sense         string             `json:"sense"`
	RightHandSide float64            `json:"rightHandSide"`
	Coefficients  []HiGHSCoefficient `json:"coefficients"`
}

type HiGHSCoefficient struct {
	Column int     `json:"column"`
	Value  float64 `json:"value"`
}

type HiGHSObjective struct {
	Name         string             `json:"name"`
	Unit         string             `json:"unit"`
	Coefficients []HiGHSCoefficient `json:"coefficients"`
}

type HiGHSCostNormalization struct {
	PerItemBaseMicros       []HiGHSCostBase `json:"perItemBaseMicros"`
	OffsetMicros            int64           `json:"offsetMicros"`
	GlobalPositiveGCDMicros int64           `json:"globalPositiveGcdMicros"`
	WorstCaseNormalized     int64           `json:"worstCaseNormalized"`
}

type HiGHSCostBase struct {
	Item       string `json:"item"`
	BaseMicros int64  `json:"baseMicros"`
}

func CompileHiGHSModel(request SolverRequest) (HiGHSCompiledModel, error) {
	if err := VerifySolverRequest(request); err != nil {
		return HiGHSCompiledModel{}, err
	}
	if request.Determinism.Seed != 0 || request.Determinism.Workers != 1 {
		return HiGHSCompiledModel{}, errors.New("HiGHS requires deterministic seed 0 and one worker")
	}
	secondary := request.Recommendation.Objective.Secondary
	if len(secondary) > 1 || (len(secondary) == 1 && secondary[0] != "minimize_estimated_cost") {
		return HiGHSCompiledModel{}, errors.New("HiGHS supports only the singleton minimize_estimated_cost secondary objective")
	}
	items, resources, eligible, err := validateProblem(request.Recommendation.Problem)
	if err != nil {
		return HiGHSCompiledModel{}, err
	}
	compiled := HiGHSCompiledModel{
		APIVersion: HiGHSCompiledModelAPIVersion, Kind: "HiGHSCompiledModel",
		RequestID: request.ID, ModelDigest: request.Model.Digest,
		Seed: request.Determinism.Seed, Workers: request.Determinism.Workers,
	}
	edgeColumns := make(map[string]map[int]int, len(items))
	for _, item := range items {
		edgeColumns[item.ID] = make(map[int]int, len(eligible[item.ID]))
		for _, resourceIndex := range eligible[item.ID] {
			column := len(compiled.Columns)
			edgeColumns[item.ID][resourceIndex] = column
			compiled.Columns = append(compiled.Columns, HiGHSColumn{
				Name: fmt.Sprintf("x[%s,%s]", item.ID, resources[resourceIndex].ID), Kind: HiGHSBinaryColumn,
				Item: item.ID, Resource: resources[resourceIndex].ID, UpperBound: 1,
			})
		}
	}
	tColumn := len(compiled.Columns)
	compiled.Columns = append(compiled.Columns, HiGHSColumn{
		Name: "T", Kind: HiGHSContinuousColumn,
		UpperBound: constructiveMakespanUpper(items, resources, eligible, &compiled),
	})
	compiled.ConstructiveMakespanUpperBound = compiled.Columns[tColumn].UpperBound
	if math.IsNaN(compiled.ConstructiveMakespanUpperBound) || math.IsInf(compiled.ConstructiveMakespanUpperBound, 0) {
		return HiGHSCompiledModel{}, errors.New("HiGHS constructive makespan upper bound is not finite")
	}
	for _, item := range items {
		row := HiGHSRow{Name: "assign[" + item.ID + "]", Sense: "equal", RightHandSide: 1}
		for _, resourceIndex := range eligible[item.ID] {
			row.Coefficients = append(row.Coefficients, HiGHSCoefficient{Column: edgeColumns[item.ID][resourceIndex], Value: 1})
		}
		compiled.Rows = append(compiled.Rows, row)
	}
	for resourceIndex, resource := range resources {
		row := HiGHSRow{Name: "capacity[" + resource.ID + "]", Sense: "less_or_equal"}
		scale := resource.Capacity
		for _, item := range items {
			if _, exists := edgeColumns[item.ID][resourceIndex]; exists {
				scale = math.Max(scale, item.Demand)
			}
		}
		for _, item := range items {
			column, exists := edgeColumns[item.ID][resourceIndex]
			if !exists {
				continue
			}
			coefficient := item.Demand / scale
			if invalidNormalizedCoefficient(coefficient) {
				return HiGHSCompiledModel{}, fmt.Errorf("HiGHS normalized coefficient for item %q and resource %q is at or below %.0e", item.ID, resource.ID, MinimumHiGHSModelCoefficient)
			}
			row.Coefficients = append(row.Coefficients, HiGHSCoefficient{Column: column, Value: coefficient})
		}
		tCoefficient := resource.Capacity / scale
		if invalidNormalizedCoefficient(tCoefficient) {
			return HiGHSCompiledModel{}, fmt.Errorf("HiGHS normalized makespan coefficient for resource %q is at or below %.0e", resource.ID, MinimumHiGHSModelCoefficient)
		}
		row.Coefficients = append(row.Coefficients, HiGHSCoefficient{Column: tColumn, Value: -tCoefficient})
		compiled.Rows = append(compiled.Rows, row)
	}
	compiled.Objectives = append(compiled.Objectives, HiGHSObjective{
		Name: "minimize_makespan", Unit: "normalized_load_ratio",
		Coefficients: []HiGHSCoefficient{{Column: tColumn, Value: 1}},
	})
	if len(secondary) == 1 {
		normalization, coefficients, normalizeErr := compileCostObjective(items, resources, eligible, edgeColumns)
		if normalizeErr != nil {
			return HiGHSCompiledModel{}, normalizeErr
		}
		compiled.CostNormalization = &normalization
		compiled.Objectives = append(compiled.Objectives, HiGHSObjective{
			Name: "minimize_estimated_cost", Unit: "normalized_exact_micros", Coefficients: coefficients,
		})
	}
	return compiled, nil
}

func invalidNormalizedCoefficient(value float64) bool {
	return math.IsNaN(value) || math.IsInf(value, 0) || value <= MinimumHiGHSModelCoefficient
}

func constructiveMakespanUpper(items []WorkItem, resources []Resource, eligible map[string][]int, compiled *HiGHSCompiledModel) float64 {
	loads := make([]float64, len(resources))
	for _, item := range items {
		best := eligible[item.ID][0]
		bestLoad := (loads[best] + item.Demand) / resources[best].Capacity
		for _, candidate := range eligible[item.ID][1:] {
			candidateLoad := (loads[candidate] + item.Demand) / resources[candidate].Capacity
			if candidateLoad < bestLoad || (candidateLoad == bestLoad && resources[candidate].ID < resources[best].ID) {
				best, bestLoad = candidate, candidateLoad
			}
		}
		loads[best] += item.Demand
		compiled.ConstructiveAssignments = append(compiled.ConstructiveAssignments, ExternalSolverAssignmentChoice{Item: item.ID, Resource: resources[best].ID})
	}
	upper := 0.0
	for index, resource := range resources {
		upper = math.Max(upper, loads[index]/resource.Capacity)
	}
	if len(items) > 0 {
		return math.Nextafter(upper, math.Inf(1))
	}
	return upper
}

func compileCostObjective(items []WorkItem, resources []Resource, eligible map[string][]int, edgeColumns map[string]map[int]int) (HiGHSCostNormalization, []HiGHSCoefficient, error) {
	normalization := HiGHSCostNormalization{GlobalPositiveGCDMicros: 1}
	deltas := make(map[int]int64)
	positiveGCD := int64(0)
	maximumByItem := make([]int64, len(items))
	for itemIndex, item := range items {
		base := int64(math.MaxInt64)
		for _, resourceIndex := range eligible[item.ID] {
			cost, exists := item.EstimatedCostMicros[resources[resourceIndex].ID]
			if !exists {
				return HiGHSCostNormalization{}, nil, fmt.Errorf("HiGHS cost objective lacks price for eligible edge %q -> %q", item.ID, resources[resourceIndex].ID)
			}
			base = min(base, cost)
		}
		if base < 0 || base > math.MaxInt64-normalization.OffsetMicros {
			return HiGHSCostNormalization{}, nil, errors.New("HiGHS cost base offset overflows int64 micros")
		}
		normalization.OffsetMicros += base
		normalization.PerItemBaseMicros = append(normalization.PerItemBaseMicros, HiGHSCostBase{Item: item.ID, BaseMicros: base})
		for _, resourceIndex := range eligible[item.ID] {
			cost := item.EstimatedCostMicros[resources[resourceIndex].ID]
			if cost < base {
				return HiGHSCostNormalization{}, nil, errors.New("HiGHS cost normalization underflow")
			}
			delta := cost - base
			column := edgeColumns[item.ID][resourceIndex]
			deltas[column] = delta
			maximumByItem[itemIndex] = max(maximumByItem[itemIndex], delta)
			if delta > 0 {
				positiveGCD = greatestCommonDivisor(positiveGCD, delta)
			}
		}
	}
	if positiveGCD > 0 {
		normalization.GlobalPositiveGCDMicros = positiveGCD
	}
	for _, maximum := range maximumByItem {
		normalized := maximum / normalization.GlobalPositiveGCDMicros
		if normalized > MaximumHiGHSExactInteger-normalization.WorstCaseNormalized {
			return HiGHSCostNormalization{}, nil, fmt.Errorf("HiGHS normalized cost objective exceeds exact binary64 integer cap %d", MaximumHiGHSExactInteger)
		}
		normalization.WorstCaseNormalized += normalized
	}
	coefficients := make([]HiGHSCoefficient, 0, len(deltas))
	for column := 0; column < len(deltas); column++ {
		normalized := deltas[column] / normalization.GlobalPositiveGCDMicros
		if normalized != 0 {
			coefficients = append(coefficients, HiGHSCoefficient{Column: column, Value: float64(normalized)})
		}
	}
	return normalization, coefficients, nil
}

func greatestCommonDivisor(left, right int64) int64 {
	for right != 0 {
		left, right = right, left%right
	}
	return left
}

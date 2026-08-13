package optimization

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

type StatePlane string

const (
	StateDesired     StatePlane = "DESIRED"
	StateAdmitted    StatePlane = "ADMITTED"
	StateExecuting   StatePlane = "EXECUTING"
	StateObserved    StatePlane = "OBSERVED"
	StateBilled      StatePlane = "BILLED"
	StateExperienced StatePlane = "EXPERIENCED"
)

type Fidelity string

const (
	FidelityInferred   Fidelity = "INFERRED"
	FidelityEstimated  Fidelity = "ESTIMATED"
	FidelitySampled    Fidelity = "SAMPLED"
	FidelityAggregated Fidelity = "AGGREGATED"
	FidelityExact      Fidelity = "EXACT"
)

type StateRequirement struct {
	ID                      string     `json:"id"`
	Plane                   StatePlane `json:"plane"`
	MinimumFidelity         Fidelity   `json:"minimumFidelity"`
	MaximumStalenessSeconds int        `json:"maximumStalenessSeconds"`
}

type FieldObservation struct {
	Requirement             string     `json:"requirement"`
	Plane                   StatePlane `json:"plane"`
	Fidelity                Fidelity   `json:"fidelity"`
	MaximumStalenessSeconds int        `json:"maximumStalenessSeconds"`
}

type AcquisitionCall struct {
	ID           string             `json:"id"`
	Source       string             `json:"source"`
	Method       string             `json:"method"`
	Endpoint     string             `json:"endpoint"`
	Consistency  string             `json:"consistency"`
	CostUnits    int64              `json:"costUnits"`
	Observations []FieldObservation `json:"observations"`
}

type AcquisitionPlan struct {
	Strategy       string            `json:"strategy"`
	Optimal        bool              `json:"optimal"`
	Complete       bool              `json:"complete"`
	TotalCostUnits int64             `json:"totalCostUnits"`
	Calls          []AcquisitionCall `json:"calls"`
	Covered        []string          `json:"covered"`
	Unknown        []string          `json:"unknown"`
}

// OptimizeAcquisitionCalls solves the bounded weighted set-cover problem
// exactly. A field is covered only when both fidelity and freshness meet its
// declared requirement; all other fields remain explicitly UNKNOWN.
func OptimizeAcquisitionCalls(requirements []StateRequirement, calls []AcquisitionCall) (AcquisitionPlan, error) {
	if len(requirements) > 63 {
		return AcquisitionPlan{}, errors.New("acquisition optimization supports at most 63 requirements")
	}
	if len(calls) > 20 {
		return AcquisitionPlan{}, errors.New("exact acquisition optimization supports at most 20 calls; partition the source domain")
	}
	requirements = append([]StateRequirement(nil), requirements...)
	calls = append([]AcquisitionCall(nil), calls...)
	sort.Slice(requirements, func(i, j int) bool { return requirements[i].ID < requirements[j].ID })
	sort.Slice(calls, func(i, j int) bool { return calls[i].ID < calls[j].ID })
	index := map[string]int{}
	for i, requirement := range requirements {
		if err := validateRequirement(requirement); err != nil {
			return AcquisitionPlan{}, err
		}
		if _, ok := index[requirement.ID]; ok {
			return AcquisitionPlan{}, fmt.Errorf("duplicate requirement %q", requirement.ID)
		}
		index[requirement.ID] = i
	}
	masks := make([]uint64, len(calls))
	maximumCost := int64(0)
	for i, call := range calls {
		if !validEvidenceCoordinate(call.ID) || !validEvidenceCoordinate(call.Source) || !validEvidenceCoordinate(call.Method) ||
			!validEvidenceCoordinate(call.Consistency) || call.Endpoint == "" || call.CostUnits <= 0 {
			return AcquisitionPlan{}, fmt.Errorf("call %q has incomplete identity, consistency, or cost", call.ID)
		}
		if i > 0 && calls[i-1].ID == call.ID {
			return AcquisitionPlan{}, fmt.Errorf("duplicate call %q", call.ID)
		}
		if call.CostUnits > math.MaxInt64-maximumCost {
			return AcquisitionPlan{}, errors.New("sum of acquisition call costs overflows cost units")
		}
		maximumCost += call.CostUnits
		for _, observation := range call.Observations {
			position, ok := index[observation.Requirement]
			if !ok {
				return AcquisitionPlan{}, fmt.Errorf("call %q observes unknown requirement %q", call.ID, observation.Requirement)
			}
			if observation.Plane == requirements[position].Plane && satisfies(requirements[position], observation) {
				masks[i] |= uint64(1) << position
			}
		}
	}
	all := uint64(0)
	if len(requirements) > 0 {
		all = (uint64(1) << len(requirements)) - 1
	}
	available := uint64(0)
	for _, mask := range masks {
		available |= mask
	}
	target := all & available
	bestCost := int64(^uint64(0) >> 1)
	var best []int
	nodes := 0
	exhausted := false
	var search func(int, uint64, int64, []int)
	search = func(position int, covered uint64, cost int64, selected []int) {
		nodes++
		if nodes > 2_000_000 {
			exhausted = true
			return
		}
		if exhausted {
			return
		}
		if cost >= bestCost {
			return
		}
		if covered == target {
			bestCost = cost
			best = append([]int(nil), selected...)
			return
		}
		if position == len(calls) {
			return
		}
		remaining := uint64(0)
		for i := position; i < len(calls); i++ {
			remaining |= masks[i]
		}
		if covered|remaining != target {
			return
		}
		if masks[position]&^covered != 0 {
			search(position+1, covered|masks[position], cost+calls[position].CostUnits, append(selected, position))
		}
		search(position+1, covered, cost, selected)
	}
	search(0, 0, 0, nil)
	if exhausted {
		return AcquisitionPlan{}, errors.New("exact acquisition optimization exceeded deterministic search budget; partition the source domain")
	}
	complete := target == all
	plan := AcquisitionPlan{Strategy: "exact-weighted-set-cover", Optimal: complete, Complete: complete}
	for _, position := range best {
		plan.Calls = append(plan.Calls, calls[position])
		plan.TotalCostUnits += calls[position].CostUnits
	}
	for i, requirement := range requirements {
		if target&(uint64(1)<<i) != 0 {
			plan.Covered = append(plan.Covered, requirement.ID)
		} else {
			plan.Unknown = append(plan.Unknown, requirement.ID)
		}
	}
	return plan, nil
}

func validateRequirement(requirement StateRequirement) error {
	if requirement.ID == "" || requirement.MaximumStalenessSeconds < 0 {
		return fmt.Errorf("requirement %q has invalid identity or freshness", requirement.ID)
	}
	switch requirement.Plane {
	case StateDesired, StateAdmitted, StateExecuting, StateObserved, StateBilled, StateExperienced:
	default:
		return fmt.Errorf("requirement %q has invalid state plane", requirement.ID)
	}
	if fidelityRank(requirement.MinimumFidelity) == 0 {
		return fmt.Errorf("requirement %q has invalid fidelity", requirement.ID)
	}
	return nil
}
func satisfies(requirement StateRequirement, observation FieldObservation) bool {
	return observation.MaximumStalenessSeconds >= 0 && observation.MaximumStalenessSeconds <= requirement.MaximumStalenessSeconds && fidelitySatisfies(requirement.MinimumFidelity, observation.Fidelity)
}

func fidelitySatisfies(minimum, observed Fidelity) bool {
	if fidelityRank(minimum) == 0 || fidelityRank(observed) == 0 {
		return false
	}
	if observed == FidelityExact || minimum == FidelityInferred {
		return true
	}
	return observed == minimum
}
func fidelityRank(fidelity Fidelity) int {
	switch fidelity {
	case FidelityInferred:
		return 1
	case FidelityEstimated, FidelitySampled, FidelityAggregated:
		return 2
	case FidelityExact:
		return 3
	default:
		return 0
	}
}

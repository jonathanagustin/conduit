// Package optimization provides deterministic, advisory resource allocation.
// It never grants authority: callers remain responsible for admission,
// approval, and rechecking domain invariants before executing a schedule.
package optimization

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

const (
	tolerance           = 1e-9
	maxItems            = 2_000
	maxResources        = 256
	maxEligibilityEdges = 100_000
)

type WorkItem struct {
	ID       string   `json:"id"`
	Demand   float64  `json:"demand"`
	Eligible []string `json:"eligible,omitempty"`
	// EstimatedCostMicros is an exact estimate for assigning this indivisible
	// item to a resource. Missing entries mean cost is unknown, not zero.
	EstimatedCostMicros map[string]int64 `json:"estimatedCostMicros,omitempty"`
}

type Resource struct {
	ID       string  `json:"id"`
	Capacity float64 `json:"capacity"`
}

type Problem struct {
	Items        []WorkItem `json:"items"`
	Resources    []Resource `json:"resources"`
	CostModel    *CostModel `json:"costModel,omitempty"`
	MakespanUnit string     `json:"makespanUnit,omitempty"`
}

type CostModel struct {
	Currency string `json:"currency"`
	Source   string `json:"source"`
	AsOf     string `json:"asOf"`
}

type Fraction struct {
	Resource string  `json:"resource"`
	Share    float64 `json:"share"`
}

type Assignment struct {
	Item                string  `json:"item"`
	Resource            string  `json:"resource"`
	Demand              float64 `json:"demand"`
	EstimatedCostMicros *int64  `json:"estimatedCostMicros,omitempty"`
}

type ResourceMetric struct {
	Resource       string  `json:"resource"`
	Load           float64 `json:"load"`
	NormalizedLoad float64 `json:"normalizedLoad"`
	Utilization    float64 `json:"utilization"`
}

type Result struct {
	RelaxedMakespan             float64               `json:"relaxedMakespan"`
	BaselineMakespan            float64               `json:"baselineMakespan"`
	RoundedMakespan             float64               `json:"roundedMakespan"`
	Improvement                 float64               `json:"improvement"`
	OptimalityGap               float64               `json:"optimalityGap"`
	MeanUtilization             float64               `json:"meanUtilization"`
	Imbalance                   float64               `json:"imbalance"`
	Fractions                   map[string][]Fraction `json:"fractions"`
	Assignments                 []Assignment          `json:"assignments"`
	Resources                   []ResourceMetric      `json:"resources"`
	CostModel                   *CostModel            `json:"costModel,omitempty"`
	BaselineEstimatedCostMicros *int64                `json:"baselineEstimatedCostMicros,omitempty"`
	RoundedEstimatedCostMicros  *int64                `json:"roundedEstimatedCostMicros,omitempty"`
	EstimatedSavingsMicros      *int64                `json:"estimatedSavingsMicros,omitempty"`
}

// Solve minimizes maximum normalized resource load in the fractional convex
// relaxation, then deterministically rounds indivisible work onto resources.
func Solve(problem Problem) (Result, error) {
	return solve(problem, true)
}

func solve(problem Problem, optimizeCost bool) (Result, error) {
	items, resources, eligible, err := validate(problem)
	if err != nil {
		return Result{}, err
	}
	if len(items) == 0 {
		return Result{Fractions: map[string][]Fraction{}}, nil
	}

	lo, hi := 0.0, 1.0
	for !feasible(items, resources, eligible, hi, nil) {
		hi *= 2
		if math.IsInf(hi, 0) {
			return Result{}, errors.New("optimization makespan overflow")
		}
	}
	for range 80 {
		mid := (lo + hi) / 2
		if feasible(items, resources, eligible, mid, nil) {
			hi = mid
		} else {
			lo = mid
		}
	}
	flows := make(map[string]map[string]float64, len(items))
	if !feasible(items, resources, eligible, hi, flows) {
		return Result{}, errors.New("failed to recover feasible relaxation")
	}
	return round(items, resources, eligible, flows, hi, problem.CostModel, optimizeCost)
}

func validate(problem Problem) ([]WorkItem, []Resource, map[string][]int, error) {
	if problem.MakespanUnit != "" && problem.MakespanUnit != "seconds" {
		return nil, nil, nil, errors.New("makespanUnit must be seconds when specified")
	}
	if len(problem.Items) > maxItems || len(problem.Resources) > maxResources {
		return nil, nil, nil, fmt.Errorf("problem exceeds limit of %d items or %d resources", maxItems, maxResources)
	}
	items := append([]WorkItem(nil), problem.Items...)
	resources := append([]Resource(nil), problem.Resources...)
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	sort.Slice(resources, func(i, j int) bool { return resources[i].ID < resources[j].ID })
	resourceIndex := make(map[string]int, len(resources))
	for i, resource := range resources {
		if resource.ID == "" || resource.Capacity <= 0 || math.IsNaN(resource.Capacity) || math.IsInf(resource.Capacity, 0) {
			return nil, nil, nil, fmt.Errorf("resource %q must have a finite positive capacity", resource.ID)
		}
		if _, exists := resourceIndex[resource.ID]; exists {
			return nil, nil, nil, fmt.Errorf("duplicate resource %q", resource.ID)
		}
		resourceIndex[resource.ID] = i
	}
	seenItems := make(map[string]struct{}, len(items))
	eligible := make(map[string][]int, len(items))
	edgeCount := 0
	hasCosts := false
	maximumEstimatedTotal := int64(0)
	for _, item := range items {
		if item.ID == "" || item.Demand <= 0 || math.IsNaN(item.Demand) || math.IsInf(item.Demand, 0) {
			return nil, nil, nil, fmt.Errorf("item %q must have a finite positive demand", item.ID)
		}
		if _, exists := seenItems[item.ID]; exists {
			return nil, nil, nil, fmt.Errorf("duplicate item %q", item.ID)
		}
		seenItems[item.ID] = struct{}{}
		maximumItemCost := int64(0)
		for resource, cost := range item.EstimatedCostMicros {
			if _, ok := resourceIndex[resource]; !ok {
				return nil, nil, nil, fmt.Errorf("item %q prices unknown resource %q", item.ID, resource)
			}
			if cost < 0 {
				return nil, nil, nil, fmt.Errorf("item %q has negative estimated cost", item.ID)
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
			for i := range resources {
				allowed[i] = resources[i].ID
			}
		}
		seen := map[int]struct{}{}
		for _, id := range allowed {
			index, ok := resourceIndex[id]
			if !ok {
				return nil, nil, nil, fmt.Errorf("item %q names unknown resource %q", item.ID, id)
			}
			seen[index] = struct{}{}
		}
		for index := range seen {
			eligible[item.ID] = append(eligible[item.ID], index)
		}
		sort.Ints(eligible[item.ID])
		edgeCount += len(eligible[item.ID])
		if edgeCount > maxEligibilityEdges {
			return nil, nil, nil, fmt.Errorf("problem exceeds limit of %d eligibility edges", maxEligibilityEdges)
		}
		if len(eligible[item.ID]) == 0 {
			return nil, nil, nil, fmt.Errorf("item %q has no eligible resource", item.ID)
		}
	}
	if len(items) > 0 && len(resources) == 0 {
		return nil, nil, nil, errors.New("work requires at least one resource")
	}
	if hasCosts {
		if problem.CostModel == nil || !validCurrency(problem.CostModel.Currency) || problem.CostModel.Source == "" {
			return nil, nil, nil, errors.New("estimated costs require a three-letter uppercase currency and price source")
		}
		if _, err := time.Parse(time.RFC3339, problem.CostModel.AsOf); err != nil {
			return nil, nil, nil, fmt.Errorf("cost model asOf must be RFC3339: %w", err)
		}
	}
	return items, resources, eligible, nil
}

func validCurrency(currency string) bool {
	return len(currency) == 3 && currency[0] >= 'A' && currency[0] <= 'Z' && currency[1] >= 'A' && currency[1] <= 'Z' && currency[2] >= 'A' && currency[2] <= 'Z'
}

// feasible reduces fractional assignment at a candidate makespan to max flow.
func feasible(items []WorkItem, resources []Resource, eligible map[string][]int, makespan float64, output map[string]map[string]float64) bool {
	n := 2 + len(items) + len(resources)
	source, sink := n-2, n-1
	g := newFlowGraph(n)
	total := 0.0
	for i, item := range items {
		total += item.Demand
		g.add(source, i, item.Demand)
		for _, r := range eligible[item.ID] {
			g.add(i, len(items)+r, item.Demand)
		}
	}
	for r, resource := range resources {
		g.add(len(items)+r, sink, makespan*resource.Capacity)
	}
	if g.max(source, sink)+tolerance < total {
		return false
	}
	if output != nil {
		for i, item := range items {
			for _, edge := range g.edges[i] {
				if edge.to < len(items) || edge.to >= len(items)+len(resources) {
					continue
				}
				used := edge.original - edge.capacity
				if used > tolerance {
					if output[item.ID] == nil {
						output[item.ID] = map[string]float64{}
					}
					output[item.ID][resources[edge.to-len(items)].ID] = used / item.Demand
				}
			}
		}
	}
	return true
}

type allocationChoice struct {
	Item     string
	Resource string
}

func round(items []WorkItem, resources []Resource, eligible map[string][]int, flows map[string]map[string]float64, lower float64, costModel *CostModel, optimizeCost bool) (Result, error) {
	ordered := append([]WorkItem(nil), items...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Demand == ordered[j].Demand {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].Demand > ordered[j].Demand
	})
	loads := make([]float64, len(resources))
	choices := make([]allocationChoice, 0, len(items))
	for _, item := range ordered {
		best := eligible[item.ID][0]
		for _, candidate := range eligible[item.ID][1:] {
			candidateLoad := (loads[candidate] + item.Demand) / resources[candidate].Capacity
			bestLoad := (loads[best] + item.Demand) / resources[best].Capacity
			candidateShare, bestShare := flows[item.ID][resources[candidate].ID], flows[item.ID][resources[best].ID]
			candidateCost, candidateKnown := item.EstimatedCostMicros[resources[candidate].ID]
			bestCost, bestKnown := item.EstimatedCostMicros[resources[best].ID]
			cheaper := optimizeCost && candidateKnown && (!bestKnown || candidateCost < bestCost)
			costsEqual := !optimizeCost || (candidateKnown == bestKnown && candidateCost == bestCost)
			if candidateLoad < bestLoad-tolerance || (math.Abs(candidateLoad-bestLoad) <= tolerance && (cheaper || (costsEqual && (candidateShare > bestShare+tolerance || (math.Abs(candidateShare-bestShare) <= tolerance && resources[candidate].ID < resources[best].ID))))) {
				best = candidate
			}
		}
		loads[best] += item.Demand
		choices = append(choices, allocationChoice{Item: item.ID, Resource: resources[best].ID})
	}
	return buildAllocationResult(items, resources, eligible, flows, lower, costModel, choices)
}

// buildAllocationResult is the compiler-owned construction boundary shared by
// native rounding and external solver choices. It derives every Result field
// from validated problem data and a deliberately small item/resource choice
// surface. verifySolverResult remains a separate implementation so a defect in
// this builder cannot make an invalid receipt self-consistent.
func buildAllocationResult(
	items []WorkItem,
	resources []Resource,
	eligible map[string][]int,
	flows map[string]map[string]float64,
	lower float64,
	costModel *CostModel,
	choices []allocationChoice,
) (Result, error) {
	if len(choices) != len(items) {
		return Result{}, errors.New("allocation choices must cover every work item exactly once")
	}
	if len(items) == 0 {
		return Result{Fractions: map[string][]Fraction{}}, nil
	}

	baselineMakespan, baselineCost, baselineCostKnown := greedyMetrics(items, resources, eligible)
	var resultCostModel *CostModel
	if costModel != nil {
		cloned := *costModel
		resultCostModel = &cloned
	}
	result := Result{RelaxedMakespan: lower, BaselineMakespan: baselineMakespan, Fractions: map[string][]Fraction{}, CostModel: resultCostModel}
	if baselineCostKnown {
		result.BaselineEstimatedCostMicros = &baselineCost
	}
	for _, item := range items {
		for resource, share := range flows[item.ID] {
			result.Fractions[item.ID] = append(result.Fractions[item.ID], Fraction{Resource: resource, Share: share})
		}
		sort.Slice(result.Fractions[item.ID], func(i, j int) bool {
			return result.Fractions[item.ID][i].Resource < result.Fractions[item.ID][j].Resource
		})
	}

	itemByID := make(map[string]WorkItem, len(items))
	resourceByID := make(map[string]int, len(resources))
	eligibleByItem := make(map[string]map[string]bool, len(items))
	for _, item := range items {
		itemByID[item.ID] = item
		eligibleByItem[item.ID] = make(map[string]bool, len(eligible[item.ID]))
		for _, resourceIndex := range eligible[item.ID] {
			eligibleByItem[item.ID][resources[resourceIndex].ID] = true
		}
	}
	for index, resource := range resources {
		resourceByID[resource.ID] = index
	}

	loads := make([]float64, len(resources))
	seen := make(map[string]bool, len(items))
	roundedCost := int64(0)
	roundedCostKnown := costModel != nil
	for _, choice := range choices {
		item, itemExists := itemByID[choice.Item]
		resourceIndex, resourceExists := resourceByID[choice.Resource]
		if !itemExists || seen[choice.Item] {
			return Result{}, errors.New("allocation choice contains an unknown or duplicate item")
		}
		if !resourceExists {
			return Result{}, errors.New("allocation choice contains an unknown resource")
		}
		if !eligibleByItem[choice.Item][choice.Resource] {
			return Result{}, errors.New("allocation choice contains an ineligible assignment")
		}
		seen[choice.Item] = true
		loads[resourceIndex] += item.Demand
		assignment := Assignment{Item: item.ID, Resource: choice.Resource, Demand: item.Demand}
		if cost, known := item.EstimatedCostMicros[choice.Resource]; known {
			if cost > math.MaxInt64-roundedCost {
				return Result{}, errors.New("allocation assignment cost overflows micro-units")
			}
			roundedCost += cost
			assignment.EstimatedCostMicros = &cost
		} else if costModel != nil {
			roundedCostKnown = false
		}
		result.Assignments = append(result.Assignments, assignment)
	}

	minUtil := math.Inf(1)
	for i, resource := range resources {
		normalizedLoad := loads[i] / resource.Capacity
		result.RoundedMakespan = math.Max(result.RoundedMakespan, normalizedLoad)
		minUtil = math.Min(minUtil, normalizedLoad)
	}
	result.Imbalance = result.RoundedMakespan - minUtil
	for i, resource := range resources {
		normalizedLoad := loads[i] / resource.Capacity
		utilization := 0.0
		if result.RoundedMakespan > tolerance {
			utilization = normalizedLoad / result.RoundedMakespan
		}
		result.Resources = append(result.Resources, ResourceMetric{Resource: resource.ID, Load: loads[i], NormalizedLoad: normalizedLoad, Utilization: utilization})
		result.MeanUtilization += utilization / float64(len(resources))
	}
	if lower > tolerance {
		result.OptimalityGap = result.RoundedMakespan/lower - 1
	}
	if result.BaselineMakespan > tolerance {
		result.Improvement = 1 - result.RoundedMakespan/result.BaselineMakespan
	}
	if roundedCostKnown {
		result.RoundedEstimatedCostMicros = &roundedCost
		if baselineCostKnown {
			savings := baselineCost - roundedCost
			result.EstimatedSavingsMicros = &savings
		}
	}
	return result, nil
}

// greedyMakespan models the incumbent online policy: accept work in stable ID
// order and place it on the eligible resource with the lowest projected load.
func greedyMetrics(items []WorkItem, resources []Resource, eligible map[string][]int) (float64, int64, bool) {
	loads := make([]float64, len(resources))
	cost, costKnown := int64(0), true
	for _, item := range items {
		best := eligible[item.ID][0]
		for _, candidate := range eligible[item.ID][1:] {
			if (loads[candidate]+item.Demand)/resources[candidate].Capacity < (loads[best]+item.Demand)/resources[best].Capacity-tolerance {
				best = candidate
			}
		}
		loads[best] += item.Demand
		itemCost, ok := item.EstimatedCostMicros[resources[best].ID]
		if !ok {
			costKnown = false
		} else {
			cost += itemCost
		}
	}
	makespan := 0.0
	for i, resource := range resources {
		makespan = math.Max(makespan, loads[i]/resource.Capacity)
	}
	return makespan, cost, costKnown
}

type flowEdge struct {
	to, reverse        int
	capacity, original float64
}
type flowGraph struct{ edges [][]*flowEdge }

func newFlowGraph(n int) *flowGraph { return &flowGraph{edges: make([][]*flowEdge, n)} }
func (g *flowGraph) add(from, to int, capacity float64) {
	f := &flowEdge{to: to, reverse: len(g.edges[to]), capacity: capacity, original: capacity}
	r := &flowEdge{to: from, reverse: len(g.edges[from])}
	g.edges[from] = append(g.edges[from], f)
	g.edges[to] = append(g.edges[to], r)
}
func (g *flowGraph) max(source, sink int) float64 {
	total := 0.0
	for {
		levels := make([]int, len(g.edges))
		for i := range levels {
			levels[i] = -1
		}
		levels[source] = 0
		q := []int{source}
		for len(q) > 0 {
			v := q[0]
			q = q[1:]
			for _, e := range g.edges[v] {
				if e.capacity > tolerance && levels[e.to] < 0 {
					levels[e.to] = levels[v] + 1
					q = append(q, e.to)
				}
			}
		}
		if levels[sink] < 0 {
			return total
		}
		next := make([]int, len(g.edges))
		for {
			f := g.push(source, sink, math.Inf(1), levels, next)
			if f <= tolerance {
				break
			}
			total += f
		}
	}
}
func (g *flowGraph) push(v, sink int, amount float64, levels, next []int) float64 {
	if v == sink {
		return amount
	}
	for next[v] < len(g.edges[v]) {
		e := g.edges[v][next[v]]
		if e.capacity > tolerance && levels[e.to] == levels[v]+1 {
			sent := g.push(e.to, sink, math.Min(amount, e.capacity), levels, next)
			if sent > tolerance {
				e.capacity -= sent
				g.edges[e.to][e.reverse].capacity += sent
				return sent
			}
		}
		next[v]++
	}
	return 0
}

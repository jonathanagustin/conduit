package optimization

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"time"
)

const (
	DiscreteSolverAPIVersion        = "optimization.platform.example/discrete-solver/v1alpha1"
	DiscreteScheduleModelRevision   = "discrete.schedule.v1"
	DiscreteScheduleCapability      = "discrete-schedule-v1"
	maximumDiscreteTasks            = 500
	maximumDiscreteLanes            = 128
	maximumDiscretePrecedences      = 5_000
	maximumDiscreteTopologyRules    = 1_000
	maximumDiscreteConcurrencyRules = 1_000
	maximumDiscreteMaintenance      = 100
	maximumDiscreteDimensions       = 16
	maximumDiscreteTopologyKeys     = 16
	maximumDiscreteHorizonSeconds   = int64(366 * 24 * 60 * 60)
	maximumDiscreteQuantity         = int64(1_000_000_000_000)
)

type DiscreteTask struct {
	ID                  string           `json:"id"`
	DurationSeconds     int64            `json:"durationSeconds"`
	EarliestStart       int64            `json:"earliestStart"`
	LatestFinish        int64            `json:"latestFinish"`
	EligibleLanes       []string         `json:"eligibleLanes"`
	Demand              map[string]int64 `json:"demand"`
	EstimatedCostMicros map[string]int64 `json:"estimatedCostMicros,omitempty"`
}

type DiscreteLane struct {
	ID       string            `json:"id"`
	Capacity map[string]int64  `json:"capacity"`
	Topology map[string]string `json:"topology,omitempty"`
}

type DiscretePrecedence struct {
	Before     string `json:"before"`
	After      string `json:"after"`
	LagSeconds int64  `json:"lagSeconds"`
}

type DiscreteTopologyRule struct {
	ID       string   `json:"id"`
	Tasks    []string `json:"tasks"`
	Key      string   `json:"key"`
	Relation string   `json:"relation"`
}

type DiscreteConcurrencyRule struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Tasks   []string `json:"tasks"`
	Maximum int      `json:"maximum"`
}

type DiscreteMaintenanceWindow struct {
	Lane  string `json:"lane"`
	Start int64  `json:"start"`
	End   int64  `json:"end"`
}

type DiscreteScheduleProblem struct {
	Origin             string                      `json:"origin"`
	TimeUnit           string                      `json:"timeUnit"`
	HorizonSeconds     int64                       `json:"horizonSeconds"`
	Tasks              []DiscreteTask              `json:"tasks"`
	Lanes              []DiscreteLane              `json:"lanes"`
	Precedences        []DiscretePrecedence        `json:"precedences,omitempty"`
	TopologyRules      []DiscreteTopologyRule      `json:"topologyRules,omitempty"`
	ConcurrencyRules   []DiscreteConcurrencyRule   `json:"concurrencyRules,omitempty"`
	MaintenanceWindows []DiscreteMaintenanceWindow `json:"maintenanceWindows,omitempty"`
	CostModel          *CostModel                  `json:"costModel,omitempty"`
}

type DiscreteRecommendationRequest struct {
	APIVersion  string                  `json:"apiVersion"`
	Kind        string                  `json:"kind"`
	Snapshot    Snapshot                `json:"snapshot"`
	Objective   Objective               `json:"objective"`
	Constraints []ConstraintEvidence    `json:"constraints"`
	Problem     DiscreteScheduleProblem `json:"problem"`
}

type DiscreteSolverRequest struct {
	APIVersion     string                        `json:"apiVersion"`
	Kind           string                        `json:"kind"`
	ID             string                        `json:"id"`
	Recommendation DiscreteRecommendationRequest `json:"recommendation"`
	Model          SolverModel                   `json:"model"`
	Capability     string                        `json:"capability"`
	Selection      SolverSelection               `json:"selection"`
	Determinism    SolverDeterminism             `json:"determinism"`
	Limits         SolverLimits                  `json:"limits"`
}

type DiscreteAssignment struct {
	Task                string `json:"task"`
	Lane                string `json:"lane"`
	Start               int64  `json:"start"`
	Finish              int64  `json:"finish"`
	EstimatedCostMicros *int64 `json:"estimatedCostMicros,omitempty"`
}

// DiscreteChoice is the complete provider-authored surface. Duration, finish,
// cost, makespan, capacity and every other result field stay compiler-owned.
type DiscreteChoice struct {
	Task  string `json:"task"`
	Lane  string `json:"lane"`
	Start int64  `json:"start"`
}

type DiscreteScheduleResult struct {
	Assignments              []DiscreteAssignment `json:"assignments"`
	MakespanSeconds          int64                `json:"makespanSeconds"`
	EstimatedTotalCostMicros *int64               `json:"estimatedTotalCostMicros,omitempty"`
	CostModel                *CostModel           `json:"costModel,omitempty"`
}

type discreteHardTask struct {
	ID              string           `json:"id"`
	DurationSeconds int64            `json:"durationSeconds"`
	EarliestStart   int64            `json:"earliestStart"`
	LatestFinish    int64            `json:"latestFinish"`
	EligibleLanes   []string         `json:"eligibleLanes"`
	Demand          map[string]int64 `json:"demand"`
}

type discreteObjectiveTask struct {
	ID                  string           `json:"id"`
	EstimatedCostMicros map[string]int64 `json:"estimatedCostMicros,omitempty"`
}

func NewDiscreteSolverRequest(recommendation DiscreteRecommendationRequest, selection SolverSelection, determinism SolverDeterminism, limits SolverLimits) (DiscreteSolverRequest, error) {
	recommendation = canonicalDiscreteRecommendation(recommendation)
	if err := validateDiscreteRecommendation(recommendation); err != nil {
		return DiscreteSolverRequest{}, err
	}
	modelDigest, err := digestJSON(recommendation.Problem)
	if err != nil {
		return DiscreteSolverRequest{}, err
	}
	hardTasks := make([]discreteHardTask, 0, len(recommendation.Problem.Tasks))
	objectiveTasks := make([]discreteObjectiveTask, 0, len(recommendation.Problem.Tasks))
	for _, task := range recommendation.Problem.Tasks {
		hardTasks = append(hardTasks, discreteHardTask{
			ID: task.ID, DurationSeconds: task.DurationSeconds, EarliestStart: task.EarliestStart,
			LatestFinish: task.LatestFinish, EligibleLanes: task.EligibleLanes, Demand: task.Demand,
		})
		objectiveTasks = append(objectiveTasks, discreteObjectiveTask{ID: task.ID, EstimatedCostMicros: task.EstimatedCostMicros})
	}
	hardConstraints := struct {
		Evidence           []ConstraintEvidence        `json:"evidence"`
		Origin             string                      `json:"origin"`
		TimeUnit           string                      `json:"timeUnit"`
		HorizonSeconds     int64                       `json:"horizonSeconds"`
		Tasks              []discreteHardTask          `json:"tasks"`
		Lanes              []DiscreteLane              `json:"lanes"`
		Precedences        []DiscretePrecedence        `json:"precedences"`
		TopologyRules      []DiscreteTopologyRule      `json:"topologyRules"`
		ConcurrencyRules   []DiscreteConcurrencyRule   `json:"concurrencyRules"`
		MaintenanceWindows []DiscreteMaintenanceWindow `json:"maintenanceWindows"`
	}{recommendation.Constraints, recommendation.Problem.Origin, recommendation.Problem.TimeUnit, recommendation.Problem.HorizonSeconds, hardTasks, recommendation.Problem.Lanes, recommendation.Problem.Precedences, recommendation.Problem.TopologyRules, recommendation.Problem.ConcurrencyRules, recommendation.Problem.MaintenanceWindows}
	hardDigest, err := digestJSON(hardConstraints)
	if err != nil {
		return DiscreteSolverRequest{}, err
	}
	objectiveDigest, err := digestJSON(struct {
		Objective Objective               `json:"objective"`
		Tasks     []discreteObjectiveTask `json:"tasks"`
		CostModel *CostModel              `json:"costModel,omitempty"`
	}{recommendation.Objective, objectiveTasks, recommendation.Problem.CostModel})
	if err != nil {
		return DiscreteSolverRequest{}, err
	}
	request := DiscreteSolverRequest{
		APIVersion: DiscreteSolverAPIVersion, Kind: "DiscreteSolverRequest", Recommendation: recommendation,
		Model:      SolverModel{Name: "discrete.schedule", Revision: DiscreteScheduleModelRevision, Digest: modelDigest, HardConstraintsDigest: hardDigest, ObjectiveDigest: objectiveDigest},
		Capability: DiscreteScheduleCapability, Selection: selection, Determinism: determinism, Limits: limits,
	}
	if err := validateDiscreteSolverEnvelope(request); err != nil {
		return DiscreteSolverRequest{}, err
	}
	request.ID, err = discreteSolverRequestID(request)
	if err != nil {
		return DiscreteSolverRequest{}, err
	}
	return request, nil
}

func VerifyDiscreteSolverRequest(request DiscreteSolverRequest) error {
	replayed, err := NewDiscreteSolverRequest(request.Recommendation, request.Selection, request.Determinism, request.Limits)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(replayed, request) {
		return errors.New("discrete solver request does not match canonical replay")
	}
	return nil
}

func NewDiscreteScheduleResult(problem DiscreteScheduleProblem, choices []DiscreteChoice) (DiscreteScheduleResult, error) {
	problem = canonicalDiscreteProblem(problem)
	if err := validateDiscreteProblem(problem, Objective{Primary: "minimize_makespan"}); err != nil {
		return DiscreteScheduleResult{}, err
	}
	tasks := make(map[string]DiscreteTask, len(problem.Tasks))
	for _, task := range problem.Tasks {
		tasks[task.ID] = task
	}
	assignments := make([]DiscreteAssignment, 0, len(choices))
	for _, choice := range choices {
		task, exists := tasks[choice.Task]
		if !exists {
			return DiscreteScheduleResult{}, errors.New("discrete choice names an unknown task")
		}
		if choice.Start < task.EarliestStart || choice.Start > task.LatestFinish-task.DurationSeconds {
			return DiscreteScheduleResult{}, errors.New("discrete choice start is outside its execution window")
		}
		assignment := DiscreteAssignment{Task: choice.Task, Lane: choice.Lane, Start: choice.Start, Finish: choice.Start + task.DurationSeconds}
		if cost, known := task.EstimatedCostMicros[choice.Lane]; known {
			value := cost
			assignment.EstimatedCostMicros = &value
		}
		assignments = append(assignments, assignment)
	}
	return buildDiscreteScheduleResultFromAssignments(problem, assignments)
}

func buildDiscreteScheduleResultFromAssignments(problem DiscreteScheduleProblem, assignments []DiscreteAssignment) (DiscreteScheduleResult, error) {
	assignments = append([]DiscreteAssignment(nil), assignments...)
	sort.Slice(assignments, func(i, j int) bool { return assignments[i].Task < assignments[j].Task })
	result := DiscreteScheduleResult{Assignments: assignments, CostModel: cloneCostModel(problem.CostModel)}
	if err := deriveDiscreteScheduleResult(problem, &result); err != nil {
		return DiscreteScheduleResult{}, err
	}
	return result, nil
}

func VerifyDiscreteScheduleResult(problem DiscreteScheduleProblem, result DiscreteScheduleResult) error {
	problem = canonicalDiscreteProblem(problem)
	if err := validateDiscreteProblem(problem, Objective{Primary: "minimize_makespan"}); err != nil {
		return err
	}
	return verifyDiscreteScheduleResultIndependently(problem, result)
}

// verifyDiscreteScheduleResultIndependently intentionally does not call the
// result builder or its derivation helpers. A compiler/result-construction bug
// must not be able to validate itself through shared semantic code.
func verifyDiscreteScheduleResultIndependently(problem DiscreteScheduleProblem, result DiscreteScheduleResult) error {
	tasks := make(map[string]DiscreteTask, len(problem.Tasks))
	lanes := make(map[string]DiscreteLane, len(problem.Lanes))
	for _, task := range problem.Tasks {
		tasks[task.ID] = task
	}
	for _, lane := range problem.Lanes {
		lanes[lane.ID] = lane
	}
	if len(result.Assignments) != len(tasks) {
		return errors.New("discrete result must independently assign every task exactly once")
	}
	assigned := make(map[string]DiscreteAssignment, len(result.Assignments))
	totalCost, makespan := int64(0), int64(0)
	allCostsKnown := problem.CostModel != nil
	for index, assignment := range result.Assignments {
		if index > 0 && result.Assignments[index-1].Task >= assignment.Task {
			return errors.New("discrete result assignments are not in canonical task order")
		}
		task, taskExists := tasks[assignment.Task]
		_, laneExists := lanes[assignment.Lane]
		if !taskExists || !laneExists || assigned[assignment.Task].Task != "" || !containsString(task.EligibleLanes, assignment.Lane) {
			return errors.New("discrete result independently found an unknown, duplicate, or ineligible assignment")
		}
		if assignment.Start < task.EarliestStart || assignment.Start > task.LatestFinish-task.DurationSeconds || assignment.Finish != assignment.Start+task.DurationSeconds {
			return errors.New("discrete result independently found an invalid execution window or duration")
		}
		for _, window := range problem.MaintenanceWindows {
			if window.Lane == assignment.Lane && intervalsOverlap(assignment.Start, assignment.Finish, window.Start, window.End) {
				return errors.New("discrete result independently found a maintenance overlap")
			}
		}
		cost, known := task.EstimatedCostMicros[assignment.Lane]
		if known {
			if assignment.EstimatedCostMicros == nil || *assignment.EstimatedCostMicros != cost || cost > math.MaxInt64-totalCost {
				return errors.New("discrete result independently found changed or overflowing estimated cost")
			}
			totalCost += cost
		} else {
			allCostsKnown = false
			if assignment.EstimatedCostMicros != nil {
				return errors.New("discrete result independently found invented estimated cost")
			}
		}
		assigned[assignment.Task] = assignment
		makespan = max(makespan, assignment.Finish)
	}
	if result.MakespanSeconds != makespan {
		return errors.New("discrete result makespan does not match independent calculation")
	}
	if allCostsKnown {
		if result.EstimatedTotalCostMicros == nil || *result.EstimatedTotalCostMicros != totalCost {
			return errors.New("discrete result total cost does not match independent calculation")
		}
	} else if result.EstimatedTotalCostMicros != nil {
		return errors.New("discrete result invents an incomplete total cost")
	}
	if !reflect.DeepEqual(result.CostModel, problem.CostModel) {
		return errors.New("discrete result cost model does not match its problem")
	}
	for _, edge := range problem.Precedences {
		if assigned[edge.After].Start < assigned[edge.Before].Finish+edge.LagSeconds {
			return errors.New("discrete result independently found a precedence violation")
		}
	}
	for _, rule := range problem.TopologyRules {
		values := make(map[string]bool, len(rule.Tasks))
		for _, taskID := range rule.Tasks {
			values[lanes[assigned[taskID].Lane].Topology[rule.Key]] = true
		}
		if (rule.Relation == "SAME" && len(values) != 1) || (rule.Relation == "DIFFERENT" && len(values) != len(rule.Tasks)) {
			return errors.New("discrete result independently found a topology violation")
		}
	}
	for _, lane := range problem.Lanes {
		for dimension, capacity := range lane.Capacity {
			events := make(map[int64]int64)
			for taskID, assignment := range assigned {
				if assignment.Lane == lane.ID {
					events[assignment.Start] += tasks[taskID].Demand[dimension]
					events[assignment.Finish] -= tasks[taskID].Demand[dimension]
				}
			}
			if maximumSweepLoad(events) > capacity {
				return errors.New("discrete result independently found a capacity violation")
			}
		}
	}
	for _, rule := range problem.ConcurrencyRules {
		events := make(map[int64]int64, len(rule.Tasks)*2)
		for _, taskID := range rule.Tasks {
			assignment := assigned[taskID]
			events[assignment.Start]++
			events[assignment.Finish]--
		}
		if maximumSweepLoad(events) > int64(rule.Maximum) {
			return errors.New("discrete result independently found a concurrency violation")
		}
	}
	return nil
}

func validateDiscreteRecommendation(request DiscreteRecommendationRequest) error {
	if request.APIVersion != EvidenceAPIVersion || request.Kind != "DiscreteRecommendationRequest" {
		return errors.New("unsupported discrete recommendation request type")
	}
	if !validContentID(request.Snapshot.ID, "snap_") || request.Snapshot.Source == "" || request.Snapshot.Revision == "" {
		return errors.New("discrete recommendation requires a content-addressed snapshot with source and revision")
	}
	capturedAt, err := time.Parse(time.RFC3339, request.Snapshot.CapturedAt)
	if err != nil {
		return fmt.Errorf("discrete snapshot capturedAt: %w", err)
	}
	seenConstraints := make(map[string]bool, len(request.Constraints))
	for _, constraint := range request.Constraints {
		if !validSolverToken(constraint.ID) || constraint.Source == "" || constraint.Revision == "" || seenConstraints[constraint.ID] {
			return errors.New("discrete constraints require unique IDs and source revisions")
		}
		seenConstraints[constraint.ID] = true
		observedAt, parseErr := time.Parse(time.RFC3339, constraint.ObservedAt)
		if parseErr != nil || observedAt.After(capturedAt) {
			return errors.New("discrete constraint observation is invalid or newer than its snapshot")
		}
	}
	return validateDiscreteProblem(request.Problem, request.Objective)
}

func validateDiscreteSolverEnvelope(request DiscreteSolverRequest) error {
	if request.APIVersion != DiscreteSolverAPIVersion || request.Kind != "DiscreteSolverRequest" || request.Model.Name != "discrete.schedule" || request.Model.Revision != DiscreteScheduleModelRevision || request.Capability != DiscreteScheduleCapability {
		return errors.New("unsupported discrete solver envelope")
	}
	if !validDigest(request.Model.Digest) || !validDigest(request.Model.HardConstraintsDigest) || !validDigest(request.Model.ObjectiveDigest) {
		return errors.New("discrete solver model requires exact SHA-256 digests")
	}
	if !validSolverToken(request.Selection.PreferredBackend) || request.Determinism.Seed > math.MaxInt32 || request.Determinism.Workers < 1 || request.Determinism.Workers > 256 {
		return errors.New("discrete solver requires a preferred backend and bounded worker count")
	}
	if request.Limits.TimeLimitMilliseconds < 1 || request.Limits.TimeLimitMilliseconds > int64((24*time.Hour)/time.Millisecond) || request.Limits.MemoryLimitBytes < 1 || request.Limits.MemoryLimitBytes > maximumSolverMemoryBytes {
		return errors.New("discrete solver requires bounded positive time and memory limits")
	}
	seen := map[string]bool{request.Selection.PreferredBackend: true}
	for _, fallback := range request.Selection.AuthorizedFallbacks {
		if !validSolverToken(fallback) || seen[fallback] {
			return errors.New("discrete solver fallbacks must be unique tokens")
		}
		seen[fallback] = true
	}
	return nil
}

func validateDiscreteProblem(problem DiscreteScheduleProblem, objective Objective) error {
	if problem.TimeUnit != "seconds" || problem.HorizonSeconds < 1 || problem.HorizonSeconds > maximumDiscreteHorizonSeconds {
		return errors.New("discrete schedule requires bounded integer seconds")
	}
	if _, err := time.Parse(time.RFC3339, problem.Origin); err != nil {
		return fmt.Errorf("discrete schedule origin: %w", err)
	}
	if objective.Primary != "minimize_makespan" || len(objective.Secondary) > 1 || (len(objective.Secondary) == 1 && objective.Secondary[0] != "minimize_estimated_cost") {
		return errors.New("discrete schedule supports makespan and optional estimated-cost objectives")
	}
	if len(problem.Tasks) == 0 || len(problem.Tasks) > maximumDiscreteTasks || len(problem.Lanes) == 0 || len(problem.Lanes) > maximumDiscreteLanes {
		return errors.New("discrete schedule task or lane count is outside bounds")
	}
	if len(problem.Precedences) > maximumDiscretePrecedences || len(problem.TopologyRules) > maximumDiscreteTopologyRules || len(problem.ConcurrencyRules) > maximumDiscreteConcurrencyRules || len(problem.MaintenanceWindows) > maximumDiscreteMaintenance {
		return errors.New("discrete schedule constraint count is outside bounds")
	}
	laneByID := make(map[string]DiscreteLane, len(problem.Lanes))
	var dimensions []string
	for index, lane := range problem.Lanes {
		if !validSolverToken(lane.ID) || laneByID[lane.ID].ID != "" || len(lane.Capacity) == 0 || len(lane.Capacity) > maximumDiscreteDimensions || len(lane.Topology) > maximumDiscreteTopologyKeys {
			return errors.New("discrete lanes require unique IDs and bounded explicit capacity dimensions and topology")
		}
		keys := sortedInt64MapKeys(lane.Capacity)
		if index == 0 {
			dimensions = keys
		} else if !reflect.DeepEqual(dimensions, keys) {
			return errors.New("every discrete lane must declare the same capacity dimensions")
		}
		for dimension, capacity := range lane.Capacity {
			if !validSolverToken(dimension) || capacity < 1 || capacity > maximumDiscreteQuantity {
				return errors.New("discrete lane capacity must be a bounded positive integer")
			}
		}
		for key, value := range lane.Topology {
			if !validSolverToken(key) || !validSolverToken(value) {
				return errors.New("discrete lane topology requires bounded tokens")
			}
		}
		laneByID[lane.ID] = lane
	}
	taskByID := make(map[string]DiscreteTask, len(problem.Tasks))
	hasCosts := false
	for _, task := range problem.Tasks {
		if !validSolverToken(task.ID) || taskByID[task.ID].ID != "" || task.DurationSeconds < 1 || task.DurationSeconds > problem.HorizonSeconds || task.EarliestStart < 0 || task.LatestFinish > problem.HorizonSeconds || task.LatestFinish-task.EarliestStart < task.DurationSeconds {
			return errors.New("discrete task identity, duration, or execution window is invalid")
		}
		if len(task.EligibleLanes) == 0 || len(task.Demand) != len(dimensions) || !reflect.DeepEqual(sortedInt64MapKeys(task.Demand), dimensions) {
			return errors.New("discrete tasks require explicit eligible lanes and complete demand dimensions")
		}
		seenLanes := make(map[string]bool, len(task.EligibleLanes))
		for _, laneID := range task.EligibleLanes {
			lane, exists := laneByID[laneID]
			if !exists || seenLanes[laneID] {
				return errors.New("discrete task has an unknown or duplicate eligible lane")
			}
			seenLanes[laneID] = true
			for dimension, demand := range task.Demand {
				if demand < 1 || demand > maximumDiscreteQuantity || demand > lane.Capacity[dimension] {
					return errors.New("discrete task demand is invalid or exceeds an eligible lane")
				}
			}
			if cost, exists := task.EstimatedCostMicros[laneID]; exists {
				hasCosts = true
				if cost < 0 || cost > maximumDiscreteQuantity {
					return errors.New("discrete estimated cost must be a bounded nonnegative integer")
				}
			}
		}
		for laneID := range task.EstimatedCostMicros {
			if !seenLanes[laneID] {
				return errors.New("discrete task prices an ineligible lane")
			}
		}
		taskByID[task.ID] = task
	}
	if hasCosts {
		if problem.CostModel == nil || !validCurrency(problem.CostModel.Currency) || problem.CostModel.Source == "" {
			return errors.New("discrete costs require an explicit currency and source")
		}
		if _, err := time.Parse(time.RFC3339, problem.CostModel.AsOf); err != nil {
			return errors.New("discrete cost model asOf must be RFC3339")
		}
	}
	if len(objective.Secondary) == 1 {
		if problem.CostModel == nil {
			return errors.New("estimated-cost objective requires a cost model")
		}
		for _, task := range problem.Tasks {
			if len(task.EstimatedCostMicros) != len(task.EligibleLanes) {
				return errors.New("estimated-cost objective requires every eligible task/lane price")
			}
		}
	}
	if err := validateDiscretePrecedences(problem, taskByID); err != nil {
		return err
	}
	if err := validateDiscreteTopologyRules(problem, taskByID, laneByID); err != nil {
		return err
	}
	if err := validateDiscreteConcurrencyRules(problem, taskByID); err != nil {
		return err
	}
	for _, window := range problem.MaintenanceWindows {
		if laneByID[window.Lane].ID == "" || window.Start < 0 || window.End <= window.Start || window.End > problem.HorizonSeconds {
			return errors.New("discrete maintenance window is invalid")
		}
	}
	return nil
}

func validateDiscretePrecedences(problem DiscreteScheduleProblem, tasks map[string]DiscreteTask) error {
	indegree := make(map[string]int, len(tasks))
	edges := make(map[string][]string, len(tasks))
	seen := make(map[string]bool, len(problem.Precedences))
	for _, edge := range problem.Precedences {
		key := edge.Before + "\x00" + edge.After
		if tasks[edge.Before].ID == "" || tasks[edge.After].ID == "" || edge.Before == edge.After || edge.LagSeconds < 0 || edge.LagSeconds > problem.HorizonSeconds || seen[key] {
			return errors.New("discrete precedence is invalid or duplicated")
		}
		seen[key] = true
		edges[edge.Before] = append(edges[edge.Before], edge.After)
		indegree[edge.After]++
	}
	queue := make([]string, 0, len(tasks))
	for task := range tasks {
		if indegree[task] == 0 {
			queue = append(queue, task)
		}
	}
	sort.Strings(queue)
	visited := 0
	for len(queue) > 0 {
		task := queue[0]
		queue = queue[1:]
		visited++
		for _, after := range edges[task] {
			indegree[after]--
			if indegree[after] == 0 {
				queue = append(queue, after)
				sort.Strings(queue)
			}
		}
	}
	if visited != len(tasks) {
		return errors.New("discrete precedence graph contains a cycle")
	}
	return nil
}

func validateDiscreteTopologyRules(problem DiscreteScheduleProblem, tasks map[string]DiscreteTask, lanes map[string]DiscreteLane) error {
	seenRules := make(map[string]bool, len(problem.TopologyRules))
	for _, rule := range problem.TopologyRules {
		if !validSolverToken(rule.ID) || seenRules[rule.ID] || !validSolverToken(rule.Key) || (rule.Relation != "SAME" && rule.Relation != "DIFFERENT") || len(rule.Tasks) < 2 {
			return errors.New("discrete topology rule is invalid")
		}
		seenRules[rule.ID] = true
		seenTasks := make(map[string]bool, len(rule.Tasks))
		for _, taskID := range rule.Tasks {
			if tasks[taskID].ID == "" || seenTasks[taskID] {
				return errors.New("discrete topology rule has an unknown or duplicate task")
			}
			seenTasks[taskID] = true
		}
		for _, lane := range lanes {
			if lane.Topology[rule.Key] == "" {
				return errors.New("discrete topology evidence is incomplete for a required key")
			}
		}
	}
	return nil
}

func validateDiscreteConcurrencyRules(problem DiscreteScheduleProblem, tasks map[string]DiscreteTask) error {
	seenRules := make(map[string]bool, len(problem.ConcurrencyRules))
	for _, rule := range problem.ConcurrencyRules {
		if !validSolverToken(rule.ID) || seenRules[rule.ID] || (rule.Kind != "CONCURRENCY" && rule.Kind != "PDB_DISRUPTION") || rule.Maximum < 1 || rule.Maximum >= len(rule.Tasks) || len(rule.Tasks) < 2 {
			return errors.New("discrete concurrency rule is invalid")
		}
		seenRules[rule.ID] = true
		seenTasks := make(map[string]bool, len(rule.Tasks))
		for _, taskID := range rule.Tasks {
			if tasks[taskID].ID == "" || seenTasks[taskID] {
				return errors.New("discrete concurrency rule has an unknown or duplicate task")
			}
			seenTasks[taskID] = true
		}
	}
	return nil
}

func deriveDiscreteScheduleResult(problem DiscreteScheduleProblem, result *DiscreteScheduleResult) error {
	tasks := make(map[string]DiscreteTask, len(problem.Tasks))
	lanes := make(map[string]DiscreteLane, len(problem.Lanes))
	for _, task := range problem.Tasks {
		tasks[task.ID] = task
	}
	for _, lane := range problem.Lanes {
		lanes[lane.ID] = lane
	}
	assigned := make(map[string]DiscreteAssignment, len(result.Assignments))
	totalCost := int64(0)
	allCostsKnown := problem.CostModel != nil
	for index := range result.Assignments {
		assignment := &result.Assignments[index]
		task, exists := tasks[assignment.Task]
		if !exists || assigned[assignment.Task].Task != "" || lanes[assignment.Lane].ID == "" || !containsString(task.EligibleLanes, assignment.Lane) {
			return errors.New("discrete assignment is unknown, duplicated, or ineligible")
		}
		if assignment.Start < task.EarliestStart || assignment.Start > task.LatestFinish-task.DurationSeconds || assignment.Finish != assignment.Start+task.DurationSeconds {
			return errors.New("discrete assignment violates its duration or execution window")
		}
		for _, window := range problem.MaintenanceWindows {
			if window.Lane == assignment.Lane && intervalsOverlap(assignment.Start, assignment.Finish, window.Start, window.End) {
				return errors.New("discrete assignment overlaps lane maintenance")
			}
		}
		cost, known := task.EstimatedCostMicros[assignment.Lane]
		if known {
			if assignment.EstimatedCostMicros == nil || *assignment.EstimatedCostMicros != cost || cost > math.MaxInt64-totalCost {
				return errors.New("discrete assignment changes or overflows estimated cost")
			}
			totalCost += cost
		} else {
			allCostsKnown = false
			if assignment.EstimatedCostMicros != nil {
				return errors.New("discrete assignment invents estimated cost")
			}
		}
		assigned[assignment.Task] = *assignment
		result.MakespanSeconds = max(result.MakespanSeconds, assignment.Finish)
	}
	if len(assigned) != len(tasks) {
		return errors.New("discrete result must assign every task exactly once")
	}
	for _, edge := range problem.Precedences {
		if assigned[edge.After].Start < assigned[edge.Before].Finish+edge.LagSeconds {
			return errors.New("discrete assignment violates precedence")
		}
	}
	if err := verifyDiscreteTopology(problem, assigned, lanes); err != nil {
		return err
	}
	if err := verifyDiscreteCapacity(problem, assigned); err != nil {
		return err
	}
	if err := verifyDiscreteConcurrency(problem, assigned); err != nil {
		return err
	}
	if allCostsKnown {
		result.EstimatedTotalCostMicros = &totalCost
	} else {
		result.EstimatedTotalCostMicros = nil
	}
	return nil
}

func verifyDiscreteTopology(problem DiscreteScheduleProblem, assigned map[string]DiscreteAssignment, lanes map[string]DiscreteLane) error {
	for _, rule := range problem.TopologyRules {
		values := make(map[string]bool, len(rule.Tasks))
		for _, taskID := range rule.Tasks {
			value := lanes[assigned[taskID].Lane].Topology[rule.Key]
			values[value] = true
		}
		if rule.Relation == "SAME" && len(values) != 1 {
			return errors.New("discrete assignment violates same-topology rule")
		}
		if rule.Relation == "DIFFERENT" && len(values) != len(rule.Tasks) {
			return errors.New("discrete assignment violates different-topology rule")
		}
	}
	return nil
}

func verifyDiscreteCapacity(problem DiscreteScheduleProblem, assigned map[string]DiscreteAssignment) error {
	taskByID := make(map[string]DiscreteTask, len(problem.Tasks))
	for _, task := range problem.Tasks {
		taskByID[task.ID] = task
	}
	for _, lane := range problem.Lanes {
		for dimension, capacity := range lane.Capacity {
			events := make(map[int64]int64)
			for taskID, assignment := range assigned {
				if assignment.Lane != lane.ID {
					continue
				}
				demand := taskByID[taskID].Demand[dimension]
				events[assignment.Start] += demand
				events[assignment.Finish] -= demand
			}
			if maximumSweepLoad(events) > capacity {
				return fmt.Errorf("discrete assignment exceeds %s capacity on lane %s", dimension, lane.ID)
			}
		}
	}
	return nil
}

func verifyDiscreteConcurrency(problem DiscreteScheduleProblem, assigned map[string]DiscreteAssignment) error {
	for _, rule := range problem.ConcurrencyRules {
		events := make(map[int64]int64, len(rule.Tasks)*2)
		for _, taskID := range rule.Tasks {
			assignment := assigned[taskID]
			events[assignment.Start]++
			events[assignment.Finish]--
		}
		if maximumSweepLoad(events) > int64(rule.Maximum) {
			return fmt.Errorf("discrete assignment exceeds %s rule %s", rule.Kind, rule.ID)
		}
	}
	return nil
}

func maximumSweepLoad(events map[int64]int64) int64 {
	times := make([]int64, 0, len(events))
	for instant := range events {
		times = append(times, instant)
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	load, maximum := int64(0), int64(0)
	for _, instant := range times {
		load += events[instant]
		maximum = max(maximum, load)
	}
	return maximum
}

func intervalsOverlap(leftStart, leftEnd, rightStart, rightEnd int64) bool {
	return leftStart < rightEnd && rightStart < leftEnd
}

func canonicalDiscreteRecommendation(request DiscreteRecommendationRequest) DiscreteRecommendationRequest {
	request.Constraints = append([]ConstraintEvidence(nil), request.Constraints...)
	sort.Slice(request.Constraints, func(i, j int) bool { return request.Constraints[i].ID < request.Constraints[j].ID })
	request.Objective.Secondary = append([]string(nil), request.Objective.Secondary...)
	request.Problem = canonicalDiscreteProblem(request.Problem)
	return request
}

func canonicalDiscreteProblem(problem DiscreteScheduleProblem) DiscreteScheduleProblem {
	problem.Tasks = append([]DiscreteTask(nil), problem.Tasks...)
	for index := range problem.Tasks {
		problem.Tasks[index].EligibleLanes = append([]string(nil), problem.Tasks[index].EligibleLanes...)
		sort.Strings(problem.Tasks[index].EligibleLanes)
		problem.Tasks[index].Demand = cloneInt64Map(problem.Tasks[index].Demand)
		problem.Tasks[index].EstimatedCostMicros = cloneInt64Map(problem.Tasks[index].EstimatedCostMicros)
	}
	sort.Slice(problem.Tasks, func(i, j int) bool { return problem.Tasks[i].ID < problem.Tasks[j].ID })
	problem.Lanes = append([]DiscreteLane(nil), problem.Lanes...)
	for index := range problem.Lanes {
		problem.Lanes[index].Capacity = cloneInt64Map(problem.Lanes[index].Capacity)
		problem.Lanes[index].Topology = cloneStringMap(problem.Lanes[index].Topology)
	}
	sort.Slice(problem.Lanes, func(i, j int) bool { return problem.Lanes[i].ID < problem.Lanes[j].ID })
	problem.Precedences = append([]DiscretePrecedence(nil), problem.Precedences...)
	sort.Slice(problem.Precedences, func(i, j int) bool {
		if problem.Precedences[i].Before == problem.Precedences[j].Before {
			return problem.Precedences[i].After < problem.Precedences[j].After
		}
		return problem.Precedences[i].Before < problem.Precedences[j].Before
	})
	problem.TopologyRules = append([]DiscreteTopologyRule(nil), problem.TopologyRules...)
	for index := range problem.TopologyRules {
		problem.TopologyRules[index].Tasks = append([]string(nil), problem.TopologyRules[index].Tasks...)
		sort.Strings(problem.TopologyRules[index].Tasks)
	}
	sort.Slice(problem.TopologyRules, func(i, j int) bool { return problem.TopologyRules[i].ID < problem.TopologyRules[j].ID })
	problem.ConcurrencyRules = append([]DiscreteConcurrencyRule(nil), problem.ConcurrencyRules...)
	for index := range problem.ConcurrencyRules {
		problem.ConcurrencyRules[index].Tasks = append([]string(nil), problem.ConcurrencyRules[index].Tasks...)
		sort.Strings(problem.ConcurrencyRules[index].Tasks)
	}
	sort.Slice(problem.ConcurrencyRules, func(i, j int) bool { return problem.ConcurrencyRules[i].ID < problem.ConcurrencyRules[j].ID })
	problem.MaintenanceWindows = append([]DiscreteMaintenanceWindow(nil), problem.MaintenanceWindows...)
	sort.Slice(problem.MaintenanceWindows, func(i, j int) bool {
		if problem.MaintenanceWindows[i].Lane == problem.MaintenanceWindows[j].Lane {
			if problem.MaintenanceWindows[i].Start == problem.MaintenanceWindows[j].Start {
				return problem.MaintenanceWindows[i].End < problem.MaintenanceWindows[j].End
			}
			return problem.MaintenanceWindows[i].Start < problem.MaintenanceWindows[j].Start
		}
		return problem.MaintenanceWindows[i].Lane < problem.MaintenanceWindows[j].Lane
	})
	problem.CostModel = cloneCostModel(problem.CostModel)
	return problem
}

func cloneInt64Map(source map[string]int64) map[string]int64 {
	if source == nil {
		return nil
	}
	result := make(map[string]int64, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneCostModel(source *CostModel) *CostModel {
	if source == nil {
		return nil
	}
	result := *source
	return &result
}

func sortedInt64MapKeys(values map[string]int64) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func discreteSolverRequestID(request DiscreteSolverRequest) (string, error) {
	request.ID = ""
	digest, err := digestJSON(request)
	if err != nil {
		return "", err
	}
	return "dsolreq_" + digest[len("sha256:"):], nil
}

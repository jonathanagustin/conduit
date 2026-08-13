package optimization

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"google.golang.org/protobuf/encoding/protowire"
)

const (
	cpSatStatusUnknown         = 0
	cpSatStatusModelInvalid    = 1
	cpSatStatusFeasible        = 2
	cpSatStatusInfeasible      = 3
	cpSatStatusOptimal         = 4
	maximumCpSatVariables      = 200_000
	maximumCpSatConstraints    = 500_000
	maximumCpSatTopologyChecks = 2_000_000
	maximumCpSatModelBytes     = 8 << 20
	maximumCpSatPhases         = maximumDiscreteTasks + 2
	cpSatCanonicalRevision     = "task-lane-start.v1"
	// CP-SAT's validator reserves half the int64 range for safe domain and
	// linear-expression arithmetic.
	maximumCpSatIntegerMagnitude = int64(math.MaxInt64 / 2)
)

type cpSatTaskBinding struct {
	Task         string
	Start        int32
	End          int32
	LaneBooleans map[string]int32
}

type cpSatCompiledModel struct {
	Model                 []byte
	Parameters            []byte
	Bindings              []cpSatTaskBinding
	VariableCount         int
	MakespanVariable      int32
	Phase                 cpSatPhaseDescriptor
	ObjectiveVariables    []int32
	ObjectiveCoefficients []int64
}

type cpSatPhaseDescriptor struct {
	Index int
	Kind  string
	Key   string
	Unit  string
}

type cpSatPhasePlanner struct {
	request          DiscreteSolverRequest
	builder          *cpSatProtoBuilder
	parameters       []byte
	bindings         []cpSatTaskBinding
	makespanVariable int32
	phases           []cpSatPhaseDescriptor
	nextPhase        int
	fixed            []cpSatPhaseFix
	current          *cpSatCompiledModel
}

type cpSatPhaseFix struct {
	Descriptor cpSatPhaseDescriptor
	Value      int64
}

type cpSatResponse struct {
	Status                   int32
	Solution                 []int64
	InnerObjectiveLowerBound *int64
}

type cpSatProtoBuilder struct {
	variables   [][]byte
	domains     [][2]int64
	constraints [][]byte
	objective   []byte
	err         error
}

func newCpSatPhasePlanner(request DiscreteSolverRequest) (*cpSatPhasePlanner, error) {
	if err := VerifyDiscreteSolverRequest(request); err != nil {
		return nil, err
	}
	if request.Determinism.Seed != 0 || request.Determinism.Workers != 1 {
		return nil, errors.New("CP-SAT candidate requires seed zero and exactly one worker")
	}
	problem := request.Recommendation.Problem
	builder := &cpSatProtoBuilder{}
	bindings := make([]cpSatTaskBinding, 0, len(problem.Tasks))
	byTask := make(map[string]*cpSatTaskBinding, len(problem.Tasks))
	for _, task := range problem.Tasks {
		binding := cpSatTaskBinding{
			Task:         task.ID,
			Start:        builder.addVariable(task.EarliestStart, task.LatestFinish-task.DurationSeconds),
			End:          builder.addVariable(task.EarliestStart+task.DurationSeconds, task.LatestFinish),
			LaneBooleans: make(map[string]int32, len(task.EligibleLanes)),
		}
		for _, lane := range task.EligibleLanes {
			binding.LaneBooleans[lane] = builder.addVariable(0, 1)
		}
		bindings = append(bindings, binding)
		byTask[task.ID] = &bindings[len(bindings)-1]
	}

	// Constraint indexes, not variable indexes, identify intervals. A mandatory
	// interval supports availability limits; optional per-lane intervals support
	// resource capacity. All interval indexes are fixed before other constraints.
	mandatoryIntervals := make(map[string]int32, len(problem.Tasks))
	optionalIntervals := make(map[string]map[string]int32, len(problem.Tasks))
	for _, task := range problem.Tasks {
		binding := byTask[task.ID]
		mandatoryIntervals[task.ID] = builder.addInterval(nil, binding.Start, binding.End, task.DurationSeconds)
		optionalIntervals[task.ID] = make(map[string]int32, len(task.EligibleLanes))
		for _, lane := range task.EligibleLanes {
			literal := binding.LaneBooleans[lane]
			optionalIntervals[task.ID][lane] = builder.addInterval([]int32{literal}, binding.Start, binding.End, task.DurationSeconds)
		}
	}

	for _, task := range problem.Tasks {
		binding := byTask[task.ID]
		builder.addLinear(nil, []int32{binding.End, binding.Start}, []int64{1, -1}, task.DurationSeconds, task.DurationSeconds)
		literals := make([]int32, 0, len(task.EligibleLanes))
		for _, lane := range task.EligibleLanes {
			literals = append(literals, binding.LaneBooleans[lane])
		}
		builder.addBooleanConstraint(29, nil, literals) // exactly_one
	}

	for _, edge := range problem.Precedences {
		before, after := byTask[edge.Before], byTask[edge.After]
		builder.addLinear(nil, []int32{after.Start, before.End}, []int64{1, -1}, edge.LagSeconds, problem.HorizonSeconds)
	}

	for _, lane := range problem.Lanes {
		for _, dimension := range sortedInt64MapKeys(lane.Capacity) {
			capacity := lane.Capacity[dimension]
			intervals := make([]int32, 0, len(problem.Tasks))
			demands := make([]int64, 0, len(problem.Tasks))
			for _, task := range problem.Tasks {
				interval, eligible := optionalIntervals[task.ID][lane.ID]
				if eligible {
					intervals = append(intervals, interval)
					demands = append(demands, task.Demand[dimension])
				}
			}
			builder.addCumulative(intervals, demands, capacity)
		}
	}

	for _, rule := range problem.ConcurrencyRules {
		intervals := make([]int32, 0, len(rule.Tasks))
		demands := make([]int64, 0, len(rule.Tasks))
		for _, task := range rule.Tasks {
			intervals = append(intervals, mandatoryIntervals[task])
			demands = append(demands, 1)
		}
		builder.addCumulative(intervals, demands, int64(rule.Maximum))
	}

	if err := validateCpSatTopologyExpansion(problem, byTask); err != nil {
		return nil, err
	}
	for _, rule := range problem.TopologyRules {
		for leftIndex := 0; leftIndex < len(rule.Tasks); leftIndex++ {
			for rightIndex := leftIndex + 1; rightIndex < len(rule.Tasks); rightIndex++ {
				left, right := byTask[rule.Tasks[leftIndex]], byTask[rule.Tasks[rightIndex]]
				for _, leftLane := range sortedCpSatLaneIDs(left.LaneBooleans) {
					leftLiteral := left.LaneBooleans[leftLane]
					leftValue := discreteLaneTopology(problem.Lanes, leftLane, rule.Key)
					for _, rightLane := range sortedCpSatLaneIDs(right.LaneBooleans) {
						rightLiteral := right.LaneBooleans[rightLane]
						rightValue := discreteLaneTopology(problem.Lanes, rightLane, rule.Key)
						forbidden := (rule.Relation == "SAME" && leftValue != rightValue) || (rule.Relation == "DIFFERENT" && leftValue == rightValue)
						if forbidden {
							builder.addBooleanConstraint(3, nil, []int32{negatedCpSatLiteral(leftLiteral), negatedCpSatLiteral(rightLiteral)})
							if builder.err != nil {
								return nil, builder.err
							}
						}
					}
				}
			}
		}
	}

	for _, window := range problem.MaintenanceWindows {
		for _, task := range problem.Tasks {
			binding := byTask[task.ID]
			selected, eligible := binding.LaneBooleans[window.Lane]
			if !eligible {
				continue
			}
			before, after := builder.addVariable(0, 1), builder.addVariable(0, 1)
			builder.addBooleanConstraint(3, []int32{selected}, []int32{before, after})
			builder.addLinear([]int32{selected, before}, []int32{binding.End}, []int64{1}, 0, window.Start)
			builder.addLinear([]int32{selected, after}, []int32{binding.Start}, []int64{1}, window.End, problem.HorizonSeconds)
		}
	}

	makespan := builder.addVariable(0, problem.HorizonSeconds)
	for _, binding := range bindings {
		builder.addLinear(nil, []int32{makespan, binding.End}, []int64{1, -1}, 0, problem.HorizonSeconds)
	}

	parameters, err := encodeCpSatParameters(request)
	if err != nil {
		return nil, err
	}
	phases := []cpSatPhaseDescriptor{{Index: 0, Kind: "OBJECTIVE", Key: "minimize_makespan", Unit: "seconds"}}
	if hasObjective(request.Recommendation.Objective.Secondary, "minimize_estimated_cost") {
		phases = append(phases, cpSatPhaseDescriptor{Index: len(phases), Kind: "OBJECTIVE", Key: "minimize_estimated_cost", Unit: "micros"})
	}
	for _, task := range problem.Tasks {
		phases = append(phases, cpSatPhaseDescriptor{Index: len(phases), Kind: "CANONICAL_CHOICE", Key: task.ID, Unit: "lane_then_start"})
	}
	if len(phases) > maximumCpSatPhases {
		return nil, errors.New("compiled CP-SAT phase plan exceeds its phase bound")
	}
	return &cpSatPhasePlanner{
		request: request, builder: builder, parameters: append([]byte(nil), parameters...), bindings: cloneCpSatBindings(bindings),
		makespanVariable: makespan, phases: phases,
	}, nil
}

// compileCpSatModel preserves the narrow first-phase helper used by static
// model tests. Runtime execution uses the planner and must fix every exact phase
// before compiling the next one.
func compileCpSatModel(request DiscreteSolverRequest) (cpSatCompiledModel, error) {
	planner, err := newCpSatPhasePlanner(request)
	if err != nil {
		return cpSatCompiledModel{}, err
	}
	return planner.compileNextPhase()
}

func (planner *cpSatPhasePlanner) compileNextPhase() (cpSatCompiledModel, error) {
	if planner == nil || planner.builder == nil || planner.nextPhase >= len(planner.phases) || planner.current != nil {
		return cpSatCompiledModel{}, errors.New("CP-SAT phase plan cannot compile another phase")
	}
	descriptor := planner.phases[planner.nextPhase]
	variables, coefficients, err := planner.phaseObjective(descriptor)
	if err != nil {
		return cpSatCompiledModel{}, err
	}
	planner.builder.setObjective(variables, coefficients)
	if planner.builder.err != nil {
		return cpSatCompiledModel{}, planner.builder.err
	}
	model := planner.builder.encode()
	if len(model) == 0 || len(model) > maximumCpSatModelBytes {
		return cpSatCompiledModel{}, errors.New("compiled CP-SAT model exceeds its byte bound")
	}
	compiled := cpSatCompiledModel{
		Model: model, Parameters: append([]byte(nil), planner.parameters...), Bindings: cloneCpSatBindings(planner.bindings),
		VariableCount: len(planner.builder.variables), MakespanVariable: planner.makespanVariable, Phase: descriptor,
		ObjectiveVariables: append([]int32(nil), variables...), ObjectiveCoefficients: append([]int64(nil), coefficients...),
	}
	planner.current = &compiled
	return compiled, nil
}

func (planner *cpSatPhasePlanner) phaseObjective(descriptor cpSatPhaseDescriptor) ([]int32, []int64, error) {
	problem := planner.request.Recommendation.Problem
	switch descriptor.Kind {
	case "OBJECTIVE":
		switch descriptor.Key {
		case "minimize_makespan":
			return []int32{planner.makespanVariable}, []int64{1}, nil
		case "minimize_estimated_cost":
			variables := make([]int32, 0)
			coefficients := make([]int64, 0)
			for taskIndex, task := range problem.Tasks {
				binding := planner.bindings[taskIndex]
				for _, lane := range task.EligibleLanes {
					variables = append(variables, binding.LaneBooleans[lane])
					coefficients = append(coefficients, task.EstimatedCostMicros[lane])
				}
			}
			return variables, coefficients, nil
		}
	case "CANONICAL_CHOICE":
		for taskIndex, task := range problem.Tasks {
			if task.ID != descriptor.Key {
				continue
			}
			binding := planner.bindings[taskIndex]
			variables := []int32{binding.Start}
			coefficients := []int64{1}
			scale := problem.HorizonSeconds + 1
			lanes := append([]string(nil), task.EligibleLanes...)
			sort.Strings(lanes)
			for rank, lane := range lanes {
				coefficient := int64(rank) * scale
				if coefficient < 0 || coefficient > maximumCpSatIntegerMagnitude {
					return nil, nil, errors.New("CP-SAT canonical choice coefficient exceeds its safe integer magnitude")
				}
				variables = append(variables, binding.LaneBooleans[lane])
				coefficients = append(coefficients, coefficient)
			}
			return variables, coefficients, nil
		}
	}
	return nil, nil, errors.New("CP-SAT phase plan contains an unsupported descriptor")
}

func (planner *cpSatPhasePlanner) acceptExactPhase(compiled cpSatCompiledModel, value int64) error {
	if planner == nil || planner.current == nil || planner.nextPhase >= len(planner.phases) || compiled.Phase != planner.phases[planner.nextPhase] || planner.current.Phase != compiled.Phase || !withinCpSatMagnitude(value) {
		return errors.New("CP-SAT phase fix does not match the current plan")
	}
	planner.builder.addLinear(nil, compiled.ObjectiveVariables, compiled.ObjectiveCoefficients, value, value)
	if planner.builder.err != nil {
		return planner.builder.err
	}
	planner.fixed = append(planner.fixed, cpSatPhaseFix{Descriptor: compiled.Phase, Value: value})
	planner.nextPhase++
	planner.current = nil
	return nil
}

func (planner *cpSatPhasePlanner) complete() bool {
	return planner != nil && planner.nextPhase == len(planner.phases) && planner.current == nil
}

func (planner *cpSatPhasePlanner) validateFixedPrefix(result DiscreteScheduleResult) error {
	for _, fixed := range planner.fixed {
		value, err := cpSatPhaseValueFromResult(planner.request, fixed.Descriptor, result)
		if err != nil || value != fixed.Value {
			return errors.New("CP-SAT solution contradicts an exact earlier phase fix")
		}
	}
	return nil
}

func cpSatPhaseValue(compiled cpSatCompiledModel, response cpSatResponse, request DiscreteSolverRequest, result DiscreteScheduleResult) (int64, error) {
	if len(response.Solution) != compiled.VariableCount || len(compiled.ObjectiveVariables) != len(compiled.ObjectiveCoefficients) {
		return 0, errors.New("CP-SAT phase response does not match its objective")
	}
	activity := int64(0)
	for index, variable := range compiled.ObjectiveVariables {
		if variable < 0 || int(variable) >= len(response.Solution) {
			return 0, errors.New("CP-SAT phase objective references an unknown response variable")
		}
		value, coefficient := response.Solution[variable], compiled.ObjectiveCoefficients[index]
		if value < 0 || coefficient < 0 || (value != 0 && coefficient > (maximumCpSatIntegerMagnitude-activity)/value) {
			return 0, errors.New("CP-SAT phase objective response exceeds its safe integer magnitude")
		}
		activity += value * coefficient
	}
	want, err := cpSatPhaseValueFromResult(request, compiled.Phase, result)
	if err != nil || activity != want {
		return 0, errors.New("CP-SAT phase objective contradicts the independently reconstructed result")
	}
	return activity, nil
}

func cpSatPhaseValueFromResult(request DiscreteSolverRequest, descriptor cpSatPhaseDescriptor, result DiscreteScheduleResult) (int64, error) {
	switch descriptor.Kind {
	case "OBJECTIVE":
		switch descriptor.Key {
		case "minimize_makespan":
			return result.MakespanSeconds, nil
		case "minimize_estimated_cost":
			if result.EstimatedTotalCostMicros == nil {
				return 0, errors.New("CP-SAT cost phase requires complete cost")
			}
			return *result.EstimatedTotalCostMicros, nil
		}
	case "CANONICAL_CHOICE":
		var assignment DiscreteAssignment
		for _, candidate := range result.Assignments {
			if candidate.Task == descriptor.Key {
				assignment = candidate
				break
			}
		}
		if assignment.Task == "" {
			return 0, errors.New("CP-SAT canonical phase lacks its task assignment")
		}
		for _, task := range request.Recommendation.Problem.Tasks {
			if task.ID != descriptor.Key {
				continue
			}
			lanes := append([]string(nil), task.EligibleLanes...)
			sort.Strings(lanes)
			for rank, lane := range lanes {
				if lane == assignment.Lane {
					return int64(rank)*(request.Recommendation.Problem.HorizonSeconds+1) + assignment.Start, nil
				}
			}
		}
	}
	return 0, errors.New("CP-SAT phase result does not match its descriptor")
}

func validateCpSatTopologyExpansion(problem DiscreteScheduleProblem, bindings map[string]*cpSatTaskBinding) error {
	checks := int64(0)
	for _, rule := range problem.TopologyRules {
		for leftIndex := 0; leftIndex < len(rule.Tasks); leftIndex++ {
			left := int64(len(bindings[rule.Tasks[leftIndex]].LaneBooleans))
			for rightIndex := leftIndex + 1; rightIndex < len(rule.Tasks); rightIndex++ {
				right := int64(len(bindings[rule.Tasks[rightIndex]].LaneBooleans))
				if left != 0 && right > (maximumCpSatTopologyChecks-checks)/left {
					return errors.New("compiled CP-SAT topology expansion exceeds its operation bound")
				}
				checks += left * right
			}
		}
	}
	return nil
}

func choicesFromCpSatResponse(compiled cpSatCompiledModel, response cpSatResponse) ([]DiscreteChoice, error) {
	if len(response.Solution) != compiled.VariableCount {
		return nil, errors.New("CP-SAT response solution length does not match the compiled variable map")
	}
	choices := make([]DiscreteChoice, 0, len(compiled.Bindings))
	for _, binding := range compiled.Bindings {
		selected := ""
		for lane, variable := range binding.LaneBooleans {
			value := response.Solution[variable]
			if value != 0 && value != 1 {
				return nil, errors.New("CP-SAT response contains a non-Boolean lane choice")
			}
			if value == 1 {
				if selected != "" {
					return nil, errors.New("CP-SAT response selects multiple lanes for one task")
				}
				selected = lane
			}
		}
		if selected == "" {
			return nil, errors.New("CP-SAT response omits a task lane choice")
		}
		choices = append(choices, DiscreteChoice{Task: binding.Task, Lane: selected, Start: response.Solution[binding.Start]})
	}
	sort.Slice(choices, func(i, j int) bool { return choices[i].Task < choices[j].Task })
	return choices, nil
}

func parseCpSatResponse(encoded []byte, maximumVariables int) (cpSatResponse, error) {
	if len(encoded) == 0 || len(encoded) > maximumCpSatResponseBytes || maximumVariables < 1 {
		return cpSatResponse{}, errors.New("CP-SAT response is empty or outside its byte bound")
	}
	response := cpSatResponse{}
	statusSeen, boundSeen := false, false
	for len(encoded) > 0 {
		number, wireType, consumed := protowire.ConsumeTag(encoded)
		if consumed < 0 {
			return cpSatResponse{}, errors.New("CP-SAT response has an invalid field tag")
		}
		encoded = encoded[consumed:]
		switch number {
		case 1:
			if wireType != protowire.VarintType || statusSeen {
				return cpSatResponse{}, errors.New("CP-SAT response has duplicate or mistyped status")
			}
			value, size := protowire.ConsumeVarint(encoded)
			if size < 0 || value > cpSatStatusOptimal {
				return cpSatResponse{}, errors.New("CP-SAT response has an unknown status")
			}
			response.Status, statusSeen, encoded = int32(value), true, encoded[size:]
		case 2:
			var err error
			response.Solution, encoded, err = consumeCpSatSolution(response.Solution, encoded, wireType, maximumVariables)
			if err != nil {
				return cpSatResponse{}, err
			}
		case 29:
			if wireType != protowire.VarintType || boundSeen {
				return cpSatResponse{}, errors.New("CP-SAT response has duplicate or mistyped inner bound")
			}
			value, size := protowire.ConsumeVarint(encoded)
			if size < 0 {
				return cpSatResponse{}, errors.New("CP-SAT response has an invalid inner bound")
			}
			parsed := int64(value)
			response.InnerObjectiveLowerBound, boundSeen, encoded = &parsed, true, encoded[size:]
		default:
			size := protowire.ConsumeFieldValue(number, wireType, encoded)
			if size < 0 {
				return cpSatResponse{}, errors.New("CP-SAT response has an invalid field value")
			}
			encoded = encoded[size:]
		}
	}
	// UNKNOWN is the proto3 default and is therefore legitimately absent from
	// an otherwise non-empty serialized response.
	return response, nil
}

func consumeCpSatSolution(current []int64, encoded []byte, wireType protowire.Type, maximum int) ([]int64, []byte, error) {
	switch wireType {
	case protowire.VarintType:
		value, size := protowire.ConsumeVarint(encoded)
		if size < 0 || len(current) >= maximum {
			return nil, nil, errors.New("CP-SAT response solution exceeds its variable bound")
		}
		return append(current, int64(value)), encoded[size:], nil
	case protowire.BytesType:
		packed, size := protowire.ConsumeBytes(encoded)
		if size < 0 {
			return nil, nil, errors.New("CP-SAT response has malformed packed solution values")
		}
		for len(packed) > 0 {
			value, valueSize := protowire.ConsumeVarint(packed)
			if valueSize < 0 || len(current) >= maximum {
				return nil, nil, errors.New("CP-SAT response solution exceeds its variable bound")
			}
			current, packed = append(current, int64(value)), packed[valueSize:]
		}
		return current, encoded[size:], nil
	default:
		return nil, nil, errors.New("CP-SAT response solution has the wrong wire type")
	}
}

func encodeCpSatParameters(request DiscreteSolverRequest) ([]byte, error) {
	memoryMiB := request.Limits.MemoryLimitBytes / (1 << 20)
	if memoryMiB < 1 {
		memoryMiB = 1
	}
	if memoryMiB > math.MaxInt32 {
		return nil, errors.New("CP-SAT memory limit exceeds its exact parameter domain")
	}
	parameters := make([]byte, 0, 48)
	parameters = appendVarintField(parameters, 31, uint64(int64(request.Determinism.Seed)))
	parameters = appendFixed64Field(parameters, 36, math.Float64bits(float64(request.Limits.TimeLimitMilliseconds)/1000))
	parameters = appendVarintField(parameters, 40, uint64(memoryMiB))
	parameters = appendFixed64Field(parameters, 159, math.Float64bits(0))
	parameters = appendFixed64Field(parameters, 160, math.Float64bits(0))
	parameters = appendVarintField(parameters, 206, uint64(request.Determinism.Workers))
	return parameters, nil
}

func maximumDiscreteSecondaryCost(problem DiscreteScheduleProblem) (int64, error) {
	total := int64(0)
	for _, task := range problem.Tasks {
		maximum := int64(0)
		for _, lane := range task.EligibleLanes {
			maximum = max(maximum, task.EstimatedCostMicros[lane])
		}
		if maximum > math.MaxInt64-total {
			return 0, errors.New("discrete maximum secondary cost overflows int64")
		}
		total += maximum
	}
	return total, nil
}

func discreteLaneTopology(lanes []DiscreteLane, laneID, key string) string {
	for _, lane := range lanes {
		if lane.ID == laneID {
			return lane.Topology[key]
		}
	}
	return ""
}

func cloneCpSatBindings(source []cpSatTaskBinding) []cpSatTaskBinding {
	result := make([]cpSatTaskBinding, len(source))
	for index, binding := range source {
		result[index] = binding
		result[index].LaneBooleans = make(map[string]int32, len(binding.LaneBooleans))
		for lane, variable := range binding.LaneBooleans {
			result[index].LaneBooleans[lane] = variable
		}
	}
	return result
}

func sortedCpSatLaneIDs(values map[string]int32) []string {
	result := make([]string, 0, len(values))
	for lane := range values {
		result = append(result, lane)
	}
	sort.Strings(result)
	return result
}

func negatedCpSatLiteral(variable int32) int32 { return -variable - 1 }

func (builder *cpSatProtoBuilder) addVariable(minimum, maximum int64) int32 {
	if builder.err != nil {
		return 0
	}
	if len(builder.variables) >= maximumCpSatVariables {
		builder.err = errors.New("compiled CP-SAT model exceeds its variable bound")
		return 0
	}
	if minimum > maximum || !withinCpSatMagnitude(minimum) || !withinCpSatMagnitude(maximum) {
		builder.err = errors.New("compiled CP-SAT variable domain exceeds its safe integer magnitude")
		return 0
	}
	variable := appendPackedInt64(nil, 2, []int64{minimum, maximum})
	builder.variables = append(builder.variables, variable)
	builder.domains = append(builder.domains, [2]int64{minimum, maximum})
	return int32(len(builder.variables) - 1)
}

func (builder *cpSatProtoBuilder) addInterval(enforcement []int32, start, end int32, duration int64) int32 {
	if builder.err != nil {
		return 0
	}
	if len(builder.constraints) >= maximumCpSatConstraints {
		builder.err = errors.New("compiled CP-SAT model exceeds its constraint bound")
		return 0
	}
	if !builder.validLiterals(enforcement) || !builder.validVariables([]int32{start, end}) || duration < 0 || !withinCpSatMagnitude(duration) {
		builder.err = errors.New("compiled CP-SAT interval exceeds its safe integer contract")
		return 0
	}
	interval := appendMessageField(nil, 4, linearExpression([]int32{start}, []int64{1}, 0))
	interval = appendMessageField(interval, 5, linearExpression([]int32{end}, []int64{1}, 0))
	interval = appendMessageField(interval, 6, linearExpression(nil, nil, duration))
	constraint := appendPackedInt32(nil, 2, enforcement)
	constraint = appendMessageField(constraint, 19, interval)
	builder.constraints = append(builder.constraints, constraint)
	return int32(len(builder.constraints) - 1)
}

func (builder *cpSatProtoBuilder) addLinear(enforcement, variables []int32, coefficients []int64, minimum, maximum int64) {
	if builder.err != nil {
		return
	}
	if len(builder.constraints) >= maximumCpSatConstraints {
		builder.err = errors.New("compiled CP-SAT model exceeds its constraint bound")
		return
	}
	if minimum > maximum || !withinCpSatMagnitude(minimum) || !withinCpSatMagnitude(maximum) || !builder.validLiterals(enforcement) {
		builder.err = errors.New("compiled CP-SAT linear constraint has an unsafe domain")
		return
	}
	if err := builder.validateLinearActivity(variables, coefficients); err != nil {
		builder.err = err
		return
	}
	linear := appendPackedInt32(nil, 1, variables)
	linear = appendPackedInt64(linear, 2, coefficients)
	linear = appendPackedInt64(linear, 3, []int64{minimum, maximum})
	constraint := appendPackedInt32(nil, 2, enforcement)
	constraint = appendMessageField(constraint, 12, linear)
	builder.constraints = append(builder.constraints, constraint)
}

func (builder *cpSatProtoBuilder) addBooleanConstraint(field protowire.Number, enforcement, literals []int32) {
	if builder.err != nil {
		return
	}
	if len(builder.constraints) >= maximumCpSatConstraints {
		builder.err = errors.New("compiled CP-SAT model exceeds its constraint bound")
		return
	}
	if !builder.validLiterals(enforcement) || !builder.validLiterals(literals) {
		builder.err = errors.New("compiled CP-SAT Boolean constraint references an unknown variable")
		return
	}
	argument := appendPackedInt32(nil, 1, literals)
	constraint := appendPackedInt32(nil, 2, enforcement)
	constraint = appendMessageField(constraint, field, argument)
	builder.constraints = append(builder.constraints, constraint)
}

func (builder *cpSatProtoBuilder) addCumulative(intervals []int32, demands []int64, capacity int64) {
	if builder.err != nil {
		return
	}
	if len(builder.constraints) >= maximumCpSatConstraints {
		builder.err = errors.New("compiled CP-SAT model exceeds its constraint bound")
		return
	}
	if len(intervals) != len(demands) || capacity < 0 || !withinCpSatMagnitude(capacity) {
		builder.err = errors.New("compiled CP-SAT cumulative constraint has an unsafe domain")
		return
	}
	totalDemand := int64(0)
	for index, demand := range demands {
		if intervals[index] < 0 || int(intervals[index]) >= len(builder.constraints) || demand < 0 || demand > maximumCpSatIntegerMagnitude-totalDemand {
			builder.err = errors.New("compiled CP-SAT cumulative activity exceeds its safe integer magnitude")
			return
		}
		totalDemand += demand
	}
	cumulative := appendMessageField(nil, 1, linearExpression(nil, nil, capacity))
	cumulative = appendPackedInt32(cumulative, 2, intervals)
	for _, demand := range demands {
		cumulative = appendMessageField(cumulative, 3, linearExpression(nil, nil, demand))
	}
	builder.constraints = append(builder.constraints, appendMessageField(nil, 22, cumulative))
}

func (builder *cpSatProtoBuilder) setObjective(variables []int32, coefficients []int64) {
	if builder.err != nil {
		return
	}
	if err := builder.validateLinearActivity(variables, coefficients); err != nil {
		builder.err = fmt.Errorf("compiled CP-SAT objective: %w", err)
		return
	}
	objective := appendPackedInt32(nil, 1, variables)
	objective = appendPackedInt64(objective, 4, coefficients)
	builder.objective = objective
}

func withinCpSatMagnitude(value int64) bool {
	return value >= -maximumCpSatIntegerMagnitude && value <= maximumCpSatIntegerMagnitude
}

func (builder *cpSatProtoBuilder) validVariables(variables []int32) bool {
	for _, variable := range variables {
		if variable < 0 || int(variable) >= len(builder.domains) {
			return false
		}
	}
	return true
}

func (builder *cpSatProtoBuilder) validLiterals(literals []int32) bool {
	for _, literal := range literals {
		variable := literal
		if literal < 0 {
			variable = -literal - 1
		}
		if variable < 0 || int(variable) >= len(builder.domains) {
			return false
		}
	}
	return true
}

func (builder *cpSatProtoBuilder) validateLinearActivity(variables []int32, coefficients []int64) error {
	if len(variables) != len(coefficients) || !builder.validVariables(variables) {
		return errors.New("linear expression has mismatched or unknown variables")
	}
	activity := int64(0)
	for index, coefficient := range coefficients {
		coefficientMagnitude, ok := cpSatMagnitude(coefficient)
		if !ok {
			return errors.New("linear coefficient exceeds its safe integer magnitude")
		}
		domain := builder.domains[variables[index]]
		minimumMagnitude, _ := cpSatMagnitude(domain[0])
		maximumMagnitude, _ := cpSatMagnitude(domain[1])
		domainMagnitude := max(minimumMagnitude, maximumMagnitude)
		if coefficientMagnitude != 0 && domainMagnitude > (maximumCpSatIntegerMagnitude-activity)/coefficientMagnitude {
			return errors.New("linear activity exceeds its safe integer magnitude")
		}
		activity += coefficientMagnitude * domainMagnitude
	}
	return nil
}

func cpSatMagnitude(value int64) (int64, bool) {
	if value == math.MinInt64 {
		return 0, false
	}
	if value < 0 {
		value = -value
	}
	return value, value <= maximumCpSatIntegerMagnitude
}

func (builder *cpSatProtoBuilder) encode() []byte {
	encoded := make([]byte, 0, len(builder.variables)*16+len(builder.constraints)*24)
	for _, variable := range builder.variables {
		encoded = appendMessageField(encoded, 2, variable)
	}
	for _, constraint := range builder.constraints {
		encoded = appendMessageField(encoded, 3, constraint)
	}
	return appendMessageField(encoded, 4, builder.objective)
}

func linearExpression(variables []int32, coefficients []int64, offset int64) []byte {
	encoded := appendPackedInt32(nil, 1, variables)
	encoded = appendPackedInt64(encoded, 2, coefficients)
	if offset != 0 {
		encoded = appendVarintField(encoded, 3, uint64(offset))
	}
	return encoded
}

func appendMessageField(destination []byte, number protowire.Number, value []byte) []byte {
	destination = protowire.AppendTag(destination, number, protowire.BytesType)
	return protowire.AppendBytes(destination, value)
}

func appendVarintField(destination []byte, number protowire.Number, value uint64) []byte {
	destination = protowire.AppendTag(destination, number, protowire.VarintType)
	return protowire.AppendVarint(destination, value)
}

func appendFixed64Field(destination []byte, number protowire.Number, value uint64) []byte {
	destination = protowire.AppendTag(destination, number, protowire.Fixed64Type)
	return protowire.AppendFixed64(destination, value)
}

func appendPackedInt32(destination []byte, number protowire.Number, values []int32) []byte {
	if len(values) == 0 {
		return destination
	}
	packed := make([]byte, 0, len(values)*2)
	for _, value := range values {
		packed = protowire.AppendVarint(packed, uint64(int64(value)))
	}
	return appendMessageField(destination, number, packed)
}

func appendPackedInt64(destination []byte, number protowire.Number, values []int64) []byte {
	if len(values) == 0 {
		return destination
	}
	packed := make([]byte, 0, len(values)*2)
	for _, value := range values {
		packed = protowire.AppendVarint(packed, uint64(value))
	}
	return appendMessageField(destination, number, packed)
}

func cpSatStatusName(status int32) (string, error) {
	switch status {
	case cpSatStatusUnknown:
		return "UNKNOWN", nil
	case cpSatStatusModelInvalid:
		return "MODEL_INVALID", nil
	case cpSatStatusFeasible:
		return "FEASIBLE", nil
	case cpSatStatusInfeasible:
		return "INFEASIBLE", nil
	case cpSatStatusOptimal:
		return "OPTIMAL", nil
	default:
		return "", fmt.Errorf("unknown CP-SAT status %d", status)
	}
}

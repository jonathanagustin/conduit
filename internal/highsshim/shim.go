// Package highsshim implements the deliberately narrow stdin/stdout adapter
// between Platform's compiler-owned allocation model and HiGHS.
package highsshim

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"time"

	optimization "github.com/jonathanagustin/conduit/internal/optimization/solverprotocol"
)

const (
	maximumInputBytes      = 8 << 20
	primalFeasibleStatus   = 2
	binaryExtractionWindow = 1e-7
)

// MatrixModel is the exact in-memory payload passed to the native engine. It
// contains no request metadata, credentials, file names, or transport state.
type MatrixModel struct {
	ColumnCost        []float64
	ColumnLower       []float64
	ColumnUpper       []float64
	RowLower          []float64
	RowUpper          []float64
	ColumnStarts      []int32
	RowIndices        []int32
	Values            []float64
	Integrality       []int32
	AssignmentColumns []optimization.ExternalSolverAssignmentChoice
}

// EngineResult is raw HiGHS evidence. In particular, a non-nil Columns slice
// is not an incumbent: PrimalSolutionStatus must independently be feasible.
type EngineResult struct {
	ModelStatus          int
	PrimalSolutionStatus int
	ObjectiveValue       float64
	BestBound            float64
	Gap                  float64
	Columns              []float64
}

// Engine is intentionally phase-agnostic. The host compiler and shim own the
// two-phase lexicographic protocol; the engine only solves one immutable MIP.
type Engine interface {
	Solve(MatrixModel, time.Duration) (EngineResult, error)
}

// Run consumes exactly one strict SolverRequest and emits exactly one response
// document. Invalid input writes no bytes. Solver failures use a bounded,
// non-sensitive response rather than forwarding native diagnostics.
func Run(input io.Reader, output io.Writer, engine Engine) error {
	if engine == nil {
		return errors.New("HiGHS engine is required")
	}
	request, err := decodeRequest(input)
	if err != nil {
		return err
	}
	compiled, err := optimization.CompileHiGHSModel(request)
	if err != nil {
		return err
	}
	response := solve(request, compiled, engine)
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(response); err != nil {
		return fmt.Errorf("encode response: %w", err)
	}
	if _, err := output.Write(encoded.Bytes()); err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	return nil
}

func decodeRequest(input io.Reader) (optimization.SolverRequest, error) {
	encoded, err := io.ReadAll(io.LimitReader(input, maximumInputBytes+1))
	if err != nil {
		return optimization.SolverRequest{}, errors.New("read solver request")
	}
	if len(encoded) > maximumInputBytes {
		return optimization.SolverRequest{}, errors.New("solver request exceeds bounded input")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var request optimization.SolverRequest
	if err := decoder.Decode(&request); err != nil {
		return optimization.SolverRequest{}, errors.New("invalid solver request")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return optimization.SolverRequest{}, errors.New("solver request contains trailing JSON")
	}
	if err := optimization.VerifySolverRequest(request); err != nil {
		return optimization.SolverRequest{}, errors.New("unverified solver request")
	}
	return request, nil
}

func solve(request optimization.SolverRequest, compiled optimization.HiGHSCompiledModel, engine Engine) optimization.ExternalSolverProcessResponse {
	started := time.Now()
	primaryModel, err := buildMatrix(compiled, 0, nil)
	if err != nil {
		return errorResponse(request, "model_error", "compiled HiGHS model is invalid")
	}
	primary, err := engine.Solve(primaryModel, time.Duration(request.Limits.TimeLimitMilliseconds)*time.Millisecond)
	if err != nil {
		return errorResponse(request, "engine_error", "HiGHS engine failed")
	}
	primaryOutcome := interpretPhase(request, compiled, primary, false)
	if len(compiled.Objectives) == 1 || primaryOutcome.termination != optimization.SolverOptimal {
		return primaryOutcome.response(request)
	}

	remaining := time.Duration(request.Limits.TimeLimitMilliseconds)*time.Millisecond - time.Since(started)
	if remaining <= 0 {
		primaryOutcome.termination = optimization.SolverTimeLimit
		primaryOutcome.explanation = "HiGHS time limit elapsed before the secondary objective"
		return primaryOutcome.response(request)
	}
	upper := math.Nextafter(primaryOutcome.makespan, math.Inf(1))
	secondaryModel, err := buildMatrix(compiled, 1, &upper)
	if err != nil {
		return errorResponse(request, "model_error", "compiled HiGHS secondary model is invalid")
	}
	secondary, err := engine.Solve(secondaryModel, remaining)
	if err != nil {
		return errorResponse(request, "engine_error", "HiGHS engine failed")
	}
	secondaryOutcome := interpretPhase(request, compiled, secondary, true)
	if secondaryOutcome.assignments == nil && secondaryOutcome.termination != optimization.SolverError {
		// The primary optimum is itself a valid incumbent for the constrained
		// secondary model. Preserve it when HiGHS stops before improving or
		// reporting a second-phase primal solution.
		secondaryOutcome.assignments = primaryOutcome.assignments
		secondaryOutcome.makespan = primaryOutcome.makespan
		if secondaryOutcome.termination != optimization.SolverTimeLimit {
			secondaryOutcome.termination = optimization.SolverFeasible
			secondaryOutcome.explanation = "HiGHS secondary phase stopped; preserving the proved primary incumbent"
		}
	}
	if secondaryOutcome.assignments != nil && secondaryOutcome.makespan > upper {
		return errorResponse(request, statusName(secondary.ModelStatus), "HiGHS secondary incumbent violates the proved primary optimum")
	}
	// A secondary solve is meaningful only under the already proved primary
	// optimum. Preserve that proof as the ordered prefix required by v1alpha3.
	if secondaryOutcome.assignments != nil {
		secondaryOutcome.proofs = append(append([]optimization.SolverObjectiveProof(nil), primaryOutcome.proofs...), secondaryOutcome.proofs...)
	}
	if secondaryOutcome.termination == optimization.SolverOptimal && len(secondaryOutcome.proofs) != 2 {
		secondaryOutcome.termination = optimization.SolverFeasible
		secondaryOutcome.explanation = "HiGHS returned an incumbent without complete exact objective proofs"
	}
	return secondaryOutcome.response(request)
}

type phaseOutcome struct {
	termination optimization.SolverTermination
	status      string
	assignments *[]optimization.ExternalSolverAssignmentChoice
	proofs      []optimization.SolverObjectiveProof
	explanation string
	makespan    float64
}

func (outcome phaseOutcome) response(request optimization.SolverRequest) optimization.ExternalSolverProcessResponse {
	evidence := &optimization.SolverBackendReportedEvidence{Status: outcome.status, Objectives: outcome.proofs}
	return optimization.ExternalSolverProcessResponse{
		APIVersion: optimization.ExternalSolverProcessAPIVersion,
		Kind:       "ExternalSolverProcessResponse", RequestID: request.ID, ModelDigest: request.Model.Digest,
		Termination: outcome.termination, BackendReportedEvidence: evidence,
		Assignments: outcome.assignments, Explanation: outcome.explanation,
	}
}

func errorResponse(request optimization.SolverRequest, status, explanation string) optimization.ExternalSolverProcessResponse {
	return phaseOutcome{termination: optimization.SolverError, status: status, explanation: explanation}.response(request)
}

func interpretPhase(request optimization.SolverRequest, compiled optimization.HiGHSCompiledModel, raw EngineResult, secondary bool) phaseOutcome {
	status := statusName(raw.ModelStatus)
	outcome := phaseOutcome{status: status}
	if raw.PrimalSolutionStatus == primalFeasibleStatus {
		choices, err := extractAssignments(compiled, raw.Columns)
		if err != nil {
			return phaseOutcome{termination: optimization.SolverError, status: status, explanation: "HiGHS emitted an invalid incumbent"}
		}
		outcome.assignments = &choices
		outcome.makespan, err = recomputeMakespan(request.Recommendation.Problem, choices)
		if err != nil {
			return phaseOutcome{termination: optimization.SolverError, status: status, explanation: "HiGHS incumbent does not bind the compiled allocation"}
		}
		if secondary {
			if proof, ok := exactCostProof(request, compiled, choices, raw); ok {
				outcome.proofs = []optimization.SolverObjectiveProof{proof}
			}
		} else if proof, ok := exactMakespanProof(outcome.makespan, raw); ok {
			outcome.proofs = []optimization.SolverObjectiveProof{proof}
		}
	}

	switch raw.ModelStatus {
	case 7: // kOptimal
		if outcome.assignments == nil {
			outcome.termination = optimization.SolverError
			outcome.explanation = "HiGHS optimal status lacks a feasible incumbent"
		} else if len(outcome.proofs) == 1 {
			outcome.termination = optimization.SolverOptimal
		} else {
			outcome.termination = optimization.SolverFeasible
			outcome.explanation = "HiGHS incumbent lacks an exact objective proof"
		}
	case 13: // kTimeLimit
		outcome.termination = optimization.SolverTimeLimit
		outcome.explanation = "HiGHS reached the configured time limit"
	case 8, 9, 10: // structurally impossible for allocation.makespan.v1
		outcome.assignments, outcome.proofs = nil, nil
		outcome.termination = optimization.SolverError
		outcome.explanation = "HiGHS contradicted the feasible bounded allocation model"
	case 11, 12, 14, 16, 17:
		if outcome.assignments != nil {
			outcome.termination = optimization.SolverFeasible
			outcome.explanation = "HiGHS stopped with a feasible incumbent"
		} else {
			outcome.termination = optimization.SolverUnknown
			outcome.explanation = "HiGHS stopped without a feasible incumbent"
		}
	case 15:
		if outcome.assignments != nil {
			outcome.termination = optimization.SolverFeasible
			outcome.explanation = "HiGHS returned an incumbent with unknown model status"
		} else {
			outcome.termination = optimization.SolverUnknown
			outcome.explanation = "HiGHS model status is unknown"
		}
	case 18:
		outcome.assignments, outcome.proofs = nil, nil
		outcome.termination = optimization.SolverError
		outcome.explanation = "HiGHS stopped in a fail-closed runtime state"
	case 19:
		if outcome.assignments != nil {
			outcome.termination = optimization.SolverFeasible
			outcome.explanation = "HiGHS internal interrupt retained a verified incumbent"
		} else {
			outcome.termination = optimization.SolverUnknown
			outcome.explanation = "HiGHS internal interrupt produced no feasible incumbent"
		}
	default: // not-set, load/model/presolve/solve/postsolve error, empty, foreign
		outcome.assignments, outcome.proofs = nil, nil
		outcome.termination = optimization.SolverError
		outcome.explanation = "HiGHS did not produce an admissible allocation result"
	}
	return outcome
}

func statusName(status int) string {
	names := [...]string{
		"kNotset", "kLoadError", "kModelError", "kPresolveError", "kSolveError",
		"kPostsolveError", "kModelEmpty", "kOptimal", "kInfeasible",
		"kUnboundedOrInfeasible", "kUnbounded", "kObjectiveBound",
		"kObjectiveTarget", "kTimeLimit", "kIterationLimit", "kUnknown",
		"kSolutionLimit", "kInterrupt", "kMemoryLimit", "kHighsInterrupt",
	}
	if status >= 0 && status < len(names) {
		return names[status]
	}
	return "kForeignStatus"
}

func exactMakespanProof(makespan float64, raw EngineResult) (optimization.SolverObjectiveProof, bool) {
	if !finite(raw.ObjectiveValue) || !finite(raw.BestBound) || raw.ObjectiveValue != makespan || raw.BestBound > makespan {
		return optimization.SolverObjectiveProof{}, false
	}
	gap := makespan - raw.BestBound
	if gap < 0 || !finite(gap) {
		return optimization.SolverObjectiveProof{}, false
	}
	return optimization.SolverObjectiveProof{
		Objective: "minimize_makespan", Unit: "normalized_load_ratio",
		Incumbent: exactFloat(makespan), BestBound: exactFloat(raw.BestBound), Gap: exactFloat(gap),
	}, true
}

func exactCostProof(request optimization.SolverRequest, compiled optimization.HiGHSCompiledModel, choices []optimization.ExternalSolverAssignmentChoice, raw EngineResult) (optimization.SolverObjectiveProof, bool) {
	if compiled.CostNormalization == nil || !finite(raw.ObjectiveValue) || !finite(raw.BestBound) || raw.ObjectiveValue != math.Trunc(raw.ObjectiveValue) || raw.BestBound != math.Trunc(raw.BestBound) {
		return optimization.SolverObjectiveProof{}, false
	}
	incumbent, ok := recomputeCost(request.Recommendation.Problem, choices)
	if !ok {
		return optimization.SolverObjectiveProof{}, false
	}
	normalization := compiled.CostNormalization
	if raw.ObjectiveValue < 0 || raw.ObjectiveValue > float64(normalization.WorstCaseNormalized) || raw.BestBound < 0 || raw.BestBound > raw.ObjectiveValue {
		return optimization.SolverObjectiveProof{}, false
	}
	normalizedIncumbent := int64(raw.ObjectiveValue)
	if normalization.GlobalPositiveGCDMicros > 0 && normalizedIncumbent > (math.MaxInt64-normalization.OffsetMicros)/normalization.GlobalPositiveGCDMicros {
		return optimization.SolverObjectiveProof{}, false
	}
	proofIncumbent := normalization.OffsetMicros + normalizedIncumbent*normalization.GlobalPositiveGCDMicros
	if proofIncumbent != incumbent {
		return optimization.SolverObjectiveProof{}, false
	}
	normalizedBound := int64(raw.BestBound)
	proofBound := normalization.OffsetMicros + normalizedBound*normalization.GlobalPositiveGCDMicros
	return optimization.SolverObjectiveProof{
		Objective: "minimize_estimated_cost", Unit: "micros",
		Incumbent: strconv.FormatInt(incumbent, 10), BestBound: strconv.FormatInt(proofBound, 10), Gap: strconv.FormatInt(incumbent-proofBound, 10),
	}, true
}

func buildMatrix(compiled optimization.HiGHSCompiledModel, objectiveIndex int, primaryUpper *float64) (MatrixModel, error) {
	if objectiveIndex < 0 || objectiveIndex >= len(compiled.Objectives) {
		return MatrixModel{}, errors.New("objective index out of range")
	}
	model := MatrixModel{
		ColumnCost: make([]float64, len(compiled.Columns)), ColumnLower: make([]float64, len(compiled.Columns)),
		ColumnUpper: make([]float64, len(compiled.Columns)), Integrality: make([]int32, len(compiled.Columns)),
	}
	for index, column := range compiled.Columns {
		model.ColumnLower[index], model.ColumnUpper[index] = column.LowerBound, column.UpperBound
		switch column.Kind {
		case optimization.HiGHSBinaryColumn:
			model.Integrality[index] = 1
			model.AssignmentColumns = append(model.AssignmentColumns, optimization.ExternalSolverAssignmentChoice{Item: column.Item, Resource: column.Resource})
		case optimization.HiGHSContinuousColumn:
		case "":
			return MatrixModel{}, errors.New("column kind is empty")
		default:
			return MatrixModel{}, errors.New("unsupported column kind")
		}
	}
	for _, coefficient := range compiled.Objectives[objectiveIndex].Coefficients {
		if coefficient.Column < 0 || coefficient.Column >= len(model.ColumnCost) || !finite(coefficient.Value) {
			return MatrixModel{}, errors.New("objective coefficient is invalid")
		}
		model.ColumnCost[coefficient.Column] = coefficient.Value
	}
	rows := append([]optimization.HiGHSRow(nil), compiled.Rows...)
	if primaryUpper != nil {
		if !finite(*primaryUpper) || len(compiled.Columns) == 0 {
			return MatrixModel{}, errors.New("primary optimum bound is invalid")
		}
		rows = append(rows, optimization.HiGHSRow{
			Name: "primary-optimum", Sense: "less_or_equal", RightHandSide: *primaryUpper,
			Coefficients: []optimization.HiGHSCoefficient{{Column: len(compiled.Columns) - 1, Value: 1}},
		})
	}
	model.RowLower = make([]float64, len(rows))
	model.RowUpper = make([]float64, len(rows))
	byColumn := make([][]struct {
		row   int32
		value float64
	}, len(compiled.Columns))
	for rowIndex, row := range rows {
		if !finite(row.RightHandSide) {
			return MatrixModel{}, errors.New("row bound is invalid")
		}
		switch row.Sense {
		case "equal":
			model.RowLower[rowIndex], model.RowUpper[rowIndex] = row.RightHandSide, row.RightHandSide
		case "less_or_equal":
			model.RowLower[rowIndex], model.RowUpper[rowIndex] = math.Inf(-1), row.RightHandSide
		default:
			return MatrixModel{}, errors.New("row sense is invalid")
		}
		for _, coefficient := range row.Coefficients {
			if coefficient.Column < 0 || coefficient.Column >= len(byColumn) || !finite(coefficient.Value) {
				return MatrixModel{}, errors.New("matrix coefficient is invalid")
			}
			byColumn[coefficient.Column] = append(byColumn[coefficient.Column], struct {
				row   int32
				value float64
			}{int32(rowIndex), coefficient.Value})
		}
	}
	model.ColumnStarts = make([]int32, len(compiled.Columns)+1)
	for column := range byColumn {
		model.ColumnStarts[column] = int32(len(model.RowIndices))
		for _, entry := range byColumn[column] {
			model.RowIndices = append(model.RowIndices, entry.row)
			model.Values = append(model.Values, entry.value)
		}
	}
	model.ColumnStarts[len(compiled.Columns)] = int32(len(model.RowIndices))
	return model, nil
}

func extractAssignments(compiled optimization.HiGHSCompiledModel, values []float64) ([]optimization.ExternalSolverAssignmentChoice, error) {
	if len(values) != len(compiled.Columns) {
		return nil, errors.New("solution column count differs from model")
	}
	selected := make(map[string]optimization.ExternalSolverAssignmentChoice)
	for index, column := range compiled.Columns {
		if column.Kind != optimization.HiGHSBinaryColumn {
			continue
		}
		value := values[index]
		if !finite(value) || (math.Abs(value) > binaryExtractionWindow && math.Abs(value-1) > binaryExtractionWindow) {
			return nil, errors.New("ambiguous binary solution value")
		}
		if math.Abs(value-1) <= binaryExtractionWindow {
			if _, exists := selected[column.Item]; exists {
				return nil, errors.New("multiple assignments for item")
			}
			selected[column.Item] = optimization.ExternalSolverAssignmentChoice{Item: column.Item, Resource: column.Resource}
		}
	}
	choices := make([]optimization.ExternalSolverAssignmentChoice, 0, len(selected))
	for _, choice := range selected {
		choices = append(choices, choice)
	}
	sort.Slice(choices, func(i, j int) bool {
		if choices[i].Item == choices[j].Item {
			return choices[i].Resource < choices[j].Resource
		}
		return choices[i].Item < choices[j].Item
	})
	items := map[string]struct{}{}
	for _, column := range compiled.Columns {
		if column.Kind == optimization.HiGHSBinaryColumn {
			items[column.Item] = struct{}{}
		}
	}
	if len(choices) != len(items) {
		return nil, errors.New("solution does not assign every item exactly once")
	}
	return choices, nil
}

func recomputeMakespan(problem optimization.Problem, choices []optimization.ExternalSolverAssignmentChoice) (float64, error) {
	items := make(map[string]optimization.WorkItem, len(problem.Items))
	resources := make(map[string]optimization.Resource, len(problem.Resources))
	for _, item := range problem.Items {
		items[item.ID] = item
	}
	for _, resource := range problem.Resources {
		resources[resource.ID] = resource
	}
	loads := make(map[string]float64, len(resources))
	seen := make(map[string]struct{}, len(choices))
	for _, choice := range choices {
		item, itemOK := items[choice.Item]
		resource, resourceOK := resources[choice.Resource]
		if !itemOK || !resourceOK {
			return 0, errors.New("choice is not in problem")
		}
		if _, exists := seen[item.ID]; exists {
			return 0, errors.New("duplicate item choice")
		}
		seen[item.ID] = struct{}{}
		loads[resource.ID] += item.Demand
	}
	if len(seen) != len(items) {
		return 0, errors.New("incomplete choices")
	}
	makespan := 0.0
	for resource, load := range loads {
		makespan = math.Max(makespan, load/resources[resource].Capacity)
	}
	return makespan, nil
}

func recomputeCost(problem optimization.Problem, choices []optimization.ExternalSolverAssignmentChoice) (int64, bool) {
	items := make(map[string]optimization.WorkItem, len(problem.Items))
	for _, item := range problem.Items {
		items[item.ID] = item
	}
	total := int64(0)
	for _, choice := range choices {
		cost, ok := items[choice.Item].EstimatedCostMicros[choice.Resource]
		if !ok || cost < 0 || total > math.MaxInt64-cost {
			return 0, false
		}
		total += cost
	}
	return total, true
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func exactFloat(value float64) string {
	if value == 0 {
		return "0"
	}
	return strconv.FormatFloat(value, 'g', -1, 64)
}

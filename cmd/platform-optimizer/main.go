// platform-optimizer evaluates advisory allocation problems. It does not read
// Platform state, authorize changes, create Plans, or execute assignments.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/jonathanagustin/conduit/internal/optimization"
)

func main() {
	if err := runCommandWithClock(os.Args[1:], os.Stdin, os.Stdout, time.Now); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(2)
	}
}

type scheduleEvaluation struct {
	Recommendation optimization.Recommendation  `json:"recommendation"`
	Outcome        optimization.ScheduleOutcome `json:"outcome"`
}

type costEvaluation struct {
	Recommendation optimization.Recommendation      `json:"recommendation"`
	Outcome        optimization.OutcomeCostEvidence `json:"outcome"`
}

type valueRecordRequest struct {
	StoreDirectory string                          `json:"storeDirectory"`
	SnapshotID     string                          `json:"snapshotId"`
	Criteria       []optimization.ValueCriterion   `json:"criteria"`
	Alternatives   []optimization.ValueAlternative `json:"alternatives"`
}

type valueEvaluation struct {
	Assessment optimization.ValueAssessmentRecord `json:"assessment"`
	Outcome    optimization.ValueOutcomeEvidence  `json:"outcome"`
}

type valuePolicyRequest struct {
	Criteria     []optimization.ValueCriterion   `json:"criteria"`
	Alternatives []optimization.ValueAlternative `json:"alternatives"`
	Policies     []optimization.ValuePolicy      `json:"policies"`
}

type valueScenarioRequest struct {
	Criteria  []optimization.ValueCriterion `json:"criteria"`
	Scenarios []optimization.ValueScenario  `json:"scenarios"`
}

type twinSnapshotRequest struct {
	StoreDirectory string                         `json:"storeDirectory"`
	ValidAt        string                         `json:"validAt"`
	KnownAt        string                         `json:"knownAt"`
	Requirements   []optimization.TwinRequirement `json:"requirements"`
	Facts          []optimization.TemporalFact    `json:"facts"`
}

type doraRequest struct {
	ServiceID   string                             `json:"serviceId"`
	WindowStart string                             `json:"windowStart"`
	WindowEnd   string                             `json:"windowEnd"`
	Deployments []optimization.DeliveryObservation `json:"deployments"`
}

type evaluatorBatchRequest struct {
	StoreDirectory string                            `json:"storeDirectory,omitempty"`
	Runs           []optimization.EvaluatorRun       `json:"runs"`
	Dispositions   []optimization.FindingDisposition `json:"dispositions"`
}

type evaluatorBatchResult struct {
	Batch   optimization.EvaluatorBatch    `json:"batch"`
	Metrics []optimization.EvaluatorMetric `json:"metrics"`
}

type evaluatorResolutionRequest struct {
	StoreDirectory string                            `json:"storeDirectory"`
	BatchID        string                            `json:"batchId"`
	Dispositions   []optimization.FindingDisposition `json:"dispositions"`
}

type evaluatorResolutionResult struct {
	Resolution optimization.EvaluatorResolution `json:"resolution"`
	Metrics    []optimization.EvaluatorMetric   `json:"metrics"`
}

type substratePreflightRecordRequest struct {
	StoreDirectory string                                 `json:"storeDirectory"`
	Environment    string                                 `json:"environment"`
	Proposal       string                                 `json:"proposal"`
	Preflight      optimization.SubstratePreflightRequest `json:"preflight"`
}

type substratePreflightGetRequest struct {
	StoreDirectory string `json:"storeDirectory"`
	ID             string `json:"id"`
}

type substratePreflightCompileRequest struct {
	Observation                     optimization.SubstrateObservationSnapshot `json:"observation"`
	Demand                          optimization.RolloutCapacityDemand        `json:"demand"`
	RequireNetworkPolicyCoverage    bool                                      `json:"requireNetworkPolicyCoverage"`
	RequireNetworkTelemetryCoverage bool                                      `json:"requireNetworkTelemetryCoverage"`
}

type solverReceiptVerification struct {
	Request optimization.SolverRequest `json:"request"`
	Receipt optimization.SolverReceipt `json:"receipt"`
}

type solverCompileRequest struct {
	Recommendation optimization.RecommendationRequest `json:"recommendation"`
	Selection      optimization.SolverSelection       `json:"selection"`
	Determinism    optimization.SolverDeterminism     `json:"determinism"`
	Limits         optimization.SolverLimits          `json:"limits"`
}

func runCommand(args []string, input io.Reader, output io.Writer, now time.Time) error {
	return runCommandWithClock(args, input, output, func() time.Time { return now })
}

func runCommandWithClock(args []string, input io.Reader, output io.Writer, now func() time.Time) error {
	if now == nil {
		return fmt.Errorf("optimizer clock is required")
	}
	if len(args) == 0 {
		return run(input, output)
	}
	if len(args) != 1 {
		return fmt.Errorf("usage: platform-optimizer [recommend|compile-backend-request|solve-backend|verify-backend-receipt|evaluate-schedule|evaluate-cost|evaluate-value|plan-acquisition|compile-substrate-preflight|assess-substrate-preflight|record-substrate-preflight|get-substrate-preflight|assess-value|assess-value-policies|assess-value-scenarios|record-value|assess-experiment|record-twin-snapshot|assess-dora|assess-evaluators|record-evaluators|resolve-evaluators]")
	}
	switch args[0] {
	case "recommend":
		var request optimization.RecommendationRequest
		if err := decodeOne(input, &request); err != nil {
			return err
		}
		recommendation, err := optimization.Recommend(request, now())
		if err != nil {
			return fmt.Errorf("recommend: %w", err)
		}
		return encodeOne(output, recommendation)
	case "compile-backend-request":
		var inputRequest solverCompileRequest
		if err := decodeOne(input, &inputRequest); err != nil {
			return err
		}
		request, err := optimization.NewSolverRequest(inputRequest.Recommendation, inputRequest.Selection, inputRequest.Determinism, inputRequest.Limits)
		if err != nil {
			return fmt.Errorf("compile backend request: %w", err)
		}
		return encodeOne(output, request)
	case "solve-backend":
		var request optimization.SolverRequest
		if err := decodeOne(input, &request); err != nil {
			return err
		}
		executableDigest, err := currentExecutableDigest()
		if err != nil {
			return fmt.Errorf("identify native solver executable: %w", err)
		}
		receipt, err := optimization.RunSolverRequest(context.Background(), request, []optimization.SolverBackend{
			optimization.NativeSolverBackend{ExecutableDigest: executableDigest, Architecture: runtime.GOARCH},
		}, now)
		if err != nil {
			return fmt.Errorf("solve backend request: %w", err)
		}
		return encodeOne(output, receipt)
	case "verify-backend-receipt":
		var verification solverReceiptVerification
		if err := decodeOne(input, &verification); err != nil {
			return err
		}
		if err := optimization.VerifySolverReceipt(verification.Request, verification.Receipt); err != nil {
			return fmt.Errorf("verify backend receipt: %w", err)
		}
		return encodeOne(output, map[string]bool{"valid": true})
	case "evaluate-schedule":
		var evaluation scheduleEvaluation
		if err := decodeOne(input, &evaluation); err != nil {
			return err
		}
		accuracy, err := optimization.MeasureScheduleAccuracy(evaluation.Recommendation, evaluation.Outcome)
		if err != nil {
			return fmt.Errorf("evaluate schedule: %w", err)
		}
		return encodeOne(output, accuracy)
	case "evaluate-cost":
		var evaluation costEvaluation
		if err := decodeOne(input, &evaluation); err != nil {
			return err
		}
		accuracy, err := optimization.MeasureCostAccuracy(evaluation.Recommendation, evaluation.Outcome)
		if err != nil {
			return fmt.Errorf("evaluate cost: %w", err)
		}
		return encodeOne(output, accuracy)
	case "plan-acquisition":
		var request struct {
			Requirements []optimization.StateRequirement `json:"requirements"`
			Calls        []optimization.AcquisitionCall  `json:"calls"`
		}
		if err := decodeOne(input, &request); err != nil {
			return err
		}
		plan, err := optimization.OptimizeAcquisitionCalls(request.Requirements, request.Calls)
		if err != nil {
			return fmt.Errorf("plan acquisition: %w", err)
		}
		return encodeOne(output, plan)
	case "compile-substrate-preflight":
		var request substratePreflightCompileRequest
		if err := decodeOne(input, &request); err != nil {
			return err
		}
		compiled, err := optimization.CompileSubstratePreflight(request.Observation, request.Demand, request.RequireNetworkPolicyCoverage, request.RequireNetworkTelemetryCoverage)
		if err != nil {
			return fmt.Errorf("compile substrate preflight: %w", err)
		}
		return encodeOne(output, compiled)
	case "assess-substrate-preflight":
		var request optimization.SubstratePreflightRequest
		if err := decodeOne(input, &request); err != nil {
			return err
		}
		assessment, err := optimization.AssessSubstratePreflight(request)
		if err != nil {
			return fmt.Errorf("assess substrate preflight: %w", err)
		}
		return encodeOne(output, assessment)
	case "record-substrate-preflight":
		var request substratePreflightRecordRequest
		if err := decodeOne(input, &request); err != nil {
			return err
		}
		record, err := optimization.NewSubstratePreflightRecord(request.Environment, request.Proposal, request.Preflight, now())
		if err != nil {
			return fmt.Errorf("record substrate preflight: %w", err)
		}
		if err := (optimization.FileSubstratePreflightStore{Directory: request.StoreDirectory}).Save(record); err != nil {
			return fmt.Errorf("store substrate preflight: %w", err)
		}
		return encodeOne(output, record)
	case "get-substrate-preflight":
		var request substratePreflightGetRequest
		if err := decodeOne(input, &request); err != nil {
			return err
		}
		record, err := (optimization.FileSubstratePreflightStore{Directory: request.StoreDirectory}).Get(request.ID)
		if err != nil {
			return fmt.Errorf("get substrate preflight: %w", err)
		}
		return encodeOne(output, record)
	case "assess-value":
		var request struct {
			Criteria     []optimization.ValueCriterion   `json:"criteria"`
			Alternatives []optimization.ValueAlternative `json:"alternatives"`
		}
		if err := decodeOne(input, &request); err != nil {
			return err
		}
		assessment, err := optimization.AssessValue(request.Criteria, request.Alternatives)
		if err != nil {
			return fmt.Errorf("assess value: %w", err)
		}
		return encodeOne(output, assessment)
	case "assess-value-policies":
		var request valuePolicyRequest
		if err := decodeOne(input, &request); err != nil {
			return err
		}
		assessment, err := optimization.AssessValuePolicies(request.Criteria, request.Alternatives, request.Policies)
		if err != nil {
			return fmt.Errorf("assess value policies: %w", err)
		}
		return encodeOne(output, assessment)
	case "assess-value-scenarios":
		var request valueScenarioRequest
		if err := decodeOne(input, &request); err != nil {
			return err
		}
		assessment, err := optimization.AssessValueScenarios(request.Criteria, request.Scenarios)
		if err != nil {
			return fmt.Errorf("assess value scenarios: %w", err)
		}
		return encodeOne(output, assessment)
	case "record-value":
		var request valueRecordRequest
		if err := decodeOne(input, &request); err != nil {
			return err
		}
		record, err := optimization.NewValueAssessmentRecord(request.SnapshotID, request.Criteria, request.Alternatives, now())
		if err != nil {
			return fmt.Errorf("record value: %w", err)
		}
		if err := (optimization.FileValueAssessmentStore{Directory: request.StoreDirectory}).Save(record); err != nil {
			return fmt.Errorf("store value: %w", err)
		}
		return encodeOne(output, record)
	case "evaluate-value":
		var evaluation valueEvaluation
		if err := decodeOne(input, &evaluation); err != nil {
			return err
		}
		accuracy, err := optimization.MeasureValueAccuracy(evaluation.Assessment, evaluation.Outcome)
		if err != nil {
			return fmt.Errorf("evaluate value: %w", err)
		}
		return encodeOne(output, accuracy)
	case "assess-experiment":
		var request struct {
			Plan    optimization.ExperimentPlan    `json:"plan"`
			Outcome optimization.ExperimentOutcome `json:"outcome"`
		}
		if err := decodeOne(input, &request); err != nil {
			return err
		}
		decision, err := optimization.AssessExperiment(request.Plan, request.Outcome)
		if err != nil {
			return fmt.Errorf("assess experiment: %w", err)
		}
		return encodeOne(output, decision)
	case "record-twin-snapshot":
		var request twinSnapshotRequest
		if err := decodeOne(input, &request); err != nil {
			return err
		}
		validAt, err := time.Parse(time.RFC3339Nano, request.ValidAt)
		if err != nil {
			return fmt.Errorf("record twin snapshot validAt: %w", err)
		}
		knownAt, err := time.Parse(time.RFC3339Nano, request.KnownAt)
		if err != nil {
			return fmt.Errorf("record twin snapshot knownAt: %w", err)
		}
		snapshot, err := optimization.BuildTwinSnapshot(request.Requirements, request.Facts, validAt, knownAt)
		if err != nil {
			return fmt.Errorf("record twin snapshot: %w", err)
		}
		store := optimization.FileTwinStore{Directory: request.StoreDirectory}
		for _, fact := range request.Facts {
			if err := store.SaveFact(fact); err != nil {
				return fmt.Errorf("store twin fact: %w", err)
			}
		}
		if err := store.SaveSnapshot(snapshot); err != nil {
			return fmt.Errorf("store twin snapshot: %w", err)
		}
		return encodeOne(output, snapshot)
	case "assess-dora":
		var request doraRequest
		if err := decodeOne(input, &request); err != nil {
			return err
		}
		start, err := time.Parse(time.RFC3339Nano, request.WindowStart)
		if err != nil {
			return fmt.Errorf("assess DORA windowStart: %w", err)
		}
		end, err := time.Parse(time.RFC3339Nano, request.WindowEnd)
		if err != nil {
			return fmt.Errorf("assess DORA windowEnd: %w", err)
		}
		assessment, err := optimization.AssessDORA(request.ServiceID, start, end, request.Deployments)
		if err != nil {
			return fmt.Errorf("assess DORA: %w", err)
		}
		return encodeOne(output, assessment)
	case "assess-evaluators", "record-evaluators":
		var request evaluatorBatchRequest
		if err := decodeOne(input, &request); err != nil {
			return err
		}
		runs := make([]optimization.EvaluatorRun, len(request.Runs))
		for i, candidate := range request.Runs {
			run, err := optimization.NewEvaluatorRun(candidate)
			if err != nil {
				return fmt.Errorf("evaluator run %d: %w", i, err)
			}
			runs[i] = run
		}
		batch, err := optimization.NewEvaluatorBatch(runs, request.Dispositions)
		if err != nil {
			return fmt.Errorf("assess evaluators: %w", err)
		}
		metrics, err := optimization.MeasureEvaluatorBatch(batch)
		if err != nil {
			return fmt.Errorf("measure evaluators: %w", err)
		}
		if args[0] == "record-evaluators" {
			store := optimization.FileEvaluatorStore{Directory: request.StoreDirectory}
			for _, run := range runs {
				if err := store.SaveRun(run); err != nil {
					return fmt.Errorf("store evaluator run: %w", err)
				}
			}
			if err := store.SaveBatch(batch); err != nil {
				return fmt.Errorf("store evaluator batch: %w", err)
			}
		}
		return encodeOne(output, evaluatorBatchResult{Batch: batch, Metrics: metrics})
	case "resolve-evaluators":
		var request evaluatorResolutionRequest
		if err := decodeOne(input, &request); err != nil {
			return err
		}
		store := optimization.FileEvaluatorStore{Directory: request.StoreDirectory}
		batch, err := store.GetBatch(request.BatchID)
		if err != nil {
			return fmt.Errorf("load evaluator batch: %w", err)
		}
		resolution, err := optimization.NewEvaluatorResolution(batch, request.Dispositions)
		if err != nil {
			return fmt.Errorf("resolve evaluators: %w", err)
		}
		if err := store.SaveResolution(batch, resolution); err != nil {
			return fmt.Errorf("store evaluator resolution: %w", err)
		}
		resolutions, err := store.ListResolutions(batch)
		if err != nil {
			return fmt.Errorf("list evaluator resolutions: %w", err)
		}
		metrics, err := optimization.MeasureEvaluatorBatchAfterResolutions(batch, resolutions)
		if err != nil {
			return fmt.Errorf("measure resolved evaluators: %w", err)
		}
		return encodeOne(output, evaluatorResolutionResult{Resolution: resolution, Metrics: metrics})
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func currentExecutableDigest() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func run(input io.Reader, output io.Writer) error {
	var problem optimization.Problem
	if err := decodeOne(input, &problem); err != nil {
		return err
	}
	result, err := optimization.Solve(problem)
	if err != nil {
		return fmt.Errorf("solve optimization problem: %w", err)
	}
	return encodeOne(output, result)
}

func decodeOne(input io.Reader, value any) error {
	decoder := json.NewDecoder(io.LimitReader(input, 8<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("decode optimization problem: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decode optimization problem: trailing JSON value")
		}
		return fmt.Errorf("decode optimization problem: %w", err)
	}
	return nil
}

func encodeOne(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("encode optimization result: %w", err)
	}
	return nil
}

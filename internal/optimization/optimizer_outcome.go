package optimization

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"sort"
	"time"
)

const AdvisoryOutcomeAPIVersion = "optimization.platform.example/advisory-outcome/v1alpha1"

const (
	OutcomeMeasurementKnown   = "KNOWN"
	OutcomeMeasurementUnknown = "UNKNOWN"
)

type OptimizerPlanExecutionEvidence struct {
	PlanID             string `json:"planId"`
	Status             string `json:"status"`
	Revision           uint64 `json:"revision"`
	StartedAt          string `json:"startedAt"`
	FinishedAt         string `json:"finishedAt"`
	TaskCount          int    `json:"taskCount"`
	SucceededTaskCount int    `json:"succeededTaskCount"`
	FailedTaskCount    int    `json:"failedTaskCount"`
}

type OptimizerObservedAssignment struct {
	WorkID     string `json:"workId"`
	Target     string `json:"target"`
	StartedAt  string `json:"startedAt"`
	FinishedAt string `json:"finishedAt"`
}

type OptimizerScheduleOutcomeEvidence struct {
	Source      string                        `json:"source"`
	Revision    string                        `json:"revision"`
	ObservedAt  string                        `json:"observedAt"`
	Assignments []OptimizerObservedAssignment `json:"assignments"`
}

type OptimizerValueOutcomeEvidence struct {
	Assessment ValueAssessmentRecord `json:"assessment"`
	Outcome    ValueOutcomeEvidence  `json:"outcome"`
}

type OptimizerOutcomeMeasurement struct {
	Status        string `json:"status"`
	Unit          string `json:"unit,omitempty"`
	Predicted     string `json:"predicted,omitempty"`
	Realized      string `json:"realized,omitempty"`
	SignedError   string `json:"signedError,omitempty"`
	AbsoluteError string `json:"absoluteError,omitempty"`
	Source        string `json:"source,omitempty"`
	Revision      string `json:"revision,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

// AdvisoryOutcomeRecord closes the immutable evidence chain from proposal to
// governed execution and post-execution observations. Missing schedule,
// authoritative billing, or experienced-value evidence remains UNKNOWN.
type AdvisoryOutcomeRecord struct {
	APIVersion                string                            `json:"apiVersion"`
	Kind                      string                            `json:"kind"`
	ID                        string                            `json:"id"`
	Proposal                  AdvisoryPlanProposal              `json:"proposal"`
	ApprovalRevalidation      ProposalRevalidation              `json:"approvalRevalidation"`
	ApprovalAcquisition       AcquisitionExecutionReceipt       `json:"approvalAcquisition"`
	LastSafePointRevalidation ProposalRevalidation              `json:"lastSafePointRevalidation"`
	LastSafePointAcquisition  AcquisitionExecutionReceipt       `json:"lastSafePointAcquisition"`
	Execution                 OptimizerPlanExecutionEvidence    `json:"execution"`
	ScheduleEvidence          *OptimizerScheduleOutcomeEvidence `json:"scheduleEvidence,omitempty"`
	BilledCostFact            *TemporalFact                     `json:"billedCostFact,omitempty"`
	ValueEvidence             *OptimizerValueOutcomeEvidence    `json:"valueEvidence,omitempty"`
	Schedule                  OptimizerOutcomeMeasurement       `json:"schedule"`
	Cost                      OptimizerOutcomeMeasurement       `json:"cost"`
	Value                     *ValueAccuracy                    `json:"value,omitempty"`
	ValueStatus               string                            `json:"valueStatus"`
	ValueReason               string                            `json:"valueReason,omitempty"`
	Unknowns                  []string                          `json:"unknowns,omitempty"`
	RecordedAt                string                            `json:"recordedAt"`
	MutatedState              bool                              `json:"mutatedState"`
}

func NewAdvisoryOutcomeRecord(
	proposal AdvisoryPlanProposal,
	approval ProposalRevalidation,
	approvalAcquisition AcquisitionExecutionReceipt,
	lastSafePoint ProposalRevalidation,
	lastSafePointAcquisition AcquisitionExecutionReceipt,
	execution OptimizerPlanExecutionEvidence,
	schedule *OptimizerScheduleOutcomeEvidence,
	billedCost *TemporalFact,
	value *OptimizerValueOutcomeEvidence,
	recordedAt time.Time,
) (AdvisoryOutcomeRecord, error) {
	record, err := buildAdvisoryOutcomeRecord(proposal, approval, approvalAcquisition, lastSafePoint, lastSafePointAcquisition, execution, schedule, billedCost, value, recordedAt)
	if err != nil {
		return AdvisoryOutcomeRecord{}, err
	}
	id, err := advisoryOutcomeID(record)
	if err != nil {
		return AdvisoryOutcomeRecord{}, err
	}
	record.ID = id
	return record, nil
}

func VerifyAdvisoryOutcomeRecord(record AdvisoryOutcomeRecord) error {
	if record.APIVersion != AdvisoryOutcomeAPIVersion || record.Kind != "AdvisoryOutcomeRecord" || !validContentID(record.ID, "optout_") || record.MutatedState {
		return errors.New("unsupported or unsafe advisory outcome record")
	}
	recordedAt, err := time.Parse(time.RFC3339Nano, record.RecordedAt)
	if err != nil {
		return errors.New("advisory outcome recordedAt is invalid")
	}
	replayed, err := buildAdvisoryOutcomeRecord(record.Proposal, record.ApprovalRevalidation, record.ApprovalAcquisition, record.LastSafePointRevalidation, record.LastSafePointAcquisition, record.Execution, record.ScheduleEvidence, record.BilledCostFact, record.ValueEvidence, recordedAt)
	if err != nil {
		return err
	}
	replayed.ID, err = advisoryOutcomeID(replayed)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(replayed, record) {
		return errors.New("advisory outcome does not match independent replay")
	}
	return nil
}

func buildAdvisoryOutcomeRecord(
	proposal AdvisoryPlanProposal,
	approval ProposalRevalidation,
	approvalAcquisition AcquisitionExecutionReceipt,
	lastSafePoint ProposalRevalidation,
	lastSafePointAcquisition AcquisitionExecutionReceipt,
	execution OptimizerPlanExecutionEvidence,
	schedule *OptimizerScheduleOutcomeEvidence,
	billedCost *TemporalFact,
	value *OptimizerValueOutcomeEvidence,
	recordedAt time.Time,
) (AdvisoryOutcomeRecord, error) {
	if err := VerifyAdvisoryPlanProposal(proposal); err != nil {
		return AdvisoryOutcomeRecord{}, fmt.Errorf("outcome proposal: %w", err)
	}
	for stage, evidence := range map[string]struct {
		revalidation ProposalRevalidation
		acquisition  AcquisitionExecutionReceipt
	}{
		ProposalRevalidationApproval:      {revalidation: approval, acquisition: approvalAcquisition},
		ProposalRevalidationLastSafePoint: {revalidation: lastSafePoint, acquisition: lastSafePointAcquisition},
	} {
		if err := VerifyAuthoritativeProposalRevalidation(proposal, evidence.revalidation, evidence.acquisition); err != nil {
			return AdvisoryOutcomeRecord{}, fmt.Errorf("outcome %s revalidation: %w", stage, err)
		}
		if evidence.revalidation.Stage != stage || evidence.revalidation.Verdict != ProposalRevalidationReady {
			return AdvisoryOutcomeRecord{}, fmt.Errorf("outcome requires READY %s revalidation", stage)
		}
	}
	if approval.Leaseable || !lastSafePoint.Leaseable {
		return AdvisoryOutcomeRecord{}, errors.New("outcome revalidation leaseability is invalid")
	}
	started, finished, err := validateOptimizerExecution(execution)
	if err != nil {
		return AdvisoryOutcomeRecord{}, err
	}
	lastSafeTime, _ := time.Parse(time.RFC3339Nano, lastSafePoint.EvaluatedAt)
	if started.Before(lastSafeTime) {
		return AdvisoryOutcomeRecord{}, errors.New("optimizer execution predates its last-safe-point revalidation")
	}
	recordedAt = recordedAt.UTC()
	if recordedAt.IsZero() || recordedAt.Before(finished) {
		return AdvisoryOutcomeRecord{}, errors.New("advisory outcome cannot predate execution completion")
	}

	scheduleMeasurement, err := buildOptimizerScheduleMeasurement(proposal, execution, schedule)
	if err != nil {
		return AdvisoryOutcomeRecord{}, err
	}
	costMeasurement, err := buildOptimizerCostMeasurement(proposal, execution, billedCost)
	if err != nil {
		return AdvisoryOutcomeRecord{}, err
	}
	valueStatus, valueReason := OutcomeMeasurementUnknown, "no snapshot-bound experienced-value outcome was supplied"
	var valueAccuracy *ValueAccuracy
	if value != nil {
		if value.Assessment.SnapshotID != proposal.Snapshot.ID {
			return AdvisoryOutcomeRecord{}, errors.New("value assessment is not bound to the proposal snapshot")
		}
		createdAt, _ := time.Parse(time.RFC3339Nano, value.Assessment.CreatedAt)
		if createdAt.After(started) {
			return AdvisoryOutcomeRecord{}, errors.New("value prediction was authored after execution began")
		}
		accuracy, err := MeasureValueAccuracy(value.Assessment, value.Outcome)
		if err != nil {
			return AdvisoryOutcomeRecord{}, fmt.Errorf("measure optimizer value outcome: %w", err)
		}
		observedAt, _ := time.Parse(time.RFC3339Nano, value.Outcome.ObservedAt)
		if observedAt.Before(finished) || observedAt.After(recordedAt) {
			return AdvisoryOutcomeRecord{}, errors.New("experienced-value outcome is outside the post-execution evidence window")
		}
		valueAccuracy = &accuracy
		valueStatus, valueReason = OutcomeMeasurementKnown, ""
	}
	unknowns := []string{}
	if scheduleMeasurement.Status == OutcomeMeasurementUnknown {
		unknowns = append(unknowns, "realized schedule: "+scheduleMeasurement.Reason)
	}
	if costMeasurement.Status == OutcomeMeasurementUnknown {
		unknowns = append(unknowns, "realized cost: "+costMeasurement.Reason)
	}
	if valueStatus == OutcomeMeasurementUnknown {
		unknowns = append(unknowns, "realized value: "+valueReason)
	}
	sort.Strings(unknowns)
	record := AdvisoryOutcomeRecord{
		APIVersion:                AdvisoryOutcomeAPIVersion,
		Kind:                      "AdvisoryOutcomeRecord",
		Proposal:                  proposal,
		ApprovalRevalidation:      approval,
		ApprovalAcquisition:       approvalAcquisition,
		LastSafePointRevalidation: lastSafePoint,
		LastSafePointAcquisition:  lastSafePointAcquisition,
		Execution:                 execution,
		ScheduleEvidence:          cloneScheduleEvidence(schedule),
		BilledCostFact:            cloneTemporalFactPointer(billedCost),
		ValueEvidence:             cloneValueOutcomeEvidence(value),
		Schedule:                  scheduleMeasurement,
		Cost:                      costMeasurement,
		Value:                     valueAccuracy,
		ValueStatus:               valueStatus,
		ValueReason:               valueReason,
		Unknowns:                  unknowns,
		RecordedAt:                recordedAt.Format(time.RFC3339Nano),
	}
	return record, nil
}

func validateOptimizerExecution(execution OptimizerPlanExecutionEvidence) (time.Time, time.Time, error) {
	if !validObservationText(execution.PlanID) || execution.Revision == 0 || execution.TaskCount < 0 || execution.SucceededTaskCount < 0 || execution.FailedTaskCount < 0 || execution.SucceededTaskCount+execution.FailedTaskCount > execution.TaskCount {
		return time.Time{}, time.Time{}, errors.New("optimizer Plan execution evidence is incomplete")
	}
	switch execution.Status {
	case "SUCCEEDED":
		if execution.SucceededTaskCount != execution.TaskCount || execution.FailedTaskCount != 0 {
			return time.Time{}, time.Time{}, errors.New("successful optimizer execution has incomplete task outcomes")
		}
	case "FAILED", "CANCELLED":
	default:
		return time.Time{}, time.Time{}, errors.New("optimizer outcome requires a terminal Plan status")
	}
	started, err := time.Parse(time.RFC3339Nano, execution.StartedAt)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("optimizer execution startedAt is invalid")
	}
	finished, err := time.Parse(time.RFC3339Nano, execution.FinishedAt)
	if err != nil || finished.Before(started) {
		return time.Time{}, time.Time{}, errors.New("optimizer execution finishedAt is invalid")
	}
	return started, finished, nil
}

func buildOptimizerScheduleMeasurement(proposal AdvisoryPlanProposal, execution OptimizerPlanExecutionEvidence, evidence *OptimizerScheduleOutcomeEvidence) (OptimizerOutcomeMeasurement, error) {
	predicted, assignments, err := proposalSchedulePrediction(proposal)
	if err != nil {
		return OptimizerOutcomeMeasurement{}, err
	}
	measurement := OptimizerOutcomeMeasurement{Status: OutcomeMeasurementUnknown, Unit: "seconds", Predicted: predicted.RatString(), Reason: "no complete assignment-level execution evidence was supplied"}
	if evidence == nil {
		return measurement, nil
	}
	if !validObservationText(evidence.Source) || !validObservationText(evidence.Revision) || len(evidence.Assignments) != len(assignments) {
		return OptimizerOutcomeMeasurement{}, errors.New("optimizer schedule outcome is incomplete")
	}
	observedAt, err := time.Parse(time.RFC3339Nano, evidence.ObservedAt)
	if err != nil {
		return OptimizerOutcomeMeasurement{}, errors.New("optimizer schedule outcome observedAt is invalid")
	}
	executionStarted, executionFinished, _ := validateOptimizerExecution(execution)
	want := make(map[string]string, len(assignments))
	for id, target := range assignments {
		want[id] = target
	}
	seen := map[string]bool{}
	var earliest, latest time.Time
	for _, assignment := range evidence.Assignments {
		if seen[assignment.WorkID] || want[assignment.WorkID] != assignment.Target {
			return OptimizerOutcomeMeasurement{}, errors.New("optimizer schedule outcome does not match solver assignments")
		}
		seen[assignment.WorkID] = true
		started, err := time.Parse(time.RFC3339Nano, assignment.StartedAt)
		if err != nil {
			return OptimizerOutcomeMeasurement{}, errors.New("optimizer schedule assignment startedAt is invalid")
		}
		finished, err := time.Parse(time.RFC3339Nano, assignment.FinishedAt)
		if err != nil || finished.Before(started) || started.Before(executionStarted) || finished.After(executionFinished) {
			return OptimizerOutcomeMeasurement{}, errors.New("optimizer schedule assignment is outside the Plan execution window")
		}
		if earliest.IsZero() || started.Before(earliest) {
			earliest = started
		}
		if latest.IsZero() || finished.After(latest) {
			latest = finished
		}
	}
	if len(seen) != len(want) || observedAt.Before(latest) {
		return OptimizerOutcomeMeasurement{}, errors.New("optimizer schedule outcome does not completely observe solver work")
	}
	realized := new(big.Rat).SetFrac(big.NewInt(latest.Sub(earliest).Nanoseconds()), big.NewInt(int64(time.Second)))
	signed := new(big.Rat).Sub(realized, predicted)
	absolute := new(big.Rat).Abs(new(big.Rat).Set(signed))
	return OptimizerOutcomeMeasurement{Status: OutcomeMeasurementKnown, Unit: "seconds", Predicted: predicted.RatString(), Realized: realized.RatString(), SignedError: signed.RatString(), AbsoluteError: absolute.RatString(), Source: evidence.Source, Revision: evidence.Revision}, nil
}

func proposalSchedulePrediction(proposal AdvisoryPlanProposal) (*big.Rat, map[string]string, error) {
	assignments := map[string]string{}
	if proposal.Solver.Discrete == nil {
		return nil, nil, errors.New("optimizer proposal lacks discrete solver evidence")
	}
	result := proposal.Solver.Discrete.Receipt.Result
	if result == nil {
		return nil, nil, errors.New("optimizer proposal has no discrete result")
	}
	for _, assignment := range result.Assignments {
		assignments[assignment.Task] = assignment.Lane
	}
	return new(big.Rat).SetInt64(result.MakespanSeconds), assignments, nil
}

func buildOptimizerCostMeasurement(proposal AdvisoryPlanProposal, execution OptimizerPlanExecutionEvidence, fact *TemporalFact) (OptimizerOutcomeMeasurement, error) {
	predicted, currency := proposalCostPrediction(proposal)
	if predicted == nil {
		if fact != nil {
			return OptimizerOutcomeMeasurement{}, errors.New("optimizer cost outcome has no corresponding complete prediction")
		}
		return OptimizerOutcomeMeasurement{Status: OutcomeMeasurementUnknown, Reason: "solver result has no complete estimated cost"}, nil
	}
	measurement := OptimizerOutcomeMeasurement{Status: OutcomeMeasurementUnknown, Unit: currency + " micros", Predicted: predicted.RatString(), Reason: "no authoritative BILLED aggregate fact was supplied"}
	if fact == nil {
		return measurement, nil
	}
	if err := VerifyTemporalFact(*fact); err != nil {
		return OptimizerOutcomeMeasurement{}, fmt.Errorf("optimizer billed cost fact: %w", err)
	}
	if fact.Plane != StateBilled || fact.KnowledgeStatus != KnowledgeKnown || fact.EntityID != execution.PlanID || fact.Attribute != "cost.realized-total-micros" || fact.Unit != currency || fact.Fidelity != FidelityExact {
		return OptimizerOutcomeMeasurement{}, errors.New("optimizer realized cost requires an exact Plan-scoped BILLED total fact")
	}
	decoder := json.NewDecoder(bytes.NewReader(fact.Value))
	decoder.UseNumber()
	var number json.Number
	if err := decoder.Decode(&number); err != nil {
		return OptimizerOutcomeMeasurement{}, errors.New("optimizer realized cost fact is not an exact integer")
	}
	realizedInteger, ok := new(big.Int).SetString(number.String(), 10)
	if !ok || realizedInteger.Sign() < 0 {
		return OptimizerOutcomeMeasurement{}, errors.New("optimizer realized cost fact is not a non-negative integer")
	}
	executionFinished, _ := time.Parse(time.RFC3339Nano, execution.FinishedAt)
	recordedAt, _ := time.Parse(time.RFC3339Nano, fact.RecordedAt)
	if recordedAt.Before(executionFinished) {
		return OptimizerOutcomeMeasurement{}, errors.New("optimizer realized cost fact predates Plan completion")
	}
	realized := new(big.Rat).SetInt(realizedInteger)
	signed := new(big.Rat).Sub(realized, predicted)
	absolute := new(big.Rat).Abs(new(big.Rat).Set(signed))
	return OptimizerOutcomeMeasurement{Status: OutcomeMeasurementKnown, Unit: currency + " micros", Predicted: predicted.RatString(), Realized: realized.RatString(), SignedError: signed.RatString(), AbsoluteError: absolute.RatString(), Source: fact.Source, Revision: fact.SourceRevision}, nil
}

func proposalCostPrediction(proposal AdvisoryPlanProposal) (*big.Rat, string) {
	if proposal.Solver.Discrete == nil {
		return nil, ""
	}
	result := proposal.Solver.Discrete.Receipt.Result
	if result == nil || result.EstimatedTotalCostMicros == nil || result.CostModel == nil {
		return nil, ""
	}
	return new(big.Rat).SetInt64(*result.EstimatedTotalCostMicros), result.CostModel.Currency
}

func cloneScheduleEvidence(source *OptimizerScheduleOutcomeEvidence) *OptimizerScheduleOutcomeEvidence {
	if source == nil {
		return nil
	}
	result := *source
	result.Assignments = append([]OptimizerObservedAssignment(nil), source.Assignments...)
	sort.Slice(result.Assignments, func(i, j int) bool { return result.Assignments[i].WorkID < result.Assignments[j].WorkID })
	return &result
}

func cloneTemporalFactPointer(source *TemporalFact) *TemporalFact {
	if source == nil {
		return nil
	}
	result := *source
	result.Value = append(json.RawMessage(nil), source.Value...)
	return &result
}

func cloneValueOutcomeEvidence(source *OptimizerValueOutcomeEvidence) *OptimizerValueOutcomeEvidence {
	if source == nil {
		return nil
	}
	encoded, _ := json.Marshal(source)
	var result OptimizerValueOutcomeEvidence
	_ = json.Unmarshal(encoded, &result)
	return &result
}

func advisoryOutcomeID(record AdvisoryOutcomeRecord) (string, error) {
	record.ID = ""
	digest, err := digestJSON(record)
	if err != nil {
		return "", err
	}
	return "optout_" + digest[len("sha256:"):], nil
}

type FileAdvisoryOutcomeStore struct{ Directory string }

func (store FileAdvisoryOutcomeStore) Save(record AdvisoryOutcomeRecord) error {
	if err := VerifyAdvisoryOutcomeRecord(record); err != nil {
		return err
	}
	return FileTwinStore(store).save("advisory-outcomes", record.ID, record)
}

func (store FileAdvisoryOutcomeStore) Get(id string) (AdvisoryOutcomeRecord, error) {
	var record AdvisoryOutcomeRecord
	if !validContentID(id, "optout_") {
		return record, errors.New("invalid advisory outcome id")
	}
	if err := FileTwinStore(store).get("advisory-outcomes", id, &record); err != nil {
		return record, err
	}
	return record, VerifyAdvisoryOutcomeRecord(record)
}

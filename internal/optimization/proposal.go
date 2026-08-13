package optimization

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	AdvisoryProposalAPIVersion       = "optimization.platform.example/advisory-proposal/v1alpha1"
	DiscreteProposalCompilerRevision = "discrete-choice-json-patch.v1"

	ProposalGuardCapacity    = "CAPACITY"
	ProposalGuardPolicy      = "POLICY"
	ProposalGuardRecall      = "RECALL"
	ProposalGuardMaintenance = "MAINTENANCE"
	ProposalGuardHealth      = "HEALTH"
	ProposalGuardCostOwner   = "COST_OWNER"
	ProposalGuardOwnership   = "OWNERSHIP"

	ProposalRevalidationApproval      = "APPROVAL"
	ProposalRevalidationLastSafePoint = "LAST_SAFE_POINT"
	ProposalRevalidationReady         = "READY"
	ProposalRevalidationDrift         = "DRIFT"
	ProposalRevalidationUnknown       = "UNKNOWN"
	ProposalRevalidationStale         = "STALE"
	ProposalRevalidationConflicting   = "CONFLICTING"

	maximumAdvisoryProposalDocumentBytes = 1 << 20
	maximumProposalOperations            = 4_096
)

var requiredProposalGuardKinds = []string{
	ProposalGuardCapacity, ProposalGuardCostOwner, ProposalGuardHealth,
	ProposalGuardMaintenance, ProposalGuardOwnership, ProposalGuardPolicy, ProposalGuardRecall,
}

type DiscreteSolverRun struct {
	Request DiscreteSolverRequest `json:"request"`
	Receipt DiscreteSolverReceipt `json:"receipt"`
}

// ProposalSolverEvidence is intentionally discrete-only. Allocation proposals
// remain unsupported until they have an equally exact item/resource compiler.
type ProposalSolverEvidence struct {
	Discrete *DiscreteSolverRun `json:"discrete"`
}

type ProposalReplaceOperation struct {
	Pointer string          `json:"pointer"`
	Value   json.RawMessage `json:"value"`
}

type DiscreteChoiceVariant struct {
	Lane       string                     `json:"lane"`
	Operations []ProposalReplaceOperation `json:"operations"`
}

type DiscreteWorkItemSelection struct {
	Task     string                  `json:"task"`
	Variants []DiscreteChoiceVariant `json:"variants"`
}

// ProposalSelectionSpec is compiler input, not a caller-authored final
// document. Replaying the verified discrete choices against these closed
// variants is the only way to produce Document.Value.
type ProposalSelectionSpec struct {
	CompilerRevision string                      `json:"compilerRevision"`
	DocumentSchema   string                      `json:"documentSchema"`
	Base             json.RawMessage             `json:"base"`
	WorkItems        []DiscreteWorkItemSelection `json:"workItems"`
}

type AdvisoryProposalDocument struct {
	Schema string          `json:"schema"`
	Digest string          `json:"digest"`
	Value  json.RawMessage `json:"value"`
}

type AdvisoryProposalGuard struct {
	Kind                string          `json:"kind"`
	Requirement         TwinRequirement `json:"requirement"`
	BaselineValueDigest string          `json:"baselineValueDigest"`
	BaselineFactIDs     []string        `json:"baselineFactIds"`
}

type AdvisoryPlanProposal struct {
	APIVersion              string                   `json:"apiVersion"`
	Kind                    string                   `json:"kind"`
	ID                      string                   `json:"id"`
	Mode                    string                   `json:"mode"`
	CreatedAt               string                   `json:"createdAt"`
	CompilerIdentity        string                   `json:"compilerIdentity"`
	Environment             string                   `json:"environment"`
	Snapshot                TwinSnapshot             `json:"snapshot"`
	Solver                  ProposalSolverEvidence   `json:"solver"`
	Selection               ProposalSelectionSpec    `json:"selection"`
	Document                AdvisoryProposalDocument `json:"document"`
	Guards                  []AdvisoryProposalGuard  `json:"guards"`
	RequiresGovernance      bool                     `json:"requiresGovernance"`
	RequiresApprovalRecheck bool                     `json:"requiresApprovalRecheck"`
	RequiresLeaseRecheck    bool                     `json:"requiresLeaseRecheck"`
	InfluencedPlan          bool                     `json:"influencedPlan"`
	MutatedState            bool                     `json:"mutatedState"`
}

type ProposalGuardObservation struct {
	Kind                string          `json:"kind"`
	RequirementID       string          `json:"requirementId"`
	Status              string          `json:"status"`
	BaselineValueDigest string          `json:"baselineValueDigest"`
	CurrentValueDigest  string          `json:"currentValueDigest,omitempty"`
	CurrentFactIDs      []string        `json:"currentFactIds,omitempty"`
	KnowledgeStatus     KnowledgeStatus `json:"knowledgeStatus"`
}

type ProposalRevalidation struct {
	APIVersion              string                     `json:"apiVersion"`
	Kind                    string                     `json:"kind"`
	ID                      string                     `json:"id"`
	ProposalID              string                     `json:"proposalId"`
	Stage                   string                     `json:"stage"`
	AcquisitionReceiptID    string                     `json:"acquisitionReceiptId"`
	EvaluatedAt             string                     `json:"evaluatedAt"`
	Snapshot                TwinSnapshot               `json:"snapshot"`
	Observations            []ProposalGuardObservation `json:"observations"`
	Verdict                 string                     `json:"verdict"`
	Leaseable               bool                       `json:"leaseable"`
	TriggeredReoptimization bool                       `json:"triggeredReoptimization"`
	MutatedState            bool                       `json:"mutatedState"`
}

func NewAdvisoryPlanProposal(environment, compilerIdentity string, selection ProposalSelectionSpec, solver ProposalSolverEvidence, snapshot TwinSnapshot, guards []AdvisoryProposalGuard, now time.Time) (AdvisoryPlanProposal, error) {
	if !validEvidenceCoordinate(environment) || !validObservationText(compilerIdentity) || now.IsZero() {
		return AdvisoryPlanProposal{}, errors.New("advisory proposal requires bounded environment, compiler, and creation time")
	}
	if err := VerifyTwinSnapshot(snapshot); err != nil || !snapshot.Complete {
		return AdvisoryPlanProposal{}, errors.New("advisory proposal requires a complete verified baseline twin snapshot")
	}
	solverSnapshot, finishedAt, result, err := verifyProposalSolverEvidence(solver)
	if err != nil {
		return AdvisoryPlanProposal{}, err
	}
	if solverSnapshot.ID != snapshot.ID || solverSnapshot.CapturedAt != snapshot.KnownAt {
		return AdvisoryPlanProposal{}, errors.New("advisory proposal solver does not bind the baseline twin snapshot")
	}
	if now.Before(finishedAt) {
		return AdvisoryPlanProposal{}, errors.New("advisory proposal predates its solver receipt")
	}
	selection, document, err := compileDiscreteProposalSelection(selection, solver.Discrete.Request, result)
	if err != nil {
		return AdvisoryPlanProposal{}, err
	}
	canonicalGuards, err := canonicalProposalGuards(snapshot, guards)
	if err != nil {
		return AdvisoryPlanProposal{}, err
	}
	proposal := AdvisoryPlanProposal{
		APIVersion: AdvisoryProposalAPIVersion, Kind: "AdvisoryPlanProposal", Mode: "ADVISORY",
		CreatedAt: now.UTC().Format(time.RFC3339Nano), CompilerIdentity: compilerIdentity, Environment: environment,
		Snapshot: snapshot, Solver: solver, Selection: selection,
		Document: AdvisoryProposalDocument{Schema: selection.DocumentSchema, Digest: sha256Digest(document), Value: document},
		Guards:   canonicalGuards, RequiresGovernance: true, RequiresApprovalRecheck: true, RequiresLeaseRecheck: true,
	}
	proposal.ID, err = advisoryProposalID(proposal)
	return proposal, err
}

func VerifyAdvisoryPlanProposal(proposal AdvisoryPlanProposal) error {
	if proposal.APIVersion != AdvisoryProposalAPIVersion || proposal.Kind != "AdvisoryPlanProposal" || proposal.Mode != "ADVISORY" || !validContentID(proposal.ID, "optprop_") {
		return errors.New("unsupported or invalid advisory proposal")
	}
	if !proposal.RequiresGovernance || !proposal.RequiresApprovalRecheck || !proposal.RequiresLeaseRecheck || proposal.InfluencedPlan || proposal.MutatedState {
		return errors.New("advisory proposal crosses its authority boundary")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, proposal.CreatedAt)
	if err != nil {
		return fmt.Errorf("advisory proposal createdAt: %w", err)
	}
	replayed, err := NewAdvisoryPlanProposal(proposal.Environment, proposal.CompilerIdentity, proposal.Selection, proposal.Solver, proposal.Snapshot, proposal.Guards, createdAt)
	if err != nil {
		return fmt.Errorf("replay advisory proposal: %w", err)
	}
	if !reflect.DeepEqual(replayed, proposal) {
		return errors.New("advisory proposal does not match independent compiler replay")
	}
	return nil
}

func verifyProposalSolverEvidence(evidence ProposalSolverEvidence) (Snapshot, time.Time, DiscreteScheduleResult, error) {
	if evidence.Discrete == nil {
		return Snapshot{}, time.Time{}, DiscreteScheduleResult{}, errors.New("advisory proposals support only exact discrete solver evidence")
	}
	if err := VerifyDiscreteSolverReceipt(evidence.Discrete.Request, evidence.Discrete.Receipt); err != nil {
		return Snapshot{}, time.Time{}, DiscreteScheduleResult{}, err
	}
	if evidence.Discrete.Receipt.Result == nil {
		return Snapshot{}, time.Time{}, DiscreteScheduleResult{}, errors.New("advisory proposal discrete solver evidence lacks a verified result")
	}
	finishedAt, _ := time.Parse(time.RFC3339Nano, evidence.Discrete.Receipt.FinishedAt)
	return evidence.Discrete.Request.Recommendation.Snapshot, finishedAt, *evidence.Discrete.Receipt.Result, nil
}

func compileDiscreteProposalSelection(spec ProposalSelectionSpec, request DiscreteSolverRequest, result DiscreteScheduleResult) (ProposalSelectionSpec, json.RawMessage, error) {
	if spec.CompilerRevision != DiscreteProposalCompilerRevision || !validObservationText(spec.DocumentSchema) {
		return ProposalSelectionSpec{}, nil, errors.New("unsupported or incomplete discrete proposal compiler selection")
	}
	base, decoded, err := canonicalProposalObject(spec.Base)
	if err != nil {
		return ProposalSelectionSpec{}, nil, err
	}
	if len(spec.WorkItems) != len(request.Recommendation.Problem.Tasks) || len(result.Assignments) != len(spec.WorkItems) {
		return ProposalSelectionSpec{}, nil, errors.New("proposal selection must cover every solver task exactly once")
	}
	tasks := make(map[string]DiscreteTask, len(request.Recommendation.Problem.Tasks))
	assignments := make(map[string]DiscreteAssignment, len(result.Assignments))
	for _, task := range request.Recommendation.Problem.Tasks {
		tasks[task.ID] = task
	}
	for _, assignment := range result.Assignments {
		assignments[assignment.Task] = assignment
	}
	canonical := ProposalSelectionSpec{CompilerRevision: spec.CompilerRevision, DocumentSchema: spec.DocumentSchema, Base: base}
	workItems := append([]DiscreteWorkItemSelection(nil), spec.WorkItems...)
	sort.Slice(workItems, func(i, j int) bool { return workItems[i].Task < workItems[j].Task })
	usedPointers := map[string]bool{}
	for index, work := range workItems {
		task, exists := tasks[work.Task]
		assignment, assigned := assignments[work.Task]
		if !exists || !assigned || index > 0 && workItems[index-1].Task == work.Task || len(work.Variants) != len(task.EligibleLanes) {
			return ProposalSelectionSpec{}, nil, errors.New("proposal selection has missing, duplicate, or foreign task coverage")
		}
		eligible := make(map[string]bool, len(task.EligibleLanes))
		for _, lane := range task.EligibleLanes {
			eligible[lane] = true
		}
		variants := append([]DiscreteChoiceVariant(nil), work.Variants...)
		sort.Slice(variants, func(i, j int) bool { return variants[i].Lane < variants[j].Lane })
		var selected *DiscreteChoiceVariant
		for variantIndex := range variants {
			variant := &variants[variantIndex]
			if !eligible[variant.Lane] || variantIndex > 0 && variants[variantIndex-1].Lane == variant.Lane || len(variant.Operations) == 0 {
				return ProposalSelectionSpec{}, nil, errors.New("proposal selection must cover each eligible lane exactly once")
			}
			operations := append([]ProposalReplaceOperation(nil), variant.Operations...)
			sort.Slice(operations, func(i, j int) bool { return operations[i].Pointer < operations[j].Pointer })
			if len(operations) > maximumProposalOperations {
				return ProposalSelectionSpec{}, nil, errors.New("proposal selection exceeds operation bound")
			}
			for operationIndex := range operations {
				operation := &operations[operationIndex]
				canonicalValue, err := canonicalProposalValue(operation.Value)
				if err != nil || operation.Pointer == "" || operationIndex > 0 && operations[operationIndex-1].Pointer == operation.Pointer {
					return ProposalSelectionSpec{}, nil, errors.New("proposal selection contains invalid or duplicate replace operation")
				}
				if _, err := resolveJSONPointer(decoded, operation.Pointer); err != nil {
					return ProposalSelectionSpec{}, nil, err
				}
				operation.Value = canonicalValue
			}
			variant.Operations = operations
			if variant.Lane == assignment.Lane {
				selected = variant
			}
		}
		if selected == nil {
			return ProposalSelectionSpec{}, nil, errors.New("solver selected a lane absent from proposal variants")
		}
		for _, operation := range selected.Operations {
			if usedPointers[operation.Pointer] {
				return ProposalSelectionSpec{}, nil, errors.New("selected proposal variants replace the same pointer more than once")
			}
			usedPointers[operation.Pointer] = true
			if err := applyJSONPointerReplace(decoded, operation.Pointer, operation.Value); err != nil {
				return ProposalSelectionSpec{}, nil, err
			}
		}
		canonical.WorkItems = append(canonical.WorkItems, DiscreteWorkItemSelection{Task: work.Task, Variants: variants})
	}
	document, err := json.Marshal(decoded)
	if err != nil || len(document) > maximumAdvisoryProposalDocumentBytes {
		return ProposalSelectionSpec{}, nil, errors.New("compiled proposal document exceeds its byte bound")
	}
	return canonical, document, nil
}

func canonicalProposalObject(value json.RawMessage) (json.RawMessage, map[string]any, error) {
	canonical, decoded, err := decodeCanonicalProposalJSON(value)
	if err != nil {
		return nil, nil, err
	}
	object, ok := decoded.(map[string]any)
	if !ok {
		return nil, nil, errors.New("proposal base must be a JSON object")
	}
	return canonical, object, nil
}

func canonicalProposalValue(value json.RawMessage) (json.RawMessage, error) {
	canonical, _, err := decodeCanonicalProposalJSON(value)
	return canonical, err
}

func decodeCanonicalProposalJSON(value json.RawMessage) (json.RawMessage, any, error) {
	if len(value) == 0 || len(value) > maximumAdvisoryProposalDocumentBytes {
		return nil, nil, errors.New("proposal JSON is empty or exceeds its byte bound")
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, nil, errors.New("proposal JSON is invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, nil, errors.New("proposal JSON has trailing input")
	}
	canonical, err := json.Marshal(decoded)
	return canonical, decoded, err
}

func parseJSONPointer(pointer string) ([]string, error) {
	if pointer == "" {
		return nil, errors.New("proposal replace pointer cannot target the document root")
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, errors.New("proposal replace pointer must use RFC6901 syntax")
	}
	parts := strings.Split(pointer[1:], "/")
	for index, part := range parts {
		var decoded strings.Builder
		for cursor := 0; cursor < len(part); cursor++ {
			if part[cursor] != '~' {
				decoded.WriteByte(part[cursor])
				continue
			}
			if cursor+1 >= len(part) || part[cursor+1] != '0' && part[cursor+1] != '1' {
				return nil, errors.New("proposal replace pointer has invalid escape")
			}
			cursor++
			if part[cursor] == '0' {
				decoded.WriteByte('~')
			} else {
				decoded.WriteByte('/')
			}
		}
		parts[index] = decoded.String()
	}
	return parts, nil
}

func resolveJSONPointer(root any, pointer string) (any, error) {
	parts, err := parseJSONPointer(pointer)
	if err != nil {
		return nil, err
	}
	current := root
	for _, part := range parts {
		switch value := current.(type) {
		case map[string]any:
			var exists bool
			current, exists = value[part]
			if !exists {
				return nil, errors.New("proposal replace pointer does not exist in base document")
			}
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(value) || strconv.Itoa(index) != part {
				return nil, errors.New("proposal replace pointer has invalid array index")
			}
			current = value[index]
		default:
			return nil, errors.New("proposal replace pointer traverses a scalar")
		}
	}
	return current, nil
}

func applyJSONPointerReplace(root any, pointer string, raw json.RawMessage) error {
	parts, err := parseJSONPointer(pointer)
	if err != nil {
		return err
	}
	var replacement any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&replacement); err != nil {
		return err
	}
	current := root
	for _, part := range parts[:len(parts)-1] {
		switch value := current.(type) {
		case map[string]any:
			current = value[part]
		case []any:
			index, _ := strconv.Atoi(part)
			current = value[index]
		}
	}
	last := parts[len(parts)-1]
	switch value := current.(type) {
	case map[string]any:
		if _, exists := value[last]; !exists {
			return errors.New("proposal replace pointer does not exist")
		}
		value[last] = replacement
	case []any:
		index, err := strconv.Atoi(last)
		if err != nil || index < 0 || index >= len(value) || strconv.Itoa(index) != last {
			return errors.New("proposal replace pointer has invalid array index")
		}
		value[index] = replacement
	default:
		return errors.New("proposal replace pointer parent is scalar")
	}
	return nil
}

// NewProposalRevalidationFromFacts is deliberately non-authoritative. It is
// useful for pure assessment, but authority-bearing governance uses
// ProposalRevalidator.Execute, which binds a complete acquisition receipt.
func NewProposalRevalidationFromFacts(proposal AdvisoryPlanProposal, stage string, facts []TemporalFact, validAt, knownAt time.Time) (ProposalRevalidation, error) {
	if err := VerifyAdvisoryPlanProposal(proposal); err != nil {
		return ProposalRevalidation{}, err
	}
	requirements := proposalTwinRequirements(proposal)
	snapshot, err := BuildTwinSnapshot(requirements, facts, validAt, knownAt)
	if err != nil {
		return ProposalRevalidation{}, err
	}
	return assessProposalRevalidation(proposal, stage, "", snapshot)
}

func VerifyProposalRevalidation(proposal AdvisoryPlanProposal, record ProposalRevalidation) error {
	if err := VerifyAdvisoryPlanProposal(proposal); err != nil {
		return err
	}
	if record.APIVersion != AdvisoryProposalAPIVersion || record.Kind != "ProposalRevalidation" || !validContentID(record.ID, "optrev_") || record.ProposalID != proposal.ID {
		return errors.New("unsupported or foreign proposal revalidation")
	}
	if record.TriggeredReoptimization || record.MutatedState {
		return errors.New("proposal revalidation crossed its authority boundary")
	}
	if record.AcquisitionReceiptID != "" && !validContentID(record.AcquisitionReceiptID, "acq_") {
		return errors.New("proposal revalidation has invalid acquisition receipt identity")
	}
	if err := VerifyTwinSnapshot(record.Snapshot); err != nil {
		return err
	}
	replayed, err := assessProposalRevalidation(proposal, record.Stage, record.AcquisitionReceiptID, record.Snapshot)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(replayed, record) {
		return errors.New("proposal revalidation does not match replay")
	}
	return nil
}

func assessProposalRevalidation(proposal AdvisoryPlanProposal, stage, acquisitionReceiptID string, snapshot TwinSnapshot) (ProposalRevalidation, error) {
	if stage != ProposalRevalidationApproval && stage != ProposalRevalidationLastSafePoint {
		return ProposalRevalidation{}, errors.New("proposal revalidation stage must be APPROVAL or LAST_SAFE_POINT")
	}
	evaluatedAt, err := time.Parse(time.RFC3339Nano, snapshot.KnownAt)
	if err != nil {
		return ProposalRevalidation{}, err
	}
	createdAt, _ := time.Parse(time.RFC3339Nano, proposal.CreatedAt)
	if evaluatedAt.Before(createdAt) {
		return ProposalRevalidation{}, errors.New("proposal revalidation predates the proposal")
	}
	fields := make(map[string]SnapshotField, len(snapshot.Fields))
	for _, field := range snapshot.Fields {
		fields[field.RequirementID] = field
	}
	record := ProposalRevalidation{APIVersion: AdvisoryProposalAPIVersion, Kind: "ProposalRevalidation", ProposalID: proposal.ID, Stage: stage, AcquisitionReceiptID: acquisitionReceiptID, EvaluatedAt: snapshot.KnownAt, Snapshot: snapshot, Verdict: ProposalRevalidationReady}
	for _, guard := range proposal.Guards {
		field, exists := fields[guard.Requirement.ID]
		observation := ProposalGuardObservation{Kind: guard.Kind, RequirementID: guard.Requirement.ID, BaselineValueDigest: guard.BaselineValueDigest, KnowledgeStatus: KnowledgeUnknown, Status: ProposalRevalidationUnknown}
		if exists {
			observation.KnowledgeStatus, observation.CurrentFactIDs = field.Status, append([]string(nil), field.FactIDs...)
			switch field.Status {
			case KnowledgeKnown:
				observation.CurrentValueDigest = sha256Digest(field.Value)
				if observation.CurrentValueDigest == guard.BaselineValueDigest {
					observation.Status = ProposalRevalidationReady
				} else {
					observation.Status = ProposalRevalidationDrift
				}
			case KnowledgeStale:
				observation.Status = ProposalRevalidationStale
			case KnowledgeConflicting:
				observation.Status = ProposalRevalidationConflicting
			default:
				observation.Status = ProposalRevalidationUnknown
			}
		}
		record.Observations = append(record.Observations, observation)
	}
	record.Verdict = proposalRevalidationVerdict(record.Observations)
	record.Leaseable = stage == ProposalRevalidationLastSafePoint && record.Verdict == ProposalRevalidationReady && acquisitionReceiptID != ""
	record.ID, err = proposalRevalidationID(record)
	return record, err
}

func proposalRevalidationVerdict(observations []ProposalGuardObservation) string {
	verdict := ProposalRevalidationReady
	for _, observation := range observations {
		switch observation.Status {
		case ProposalRevalidationConflicting:
			return ProposalRevalidationConflicting
		case ProposalRevalidationStale:
			verdict = ProposalRevalidationStale
		case ProposalRevalidationUnknown:
			if verdict != ProposalRevalidationStale {
				verdict = ProposalRevalidationUnknown
			}
		case ProposalRevalidationDrift:
			if verdict == ProposalRevalidationReady {
				verdict = ProposalRevalidationDrift
			}
		}
	}
	return verdict
}

func proposalTwinRequirements(proposal AdvisoryPlanProposal) []TwinRequirement {
	requirements := make([]TwinRequirement, 0, len(proposal.Guards))
	for _, guard := range proposal.Guards {
		requirements = append(requirements, guard.Requirement)
	}
	return requirements
}

func canonicalProposalGuards(snapshot TwinSnapshot, guards []AdvisoryProposalGuard) ([]AdvisoryProposalGuard, error) {
	guards = append([]AdvisoryProposalGuard(nil), guards...)
	for index := range guards {
		guards[index].BaselineFactIDs = append([]string(nil), guards[index].BaselineFactIDs...)
		sort.Strings(guards[index].BaselineFactIDs)
	}
	sort.Slice(guards, func(i, j int) bool { return guards[i].Kind < guards[j].Kind })
	if len(guards) != len(requiredProposalGuardKinds) {
		return nil, errors.New("advisory proposal requires every governance revalidation guard")
	}
	fields := make(map[string]SnapshotField, len(snapshot.Fields))
	for _, field := range snapshot.Fields {
		fields[field.RequirementID] = field
	}
	seenRequirements := map[string]bool{}
	for index, guard := range guards {
		if guard.Kind != requiredProposalGuardKinds[index] || seenRequirements[guard.Requirement.ID] {
			return nil, errors.New("advisory proposal guards must be unique and complete")
		}
		seenRequirements[guard.Requirement.ID] = true
		if guard.Requirement.Applicable != nil && !*guard.Requirement.Applicable {
			return nil, errors.New("proposal guard cannot be not-applicable")
		}
		if _, err := BuildTwinSnapshot([]TwinRequirement{guard.Requirement}, nil, time.Unix(1, 0).UTC(), time.Unix(1, 0).UTC()); err != nil {
			return nil, err
		}
		field, exists := fields[guard.Requirement.ID]
		if !exists || field.Status != KnowledgeKnown || field.Unit != guard.Requirement.Unit {
			return nil, fmt.Errorf("proposal guard %s lacks known baseline evidence", guard.Kind)
		}
		wantDigest, wantFacts := sha256Digest(field.Value), append([]string(nil), field.FactIDs...)
		sort.Strings(wantFacts)
		if guard.BaselineValueDigest == "" {
			guards[index].BaselineValueDigest = wantDigest
		} else if guard.BaselineValueDigest != wantDigest {
			return nil, fmt.Errorf("proposal guard %s baseline digest does not match snapshot", guard.Kind)
		}
		if len(guard.BaselineFactIDs) == 0 {
			guards[index].BaselineFactIDs = wantFacts
		} else if !reflect.DeepEqual(guard.BaselineFactIDs, wantFacts) {
			return nil, fmt.Errorf("proposal guard %s fact ids do not match snapshot", guard.Kind)
		}
	}
	return guards, nil
}

func advisoryProposalID(proposal AdvisoryPlanProposal) (string, error) {
	proposal.ID = ""
	encoded, err := json.Marshal(proposal)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "optprop_" + hex.EncodeToString(digest[:]), nil
}
func proposalRevalidationID(record ProposalRevalidation) (string, error) {
	record.ID = ""
	encoded, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "optrev_" + hex.EncodeToString(digest[:]), nil
}

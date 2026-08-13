package optimization

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestAdvisoryPlanProposalCompilesDiscreteChoicesAndGuards(t *testing.T) {
	proposal, _, _ := testAdvisoryPlanProposal(t)
	if proposal.Mode != "ADVISORY" || !proposal.RequiresGovernance || !proposal.RequiresApprovalRecheck || !proposal.RequiresLeaseRecheck || proposal.InfluencedPlan || proposal.MutatedState {
		t.Fatalf("proposal authority = %#v", proposal)
	}
	if proposal.Solver.Discrete == nil || !validContentID(proposal.ID, "optprop_") || !validDigest(proposal.Document.Digest) || len(proposal.Guards) != len(requiredProposalGuardKinds) {
		t.Fatalf("proposal bindings = %#v", proposal)
	}
	if err := VerifyAdvisoryPlanProposal(proposal); err != nil {
		t.Fatal(err)
	}
	var document struct {
		Placements map[string]string `json:"placements"`
	}
	if err := json.Unmarshal(proposal.Document.Value, &document); err != nil {
		t.Fatal(err)
	}
	for _, assignment := range proposal.Solver.Discrete.Receipt.Result.Assignments {
		if document.Placements[assignment.Task] != assignment.Lane {
			t.Fatalf("compiled placement for %s = %q, want %q", assignment.Task, document.Placements[assignment.Task], assignment.Lane)
		}
	}

	copy := proposal
	copy.Document.Value = json.RawMessage(`{"environment":{"id":"dev"}}`)
	if err := VerifyAdvisoryPlanProposal(copy); err == nil {
		t.Fatal("accepted caller-authored document bytes")
	}
	copy = proposal
	copy.Selection.WorkItems[0].Variants[0].Operations[0].Value = json.RawMessage(`"foreign-lane"`)
	if err := VerifyAdvisoryPlanProposal(copy); err == nil {
		t.Fatal("accepted selection variant tampering")
	}
}

func TestProposalRevalidationFromFactsPreservesKnowledgeWithoutAuthority(t *testing.T) {
	proposal, _, baselineFacts := testAdvisoryPlanProposal(t)
	validAt := time.Date(2026, 8, 12, 12, 0, 8, 0, time.UTC)
	knownAt := validAt.Add(time.Second)
	fresh := reobservedProposalFacts(t, baselineFacts, validAt.Add(-time.Second), validAt)

	approval, err := NewProposalRevalidationFromFacts(proposal, ProposalRevalidationApproval, fresh, validAt, knownAt)
	if err != nil || approval.Verdict != ProposalRevalidationReady || approval.Leaseable || approval.AcquisitionReceiptID != "" {
		t.Fatalf("non-authoritative approval = %#v, %v", approval, err)
	}
	lastSafe, err := NewProposalRevalidationFromFacts(proposal, ProposalRevalidationLastSafePoint, fresh, validAt, knownAt)
	if err != nil || lastSafe.Verdict != ProposalRevalidationReady || lastSafe.Leaseable {
		t.Fatalf("non-authoritative last safe point = %#v, %v", lastSafe, err)
	}

	drifted := cloneTemporalFacts(fresh)
	drifted[0] = replaceProposalFactValue(t, drifted[0], json.RawMessage(`{"state":"changed"}`))
	drift, err := NewProposalRevalidationFromFacts(proposal, ProposalRevalidationLastSafePoint, drifted, validAt, knownAt)
	if err != nil || drift.Verdict != ProposalRevalidationDrift || drift.Leaseable {
		t.Fatalf("drift = %#v, %v", drift, err)
	}
	missing, err := NewProposalRevalidationFromFacts(proposal, ProposalRevalidationLastSafePoint, fresh[1:], validAt, knownAt)
	if err != nil || missing.Verdict != ProposalRevalidationUnknown {
		t.Fatalf("missing = %#v, %v", missing, err)
	}
	staleFacts := reobservedProposalFacts(t, baselineFacts, validAt.Add(-2*time.Minute), validAt.Add(-2*time.Minute+time.Second))
	stale, err := NewProposalRevalidationFromFacts(proposal, ProposalRevalidationLastSafePoint, staleFacts, validAt, knownAt)
	if err != nil || stale.Verdict != ProposalRevalidationStale {
		t.Fatalf("stale = %#v, %v", stale, err)
	}
	conflicting := append(cloneTemporalFacts(fresh), replaceProposalFactValue(t, fresh[0], json.RawMessage(`{"state":"conflict"}`)))
	conflict, err := NewProposalRevalidationFromFacts(proposal, ProposalRevalidationLastSafePoint, conflicting, validAt, knownAt)
	if err != nil || conflict.Verdict != ProposalRevalidationConflicting {
		t.Fatalf("conflict = %#v, %v", conflict, err)
	}
}

func TestProposalRevalidatorReacquiresAndPersistsAuthorityEvidence(t *testing.T) {
	proposal, _, _ := testAdvisoryPlanProposal(t)
	observedAt := time.Date(2026, 8, 12, 12, 0, 8, 0, time.UTC)
	revalidator, store := testProposalRevalidator(t, proposal, observedAt)
	record, err := revalidator.Execute(context.Background(), proposal, ProposalRevalidationLastSafePoint)
	if err != nil {
		t.Fatal(err)
	}
	if record.Verdict != ProposalRevalidationReady || !record.Leaseable || !validContentID(record.AcquisitionReceiptID, "acq_") {
		t.Fatalf("authoritative last safe point = %#v", record)
	}
	receipt, err := store.GetAcquisitionReceipt(record.AcquisitionReceiptID)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyAuthoritativeProposalRevalidation(proposal, record, receipt); err != nil {
		t.Fatal(err)
	}
	if loaded, err := store.GetProposal(proposal.ID); err != nil || !reflect.DeepEqual(loaded, proposal) {
		t.Fatalf("stored proposal = %#v, %v", loaded, err)
	}
	if loaded, err := store.GetRevalidation(record.ID); err != nil || !reflect.DeepEqual(loaded, record) {
		t.Fatalf("stored revalidation = %#v, %v", loaded, err)
	}
	tampered := record
	tampered.AcquisitionReceiptID = "acq_" + strings.Repeat("f", 64)
	if err := VerifyAuthoritativeProposalRevalidation(proposal, tampered, receipt); err == nil {
		t.Fatal("accepted foreign acquisition receipt binding")
	}
}

func TestAdvisoryProposalRejectsIncompleteCompilerAndGovernanceEvidence(t *testing.T) {
	proposal, _, _ := testAdvisoryPlanProposal(t)
	mutations := []struct {
		name   string
		mutate func(*AdvisoryPlanProposal)
	}{
		{"plan influence", func(value *AdvisoryPlanProposal) { value.InfluencedPlan = true }},
		{"state mutation", func(value *AdvisoryPlanProposal) { value.MutatedState = true }},
		{"governance bypass", func(value *AdvisoryPlanProposal) { value.RequiresGovernance = false }},
		{"guard", func(value *AdvisoryPlanProposal) { value.Guards[0].BaselineValueDigest = digest('f') }},
		{"solver", func(value *AdvisoryPlanProposal) {
			value.Solver.Discrete.Receipt.RequestID = "dsolreq_" + strings.Repeat("a", 64)
		}},
	}
	for _, testCase := range mutations {
		t.Run(testCase.name, func(t *testing.T) {
			candidate := cloneAdvisoryProposal(t, proposal)
			testCase.mutate(&candidate)
			if err := VerifyAdvisoryPlanProposal(candidate); err == nil {
				t.Fatal("accepted tampered advisory proposal")
			}
		})
	}
	guards := append([]AdvisoryProposalGuard(nil), proposal.Guards[:len(proposal.Guards)-1]...)
	if _, err := NewAdvisoryPlanProposal(proposal.Environment, proposal.CompilerIdentity, proposal.Selection, proposal.Solver, proposal.Snapshot, guards, time.Date(2026, 8, 12, 12, 0, 5, 0, time.UTC)); err == nil {
		t.Fatal("accepted proposal without every revalidation guard")
	}
}

func TestProposalGuardOrderDoesNotChangeIdentity(t *testing.T) {
	proposal, _, _ := testAdvisoryPlanProposal(t)
	reversed := append([]AdvisoryProposalGuard(nil), proposal.Guards...)
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}
	replayed, err := NewAdvisoryPlanProposal(proposal.Environment, proposal.CompilerIdentity, proposal.Selection, proposal.Solver, proposal.Snapshot, reversed, time.Date(2026, 8, 12, 12, 0, 4, 0, time.UTC))
	if err != nil || replayed.ID != proposal.ID {
		t.Fatalf("guard reorder = %q/%q, %v", proposal.ID, replayed.ID, err)
	}
}

func testAdvisoryPlanProposal(t *testing.T) (AdvisoryPlanProposal, []TwinRequirement, []TemporalFact) {
	t.Helper()
	baseline, requirements, facts := testProposalTwin(t)
	seed := testDiscreteSolverRequest(t, SolverSelection{PreferredBackend: NativeBackendName})
	recommendation := seed.Recommendation
	recommendation.Snapshot = Snapshot{ID: baseline.ID, Source: "twin", Revision: "r1", CapturedAt: baseline.KnownAt}
	request, err := NewDiscreteSolverRequest(recommendation, seed.Selection, seed.Determinism, seed.Limits)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 8, 12, 12, 0, 2, 0, time.UTC)
	receipt, err := RunDiscreteSolverRequest(context.Background(), request, []DiscreteSolverBackend{NativeDiscreteSolverBackend{ExecutableDigest: testExecutableDigest, Architecture: runtime.GOARCH}}, sequenceClock(start, start.Add(time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	selection := proposalSelectionForRequest(t, "platform.desired-state.v1", json.RawMessage(`{"environment":{"id":"dev"},"entities":[],"placements":{}}`), request)
	proposal, err := NewAdvisoryPlanProposal("dev", "compiler:test", selection, ProposalSolverEvidence{Discrete: &DiscreteSolverRun{Request: request, Receipt: receipt}}, baseline, proposalGuardsFromRequirements(requirements), start.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	return proposal, requirements, facts
}

func proposalSelectionForRequest(t *testing.T, schema string, base json.RawMessage, request DiscreteSolverRequest) ProposalSelectionSpec {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(base, &document); err != nil {
		t.Fatal(err)
	}
	placements, ok := document["placements"].(map[string]any)
	if !ok {
		placements = map[string]any{}
		document["placements"] = placements
	}
	spec := ProposalSelectionSpec{CompilerRevision: DiscreteProposalCompilerRevision, DocumentSchema: schema}
	for _, task := range request.Recommendation.Problem.Tasks {
		placements[task.ID] = ""
		work := DiscreteWorkItemSelection{Task: task.ID}
		for _, lane := range task.EligibleLanes {
			value, _ := json.Marshal(lane)
			work.Variants = append(work.Variants, DiscreteChoiceVariant{Lane: lane, Operations: []ProposalReplaceOperation{{Pointer: "/placements/" + escapeJSONPointer(task.ID), Value: value}}})
		}
		spec.WorkItems = append(spec.WorkItems, work)
	}
	spec.Base, _ = json.Marshal(document)
	return spec
}

func escapeJSONPointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

func testProposalTwin(t *testing.T) (TwinSnapshot, []TwinRequirement, []TemporalFact) {
	t.Helper()
	validAt := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	knownAt := validAt.Add(time.Second)
	requirements := make([]TwinRequirement, 0, len(requiredProposalGuardKinds))
	facts := make([]TemporalFact, 0, len(requiredProposalGuardKinds))
	for index, kind := range requiredProposalGuardKinds {
		id := "guard." + strings.ToLower(kind)
		requirement := TwinRequirement{ID: id, EntityID: "dev", Attribute: strings.ToLower(kind), Unit: "state", Plane: StateObserved, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 60}
		fact, err := NewTemporalFact(TemporalFact{
			EntityID: "dev", Attribute: requirement.Attribute, Value: json.RawMessage(`{"state":"ready"}`), Unit: "state", Plane: StateObserved,
			ValidFrom: validAt.Add(-time.Second).Format(time.RFC3339Nano), RecordedAt: validAt.Format(time.RFC3339Nano),
			Source: "collector.guard", SourceRevision: "r1", RequestDigest: digest(byte('a' + index)), ResponseDigest: digest(byte('h' + index)), CollectorIdentity: "collector:test", Fidelity: FidelityExact,
		})
		if err != nil {
			t.Fatal(err)
		}
		requirements = append(requirements, requirement)
		facts = append(facts, fact)
	}
	snapshot, err := BuildTwinSnapshot(requirements, facts, validAt, knownAt)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot, requirements, facts
}

func proposalGuardsFromRequirements(requirements []TwinRequirement) []AdvisoryProposalGuard {
	guards := make([]AdvisoryProposalGuard, len(requirements))
	for index, requirement := range requirements {
		guards[index] = AdvisoryProposalGuard{Kind: requiredProposalGuardKinds[index], Requirement: requirement}
	}
	return guards
}

func reobservedProposalFacts(t *testing.T, baseline []TemporalFact, validFrom, recordedAt time.Time) []TemporalFact {
	t.Helper()
	result := make([]TemporalFact, 0, len(baseline))
	for index, fact := range baseline {
		candidate := fact
		candidate.ID = ""
		candidate.ValidFrom = validFrom.Format(time.RFC3339Nano)
		candidate.RecordedAt = recordedAt.Format(time.RFC3339Nano)
		candidate.SourceRevision = "r2"
		candidate.RequestDigest = digest(byte('A' + index))
		candidate.ResponseDigest = digest(byte('H' + index))
		created, err := NewTemporalFact(candidate)
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, created)
	}
	return result
}

func replaceProposalFactValue(t *testing.T, fact TemporalFact, value json.RawMessage) TemporalFact {
	t.Helper()
	fact.ID = ""
	fact.Value = value
	created, err := NewTemporalFact(fact)
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func cloneTemporalFacts(source []TemporalFact) []TemporalFact {
	encoded, _ := json.Marshal(source)
	var cloned []TemporalFact
	_ = json.Unmarshal(encoded, &cloned)
	return cloned
}

func cloneAdvisoryProposal(t *testing.T, source AdvisoryPlanProposal) AdvisoryPlanProposal {
	t.Helper()
	encoded, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var cloned AdvisoryPlanProposal
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}

type proposalTestReader struct{ observedAt time.Time }

func (reader proposalTestReader) Read(_ context.Context, request AcquisitionReadRequest) (AcquisitionReadResponse, error) {
	if request.Source != "proposal-test" {
		return AcquisitionReadResponse{}, errors.New("foreign proposal test source")
	}
	return AcquisitionReadResponse{StatusCode: 200, ObservedAt: reader.observedAt.Format(time.RFC3339Nano), SourceRevision: "guard-r2", Body: []byte(`{}`)}, nil
}

type proposalTestExtractor struct{ guards []AdvisoryProposalGuard }

func (extractor proposalTestExtractor) Extract(_ AcquisitionReadRequest, response AcquisitionReadResponse) ([]ExtractedAcquisitionFact, error) {
	facts := make([]ExtractedAcquisitionFact, 0, len(extractor.guards))
	for _, guard := range extractor.guards {
		facts = append(facts, ExtractedAcquisitionFact{
			RequirementID: guard.Requirement.ID, EntityID: guard.Requirement.EntityID, Attribute: guard.Requirement.Attribute,
			Value: json.RawMessage(`{"state":"ready"}`), Unit: guard.Requirement.Unit, Plane: guard.Requirement.Plane,
			Fidelity: guard.Requirement.MinimumFidelity, ValidFrom: response.ObservedAt,
		})
	}
	return facts, nil
}

func testProposalRevalidator(t *testing.T, proposal AdvisoryPlanProposal, observedAt time.Time) (ProposalRevalidator, FileProposalEvidenceStore) {
	t.Helper()
	observations := make([]FieldObservation, 0, len(proposal.Guards))
	for _, guard := range proposal.Guards {
		observations = append(observations, FieldObservation{Requirement: guard.Requirement.ID, Plane: guard.Requirement.Plane, Fidelity: guard.Requirement.MinimumFidelity, MaximumStalenessSeconds: int(guard.Requirement.MaximumStalenessSeconds)})
	}
	sort.Slice(observations, func(i, j int) bool { return observations[i].Requirement < observations[j].Requirement })
	call := AcquisitionCall{ID: "proposal-guards", Source: "proposal-test", Method: "GET", Endpoint: "/guards", Consistency: "snapshot", CostUnits: 1, Observations: observations}
	store := FileProposalEvidenceStore{Directory: filepath.Join(t.TempDir(), "proposal-evidence")}
	if err := store.SaveProposal(proposal); err != nil {
		t.Fatal(err)
	}
	ticks := []time.Time{observedAt, observedAt.Add(time.Second), observedAt.Add(2 * time.Second)}
	index := 0
	clock := func() time.Time {
		value := ticks[min(index, len(ticks)-1)]
		index++
		return value
	}
	return ProposalRevalidator{
		Executor:   AcquisitionExecutor{Reader: proposalTestReader{observedAt: observedAt}, Extractor: proposalTestExtractor{guards: proposal.Guards}, CollectorIdentity: "collector:proposal-test", Now: clock},
		Candidates: []AcquisitionCall{call}, Evidence: store,
	}, store
}

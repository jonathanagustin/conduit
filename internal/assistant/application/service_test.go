package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/assistant/domain"
)

type memoryObjects struct{ page ObjectPage }

func (objects memoryObjects) Query(context.Context, Actor, domain.QueryArgs) (ObjectPage, error) {
	return objects.page, nil
}

type scriptGovernor struct{ decision Decision }

func (governor scriptGovernor) Evaluate(context.Context, string, string, string, []string) (Decision, error) {
	return governor.decision, nil
}

type memoryChanges struct{ calls int }

func (changes *memoryChanges) Open(context.Context, Proposal) (OpenedChange, error) {
	changes.calls++
	return OpenedChange{ID: "crq_test", Status: "PENDING_APPROVALS"}, nil
}

type memoryDiffs struct{}

func (memoryDiffs) Read(context.Context, string) (ChangeDiff, error) {
	return ChangeDiff{
		ID: "crq_mem", Title: "Proposed memory limit 512Mi", Status: "PENDING_APPROVALS",
		Hunks: []Hunk{{Path: "limits.memory", Before: "256Mi", After: "512Mi"}},
	}, nil
}

type memoryAudit struct{ rows []AuditRecord }

func (audit *memoryAudit) Record(_ context.Context, record AuditRecord) (string, error) {
	audit.rows = append(audit.rows, record)
	return "aud_test", nil
}

func TestQueryReportsReadsAndNoWrites(t *testing.T) {
	audit := &memoryAudit{}
	service := Service{
		Objects: memoryObjects{page: ObjectPage{Hits: []ObjectHit{{ID: "dev", Type: "Environment", Label: "dev"}}, ReadOps: 1, Total: 1}},
		Audit:   audit, Now: fixedClock(),
	}
	result, err := service.Invoke(context.Background(), operator(), domain.ModeExplore, domain.ToolQuery, []byte(`{"objectType":"Environment","filter":{"field":"id","value":"dev"}}`))
	if err != nil || !result.OK || result.MatchedCount != 1 || result.ReadOps != 1 || result.WriteOps != 0 || result.Label != domain.LabelNoModel {
		t.Fatalf("query = %#v %v", result, err)
	}
	if len(audit.rows) != 1 || audit.rows[0].Tool != domain.ToolQuery || audit.rows[0].Decision != "ALLOW" {
		t.Fatalf("audit = %#v", audit.rows)
	}
}

func TestDeniedActionDoesNotOpenAChangeRequest(t *testing.T) {
	changes := &memoryChanges{}
	service := Service{
		Governor: scriptGovernor{decision: Decision{Known: true, Allowed: false, Reason: "caller does not have a role for this action", AuditEventID: "aud_rule"}},
		Changes:  changes, Audit: &memoryAudit{}, Now: fixedClock(),
	}
	result, err := service.Invoke(context.Background(), operator(), domain.ModeReleasePlan, domain.ToolPropose, proposeBody())
	if err != nil || result.Allowed || result.ChangeRequestID != "" || changes.calls != 0 || result.WriteOps != 0 {
		t.Fatalf("deny = %#v calls=%d %v", result, changes.calls, err)
	}
	if result.Reason != "caller does not have a role for this action" || result.Constraints[0].Status != "DENY" || result.DecisionAuditEventID != "aud_rule" {
		t.Fatalf("reason = %#v", result)
	}
}

func TestAllowedActionOpensAPendingChangeAndDoesNotApply(t *testing.T) {
	changes := &memoryChanges{}
	service := Service{
		Governor: scriptGovernor{decision: Decision{Known: true, Allowed: true, Reason: "action allowed", AuditEventID: "aud_rule"}},
		Changes:  changes, Audit: &memoryAudit{}, Now: fixedClock(),
	}
	result, err := service.Invoke(context.Background(), Actor{Username: "operator", Groups: []string{"Operator"}}, domain.ModeOntology, domain.ToolPropose, proposeBody())
	if err != nil || !result.Allowed || result.ChangeRequestID != "crq_test" || result.ChangeRequestStatus != "PENDING_APPROVALS" || changes.calls != 1 || result.WriteOps != 1 {
		t.Fatalf("allow = %#v calls=%d %v", result, changes.calls, err)
	}
}

func TestExploreModeRejectsAWrite(t *testing.T) {
	changes := &memoryChanges{}
	service := Service{Changes: changes, Audit: &memoryAudit{}, Now: fixedClock()}
	result, err := service.Invoke(context.Background(), operator(), domain.ModeExplore, domain.ToolPropose, proposeBody())
	if err != nil || result.OK || changes.calls != 0 || !strings.Contains(result.Error, "does not include") {
		t.Fatalf("mode = %#v %v", result, err)
	}
}

func TestDiffReturnsTheStoredHunk(t *testing.T) {
	service := Service{Diffs: memoryDiffs{}, Audit: &memoryAudit{}, Now: fixedClock()}
	result, err := service.Invoke(context.Background(), operator(), domain.ModeConfigDiff, domain.ToolDiff, []byte(`{"changeRequestId":"crq_mem"}`))
	if err != nil || len(result.Hunks) != 1 || result.Hunks[0].Before != "256Mi" || result.Hunks[0].After != "512Mi" || result.WriteOps != 0 {
		t.Fatalf("diff = %#v %v", result, err)
	}
}

type scriptModel struct{ body string }

func (model scriptModel) Complete(context.Context, string) (string, domain.Provenance, error) {
	return model.body, domain.Provenance{Adapter: "script"}, nil
}

func TestModelTurnValidatesThenGatesTheAction(t *testing.T) {
	changes := &memoryChanges{}
	service := Service{
		Model:    scriptModel{body: `{"text":"checking the rule","toolCalls":[{"name":"propose_governed_action","arguments":` + string(proposeBody()) + `}]}`},
		Governor: scriptGovernor{decision: Decision{Known: true, Reason: "run is not queued, running, or staged"}},
		Changes:  changes, Audit: &memoryAudit{}, Now: fixedClock(),
	}
	turn, err := service.Turn(context.Background(), operator(), domain.ModeOntology, "cancel the run", nil)
	if err != nil || turn.Label != domain.LabelLiveModel || turn.Text != "checking the rule" || len(turn.Results) != 1 {
		t.Fatalf("turn = %#v %v", turn, err)
	}
	if turn.Results[0].Allowed || changes.calls != 0 || turn.Results[0].Reason == "" {
		t.Fatalf("gated = %#v calls=%d", turn.Results[0], changes.calls)
	}
}

func TestTurnWithoutAModelIsRefused(t *testing.T) {
	_, err := (Service{}).Turn(context.Background(), operator(), domain.ModeExplore, "list environments", nil)
	if !errors.Is(err, domain.ErrModelUnavailable) {
		t.Fatal(err)
	}
}

func TestInvalidToolJSONIsAuditedAndNotExecuted(t *testing.T) {
	changes := &memoryChanges{}
	audit := &memoryAudit{}
	service := Service{Changes: changes, Audit: audit, Now: fixedClock()}
	_, err := service.Invoke(context.Background(), operator(), domain.ModeOntology, domain.ToolPropose, []byte(`{"actionId":`))
	if err == nil || changes.calls != 0 || len(audit.rows) != 1 || audit.rows[0].Decision != "DENY" {
		t.Fatalf("invalid = calls %d audit %#v err %v", changes.calls, audit.rows, err)
	}
}

type memoryCases struct{ ids []string }

func (cases *memoryCases) Add(scenario, _, _, _ string) (string, error) {
	cases.ids = append(cases.ids, scenario)
	return "case-1", nil
}

type memoryAuthors struct{ author string }

func (authors memoryAuthors) Author(context.Context, string) (string, error) {
	return authors.author, nil
}

func TestElicitationDoesNotOpenAChange(t *testing.T) {
	changes := &memoryChanges{}
	service := Service{Changes: changes, Audit: &memoryAudit{}, Now: fixedClock()}
	result, err := service.Invoke(context.Background(), operator(), domain.ModeOntology, domain.ToolWrite, []byte(`{"action":"propose_memory"}`))
	if err != nil || len(result.Fields) == 0 || changes.calls != 0 || result.ChangeRequestID != "" {
		t.Fatalf("elicit = %#v calls=%d %v", result, changes.calls, err)
	}
}

func TestHighRiskOpensAChangeRequest(t *testing.T) {
	changes := &memoryChanges{}
	service := Service{Changes: changes, Audit: &memoryAudit{}, Now: fixedClock()}
	result, err := service.Invoke(context.Background(), operator(), domain.ModeOntology, domain.ToolWrite, []byte(`{"action":"propose_memory","value":"512Mi","justification":"headroom","environment":"dev"}`))
	if err != nil || result.ChangeRequestID != "crq_test" || result.Applied || changes.calls != 1 {
		t.Fatalf("high = %#v calls=%d %v", result, changes.calls, err)
	}
}

func TestLowRiskAppliesOnlyAfterConfirm(t *testing.T) {
	cases := &memoryCases{}
	service := Service{Cases: cases, Audit: &memoryAudit{}, Now: fixedClock()}
	body := []byte(`{"action":"add_eval_case","justification":"operator rejected 2Gi","value":"2Gi","expectedOutcome":"FAIL_REQUIRED"}`)
	pending, err := service.Invoke(context.Background(), operator(), domain.ModeOntology, domain.ToolWrite, body)
	if err != nil || !pending.NeedsConfirm || pending.Applied || len(cases.ids) != 0 {
		t.Fatalf("pending = %#v %v", pending, err)
	}
	confirmed, err := service.Invoke(context.Background(), operator(), domain.ModeOntology, domain.ToolWrite, []byte(`{"action":"add_eval_case","justification":"operator rejected 2Gi","value":"2Gi","expectedOutcome":"FAIL_REQUIRED","confirm":true}`))
	if err != nil || !confirmed.Applied || confirmed.ObjectID != "case-1" || len(cases.ids) != 1 {
		t.Fatalf("applied = %#v ids=%v %v", confirmed, cases.ids, err)
	}
}

func TestCannotApproveOwnChange(t *testing.T) {
	changes := &memoryChanges{}
	service := Service{
		Authors: memoryAuthors{author: "operator"}, Changes: changes, Audit: &memoryAudit{}, Now: fixedClock(),
		Governor: scriptGovernor{decision: Decision{Known: true, Allowed: true, Reason: "action allowed"}},
	}
	result, err := service.Invoke(context.Background(), operator(), domain.ModeOntology, domain.ToolWrite, []byte(`{"action":"approve_change","targetId":"crq_own","justification":"looks right"}`))
	if err != nil || result.Allowed || changes.calls != 0 || !strings.Contains(result.Reason, "own change request") {
		t.Fatalf("self = %#v calls=%d %v", result, changes.calls, err)
	}
}

func TestRuleDenyDoesNotApply(t *testing.T) {
	changes := &memoryChanges{}
	service := Service{
		Changes: changes, Audit: &memoryAudit{}, Now: fixedClock(),
		Governor: scriptGovernor{decision: Decision{Known: true, Allowed: false, Reason: "run is not queued, running, or staged"}},
	}
	result, err := service.Invoke(context.Background(), operator(), domain.ModeOntology, domain.ToolWrite, []byte(`{"action":"cancel_packaging_run","targetId":"sample-app-blocked","confirm":true}`))
	if err != nil || result.Allowed || result.Applied || changes.calls != 0 || !strings.Contains(result.Reason, "not queued") {
		t.Fatalf("deny = %#v calls=%d %v", result, changes.calls, err)
	}
}

func TestUnknownWriteActionIsRejected(t *testing.T) {
	_, err := (Service{Audit: &memoryAudit{}, Now: fixedClock()}).Invoke(context.Background(), operator(), domain.ModeOntology, domain.ToolWrite, []byte(`{"action":"drop_database"}`))
	if err == nil {
		t.Fatal("accepted an unknown action")
	}
}

func operator() Actor { return Actor{Username: "operator"} }

func proposeBody() json.RawMessage {
	return []byte(`{"actionId":"cancel_packaging_run","targetEntityId":"sample-app-1","targetObjectType":"PackagingRun","environment":"dev","installationId":"sample-app","productId":"app.example:sample-app"}`)
}

func fixedClock() func() time.Time {
	start := time.Date(2026, 11, 14, 13, 0, 0, 0, time.UTC)
	calls := 0
	return func() time.Time {
		calls++
		return start.Add(time.Duration(calls) * time.Millisecond)
	}
}

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

type countingGovernor struct{ rules []string }

func (governor *countingGovernor) Evaluate(_ context.Context, _, action, _ string, _ []string) (Decision, error) {
	governor.rules = append(governor.rules, action)
	return Decision{Known: true, Allowed: true, Reason: "action allowed"}, nil
}

type memoryChanges struct {
	calls int
	last  Proposal
}

func (changes *memoryChanges) Open(_ context.Context, proposal Proposal) (OpenedChange, error) {
	changes.calls++
	changes.last = proposal
	return OpenedChange{ID: "crq_test", Status: "PENDING_APPROVALS"}, nil
}

type memoryReviews struct{ calls int }

func (reviews *memoryReviews) Approve(context.Context, string, string, string, []string) (string, error) {
	reviews.calls++
	return "APPROVED", nil
}

func (reviews *memoryReviews) Reject(context.Context, string, string, string, []string) (string, error) {
	reviews.calls++
	return "REJECTED", nil
}

func allow() scriptGovernor {
	return scriptGovernor{decision: Decision{Known: true, Allowed: true, Reason: "action allowed"}}
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

func TestModelTurnValidatesThenGatesTheAction(t *testing.T) {
	changes := &memoryChanges{}
	model := &scripted{replies: []string{
		`{"text":"checking the rule","toolCalls":[{"name":"propose_governed_action","arguments":` + string(proposeBody()) + `}]}`,
		`{"text":"the rule refused it: the run is not running"}`,
	}}
	service := Service{
		Model:    model,
		Governor: scriptGovernor{decision: Decision{Known: true, Reason: "run is not queued, running, or staged"}},
		Changes:  changes, Audit: &memoryAudit{}, Now: fixedClock(),
	}
	turn, err := service.Turn(context.Background(), operator(), domain.ModeOntology, "cancel the run", nil)
	if err != nil || turn.Label != domain.LabelLiveModel || turn.Text != "the rule refused it: the run is not running" || len(turn.Results) != 1 {
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
	service := Service{Governor: allow(), Changes: changes, Audit: &memoryAudit{}, Now: fixedClock()}
	result, err := service.Invoke(context.Background(), operator(), domain.ModeOntology, domain.ToolWrite, []byte(`{"action":"promote"}`))
	if err != nil || len(result.Fields) == 0 || changes.calls != 0 || result.ChangeRequestID != "" {
		t.Fatalf("elicit = %#v calls=%d %v", result, changes.calls, err)
	}
}

// A change request is filed against the installation, product, and environment
// the caller named. One that is missing is asked for; none is defaulted.
func TestChangeRequestTargetComesFromTheArguments(t *testing.T) {
	changes := &memoryChanges{}
	service := Service{Governor: allow(), Changes: changes, Audit: &memoryAudit{}, Now: fixedClock()}
	result, err := service.Invoke(context.Background(), operator(), domain.ModeOntology, domain.ToolWrite,
		[]byte(`{"action":"promote","targetId":"rel-7","justification":"ready","environment":"prod-east","installationId":"billing-api","productId":"com.acme:billing"}`))
	if err != nil || result.ChangeRequestID != "crq_test" || result.Applied || changes.calls != 1 {
		t.Fatalf("high = %#v calls=%d %v", result, changes.calls, err)
	}
	if changes.last.InstallationID != "billing-api" || changes.last.ProductID != "com.acme:billing" || changes.last.Environment != "prod-east" || changes.last.TargetEntityID != "rel-7" {
		t.Fatalf("proposal = %#v", changes.last)
	}
	for _, body := range []string{
		`{"action":"promote","targetId":"rel-7","justification":"ready","installationId":"billing-api","productId":"com.acme:billing"}`,
		`{"action":"promote","targetId":"rel-7","justification":"ready","environment":"prod-east","productId":"com.acme:billing"}`,
		`{"action":"promote","targetId":"rel-7","justification":"ready","environment":"prod-east","installationId":"billing-api"}`,
		`{"action":"cancel_packaging_run","targetId":"run-1","environment":" ","installationId":"billing-api","productId":"com.acme:billing"}`,
	} {
		changes.calls = 0
		missing, err := service.Invoke(context.Background(), operator(), domain.ModeOntology, domain.ToolWrite, []byte(body))
		if err != nil || changes.calls != 0 || missing.ChangeRequestID != "" || missing.Allowed || len(missing.Fields) != 1 {
			t.Fatalf("%s = %#v calls=%d %v", body, missing, changes.calls, err)
		}
	}
}

// An opened change request says what it is: nothing runs the action on approval.
func TestOpenedChangeRequestDoesNotClaimTheActionWillRun(t *testing.T) {
	service := Service{Governor: allow(), Changes: &memoryChanges{}, Audit: &memoryAudit{}, Now: fixedClock()}
	result, err := service.Invoke(context.Background(), operator(), domain.ModeOntology, domain.ToolWrite,
		[]byte(`{"action":"cancel_packaging_run","targetId":"run-1","environment":"dev","installationId":"billing-api","productId":"com.acme:billing"}`))
	if err != nil || result.Applied || !strings.Contains(result.Reason, "does not run the action") {
		t.Fatalf("opened = %#v %v", result, err)
	}
}

// start_packaging_run and rerun_packaging_run have no service behind them here.
func TestActionWithoutAnExecutorIsRefusedNotApplied(t *testing.T) {
	for _, action := range []string{"start_packaging_run", "rerun_packaging_run"} {
		changes, cases, reviews := &memoryChanges{}, &memoryCases{}, &memoryReviews{}
		service := Service{Governor: allow(), Changes: changes, Cases: cases, Reviews: reviews, Audit: &memoryAudit{}, Now: fixedClock()}
		for _, confirm := range []string{"true", "false"} {
			result, err := service.Invoke(context.Background(), operator(), domain.ModeOntology, domain.ToolWrite,
				[]byte(`{"action":"`+action+`","targetId":"run-1","confirm":`+confirm+`}`))
			if err != nil || result.Applied || result.Allowed || result.NeedsConfirm || result.WriteOps != 0 || result.State != "" || result.Reason != ReasonNotExecutable {
				t.Fatalf("%s confirm=%s = %#v %v", action, confirm, result, err)
			}
		}
		if changes.calls+len(cases.ids)+reviews.calls != 0 {
			t.Fatalf("%s reached a service", action)
		}
	}
}

func TestActionWithoutARuleIsRefused(t *testing.T) {
	for _, action := range []string{"recall", "propose_memory", "select_course"} {
		changes := &memoryChanges{}
		governor := &countingGovernor{}
		service := Service{Governor: governor, Changes: changes, Audit: &memoryAudit{}, Now: fixedClock()}
		result, err := service.Invoke(context.Background(), operator(), domain.ModeOntology, domain.ToolWrite, fullWrite(action))
		if err != nil || result.Allowed || result.Applied || changes.calls != 0 || result.Reason != ReasonNoActionRule || len(result.Fields) != 0 {
			t.Fatalf("%s = %#v calls=%d %v", action, result, changes.calls, err)
		}
	}
}

func TestRuledWriteWithoutAGovernorIsRefused(t *testing.T) {
	changes := &memoryChanges{}
	service := Service{Changes: changes, Audit: &memoryAudit{}, Now: fixedClock()}
	result, err := service.Invoke(context.Background(), operator(), domain.ModeOntology, domain.ToolWrite, fullWrite("promote"))
	if err != nil || result.Allowed || changes.calls != 0 || result.Reason != ReasonNoGovernor {
		t.Fatalf("no governor = %#v calls=%d %v", result, changes.calls, err)
	}
}

// Every action in the schema is evaluated by a rule or refused, and none
// reports applied without a service having been called. add_eval_case is the
// one write that runs with no rule.
func TestEveryWriteActionIsGovernedOrRefusedAndNeverAppliedWithoutAnEffect(t *testing.T) {
	for _, action := range domain.WriteActions() {
		changes, cases, reviews, governor := &memoryChanges{}, &memoryCases{}, &memoryReviews{}, &countingGovernor{}
		service := Service{
			Governor: governor, Changes: changes, Cases: cases, Reviews: reviews,
			Authors: memoryAuthors{author: "someone-else"}, Audit: &memoryAudit{}, Now: fixedClock(),
		}
		result, err := service.Invoke(context.Background(), operator(), domain.ModeOntology, domain.ToolWrite, fullWrite(action))
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		effects := changes.calls + len(cases.ids) + reviews.calls
		refused := !result.Allowed && !result.Applied && effects == 0 && result.Reason != "" && len(result.Constraints) == 1
		if len(governor.rules) == 0 && !refused && action != "add_eval_case" {
			t.Fatalf("%s ran with no action rule: %#v", action, result)
		}
		if (result.Applied || result.WriteOps != 0 || result.State != "") && effects != 1 {
			t.Fatalf("%s reports a write with %d service calls: %#v", action, effects, result)
		}
		if result.Applied && len(cases.ids)+reviews.calls != 1 {
			t.Fatalf("%s says applied and only opened a change request: %#v", action, result)
		}
	}
}

func fullWrite(action string) []byte {
	return []byte(`{"action":"` + action + `","targetId":"target-1","value":"512Mi","justification":"because","expectedOutcome":"PASS",` +
		`"environment":"prod-east","installationId":"billing-api","productId":"com.acme:billing","confirm":true}`)
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
	result, err := service.Invoke(context.Background(), operator(), domain.ModeOntology, domain.ToolWrite, []byte(`{"action":"cancel_packaging_run","targetId":"sample-app-blocked","environment":"dev","installationId":"sample-app","productId":"app.example:sample-app","confirm":true}`))
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
